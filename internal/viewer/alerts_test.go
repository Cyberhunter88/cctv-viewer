package viewer

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// alertReceiver records webhook deliveries and answers with a configurable status.
type alertReceiver struct {
	mu       sync.Mutex
	messages []alertMessage
	status   int
	server   *httptest.Server
}

func newAlertReceiver(t *testing.T) *alertReceiver {
	t.Helper()
	receiver := &alertReceiver{status: 204}
	receiver.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var message alertMessage
		receiver.mu.Lock()
		defer receiver.mu.Unlock()
		if r.Method != http.MethodPost || !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") || json.Unmarshal(body, &message) != nil {
			w.WriteHeader(400)
			return
		}
		if receiver.status == 302 {
			w.Header().Set("Location", "/elsewhere")
		}
		if receiver.status >= 200 && receiver.status < 300 || r.URL.Path == "/elsewhere" {
			receiver.messages = append(receiver.messages, message)
		}
		w.WriteHeader(receiver.status)
	}))
	t.Cleanup(receiver.server.Close)
	return receiver
}

func (r *alertReceiver) events() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []string{}
	for _, message := range r.messages {
		out = append(out, message.Event+":"+message.CameraID)
	}
	return strings.Join(out, " ")
}

func (r *alertReceiver) setStatus(status int) {
	r.mu.Lock()
	r.status = status
	r.mu.Unlock()
}

func TestAlertsReportEachStateChangeOnce(t *testing.T) {
	a := testApp(t)
	receiver := newAlertReceiver(t)
	for _, id := range []string{"front", "garden"} {
		if err := os.Mkdir(filepath.Join(a.cfg.RecordingsDir, id), 0700); err != nil {
			t.Fatal(err)
		}
		if err := a.db.SaveCamera(Camera{ID: id, Name: "Kamera " + id, Folder: id, Pattern: "{YYYY}{MM}{DD}{hh}{mm}{ss}.mp4", Timezone: "Europe/Berlin"}); err != nil {
			t.Fatal(err)
		}
	}
	insert := func(id, camera string, age time.Duration) {
		t.Helper()
		if _, err := a.db.Exec(`INSERT INTO recordings(id,camera_id,path,start,duration,codec,size,mod,filename) VALUES(?,?,?,?,30,'h264',1,1,?)`, id, camera, camera+"/"+id+".mp4", float64(time.Now().Add(-age).Unix()), id+".mp4"); err != nil {
			t.Fatal(err)
		}
	}
	configure := func(target string, hours int) {
		a.cfgMu.Lock()
		a.cfg.AlertWebhookURL, a.cfg.AlertStaleHours = target, hours
		a.cfgMu.Unlock()
	}
	expect := func(step, want string) {
		t.Helper()
		a.checkAlerts()
		if got := receiver.events(); got != want {
			t.Fatalf("%s:\n got %q\nwant %q", step, got, want)
		}
	}
	// Without an address nothing is sent, whatever the state.
	insert("old", "front", 5*time.Hour)
	configure("", 2)
	expect("disabled", "")
	configure(receiver.server.URL+"/hook", 2)
	expect("first check reports the stale camera", "recordings_stale:front")
	expect("unchanged state stays quiet", "recordings_stale:front")
	if message := receiver.messages[0]; !strings.Contains(message.Message, "Kamera front") || message.Camera != "Kamera front" || message.LastRecording == "" || message.Time == "" {
		t.Fatal(message)
	}
	insert("fresh", "front", time.Minute)
	expect("new recording", "recordings_stale:front recordings_resumed:front")
	// A camera without any recording is not stale; an unreachable source is reported once.
	if err := os.Remove(filepath.Join(a.cfg.RecordingsDir, "garden")); err != nil {
		t.Fatal(err)
	}
	expect("offline", "recordings_stale:front recordings_resumed:front source_offline:garden")
	expect("still offline", "recordings_stale:front recordings_resumed:front source_offline:garden")
	// A failed delivery is repeated with the next check instead of being lost.
	if err := os.Mkdir(filepath.Join(a.cfg.RecordingsDir, "garden"), 0700); err != nil {
		t.Fatal(err)
	}
	receiver.setStatus(500)
	expect("delivery fails", "recordings_stale:front recordings_resumed:front source_offline:garden")
	if a.metrics.alertFailures.Load() != 1 {
		t.Fatal("failure not counted", a.metrics.alertFailures.Load())
	}
	receiver.setStatus(204)
	expect("retry", "recordings_stale:front recordings_resumed:front source_offline:garden source_online:garden")
	if a.metrics.alertsSent.Load() != 4 {
		t.Fatal(a.metrics.alertsSent.Load())
	}
	// Disabling the stale check reports the recovery; removed cameras are forgotten.
	if _, err := a.db.Exec(`UPDATE recordings SET start=start-86400 WHERE camera_id='front'`); err != nil {
		t.Fatal(err)
	}
	expect("stale again", "recordings_stale:front recordings_resumed:front source_offline:garden source_online:garden recordings_stale:front")
	if _, err := a.db.Exec(`DELETE FROM cameras WHERE id='front'`); err != nil {
		t.Fatal(err)
	}
	a.checkAlerts()
	a.alertMu.Lock()
	_, kept := a.alertState["front"]
	a.alertMu.Unlock()
	if kept {
		t.Fatal("state of a removed camera kept")
	}
}

