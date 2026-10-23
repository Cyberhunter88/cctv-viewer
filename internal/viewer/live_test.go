package viewer

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func liveTestCamera(t *testing.T, a *App, id string) {
	t.Helper()
	e := a.db.SaveCamera(Camera{ID: id, Name: id, Folder: id, Pattern: "{YYYY}/{MM}/{DD}/{hh}-{mm}-{ss}.mp4", Timezone: "UTC"})
	if e != nil {
		t.Fatal(e)
	}
}
func TestLiveConfigurationSecretsAndValidation(t *testing.T) {
	a := testApp(t)
	cookie, session := login(t, a)
	liveTestCamera(t, a, "camera")
	payload := map[string]any{"enabled": true, "username": "camera-user", "password": "synthetic camera credential", "shared": true, "sources": map[string]string{"fast": "rtsp://192.0.2.20/sub"}}
	w := request(a, "PUT", "/api/cameras/camera/live", payload, cookie, session.CSRF)
	if w.Code != 200 || strings.Contains(w.Body.String(), "synthetic camera credential") {
		t.Fatal(w.Code, w.Body.String())
	}
	delete(payload, "password")
	w = request(a, "PUT", "/api/cameras/camera/live", payload, cookie, session.CSRF)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	saved, _ := a.db.liveCamera("camera")
	if saved.Password != "synthetic camera credential" {
		t.Fatal("password was not preserved")
	}
	w = request(a, "GET", "/api/live", nil, cookie, "")
	if w.Code != 200 || strings.Contains(w.Body.String(), "192.0.2.20") || strings.Contains(w.Body.String(), "credential") {
		t.Fatal(w.Body.String())
	}
	payload["clearPassword"] = true
	request(a, "PUT", "/api/cameras/camera/live", payload, cookie, session.CSRF)
	saved, _ = a.db.liveCamera("camera")
	if saved.Password != "" {
		t.Fatal("password not cleared")
	}
	for _, raw := range []string{"exec:touch /tmp/no", "rtsp://user:pass@example.org/sub", "rtsp://example.org/sub#exec=bad", "http://example.org", "rtsp://example.org/\nsub"} {
		if validateLive(liveCamera{Enabled: true, Sources: liveSources{Fast: raw}}) == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
	if request(a, "PUT", "/api/cameras/camera/live", payload, nil, "").Code != 401 {
		t.Fatal("unauthenticated configuration")
	}
	viewerResponse := httptest.NewRecorder()
	if err := a.newSession(viewerResponse, Session{Subject: "reader", Name: "Reader", Role: "viewer", Kind: "local"}); err != nil {
		t.Fatal(err)
	}
	viewerCookie := viewerResponse.Result().Cookies()[0]
	if request(a, "GET", "/api/cameras/camera/live", nil, viewerCookie, "").Code != 403 || request(a, "GET", "/api/admin/live-share", nil, viewerCookie, "").Code != 403 {
		t.Fatal("viewer can read administrator configuration")
	}
	if request(a, "PUT", "/api/cameras/camera/live", payload, cookie, "wrong").Code != 403 {
		t.Fatal("missing CSRF check")
	}
	if request(a, "DELETE", "/api/cameras/camera/live", nil, cookie, session.CSRF).Code != 200 {
		t.Fatal("delete failed")
	}
	saved, _ = a.db.liveCamera("camera")
	if saved.Enabled || saved.Sources.Fast != "" {
		t.Fatal("delete did not clear configuration")
	}
}
func TestLivePreferencesAndExportPaths(t *testing.T) {
	if !validPreferences(map[string]json.RawMessage{"liveCameras": json.RawMessage(`[]`), "liveOrder": json.RawMessage(`["a"]`), "liveLayout": json.RawMessage(`"2x2"`)}) {
		t.Fatal("valid preferences rejected")
	}
	for _, path := range []string{"live/a/wide/fast", "live/a/zoom/high"} {
		if _, _, _, ok := parseLivePath(path); !ok {
			t.Fatal(path)
		}
	}
	for _, path := range []string{"live/a/wide/fast/other", "live/a/wide/exec", "api/streams", "live/a/../fast"} {
		if _, _, _, ok := parseLivePath(path); ok {
			t.Fatal(path)
		}
	}
	a := testApp(t)
	cookie, session := login(t, a)
	w := request(a, "PUT", "/api/admin/live-share", map[string]any{"enabled": false, "resetPassword": true}, cookie, session.CSRF)
	var first struct {
		Password string `json:"password"`
	}
	json.Unmarshal(w.Body.Bytes(), &first)
	if len(first.Password) < 32 {
		t.Fatal(w.Body.String())
	}
	w = request(a, "GET", "/api/admin/live-share", nil, cookie, "")
	if strings.Contains(w.Body.String(), first.Password) {
		t.Fatal("export password leaked on read")
	}
}
func TestLiveMigrationAndMissingBinary(t *testing.T) {
	a := testApp(t)
	liveTestCamera(t, a, "before")
	if _, e := a.db.Exec(`PRAGMA user_version=4; DROP TABLE camera_live; DROP TABLE live_share`); e != nil {
		t.Fatal(e)
	}
	// Open a second store to verify the migration includes committed WAL data.
	store, e := OpenStore(a.cfg.DataDir)
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	if _, e = store.Camera("before"); e != nil {
		t.Fatal(e)
	}
	var version int
	store.QueryRow(`PRAGMA user_version`).Scan(&version)
	if version != 5 {
		t.Fatal(version)
	}
	info, e := os.Stat(filepath.Join(a.cfg.DataDir, "viewer-before-v5.db"))
	if e != nil || info.Mode().Perm() != 0600 {
		t.Fatal("missing protected migration backup", e)
	}
	if e = store.saveLiveCamera("before", liveCamera{}); e != nil {
		t.Fatal(e)
	}
}
func TestLiveSyntheticRestream(t *testing.T) {
	binary := os.Getenv("CTV_TEST_GO2RTC")
	if binary == "" {
		t.Skip("CTV_TEST_GO2RTC required for real RTSP/WebSocket integration")
	}
	address, e := freeLocalAddress()
	if e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	source := filepath.Join(dir, "camera.mp4")
	cmd := exec.Command("ffmpeg", "-nostdin", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=160x90:rate=10", "-f", "lavfi", "-i", "sine=frequency=880:sample_rate=48000", "-t", "20", "-c:v", "libx264", "-threads", "1", "-g", "10", "-bf", "0", "-c:a", "aac", "-y", source)
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatal(e, string(out))
	}
	config := map[string]any{"modules": []string{"rtsp", "exec", "ffmpeg"}, "api": map[string]string{"listen": ""}, "rtsp": map[string]string{"listen": address}, "streams": map[string]string{"camera": "ffmpeg:" + source + "#video=copy#audio=copy"}, "log": map[string]string{"level": "error"}}
	b, _ := json.Marshal(config)
	file := filepath.Join(dir, "source.json")
	os.WriteFile(file, b, 0600)
	sourceCtx, sourceCancel := context.WithCancel(context.Background())
	defer sourceCancel()
	upstream := exec.CommandContext(sourceCtx, binary, "-config", file)
	if e = upstream.Start(); e != nil {
		t.Fatal(e)
	}
	defer func() { sourceCancel(); upstream.Wait() }()
	a := testApp(t)
	a.live.close()
	a.cfg.Go2RTCPath = binary
	a.cfg.LiveRTSPAddress, _ = freeLocalAddress()
	a.live = newLiveService(a)
	deadline := time.Now().Add(15 * time.Second)
	for {
		ready, _ := a.live.status()
		if ready {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("live service not ready")
		}
		time.Sleep(50 * time.Millisecond)
	}
	cookie, session := login(t, a)
	liveTestCamera(t, a, "camera")
	c := liveCamera{Enabled: true, Shared: true, Sources: liveSources{Fast: "rtsp://" + address + "/camera", ZoomFast: "rtsp://" + address + "/camera"}}
	if e = a.db.saveLiveCamera("camera", c); e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(a.Handler())
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	headers := http.Header{"Origin": []string{a.cfg.BaseURL}, "Cookie": []string{cookie.String()}}
	ws, _, e := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/api/live/camera/ws?mode=mse", &websocket.DialOptions{HTTPHeader: headers})
	if e != nil {
		t.Fatal(e)
	}
	defer ws.CloseNow()
	ws.Write(ctx, websocket.MessageText, []byte(`{"type":"mse","value":"avc1.640029,mp4a.40.2"}`))
	binaryReceived := false
	for !binaryReceived {
		kind, data, err := ws.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if kind == websocket.MessageText && strings.Contains(string(data), `"error"`) {
			t.Fatal(string(data))
		}
		binaryReceived = kind == websocket.MessageBinary && len(data) > 0
	}
	badHeaders := headers.Clone()
	badHeaders.Set("Origin", "https://other.example.org")
	if bad, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/api/live/camera/ws?mode=mse", &websocket.DialOptions{HTTPHeader: badHeaders}); err == nil || resp == nil || resp.StatusCode != 403 {
		if bad != nil {
			bad.CloseNow()
		}
		t.Fatal("cross-origin live connection was accepted")
	}
	// The RTSP export must authenticate and share the same source independently of UI selection.
	w := request(a, "PUT", "/api/admin/live-share", map[string]bool{"enabled": true}, cookie, session.CSRF)
	var share struct {
		Password string `json:"password"`
	}
	json.Unmarshal(w.Body.Bytes(), &share)
	if share.Password == "" {
		t.Fatal(w.Code, w.Body.String())
	}
	// Enabling export must not interrupt a viewer already watching this source.
	for {
		kind, _, err := ws.Read(ctx)
		if err != nil {
			t.Fatal("export interrupted viewer stream", err)
		}
		if kind == websocket.MessageBinary {
			break
		}
	}
	u := &url.URL{Scheme: "rtsp", Host: a.cfg.LiveRTSPAddress, Path: "/live/camera/wide/fast", User: url.UserPassword("homeassistant", share.Password)}
	probe := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-rtsp_transport", "tcp", "-show_entries", "stream=codec_name", "-of", "json", u.String())
	if out, err := probe.CombinedOutput(); err != nil || !strings.Contains(string(out), "h264") {
		t.Fatal("RTSP export failed", err, strings.ReplaceAll(string(out), share.Password, "[redacted]"))
	}
	conn, err := net.DialTimeout("tcp", a.cfg.LiveRTSPAddress, 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	conn.SetDeadline(time.Now().Add(2 * time.Second))
	_, err = conn.Write([]byte("ANNOUNCE rtsp://localhost/live/camera/wide/fast RTSP/1.0\r\nCSeq: 1\r\nContent-Length: 0\r\n\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	conn.Close()
	if err != nil || !strings.Contains(line, "501") {
		t.Fatal("publisher method was not rejected", line, err)
	}
	// A crashed child is restarted, and a subsequent request recreates its sources.
	a.live.mu.Lock()
	oldAPI := a.live.api
	a.live.mu.Unlock()
	_ = a.live.call(ctx, http.MethodPost, "/api/exit?code=0", nil, nil)
	deadline = time.Now().Add(15 * time.Second)
	for {
		a.live.mu.Lock()
		restarted := a.live.ready && a.live.api != oldAPI
		a.live.mu.Unlock()
		if restarted {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("live supervisor did not restart")
		}
		time.Sleep(50 * time.Millisecond)
	}
	ws2, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http")+"/api/live/camera/ws?mode=mse", &websocket.DialOptions{HTTPHeader: headers})
	if err != nil {
		t.Fatal(err)
	}
	defer ws2.CloseNow()
	ws2.Write(ctx, websocket.MessageText, []byte(`{"type":"mse","value":"avc1.640029,mp4a.40.2"}`))
	for {
		kind, _, err := ws2.Read(ctx)
		if err != nil {
			t.Fatal("restarted stream did not deliver media", err)
		}
		if kind == websocket.MessageBinary {
			break
		}
	}
	if request(a, "POST", "/api/logout", nil, cookie, session.CSRF).Code != 200 {
		t.Fatal("logout failed")
	}
	closeCtx, closeCancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer closeCancel()
	for {
		_, _, err = ws2.Read(closeCtx)
		if err != nil {
			break
		}
	}
	if closeCtx.Err() != nil {
		t.Fatal("live stream survived session revocation")
	}

	runtimeFiles, err := filepath.Glob(filepath.Join(a.cfg.DataDir, "cache", "live-runtime-*"))
	if err != nil || len(runtimeFiles) != 0 {
		t.Fatal("live source API persisted a runtime configuration", runtimeFiles, err)
	}

}
