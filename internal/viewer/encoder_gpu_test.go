package viewer

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestGPUDecodeAndFallbackOnRealHardware runs the generated ffmpeg commands on a VAAPI
// device: H.265 decodes on the GPU, and a codec the GPU refuses still converts through
// the "-upload" variant. Set CTV_TEST_VAAPI to a render node, e.g. /dev/dri/renderD128.
func TestGPUDecodeAndFallbackOnRealHardware(t *testing.T) {
	device := os.Getenv("CTV_TEST_VAAPI")
	if device == "" {
		t.Skip("CTV_TEST_VAAPI required for hardware decoding")
	}
	dir := t.TempDir()
	source := func(name string, codec ...string) string {
		path := filepath.Join(dir, name)
		args := append([]string{"-nostdin", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=1280x720:rate=25", "-t", "3"}, codec...)
		if out, e := testCommand(t, "ffmpeg", append(args, "-y", path)...).CombinedOutput(); e != nil {
			t.Fatal(e, string(out))
		}
		return path
	}
	runs := 0
	convert := func(encoder, input string, profile StreamProfile) error {
		// Each stream gets a new directory, as in the server; ffmpeg never overwrites output.
		runs++
		out := filepath.Join(dir, fmt.Sprintf("%s-%d", encoder, runs))
		if e := os.Mkdir(out, 0700); e != nil {
			return e
		}
		args, _ := streamArgs(encoder, device, profile, 0, 3, 1, "mp4", out)
		f, e := os.Open(input)
		if e != nil {
			return e
		}
		defer f.Close()
		cmd := testCommand(t, "ffmpeg", args...)
		cmd.ExtraFiles = []*os.File{f}
		if b, e := cmd.CombinedOutput(); e != nil {
			return &exec.ExitError{ProcessState: cmd.ProcessState, Stderr: b}
		}
		if st, e := os.Stat(filepath.Join(out, "video.mp4")); e != nil || st.Size() < 4096 {
			t.Fatalf("%s produced no video: %v", encoder, e)
		}
		return nil
	}
	hevc := source("hevc.mp4", "-c:v", "libx265", "-preset", "ultrafast", "-x265-params", "log-level=error", "-tag:v", "hvc1")
	for _, profile := range []StreamProfile{fullProfile, {Scale: 50, FPS: 15, Bitrate: 1200}} {
		if e := convert("vaapi", hevc, profile); e != nil {
			t.Fatalf("GPU decoding of H.265 failed: %s", e.(*exec.ExitError).Stderr)
		}
	}
	mpeg4 := source("mpeg4.mp4", "-c:v", "mpeg4")
	if convert("vaapi", mpeg4, fullProfile) == nil {
		t.Skip("this GPU decodes MPEG-4 Part 2; the fallback is not exercised")
	}
	if e := convert("vaapi-upload", mpeg4, fullProfile); e != nil {
		t.Fatalf("software decoding with GPU encoding failed: %s", e.(*exec.ExitError).Stderr)
	}
}

// TestStreamsDecodeOnTheGPUAndFallBackPerCodec drives the stream endpoint with VAAPI:
// an H.265 recording converts with GPU decoding, a codec the GPU refuses converts with
// software decoding, and only that codec is remembered; the hardware encoder stays in use.
func TestStreamsDecodeOnTheGPUAndFallBackPerCodec(t *testing.T) {
	device := os.Getenv("CTV_TEST_VAAPI")
	if device == "" {
		t.Skip("CTV_TEST_VAAPI required for hardware decoding")
	}
	a := testApp(t)
	a.cfg.Encoder, a.cfg.VAAPIDevice = "vaapi", device
	cookie, session := login(t, a)
	for _, source := range []struct{ camera, codec string }{{"gpuhevc", "libx265"}, {"gpumpeg4", "mpeg4"}} {
		c := Camera{ID: source.camera, Name: source.camera, Folder: source.camera, Pattern: "{YYYY}/{MM}/{DD}/{hh}-{mm}-{ss}.mp4", Timezone: "UTC"}
		if e := a.db.SaveCamera(c); e != nil {
			t.Fatal(e)
		}
		path := filepath.Join(a.cfg.RecordingsDir, source.camera, "2026/10/14/08-30-00.mp4")
		os.MkdirAll(filepath.Dir(path), 0755)
		args := []string{"-nostdin", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=1280x720:rate=25", "-t", "4", "-c:v", source.codec}
		if source.codec == "libx265" {
			args = append(args, "-preset", "ultrafast", "-x265-params", "log-level=error", "-tag:v", "hvc1")
		}
		if out, e := testCommand(t, "ffmpeg", append(args, "-y", path)...).CombinedOutput(); e != nil {
			t.Fatal(e, string(out))
		}
		// Recently modified files count as still being written and are not indexed yet.
		past := time.Now().Add(-time.Minute)
		os.Chtimes(path, past, past)
	}
	a.scanDay("2026-10-14")
	for _, camera := range []string{"gpuhevc", "gpumpeg4"} {
		records, e := a.db.Recordings(camera, 0, 2e10)
		if e != nil || len(records) != 1 {
			t.Fatalf("%s recordings: %v %v", camera, records, e)
		}
		w := request(a, "POST", "/api/streams", map[string]any{"recording": records[0].ID, "transport": "mp4", "profile": "full"}, cookie, session.CSRF)
		if w.Code != 201 {
			t.Fatal(camera, w.Code, w.Body.String())
		}
		var result struct{ ID string }
		json.Unmarshal(w.Body.Bytes(), &result)
		a.removeStream(result.ID)
		refused := a.hardwareDecodeRefused(records[0].Codec)
		if camera == "gpuhevc" && refused || camera == "gpumpeg4" && !refused {
			t.Fatalf("%s (%s): GPU decoding refused=%v", camera, records[0].Codec, refused)
		}
	}
	if a.hardwareBlocked.Load() != 0 || a.metrics.encoderFallbacks.Load() != 0 {
		t.Fatal("a codec the GPU cannot decode must not disable the hardware encoder")
	}
}
