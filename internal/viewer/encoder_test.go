package viewer

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEncoderSettingIsValidated(t *testing.T) {
	a := testApp(t)
	cookie, s := login(t, a)
	save := func(values map[string]any, reset []string) *httptest.ResponseRecorder {
		return request(a, "PUT", "/api/admin/settings", map[string]any{"version": a.settingsVersion, "values": values, "reset": reset}, cookie, s.CSRF)
	}
	for name, values := range map[string]map[string]any{
		"encoder":         {"encoder": "nvenc"},
		"empty encoder":   {"encoder": ""},
		"retired webhook": {"alertWebhookURL": "https://example.org/hook"},
		"retired hours":   {"alertStaleHours": 12},
	} {
		if w := save(values, nil); w.Code != 400 {
			t.Fatalf("%s accepted: %d %s", name, w.Code, w.Body.String())
		}
	}
	if w := save(map[string]any{"encoder": "vaapi"}, nil); w.Code != 200 || a.config().Encoder != "vaapi" {
		t.Fatal(w.Code, w.Body.String(), a.config().Encoder)
	}
	if w := save(map[string]any{}, []string{"unknown"}); w.Code != 400 {
		t.Fatal(w.Code)
	}
}

// Settings saved by versions with webhook alerts must still load; the retired
// values are dropped and disappear with the next save.
func TestRetiredAlertSettingsAreIgnoredOnLoad(t *testing.T) {
	a := testApp(t)
	cookie, s := login(t, a)
	if w := request(a, "PUT", "/api/admin/settings", map[string]any{"version": a.settingsVersion, "values": map[string]any{"encoder": "qsv"}}, cookie, s.CSRF); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	a.settingsMu.Lock()
	a.overrides["alertWebhookURL"] = json.RawMessage(`"https://example.org/hook?token=SECRETTOKEN"`)
	a.overrides["alertStaleHours"] = json.RawMessage(`12`)
	payload, _ := json.Marshal(a.overrides)
	a.settingsMu.Unlock()
	sealed, err := a.db.sealSettings(string(payload))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`UPDATE settings SET payload=? WHERE id=1`, sealed); err != nil {
		t.Fatal(err)
	}
	if err := a.loadSettings(); err != nil {
		t.Fatal("retired settings block loading", err)
	}
	if _, ok := a.overrides["alertWebhookURL"]; ok || a.config().Encoder != "qsv" {
		t.Fatal(a.overrides, a.config().Encoder)
	}
	if w := request(a, "GET", "/api/admin/settings", nil, cookie, ""); strings.Contains(w.Body.String(), "alert") {
		t.Fatal(w.Body.String())
	}
}