func TestAlertRedirectsAreNotFollowed(t *testing.T) {
	a := testApp(t)
	receiver := newAlertReceiver(t)
	receiver.setStatus(302)
	if err := a.sendAlert(t.Context(), receiver.server.URL+"/hook", alertMessage{Event: "test"}); err == nil || strings.Contains(err.Error(), receiver.server.URL) {
		t.Fatal("redirect accepted or address leaked", err)
	}
	if receiver.events() != "" {
		t.Fatal("redirect target was contacted", receiver.events())
	}
}

func TestAlertSettingsStaySecretAndValidated(t *testing.T) {
	a := testApp(t)
	cookie, s := login(t, a)
	receiver := newAlertReceiver(t)
	save := func(values map[string]any, reset []string) *httptest.ResponseRecorder {
		return request(a, "PUT", "/api/admin/settings", map[string]any{"version": a.settingsVersion, "values": values, "reset": reset}, cookie, s.CSRF)
	}
	if w := request(a, "POST", "/api/admin/alerts/test", nil, cookie, s.CSRF); w.Code != 400 {
		t.Fatal("test without address", w.Code)
	}
	for name, values := range map[string]map[string]any{
		"file scheme":    {"alertWebhookURL": "file:///etc/passwd"},
		"no scheme":      {"alertWebhookURL": "example.org/hook"},
		"no host":        {"alertWebhookURL": "https:///hook"},
		"fragment":       {"alertWebhookURL": "https://example.org/hook#x"},
		"too long":       {"alertWebhookURL": "https://example.org/" + strings.Repeat("a", 500)},
		"negative hours": {"alertStaleHours": -1},
		"too many hours": {"alertStaleHours": 169},
		"encoder":        {"encoder": "nvenc"},
		"empty encoder":  {"encoder": ""},
	} {
		if w := save(values, nil); w.Code != 400 {
			t.Fatalf("%s accepted: %d %s", name, w.Code, w.Body.String())
		}
	}
	target := receiver.server.URL + "/hook?token=SECRETTOKEN"
	if w := save(map[string]any{"alertWebhookURL": target, "alertStaleHours": 12, "encoder": "vaapi"}, nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if c := a.config(); c.AlertWebhookURL != target || c.AlertStaleHours != 12 || c.Encoder != "vaapi" {
		t.Fatal(c.AlertWebhookURL, c.AlertStaleHours, c.Encoder)
	}
	w := request(a, "GET", "/api/admin/settings", nil, cookie, "")
	var settings struct {
		Values          map[string]any    `json:"values"`
		Sources         map[string]string `json:"sources"`
		AlertWebhookSet bool              `json:"alertWebhookSet"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &settings); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(w.Body.String(), "SECRETTOKEN") || !settings.AlertWebhookSet || settings.Values["alertStaleHours"] != float64(12) || settings.Values["encoder"] != "vaapi" || settings.Sources["alertWebhookURL"] != "Weboberfläche" {
		t.Fatal(w.Body.String())
	}
	if w := request(a, "GET", "/api/admin/backup", nil, cookie, ""); w.Code != 200 || strings.Contains(w.Body.String(), "SECRETTOKEN") || !strings.Contains(w.Body.String(), "alertStaleHours") {
		t.Fatal(w.Code, w.Body.String())
	}
	var stored string
	if err := a.db.QueryRow(`SELECT payload FROM settings WHERE id=1`).Scan(&stored); err != nil || strings.Contains(stored, "SECRETTOKEN") {
		t.Fatal("address stored in plain text", err)
	}
	if w := request(a, "POST", "/api/admin/alerts/test", nil, cookie, ""); w.Code != 403 {
		t.Fatal("test without CSRF token", w.Code)
	}
	if w := request(a, "POST", "/api/admin/alerts/test", nil, cookie, s.CSRF); w.Code != 200 || receiver.events() != "test:" {
		t.Fatal(w.Code, w.Body.String(), receiver.events())
	}
	receiver.setStatus(503)
	if w := request(a, "POST", "/api/admin/alerts/test", nil, cookie, s.CSRF); w.Code != 502 || strings.Contains(w.Body.String(), "SECRETTOKEN") || !strings.Contains(w.Body.String(), "503") {
		t.Fatal(w.Code, w.Body.String())
	}
	// The rejected attempt without an address is recorded as well.
	var audited int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action='alert.test'`).Scan(&audited); err != nil || audited != 3 {
		t.Fatal(audited, err)
	}
	// Resetting removes the address and thereby disables alerts.
	if w := save(map[string]any{}, []string{"alertWebhookURL"}); w.Code != 200 || a.config().AlertWebhookURL != "" {
		t.Fatal(w.Code, w.Body.String(), a.config().AlertWebhookURL)
	}
	if w := save(map[string]any{}, []string{"unknown"}); w.Code != 400 {
		t.Fatal(w.Code)
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
