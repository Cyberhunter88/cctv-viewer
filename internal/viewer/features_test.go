package viewer

import (
	"bytes"
	"context"
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
	"testing/fstest"
	"time"
)

func TestReolinkTimestamp(t *testing.T) {
	c := Camera{Format: "reolink", Prefix: "Eingang", Timezone: "Europe/Berlin"}
	for _, name := range []string{"2026/10/14/Eingang_00_20261014061217.mp4", "2026/10/14/Eingang_01_20261014061217.jpg"} {
		got, e := cameraTimestamp(c, name)
		if e != nil || got.Format(time.RFC3339) != "2026-10-14T04:12:17Z" {
			t.Fatal(got, e)
		}
	}
	for _, name := range []string{"2026/10/13/Eingang_00_20261014061217.mp4", "2026/10/25/Eingang_00_20261025023000.mp4", "2026/03/29/Eingang_00_20260329023000.jpg", "2026/10/14/Other_00_20261014061217.mp4"} {
		if _, e := cameraTimestamp(c, name); e == nil {
			t.Fatal("accepted", name)
		}
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
func TestReolinkImagesAndFolderAccess(t *testing.T) {
	a := testApp(t)
	cookie, s := login(t, a)
	c := Camera{ID: "reolink", Name: "Eingang", Folder: "Reolink/Eingang", Format: "reolink", Prefix: "Eingang", Timezone: "Europe/Berlin"}
	file := filepath.Join(a.config().RecordingsDir, c.Folder, "2026/10/14/Eingang_00_20261014061217.jpg")
	writeJPEG(t, file)
	a.db.SaveCamera(c)
	bad := filepath.Join(filepath.Dir(file), "Eingang_00_20261014061317.jpg")
	os.WriteFile(bad, []byte("broken"), 0600)
	past := time.Now().Add(-time.Minute)
	os.Chtimes(bad, past, past)
	a.scanDay("2026-10-14")
	records, e := a.db.Recordings(c.ID, 1791936000, 1792022400)
	if e != nil || len(records) != 1 || records[0].Kind != "image" || records[0].Duration != 0 {
		t.Fatal(records, e)
	}
	if len(a.scanStatus().Warnings) == 0 {
		t.Fatal("corrupt JPG warning missing")
	}
	r := records[0]
	for _, endpoint := range []string{"file", "thumbnail"} {
		w := request(a, "GET", "/api/recordings/"+r.ID+"/"+endpoint, nil, cookie, "")
		if w.Code != 200 || !bytes.HasPrefix(w.Body.Bytes(), []byte{0xff, 0xd8}) {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if w := request(a, "POST", "/api/streams", map[string]any{"recording": r.ID, "offset": 0}, cookie, s.CSRF); w.Code != 400 {
		t.Fatal(w.Code)
	}
	w := request(a, "POST", "/api/cameras/preview", c, cookie, s.CSRF)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "04:12:17Z") || !strings.Contains(w.Body.String(), "beschädigt") {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, p := range []string{"../", "/etc"} {
		if w = request(a, "GET", "/api/cameras/folders?path="+p, nil, cookie, ""); w.Code != 400 {
			t.Fatal(w.Code)
		}
	}
	w = request(a, "GET", "/api/cameras/folders?path=Reolink", nil, cookie, "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Reolink/Eingang") {
		t.Fatal(w.Code, w.Body.String())
	}
	outside := filepath.Join(t.TempDir(), "outside")
	os.Mkdir(outside, 0700)
	os.Symlink(outside, filepath.Join(a.config().RecordingsDir, "escape"))
	if w = request(a, "GET", "/api/cameras/folders?path=escape", nil, cookie, ""); w.Code != 400 {
		t.Fatal("outside mount", w.Code)
	}
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
	a.slots <- struct{}{}
	a.slots <- struct{}{}
	if w = update(3, map[string]any{"maxTranscoders": 1}, nil); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if a.acquireEncoder() {
		t.Fatal("limit reduction ignored")
	}
	<-a.slots
	<-a.slots
	if !a.acquireEncoder() {
		t.Fatal("slot not released")
	}
	<-a.slots
}
func TestLegacyStoreMigration(t *testing.T) {
	dir := t.TempDir()
	db, e := sql.Open("sqlite", filepath.Join(dir, "viewer.db"))
	if e != nil {
		t.Fatal(e)
	}
	_, e = db.Exec(`CREATE TABLE cameras(id TEXT PRIMARY KEY,name TEXT NOT NULL,folder TEXT NOT NULL,pattern TEXT NOT NULL,timezone TEXT NOT NULL,offset INTEGER NOT NULL DEFAULT 0);CREATE TABLE admins(username TEXT PRIMARY KEY,hash TEXT NOT NULL);INSERT INTO cameras VALUES('old','Old','old','pattern','UTC',0);INSERT INTO admins VALUES('old','original-hash');`)
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
func TestExportCoverage(t *testing.T) {
	records := []Recording{{ID: "b", Start: 100, Duration: 20}, {ID: "a", Start: 110, Duration: 5}, {ID: "img", Kind: "image", Start: 120}, {ID: "c", Start: 125, Duration: 5}}
	parts, gaps := exportParts(records, 105, 130)
	if len(parts) != 4 || len(gaps) != 1 || gaps[0] != [2]float64{120, 125} || parts[1].record.ID != "a" || parts[2].record.ID != "b" {
		t.Fatal(parts, gaps)
	}
	_, gaps = exportParts([]Recording{{Kind: "image", Start: 100}}, 100, 101)
	if len(gaps) != 1 {
		t.Fatal("image fills gap")
	}
}
func TestExportEncodingOwnershipAndCleanup(t *testing.T) {
	a := testApp(t)
	c := fixture(t, a, "libx264")
	cookie, s := login(t, a)
	file := filepath.Join(a.config().RecordingsDir, c.Folder, "2026/10/14/08-30-03.mp4")
	out, e := exec.Command("ffmpeg", "-nostdin", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=320x180:rate=15", "-t", "3", "-c:v", "libx265", "-x265-params", "pools=none:frame-threads=1:log-level=error", "-threads", "1", "-y", file).CombinedOutput()
	if e != nil {
		t.Fatal(e, string(out))
	}
	past := time.Now().Add(-time.Minute)
	os.Chtimes(file, past, past)
	a.scanDay("2026-10-14")
	records, _ := a.db.Recordings(c.ID, 0, 2e10)
	from := records[0].Start
	in := map[string]any{"camera": c.ID, "from": from + 1, "to": from + 5}
	w := request(a, "POST", "/api/exports", in, cookie, s.CSRF)
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	var result struct{ ID string }
	json.Unmarshal(w.Body.Bytes(), &result)
	a.exportMu.Lock()
	job := a.exports[result.ID]
	a.exportMu.Unlock()
	select {
	case <-job.done:
	case <-time.After(30 * time.Second):
		t.Fatal("export timeout")
	}
	a.exportMu.Lock()
	state := jobView(job)
	a.exportMu.Unlock()
	if state["status"] != "ready" {
		t.Fatal(state)
	}
	f, e := os.Open(filepath.Join(job.dir, "clip.mp4"))
	if e != nil {
		t.Fatal(e)
	}
	duration, codec, e := probe(context.Background(), f)
	f.Seek(0, 0)
	width, height, de := exportDimensions(context.Background(), f)
	f.Close()
	if e != nil || de != nil || codec != "h264" || duration < 3.9 || duration > 4.2 || width != 160 || height != 90 {
		t.Fatal(duration, codec, width, height, e, de)
	}
	other := httptest.NewRecorder()
	a.newSession(other, Session{Subject: "other", Kind: "local", Role: "viewer"})
	for _, url := range []string{"/api/exports/" + job.ID, "/api/exports/" + job.ID + "/file"} {
		if w = request(a, "GET", url, nil, other.Result().Cookies()[0], ""); w.Code != 404 {
			t.Fatal("cross owner", w.Code)
		}
	}
	if w = request(a, "GET", "/api/exports/"+job.ID+"/file", nil, cookie, ""); w.Code != 200 || !strings.Contains(w.Header().Get("Content-Disposition"), "attachment") {
		t.Fatal(w.Code)
	}
	a.exportMu.Lock()
	job.downloads = 1
	job.created = time.Now().Add(-2 * time.Hour)
	a.exportMu.Unlock()
	a.pruneExports()
	if _, e = os.Stat(job.dir); e != nil {
		t.Fatal("active download pruned")
	}
	a.exportMu.Lock()
	job.downloads = 0
	a.exportMu.Unlock()
	a.pruneExports()
	if _, e = os.Stat(job.dir); !os.IsNotExist(e) {
		t.Fatal("expired export retained")
	}
	if w = request(a, "POST", "/api/exports/preview", map[string]any{"camera": c.ID, "from": from, "to": from + 7}, cookie, s.CSRF); w.Code != 422 || !strings.Contains(w.Body.String(), "gaps") {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestExportRestartCleanup(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "recordings"), 0700)
	os.MkdirAll(filepath.Join(dir, "data/cache/export-old"), 0700)
	a, e := New(context.Background(), Config{DataDir: filepath.Join(dir, "data"), RecordingsDir: filepath.Join(dir, "recordings"), LocalAdmin: true, ScanInterval: time.Hour, SessionTTL: time.Hour, CacheBytes: 32 << 20, MaxTranscoders: 1}, fstest.MapFS{})
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	if _, e = os.Stat(filepath.Join(dir, "data/cache/export-old")); !os.IsNotExist(e) {
		t.Fatal("stale export retained")
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
	a.db.Exec(`INSERT INTO settings VALUES(1,5,'{"localLogin":false,"oidcIssuer":"https://unreachable.example","cacheMB":64}')`)
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
func TestExportCancellationAndLimits(t *testing.T) {
	a := testApp(t)
	c := fixture(t, a, "libx264")
	a.scanDay("2026-10-14")
	records, _ := a.db.Recordings(c.ID, 0, 2e10)
	cookie, s := login(t, a)
	in := map[string]any{"camera": c.ID, "from": records[0].Start, "to": records[0].Start + 2}
	if w := request(a, "POST", "/api/exports", in, cookie, ""); w.Code != 403 {
		t.Fatal("CSRF missing", w.Code)
	}
	a.slots <- struct{}{}
	if w := request(a, "POST", "/api/exports", in, cookie, s.CSRF); w.Code != 429 {
		t.Fatal("shared limit", w.Code)
	}
	<-a.slots
	w := request(a, "POST", "/api/exports", in, cookie, s.CSRF)
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	var job struct{ ID string }
	json.Unmarshal(w.Body.Bytes(), &job)
	if w = request(a, "DELETE", "/api/exports/"+job.ID, nil, cookie, s.CSRF); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = request(a, "GET", "/api/exports/"+job.ID, nil, cookie, ""); w.Code != 404 {
		t.Fatal("cancelled job retained")
	}
	if len(a.slots) != 0 {
		t.Fatal("encoder leaked")
	}
	if _, e := os.Stat(filepath.Join(a.config().DataDir, "cache", "export-"+job.ID)); !os.IsNotExist(e) {
		t.Fatal("cancelled files retained")
	}
	big := filepath.Join(a.config().DataDir, "cache", "budget-full")
	f, _ := os.Create(big)
	f.Truncate(a.config().CacheBytes)
	f.Close()
	if w = request(a, "POST", "/api/exports", in, cookie, s.CSRF); w.Code != 503 {
		t.Fatal("cache limit", w.Code, w.Body.String())
	}
}
func TestExportAcrossMidnight(t *testing.T) {
	records := []Recording{{ID: "before", Start: 1792015198, Duration: 3}, {ID: "after", Start: 1792015201, Duration: 3}}
	parts, gaps := exportParts(records, 1792015199, 1792015203)
	if len(parts) != 2 || len(gaps) != 0 || parts[0].end != parts[1].start {
		t.Fatal(parts, gaps)
	}
}

func TestPairedImagePreferredOverGeneratedThumbnail(t *testing.T) {
	a := testApp(t)
	c := fixture(t, a, "libx264")
	file := filepath.Join(a.config().RecordingsDir, c.Folder, "2026/10/14/08-30-00.jpg")
	writeJPEG(t, file)
	a.scanDay("2026-10-14")
	records, _ := a.db.Recordings(c.ID, 0, 2e10)
	if len(records) != 2 {
		t.Fatal("video pattern must also recognize its companion image", records)
	}
	var video Recording
	for _, r := range records {
		if r.Kind == "video" {
			video = r
		}
	}
	cookie, _ := login(t, a)
	w := request(a, "GET", "/api/recordings/"+video.ID+"/thumbnail", nil, cookie, "")
	expected, _ := os.ReadFile(file)
	if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), expected) {
		t.Fatal("companion image not preferred", w.Code)
	}
}
func TestCacheEvictionLeavesExportFailureVisible(t *testing.T) {
	a := testApp(t)
	cookie, s := login(t, a)
	dir := filepath.Join(a.config().DataDir, "cache", "export-full")
	os.Mkdir(dir, 0700)
	f, _ := os.Create(filepath.Join(dir, "clip.mp4"))
	f.Truncate(a.config().CacheBytes + 1)
	f.Close()
	done := make(chan struct{})
	close(done)
	job := &exportJob{ID: "full", Status: "ready", owner: digest(cookie.Value), dir: dir, created: time.Now(), cancel: func() {}, done: done}
	a.exportMu.Lock()
	a.exports[job.ID] = job
	a.exportMu.Unlock()
	a.pruneCache()
	w := request(a, "GET", "/api/exports/full", nil, cookie, s.CSRF)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "failed") || !strings.Contains(w.Body.String(), "Cachebudget") {
		t.Fatal(w.Code, w.Body.String())
	}
	if _, e := os.Stat(dir); !os.IsNotExist(e) {
		t.Fatal("cache eviction failed")
	}
}
