package viewer

import (
	"database/sql"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReolinkTimestamp(t *testing.T) {
	for _, prefix := range []string{"", "Testkamera-A"} {
		c := Camera{Format: "reolink", Prefix: prefix, Timezone: "Europe/Berlin", Offset: 5}
		for _, name := range []string{
			"2026/10/14/Testkamera-A_00_20261014061217.mp4",
			"2026/10/14/Andere Testkamera_01_20261014061217.MP4",
			"2026/10/14/Testkamera-A_01_20261014061217.jpg",
			"2026/10/14/Bild_mit_Unterstrichen_12_20261014061217.JPG",
			"2026/10/14/Other_00_20261014061217.JPEG",
		} {
			got, e := cameraTimestamp(c, name)
			if e != nil || got.Format(time.RFC3339) != "2026-10-14T04:12:22Z" {
				t.Fatalf("prefix %q, %s: %v, %v", prefix, name, got, e)
			}
		}
		for _, name := range []string{
			"2026/10/13/Testkamera-A_00_20261014061217.mp4",
			"2026/10/25/Testkamera-A_00_20261025023000.mp4",
			"2026/03/29/Testkamera-A_00_20260329023000.jpg",
			"2026/10/14/Other_00_20261014253000.mp4",
			"2026/10/14/Other_00_20261014061217.mp4.bak",
			"2026/10/14/Other_00_20261014061217.png",
			"2026/10/14/Other.mp4",
			"2026/10/14/Other_00_20261014061217/Other_00_20261014061217.mp4",
		} {
			if _, e := cameraTimestamp(c, name); e == nil {
				t.Fatal("accepted", name)
			}
		}
	}
}

func TestReolinkWithoutPrefix(t *testing.T) {
	for _, prefix := range []string{"", "Old camera name"} {
		t.Run("prefix="+prefix, func(t *testing.T) {
			a := testApp(t)
			cookie, session := login(t, a)
			c := Camera{Name: "Testkamera-A", Folder: "Reolink/Testkamera-A", Format: "reolink", Prefix: prefix, Timezone: "Europe/Berlin"}
			folder := filepath.Join(a.config().RecordingsDir, c.Folder, "2026/10/14")
			testVideo(t, filepath.Join(folder, "Video_00_20261014061217.MP4"), false)
			writeJPEG(t, filepath.Join(folder, "Anderer Name_01_20261014061217.JPG"))
			writeJPEG(t, filepath.Join(folder, "Dritter_Name_02_20261014061217.JPEG"))
			w := request(a, "POST", "/api/cameras", c, cookie, session.CSRF)
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			if e := json.Unmarshal(w.Body.Bytes(), &c); e != nil {
				t.Fatal(e)
			}
			w = request(a, "POST", "/api/cameras/preview", c, cookie, session.CSRF)
			if w.Code != 200 || strings.Count(w.Body.String(), "04:12:17Z") != 1 || strings.Contains(w.Body.String(), `"error"`) {
				t.Fatal(w.Code, w.Body.String())
			}
			a.scanDay("2026-10-14")
			records, e := a.db.Recordings(c.ID, 1791936000, 1792022400)
			if e != nil || len(records) != 1 {
				t.Fatal(records, e)
			}
			images, videos := 0, 0
			for _, record := range records {
				if record.Kind == "image" {
					images++
				} else {
					videos++
				}
			}
			if images != 0 || videos != 1 {
				t.Fatal("wrong media kinds", records)
			}
			// Saving the new form must not discard the index when clearing a legacy prefix.
			c.Prefix = ""
			w = request(a, "POST", "/api/cameras", c, cookie, session.CSRF)
			if w.Code != 200 {
				t.Fatal(w.Code, w.Body.String())
			}
			records, e = a.db.Recordings(c.ID, 1791936000, 1792022400)
			if e != nil || len(records) != 1 {
				t.Fatal(records, e)
			}
		})
	}
}
func writeJPEG(t *testing.T, file string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(file), 0700)
	f, e := os.Create(file)
	if e != nil {
		t.Fatal(e)
	}
	img := image.NewRGBA(image.Rect(0, 0, 32, 20))
	img.Set(1, 1, color.White)
	if e = jpeg.Encode(f, img, nil); e != nil {
		t.Fatal(e)
	}
	f.Close()
	past := time.Now().Add(-time.Minute)
	os.Chtimes(file, past, past)
}
func TestUserManagement(t *testing.T) {
	a := testApp(t)
	cookie, s := login(t, a)
	edit := func(name, role, password string, disabled, create bool) *httptest.ResponseRecorder {
		return request(a, "POST", "/api/admin/users", map[string]any{"username": name, "role": role, "password": password, "disabled": disabled, "create": create}, cookie, s.CSRF)
	}
	if w := edit("test", "viewer", "", false, false); w.Code != 409 {
		t.Fatal("last admin", w.Code)
	}
	if w := edit("test", "admin", "", true, false); w.Code != 409 {
		t.Fatal("last admin disabled", w.Code)
	}
	if w := request(a, "DELETE", "/api/admin/users/test", nil, cookie, s.CSRF); w.Code != 409 {
		t.Fatal("last admin deleted", w.Code)
	}
	if w := edit("reader", "viewer", "secure reader password", false, true); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := edit("reader", "viewer", "secure reader password", false, true); w.Code != 409 {
		t.Fatal("duplicate", w.Code)
	}
	w := request(a, "POST", "/auth/local", map[string]string{"username": "reader", "password": "secure reader password"}, nil, "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	reader := w.Result().Cookies()[0]
	w = request(a, "GET", "/api/session", nil, reader, "")
	var rs Session
	json.Unmarshal(w.Body.Bytes(), &rs)
	if rs.Role != "viewer" {
		t.Fatal(rs)
	}
	for _, url := range []string{"/api/admin/users", "/api/admin/settings", "/api/cameras/folders"} {
		if w = request(a, "GET", url, nil, reader, ""); w.Code != 403 {
			t.Fatal("viewer admin access", url, w.Code)
		}
	}
	if w = edit("reader", "viewer", "", true, false); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w = request(a, "GET", "/api/session", nil, reader, ""); w.Code != 401 {
		t.Fatal("session not revoked", w.Code)
	}
	if w = edit("reader", "admin", "", false, false); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w = request(a, "DELETE", "/api/admin/users/test", nil, cookie, s.CSRF); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = request(a, "GET", "/api/session", nil, cookie, ""); w.Code != 401 {
		t.Fatal("deleted admin session", w.Code)
	}
}
func TestPasswordAndStaleLogin(t *testing.T) {
	a := testApp(t)
	cookie, s := login(t, a)
	var old string
	a.db.QueryRow(`SELECT hash FROM admins WHERE username='test'`).Scan(&old)
	w := request(a, "POST", "/api/password", map[string]string{"current": "wrong", "password": "new secure password"}, cookie, s.CSRF)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
	w = request(a, "POST", "/api/password", map[string]string{"current": "a long secure password", "password": "new secure password"}, cookie, s.CSRF)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if request(a, "GET", "/api/session", nil, cookie, "").Code != 401 {
		t.Fatal("old session survived")
	}
	if e := a.newLocalSession(httptest.NewRecorder(), "test", "admin", old); e == nil {
		t.Fatal("stale password issued session")
	}
}
func TestSettingsPersistenceValidationAndReset(t *testing.T) {
	a := testApp(t)
	cookie, s := login(t, a)
	update := func(version int, values map[string]any, reset []string) *httptest.ResponseRecorder {
		return request(a, "PUT", "/api/admin/settings", map[string]any{"version": version, "values": values, "reset": reset}, cookie, s.CSRF)
	}
	if w := update(0, map[string]any{"cacheMB": 64, "scanInterval": 900, "maxTranscoders": 3, "sessionMinutes": 30}, nil); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if a.config().CacheBytes != 64<<20 || a.config().MaxTranscoders != 3 {
		t.Fatal(a.config())
	}
	if w := update(0, map[string]any{"cacheMB": 128}, nil); w.Code != 409 {
		t.Fatal("stale version", w.Code)
	}
	if w := update(1, map[string]any{"sessionMinutes": 1}, nil); w.Code != 400 {
		t.Fatal(w.Code)
	}
	if w := update(1, map[string]any{"localLogin": false}, nil); w.Code != 400 {
		t.Fatal("lockout accepted", w.Code)
	}
	if w := update(1, map[string]any{"oidcIssuer": "http://bad", "oidcClientId": "client", "oidcSecret": "TOPSECRET"}, nil); w.Code != 400 {
		t.Fatal(w.Code)
	}
	if w := update(1, map[string]any{"oidcSecret": "TOPSECRET"}, nil); w.Code != 200 {
		t.Fatal(w.Code)
	}
	w := request(a, "GET", "/api/admin/settings", nil, cookie, "")
	var saved struct {
		Sources map[string]string `json:"sources"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &saved); err != nil || saved.Sources["oidcSecret"] != "Weboberfläche" {
		t.Fatal("secret source not visible", err)
	}
	if strings.Contains(w.Body.String(), "TOPSECRET") {
		t.Fatal("secret leaked")
	}
	// Re-open the same database and apply settings to an independent configuration.
	b := &App{db: a.db, envCfg: a.envCfg, overrides: map[string]json.RawMessage{}}
	if e := b.loadSettings(); e != nil || b.config().CacheBytes != 64<<20 || b.config().OIDCSecret != "TOPSECRET" {
		t.Fatal(e, b.config())
	}
	if w = update(2, map[string]any{}, []string{"cacheMB", "oidcSecret"}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if a.config().CacheBytes != a.envCfg.CacheBytes || a.config().OIDCSecret != "" {
		t.Fatal("reset failed")
	}
	a.acquireEncoder()
	a.acquireEncoder()
	if w = update(3, map[string]any{"maxTranscoders": 1}, nil); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if a.acquireEncoder() {
		t.Fatal("limit reduction ignored")
	}
	a.releaseEncoder()
	a.releaseEncoder()
	if !a.acquireEncoder() {
		t.Fatal("slot not released")
	}
	a.releaseEncoder()
}
func TestLegacyStoreMigration(t *testing.T) {
	dir := t.TempDir()
	db, e := sql.Open("sqlite", filepath.Join(dir, "viewer.db"))
	if e != nil {
		t.Fatal(e)
	}
	_, e = db.Exec(`CREATE TABLE cameras(id TEXT PRIMARY KEY,name TEXT NOT NULL,folder TEXT NOT NULL,pattern TEXT NOT NULL,timezone TEXT NOT NULL,offset INTEGER NOT NULL DEFAULT 0,format TEXT NOT NULL DEFAULT 'custom',prefix TEXT NOT NULL DEFAULT '',indexing_mode TEXT NOT NULL DEFAULT 'partitioned',directory_pattern TEXT NOT NULL DEFAULT '{YYYY}/{MM}/{DD}',view_mode TEXT NOT NULL DEFAULT 'single',wide_marker TEXT NOT NULL DEFAULT '_00_',zoom_marker TEXT NOT NULL DEFAULT '_01_',zoom_offset REAL NOT NULL DEFAULT 0,supports_zoom INTEGER NOT NULL DEFAULT 1);CREATE TABLE admins(username TEXT PRIMARY KEY,hash TEXT NOT NULL);INSERT INTO cameras VALUES('old','Old','old','pattern','UTC',0,'custom','','partitioned','{YYYY}/{MM}/{DD}','single','_00_','_01_',0,1);INSERT INTO admins VALUES('old','original-hash');`)
	db.Close()
	if e != nil {
		t.Fatal(e)
	}
	s, e := OpenStore(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	c, e := s.Camera("old")
	if e != nil || c.Format != "custom" {
		t.Fatal(c, e)
	}
	var hash, role string
	s.QueryRow(`SELECT hash,role FROM admins WHERE username='old'`).Scan(&hash, &role)
	if hash != "original-hash" || role != "admin" {
		t.Fatal(hash, role)
	}
}
func TestSettingsDoNotMutateEnvironmentLists(t *testing.T) {
	env := Config{LocalAdmin: true, AllowedGroups: []string{"original", "second"}}
	c, e := configWithValues(env, map[string]json.RawMessage{"allowedGroups": json.RawMessage(`["changed"]`)})
	if e != nil || c.AllowedGroups[0] != "changed" || env.AllowedGroups[0] != "original" {
		t.Fatal(c, env, e)
	}
}
func TestCLIRecovery(t *testing.T) {
	a := testApp(t)
	payload, err := a.db.sealSettings(`{"localLogin":false,"oidcIssuer":"https://unreachable.example","cacheMB":64}`)
	if err != nil {
		t.Fatal(err)
	}
	a.db.Exec("INSERT INTO settings VALUES(1,5,?)", payload)
	a.db.Exec(`INSERT INTO sessions VALUES('old','old','old','admin','oidc','csrf',9999999999)`)
	if e := a.db.CreateAdminRecovery("rescue", "long rescue password", true); e != nil {
		t.Fatal(e)
	}
	b := &App{db: a.db, envCfg: a.envCfg, overrides: map[string]json.RawMessage{}}
	if e := b.loadSettings(); e != nil || !b.config().LocalAdmin || b.config().OIDCIssuer != "" || b.config().CacheBytes != 64<<20 || b.settingsVersion != 6 {
		t.Fatal(e, b.config())
	}
	var n int
	a.db.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&n)
	if n != 0 {
		t.Fatal("recovery did not revoke sessions")
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
