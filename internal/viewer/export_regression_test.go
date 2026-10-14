package viewer

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExportCalendarDays(t *testing.T) {
	epoch := func(s string) float64 {
		v, e := time.Parse(time.RFC3339, s)
		if e != nil {
			t.Fatal(e)
		}
		return float64(v.UnixNano()) / 1e9
	}
	for _, tc := range []struct {
		from, to string
		dates    []string
	}{
		{"2026-10-14T23:59:58+02:00", "2026-10-15T00:00:02+02:00", []string{"2026-10-14", "2026-10-15"}},
		{"2026-10-14T23:59:58+02:00", "2026-10-15T00:00:00+02:00", []string{"2026-10-14"}},
		{"2026-10-14T23:59:58+02:00", "2026-10-15T00:00:00.5+02:00", []string{"2026-10-14", "2026-10-15"}},
		{"2026-03-29T01:59:58+01:00", "2026-03-29T03:00:02+02:00", []string{"2026-03-29"}},
		{"2026-10-25T02:59:58+02:00", "2026-10-25T02:00:02+01:00", []string{"2026-10-25"}},
	} {
		got, e := exportDates(epoch(tc.from), epoch(tc.to))
		if e != nil || strings.Join(got, ",") != strings.Join(tc.dates, ",") {
			t.Fatalf("%s–%s: %v, %v", tc.from, tc.to, got, e)
		}
	}
}

