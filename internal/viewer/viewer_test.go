package viewer

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func testApp(t *testing.T) *App {
	t.Helper()
	dir := t.TempDir()
	recordings := filepath.Join(dir, "recordings")
	os.Mkdir(recordings, 0700)
	a, e := New(context.Background(), Config{DataDir: filepath.Join(dir, "data"), RecordingsDir: recordings, BaseURL: "http://localhost:8080", LocalAdmin: true, SessionTTL: time.Hour, ScanInterval: time.Hour, MaxTranscoders: 1, CacheBytes: 32 << 20, DevHTTP: true}, fstest.MapFS{"index.html": {Data: []byte("test")}})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(a.Close)
	return a
}
func request(a *App, method, url string, body any, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
	var b bytes.Buffer
	if body != nil {
		json.NewEncoder(&b).Encode(body)
	}
	r := httptest.NewRequest(method, url, &b)
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("Origin", a.cfg.BaseURL)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", csrf)
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	return w
}
func login(t *testing.T, a *App) (*http.Cookie, Session) {
	t.Helper()
	if e := a.db.CreateAdmin("test", "a long secure password"); e != nil {
		t.Fatal(e)
	}
	w := request(a, "POST", "/auth/local", map[string]string{"username": "test", "password": "a long secure password"}, nil, "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	cookie := w.Result().Cookies()[0]
	w = request(a, "GET", "/api/session", nil, cookie, "")
	var s Session
	if e := json.Unmarshal(w.Body.Bytes(), &s); e != nil {
		t.Fatal(e)
	}
	return cookie, s
}
func TestTimestamps(t *testing.T) {
	pattern := "{YYYY}/{MM}/{DD}/{hh}-{mm}-{ss}.mp4"
	cases := []struct {
		name, expected string
		bad            bool
	}{
		{"2026/10/14/08-30-00.mp4", "2026-10-14T06:30:00Z", false}, {"2026/03/29/02-30-00.mp4", "", true}, {"2026/10/25/02-30-00.mp4", "", true}, {"2026/02/30/08-00-00.mp4", "", true}, {"2026/10/14/25-30-00.mp4", "", true}, {"2026/10/14/08-30-00.mp4.bak", "", true}}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v, e := ParseTimestamp(pattern, c.name, "Europe/Berlin", 0)
			if c.bad {
				if e == nil {
					t.Fatal("must reject ambiguous or invalid timestamp")
				}
			} else if e != nil || v.Format(time.RFC3339) != c.expected {
				t.Fatal(v, e)
			}
		})
	}
	v, e := ParseTimestamp(pattern, cases[0].name, "Europe/Berlin", -5)
	if e != nil || v.Second() != 55 {
		t.Fatal(v, e)
	}
	if _, _, e := dayBounds("2026-03-29"); e != nil {
		t.Fatal(e)
	}
	f, to, _ := dayBounds("2026-03-29")
	if to.Sub(f) != 23*time.Hour {
		t.Fatal("spring day must be 23 hours")
	}
	f, to, _ = dayBounds("2026-10-25")
	if to.Sub(f) != 25*time.Hour {
		t.Fatal("autumn day must be 25 hours")
	}
	if _, e := patternRE("{YYYY}/{MM}/{DD}/file.mp4"); e == nil {
		t.Fatal("missing clock tokens")
	}
}
func TestRootConfinement(t *testing.T) {
	a := testApp(t)
	outside := filepath.Join(t.TempDir(), "secret.mp4")
	os.WriteFile(outside, []byte("secret"), 0600)
	os.Symlink(outside, filepath.Join(a.cfg.RecordingsDir, "escape.mp4"))
	for _, p := range []string{"../secret.mp4", outside, "escape.mp4", "a/../../x", "/etc/passwd"} {
		if f, e := openSource(a.root, p); e == nil {
			f.Close()
			t.Fatal("escaped root", p)
		}
	}
	os.WriteFile(filepath.Join(a.cfg.RecordingsDir, "good.mp4"), []byte("ok"), 0600)
	f, e := openSource(a.root, "good.mp4")
	if e != nil {
		t.Fatal(e)
	}
	f.Close()
}
func TestAuthAndCSRF(t *testing.T) {
	a := testApp(t)
	for _, url := range []string{"/api/cameras", "/api/day?date=2026-10-14", "/api/recordings/x/file", "/api/recordings/x/thumbnail", "/api/streams/x/index.m3u8"} {
		if w := request(a, "GET", url, nil, nil, ""); w.Code != 401 {
			t.Fatal(url, w.Code)
		}
	}
	cookie, s := login(t, a)
	if s.Role != "admin" || s.CSRF == "" {
		t.Fatal(s)
	}
	if w := request(a, "POST", "/api/logout", nil, cookie, ""); w.Code != 403 {
		t.Fatal(w.Code)
	}
	if w := request(a, "POST", "/api/logout", nil, cookie, s.CSRF); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := request(a, "GET", "/api/session", nil, cookie, ""); w.Code != 401 {
		t.Fatal("logout did not revoke session")
	}
	// Login attempts remain bounded even for unknown users.
	for i := 0; i < 9; i++ {
		w := request(a, "POST", "/auth/local", map[string]string{"username": "unknown", "password": "wrong"}, nil, "")
		if i == 8 && w.Code != 429 {
			t.Fatal("login limiter", w.Code)
		}
	}
}
func TestViewerPermissionsAndExpiry(t *testing.T) {
	a := testApp(t)
	w := httptest.NewRecorder()
	a.newSession(w, Session{Subject: "viewer", Name: "Viewer", Role: "viewer", Kind: "local"})
	cookie := w.Result().Cookies()[0]
	s, _ := a.session(httptest.NewRequest("GET", "/", nil))
	_ = s
	w = request(a, "POST", "/api/cameras", Camera{}, cookie, "bad")
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	a.db.Exec(`UPDATE sessions SET expires=0`)
	if w = request(a, "GET", "/api/session", nil, cookie, ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	a.cfg.DevHTTP = false
	w = httptest.NewRecorder()
	a.newSession(w, Session{Subject: "admin", Name: "Admin", Role: "admin", Kind: "local"})
	c := w.Result().Cookies()[0]
	if !c.Secure || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode {
		t.Fatal("unsafe cookie", c)
	}
	a.cfg.LocalAdmin = false
	if w = request(a, "GET", "/api/session", nil, c, ""); w.Code != 401 {
		t.Fatal("disabled local login session still allowed")
	}
}
func fixture(t *testing.T, a *App, codec string) Camera {
	t.Helper()
	folder := "Testkamera-A"
	os.MkdirAll(filepath.Join(a.cfg.RecordingsDir, folder, "2026/10/14"), 0700)
	file := filepath.Join(a.cfg.RecordingsDir, folder, "2026/10/14/08-30-00.mp4")
	args := []string{"-nostdin", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=160x90:rate=10", "-t", "3", "-c:v", codec, "-threads", "1", "-pix_fmt", "yuv420p"}
	if codec == "libx265" {
		args = append(args, "-x265-params", "pools=none:frame-threads=1:log-level=error")
	}
	args = append(args, "-y", file)
	if out, e := exec.Command("ffmpeg", args...).CombinedOutput(); e != nil {
		t.Fatalf("fixture: %s %v", out, e)
	}
	past := time.Now().Add(-time.Minute)
	os.Chtimes(file, past, past)
	c := Camera{ID: "camera-a", Name: "Testkamera-A", Folder: folder, Pattern: "{YYYY}/{MM}/{DD}/{hh}-{mm}-{ss}.mp4", Timezone: "Europe/Berlin"}
	if e := a.db.SaveCamera(c); e != nil {
		t.Fatal(e)
	}
	return c
}
func TestIndexAndMedia(t *testing.T) {
	a := testApp(t)
	c := fixture(t, a, "libx264")
	a.scanDay("2026-10-14")
	rs, e := a.db.Recordings(c.ID, 0, 2e10)
	if e != nil || len(rs) != 1 {
		t.Fatal(rs, e, a.scanStatus())
	}
	r := rs[0]
	if r.Duration < 2.9 || r.Codec != "h264" || r.Start != 1791959400 {
		t.Fatal("recording timestamp or metadata", r)
	}
	cookie, s := login(t, a)
	req := httptest.NewRequest("GET", "/api/recordings/"+r.ID+"/file", nil)
	req.AddCookie(cookie)
	req.Header.Set("Range", "bytes=0-15")
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, req)
	if w.Code != 206 || w.Body.Len() != 16 {
		t.Fatal("range", w.Code, w.Body.Len())
	}
	w = request(a, "GET", "/api/recordings/"+r.ID+"/thumbnail", nil, cookie, "")
	if w.Code != 200 || !bytes.HasPrefix(w.Body.Bytes(), []byte{0xff, 0xd8}) {
		t.Fatal("thumbnail", w.Code, w.Body.String())
	}
	w = request(a, "POST", "/api/streams", map[string]any{"recording": r.ID, "offset": 0}, cookie, s.CSRF)
	if w.Code != 201 {
		t.Fatal("hls", w.Code, w.Body.String())
	}
	var stream struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	}
	json.Unmarshal(w.Body.Bytes(), &stream)
	w = request(a, "GET", stream.URL, nil, cookie, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "#EXTINF") {
		t.Fatal("playlist", w.Code, w.Body.String())
	}
	owner := httptest.NewRecorder()
	a.newSession(owner, Session{Subject: "another", Name: "Other", Role: "viewer", Kind: "local"})
	if w = request(a, "GET", stream.URL, nil, owner.Result().Cookies()[0], ""); w.Code != 404 {
		t.Fatal("cross-session stream access", w.Code)
	}
	if w = request(a, "DELETE", "/api/streams/"+stream.ID, nil, cookie, s.CSRF); w.Code != 200 {
		t.Fatal(w.Code)
	}
	a.acquireEncoder()
	w = request(a, "POST", "/api/streams", map[string]any{"recording": r.ID, "offset": 0}, cookie, s.CSRF)
	a.releaseEncoder()
	if w.Code != 429 {
		t.Fatal("transcode limit", w.Code)
	}
	// Growing files are not trusted until they settle, even if previously indexed.
	file := filepath.Join(a.cfg.RecordingsDir, r.Path)
	data, _ := os.ReadFile(file)
	os.WriteFile(file, append(data, 0), 0600)
	w = request(a, "GET", "/api/recordings/"+r.ID+"/file", nil, cookie, "")
	if w.Code != 404 {
		t.Fatal("changed file served", w.Code)
	}
	a.scanDay("2026-10-14")
	past := time.Now().Add(-time.Minute)
	os.Chtimes(file, past, past)
	a.scanDay("2026-10-14")
	rs, _ = a.db.Recordings(c.ID, 0, 2e10)
	if len(rs) != 1 || rs[0].Size != int64(len(data)+1) {
		t.Fatal("incremental replacement", rs)
	}
	os.Remove(file)
	a.scanDay("2026-10-14")
	rs, _ = a.db.Recordings(c.ID, 0, 2e10)
	if len(rs) != 0 {
		t.Fatal("removed recording retained")
	}
}
func TestH265AndCorruptFile(t *testing.T) {
	a := testApp(t)
	c := fixture(t, a, "libx265")
	dir := filepath.Join(a.cfg.RecordingsDir, c.Folder, "2026/10/14")
	bad := filepath.Join(dir, "09-00-00.mp4")
	os.WriteFile(bad, []byte("not video"), 0600)
	past := time.Now().Add(-time.Minute)
	os.Chtimes(bad, past, past)
	a.scanDay("2026-10-14")
	rs, _ := a.db.Recordings(c.ID, 0, 2e10)
	if len(rs) != 1 || rs[0].Codec != "hevc" || len(a.scanStatus().Warnings) == 0 {
		t.Fatal(rs, a.scanStatus())
	}
	cookie, s := login(t, a)
	w := request(a, "POST", "/api/streams", map[string]any{"recording": rs[0].ID, "offset": .5}, cookie, s.CSRF)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	var stream struct {
		ID  string `json:"id"`
		URL string `json:"url"`
	}
	json.Unmarshal(w.Body.Bytes(), &stream)
	w = request(a, "GET", strings.TrimSuffix(stream.URL, "index.m3u8")+"segment-000000.ts", nil, cookie, "")
	if w.Code != 200 || w.Body.Len() == 0 {
		t.Fatal(w.Code)
	}
	a.removeStream(stream.ID)
}
func TestStorePersistenceAndCameraChange(t *testing.T) {
	dir := t.TempDir()
	s, e := OpenStore(dir)
	if e != nil {
		t.Fatal(e)
	}
	c := Camera{ID: "1", Name: "A", Folder: "a", Pattern: "x", Timezone: "UTC"}
	if e = s.SaveCamera(c); e != nil {
		t.Fatal(e)
	}
	s.Exec(`INSERT INTO recordings(id,camera_id,path,start,duration,codec,size,mod) VALUES('r','1','a/x',0,2,'h264',1,1)`)
	s.Close()
	s, e = OpenStore(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	rs, _ := s.Recordings("1", 0, 10)
	if len(rs) != 1 {
		t.Fatal("index did not persist")
	}
	c.Offset = 10
	s.SaveCamera(c)
	rs, _ = s.Recordings("1", 0, 10)
	if len(rs) != 0 {
		t.Fatal("changed timestamp config retained stale index")
	}
}
func TestOfflineSourcePreservesIndex(t *testing.T) {
	a := testApp(t)
	c := fixture(t, a, "libx264")
	a.scanDay("2026-10-14")
	os.Rename(filepath.Join(a.cfg.RecordingsDir, c.Folder), filepath.Join(a.cfg.RecordingsDir, "offline"))
	a.scanDay("2026-10-14")
	rs, _ := a.db.Recordings(c.ID, 0, 2e10)
	if len(rs) != 1 {
		t.Fatal("offline camera erased index")
	}
}
func TestOriginAndHealth(t *testing.T) {
	a := testApp(t)
	w := request(a, "GET", "/healthz", nil, nil, "")
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	r := httptest.NewRequest("POST", "/auth/local", strings.NewReader(`{}`))
	r.Header.Set("Origin", "https://evil.example")
	w = httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	_, _ = io.Copy(io.Discard, w.Result().Body)
}