func TestStreamArgumentsPerEncoder(t *testing.T) {
	profile := StreamProfile{Scale: 50, FPS: 15, Bitrate: 1200}
	dir := filepath.Join("cache", "hls-x")
	software, output := streamArgs("software", "/dev/dri/renderD128", profile, 1.5, 60, 1, "hls", dir)
	want := []string{"-nostdin", "-v", "error", "-threads", "1", "-ss", "1.500", "-protocol_whitelist", "file,pipe,fd", "-format_whitelist", formats, "-i", "/proc/self/fd/3", "-t", "60.000", "-map", "0:v:0", "-map", "0:a:0?", "-af", "asetpts=PTS-STARTPTS,atempo=1.000", "-c:a", "aac", "-b:a", "96k", "-vf", "setpts=(PTS-STARTPTS)/1.000,scale=trunc(iw*50/100/2)*2:trunc(ih*50/100/2)*2,fps=15", "-c:v", "libx264", "-bf", "0", "-preset", "veryfast", "-threads", "1", "-crf", "25", "-maxrate", "1200k", "-bufsize", "2400k", "-pix_fmt", "yuv420p", "-force_key_frames", "expr:gte(t,n_forced*1)", "-f", "hls", "-hls_time", "1", "-hls_list_size", "0", "-hls_playlist_type", "event", "-hls_segment_filename", filepath.Join(dir, "segment-%06d.ts"), filepath.Join(dir, "index.m3u8")}
	if output != "index.m3u8" || fmt.Sprint(software) != fmt.Sprint(want) {
		t.Fatalf("software arguments changed:\n got %q\nwant %q", software, want)
	}
	fast, output := streamArgs("software", "", profile, 0, 80, 8, "mp4", dir)
	joined := strings.Join(fast, " ")
	if output != "video.mp4" || !strings.Contains(joined, formats+" -skip_frame nokey -i /proc/self/fd/3 -t 10.000 ") || !strings.Contains(joined, "atempo=2,atempo=2,atempo=2.000") || !strings.HasSuffix(joined, "-f mp4 -movflags +frag_keyframe+empty_moov+default_base_moof "+filepath.Join(dir, "video.mp4")) {
		t.Fatal(joined)
	}
	if seeking, _ := streamArgs("software", "", profile, 5, 80, 8, "mp4", dir); strings.Contains(strings.Join(seeking, " "), "-skip_frame") {
		t.Fatal("keyframe-only decoding must be limited to the clip start")
	}
	vaapi, _ := streamArgs("vaapi", "/dev/dri/renderD129", profile, 0, 60, 1, "hls", dir)
	joined = strings.Join(vaapi, " ")
	for _, part := range []string{"-threads 1 -vaapi_device /dev/dri/renderD129 -ss 0.000", "fps=15,format=nv12,hwupload -c:v h264_vaapi -bf 0 -b:v 1200k -maxrate 1200k -bufsize 2400k -force_key_frames"} {
		if !strings.Contains(joined, part) {
			t.Fatalf("missing %q in %s", part, joined)
		}
	}
	qsv, _ := streamArgs("qsv", "", profile, 0, 60, 1, "hls", dir)
	joined = strings.Join(qsv, " ")
	for _, part := range []string{"-init_hw_device qsv=hw -filter_hw_device hw -ss", "fps=15,format=nv12,hwupload=extra_hw_frames=64,format=qsv -c:v h264_qsv -bf 0 -preset veryfast -b:v 1200k"} {
		if !strings.Contains(joined, part) {
			t.Fatalf("missing %q in %s", part, joined)
		}
	}
	for _, hardware := range [][]string{vaapi, qsv} {
		text := strings.Join(hardware, " ")
		if strings.Contains(text, "libx264") || strings.Contains(text, "-crf") || strings.Contains(text, "-pix_fmt") {
			t.Fatal("software options in hardware arguments", text)
		}
	}
}

func TestPlaybackEncoderSkipsHardwareAfterFailure(t *testing.T) {
	a := testApp(t)
	if a.playbackEncoder() != "software" {
		t.Fatal("default encoder", a.playbackEncoder())
	}
	a.cfgMu.Lock()
	a.cfg.Encoder = "vaapi"
	a.cfgMu.Unlock()
	if a.playbackEncoder() != "vaapi" {
		t.Fatal(a.playbackEncoder())
	}
	a.hardwareBlocked.Store(time.Now().Add(time.Minute).Unix())
	if a.playbackEncoder() != "software" {
		t.Fatal("blocked hardware encoder was used")
	}
	a.hardwareBlocked.Store(time.Now().Add(-time.Second).Unix())
	if a.playbackEncoder() != "vaapi" {
		t.Fatal("hardware encoder not retried")
	}
}

// Without a render device the hardware encoder fails at once; playback must
// still start through the software encoder.
func TestUnavailableHardwareEncoderFallsBackToSoftware(t *testing.T) {
	a := testApp(t)
	c := fixture(t, a, "libx264")
	a.scanDay("2026-10-14")
	rs, _ := a.db.Recordings(c.ID, 0, 2e10)
	if len(rs) == 0 {
		t.Fatal("fixture not indexed")
	}
	a.cfgMu.Lock()
	a.cfg.Encoder, a.cfg.VAAPIDevice = "vaapi", "/dev/dri/missing-render-device"
	a.cfgMu.Unlock()
	cookie, session := login(t, a)
	for i := 0; i < 2; i++ {
		w := request(a, "POST", "/api/streams", map[string]any{"recording": rs[0].ID, "offset": 0, "profile": "fast", "transport": "hls"}, cookie, session.CSRF)
		if w.Code != 201 {
			t.Fatal(i, w.Code, w.Body.String())
		}
		var stream struct{ ID string }
		json.Unmarshal(w.Body.Bytes(), &stream)
		request(a, "DELETE", "/api/streams/"+stream.ID, nil, cookie, session.CSRF)
	}
	// The second stream starts in software directly.
	if a.metrics.encoderFallbacks.Load() != 1 || a.playbackEncoder() != "software" {
		t.Fatal(a.metrics.encoderFallbacks.Load(), a.playbackEncoder())
	}
}