func testVideo(t *testing.T, file string, audio bool) {
	t.Helper()
	if e := os.MkdirAll(filepath.Dir(file), 0700); e != nil {
		t.Fatal(e)
	}
	args := []string{"-nostdin", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=160x90:rate=10"}
	if audio {
		args = append(args, "-f", "lavfi", "-i", "sine=frequency=880:sample_rate=44100", "-c:a", "aac")
	}
	args = append(args, "-t", "3", "-c:v", "libx264", "-threads", "1", "-pix_fmt", "yuv420p", "-y", file)
	if out, e := exec.Command("ffmpeg", args...).CombinedOutput(); e != nil {
		t.Fatalf("fixture: %v %s", e, out)
	}
	past := time.Now().Add(-time.Minute)
	if e := os.Chtimes(file, past, past); e != nil {
		t.Fatal(e)
	}
}
func midnightCamera(t *testing.T, a *App, firstAudio, secondAudio bool) Camera {
	t.Helper()
	c := Camera{ID: "midnight", Name: "Mitternacht", Folder: "Midnight", Pattern: "{YYYY}/{MM}/{DD}/{hh}-{mm}-{ss}.mp4", Timezone: "Europe/Berlin"}
	testVideo(t, filepath.Join(a.config().RecordingsDir, c.Folder, "2026/10/14/23-59-57.mp4"), firstAudio)
	testVideo(t, filepath.Join(a.config().RecordingsDir, c.Folder, "2026/10/15/00-00-00.mp4"), secondAudio)
	if e := a.db.SaveCamera(c); e != nil {
		t.Fatal(e)
	}
	a.scanDay("2026-10-14")
	return c
}
func exportRequest(t *testing.T, a *App, cookie *http.Cookie, s Session, endpoint string, body any) *httptest.ResponseRecorder {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		w := request(a, "POST", endpoint, body, cookie, s.CSRF)
		if w.Code != 409 {
			return w
		}
		var value struct{ Indexing bool }
		if e := json.Unmarshal(w.Body.Bytes(), &value); e != nil || !value.Indexing {
			t.Fatal(w.Code, w.Body.String())
		}
		if time.Now().After(deadline) {
			t.Fatal("export indexing did not finish")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
func awaitExport(t *testing.T, a *App, w *httptest.ResponseRecorder) *exportJob {
	t.Helper()
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	var value struct{ ID string }
	if e := json.Unmarshal(w.Body.Bytes(), &value); e != nil {
		t.Fatal(e)
	}
	a.exportMu.Lock()
	job := a.exports[value.ID]
	a.exportMu.Unlock()
	if job == nil {
		t.Fatal("job missing")
	}
	select {
	case <-job.done:
	case <-time.After(30 * time.Second):
		t.Fatal("export timeout")
	}
	a.exportMu.Lock()
	status, err := job.Status, job.Error
	a.exportMu.Unlock()
	if status != "ready" {
		t.Fatal(status, err)
	}
	return job
}
func TestExportIndexesUnopenedFollowingDay(t *testing.T) {
	a := testApp(t)
	c := midnightCamera(t, a, false, false)
	cookie, s := login(t, a)
	start, _ := time.Parse(time.RFC3339, "2026-10-14T23:59:58+02:00")
	body := map[string]any{"camera": c.ID, "from": start.Unix(), "to": start.Unix() + 4}
	var count int
	a.db.QueryRow(`SELECT COUNT(*) FROM recordings WHERE camera_id=?`, c.ID).Scan(&count)
	if count != 1 {
		t.Fatal("fixture following day already indexed", count)
	}
	// Do not open the following day or run a scan manually. Creation itself must
	// initiate preparation and eventually include both source files.
	w := request(a, "POST", "/api/exports", body, cookie, s.CSRF)
	if w.Code != 409 || !strings.Contains(w.Body.String(), `"indexing":true`) {
		t.Fatal(w.Code, w.Body.String())
	}
	job := awaitExport(t, a, exportRequest(t, a, cookie, s, "/api/exports", body))
	f, e := os.Open(filepath.Join(job.dir, "clip.mp4"))
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	duration, codec, e := probe(context.Background(), f)
	if e != nil || codec != "h264" || duration < 3.9 || duration > 4.2 {
		t.Fatal(duration, codec, e)
	}
	// A real gap remains a gap once indexing has completed.
	body["to"] = start.Unix() + 8
	w = exportRequest(t, a, cookie, s, "/api/exports/preview", body)
	if w.Code != 422 || !strings.Contains(w.Body.String(), "gaps") {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestExportEndingAtMidnightDoesNotScanFollowingDay(t *testing.T) {
	a := testApp(t)
	c := midnightCamera(t, a, false, false)
	cookie, s := login(t, a)
	start, _ := time.Parse(time.RFC3339, "2026-10-14T23:59:58+02:00")
	w := request(a, "POST", "/api/exports/preview", map[string]any{"camera": c.ID, "from": start.Unix(), "to": start.Unix() + 2}, cookie, s.CSRF)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var count int
	a.db.QueryRow(`SELECT COUNT(*) FROM camera_days WHERE camera_id=? AND date='2026-10-15'`, c.ID).Scan(&count)
	if count != 0 {
		t.Fatal("exclusive end scanned unnecessary next day")
	}
}
func TestOfflineExportIsNotReportedAsRecordingGap(t *testing.T) {
	a := testApp(t)
	c := midnightCamera(t, a, false, false)
	cookie, s := login(t, a)
	if e := os.Rename(filepath.Join(a.config().RecordingsDir, c.Folder), filepath.Join(a.config().RecordingsDir, "Offline")); e != nil {
		t.Fatal(e)
	}
	start, _ := time.Parse(time.RFC3339, "2026-10-14T23:59:58+02:00")
	w := request(a, "POST", "/api/exports/preview", map[string]any{"camera": c.ID, "from": start.Unix(), "to": start.Unix() + 4}, cookie, s.CSRF)
	if w.Code != 503 || !strings.Contains(w.Body.String(), "nicht erreichbar") || strings.Contains(w.Body.String(), "gaps") {
		t.Fatal(w.Code, w.Body.String())
	}
}
func audioRMS(t *testing.T, file string, offset float64) float64 {
	t.Helper()
	out, e := exec.Command("ffmpeg", "-nostdin", "-v", "error", "-ss", fmt.Sprintf("%.3f", offset), "-i", file, "-t", "0.5", "-map", "0:a:0", "-ac", "1", "-ar", "8000", "-f", "f32le", "pipe:1").Output()
	if e != nil {
		t.Fatal(e)
	}
	if len(out) < 400 {
		t.Fatal("audio missing")
	}
	sum := 0.0
	for i := 0; i+4 <= len(out); i += 4 {
		v := float64(math.Float32frombits(binary.LittleEndian.Uint32(out[i : i+4])))
		sum += v * v
	}
	return math.Sqrt(sum / float64(len(out)/4))
}
func TestExportAudioAcrossMixedSourceFiles(t *testing.T) {
	for _, tc := range []struct {
		name          string
		first, second bool
	}{{"silent then audio", false, true}, {"audio then silent", true, false}, {"audio in both", true, true}} {
		t.Run(tc.name, func(t *testing.T) {
			a := testApp(t)
			c := midnightCamera(t, a, tc.first, tc.second)
			cookie, s := login(t, a)
			start, _ := time.Parse(time.RFC3339, "2026-10-14T23:59:58+02:00")
			body := map[string]any{"camera": c.ID, "from": start.Unix(), "to": start.Unix() + 4}
			job := awaitExport(t, a, exportRequest(t, a, cookie, s, "/api/exports", body))
			file := filepath.Join(job.dir, "clip.mp4")
			out, e := exec.Command("ffprobe", "-v", "error", "-select_streams", "a:0", "-show_entries", "stream=codec_name,sample_rate,channels,duration", "-of", "json", file).Output()
			if e != nil {
				t.Fatal(e)
			}
			var info struct {
				Streams []struct {
					Codec    string `json:"codec_name"`
					Rate     string `json:"sample_rate"`
					Channels int
					Duration string
				}
			}
			if e = json.Unmarshal(out, &info); e != nil || len(info.Streams) != 1 || info.Streams[0].Codec != "aac" || info.Streams[0].Rate != "48000" || info.Streams[0].Channels != 2 {
				t.Fatal(string(out), e)
			}
			first, last := audioRMS(t, file, .5), audioRMS(t, file, 2.5)
			if (first > .005) != tc.first || (last > .005) != tc.second {
				t.Fatalf("audio/silence boundaries wrong: %f %f", first, last)
			}
		})
	}
}
func TestCompatibleStreamingRetainsAudio(t *testing.T) {
	a := testApp(t)
	c := Camera{ID: "audio", Name: "Ton", Folder: "Audio", Pattern: "{YYYY}/{MM}/{DD}/{hh}-{mm}-{ss}.mp4", Timezone: "Europe/Berlin"}
	testVideo(t, filepath.Join(a.config().RecordingsDir, c.Folder, "2026/10/14/08-30-00.mp4"), true)
	a.db.SaveCamera(c)
	a.scanDay("2026-10-14")
	records, _ := a.db.Recordings(c.ID, 0, 2e10)
	if len(records) != 1 {
		t.Fatal(records)
	}
	cookie, s := login(t, a)
	w := request(a, "POST", "/api/streams", map[string]any{"recording": records[0].ID, "offset": 0}, cookie, s.CSRF)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var value struct{ ID string }
	json.Unmarshal(w.Body.Bytes(), &value)
	a.mediaMu.Lock()
	stream := a.streams[value.ID]
	a.mediaMu.Unlock()
	<-stream.done
	output, e := exec.Command("ffprobe", "-v", "error", "-select_streams", "a:0", "-show_entries", "stream=codec_name", "-of", "json", filepath.Join(stream.dir, "segment-000000.ts")).Output()
	if e != nil || !bytes.Contains(output, []byte(`"aac"`)) {
		t.Fatal(string(output), e)
	}
}
