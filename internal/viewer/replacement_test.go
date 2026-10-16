package viewer

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func TestReplacementTimestampAndFractionalOffset(t *testing.T) {
	c := Camera{Format: "automatic", Timezone: "Europe/Berlin", Offset: -5.1, DirectoryPattern: "{YYYY}/{MM}/{DD}"}
	for _, name := range []string{"Camera_00_20261014083005.mp4", "20261014_083005.mkv", "camera_2026-10-14_08-30-05.DAV"} {
		v, e := cameraTimestamp(c, name)
		if e != nil || math.Abs(float64(v.UnixNano())/1e9-1791959399.9) > .00001 {
			t.Fatal(name, v, e)
		}
	}
	for _, name := range []string{"20261025023000.mp4", "20260329023000.mp4"} {
		if _, e := cameraTimestamp(c, name); e == nil {
			t.Fatal("DST ambiguity accepted", name)
		}
	}
	for _, name := range []string{"x.JPG", "x.JPEG", "x.png"} {
		if supportedMedia(name) {
			t.Fatal("image indexed", name)
		}
	}
	if !supportedMedia("x.DAV") {
		t.Fatal("DAV missing")
	}
	for _, pattern := range []string{"../{YYYY}/{MM}/{DD}", "{YYYY}/{MM}/{XX}"} {
		if validateDirectoryPattern(pattern) == nil {
			t.Fatal(pattern)
		}
	}
}
func TestMetadataFallbackAndFullDirectory(t *testing.T) {
	a := testApp(t)
	c := Camera{ID: "auto", Name: "Automatic", Folder: "auto", Format: "automatic", IndexingMode: "full", Timezone: "UTC"}
	if e := a.db.SaveCamera(c); e != nil {
		t.Fatal(e)
	}
	file := filepath.Join(a.cfg.RecordingsDir, "auto/nested/metadata.mp4")
	testVideo(t, file, false)
	tagged := filepath.Join(filepath.Dir(file), "tagged.mp4")
	out, e := exec.Command("ffmpeg", "-nostdin", "-v", "error", "-i", file, "-c", "copy", "-metadata", "creation_time=2026-10-14T08:30:00Z", "-y", tagged).CombinedOutput()
	if e != nil {
		t.Fatal(e, string(out))
	}
	mtime := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	os.Chtimes(file, mtime, mtime)
	os.Chtimes(tagged, mtime, mtime)
	a.scanDay("2026-10-14")
	rs, e := a.db.Recordings(c.ID, 0, 2e10)
	if e != nil || len(rs) != 2 {
		t.Fatal(rs, e)
	}
	for _, r := range rs {
		if strings.HasSuffix(r.Path, "metadata.mp4") && r.Start != float64(mtime.Unix()) {
			t.Fatal("mtime fallback", r)
		}
		if strings.HasSuffix(r.Path, "tagged.mp4") && r.Start != 1791966600 {
			t.Fatal("metadata fallback", r)
		}
	}
	old := rs[0]
	c.Offset = .3
	if e = a.db.SaveCamera(c); e != nil {
		t.Fatal(e)
	}
	r, e := a.db.Recording(old.ID)
	if e != nil || math.Abs(r.Start-old.Start-.3) > .00001 || r.Mod != old.Mod {
		t.Fatal("offset must update without probing", r, e)
	}
}
func TestReplacementMigrationBackupAndDormantData(t *testing.T) {
	dir := t.TempDir()
	db, e := sql.Open("sqlite", filepath.Join(dir, "viewer.db"))
	if e != nil {
		t.Fatal(e)
	}
	_, e = db.Exec(`PRAGMA journal_mode=WAL; CREATE TABLE cameras(id TEXT PRIMARY KEY,name TEXT NOT NULL,folder TEXT NOT NULL,pattern TEXT NOT NULL,timezone TEXT NOT NULL,offset INTEGER NOT NULL DEFAULT 0,format TEXT NOT NULL DEFAULT 'custom',prefix TEXT NOT NULL DEFAULT '',indexing_mode TEXT NOT NULL DEFAULT 'partitioned',directory_pattern TEXT NOT NULL DEFAULT '{YYYY}/{MM}/{DD}',view_mode TEXT NOT NULL DEFAULT 'single',wide_marker TEXT NOT NULL DEFAULT '_00_',zoom_marker TEXT NOT NULL DEFAULT '_01_',zoom_offset REAL NOT NULL DEFAULT 0,supports_zoom INTEGER NOT NULL DEFAULT 1);CREATE TABLE admins(username TEXT PRIMARY KEY,hash TEXT NOT NULL);CREATE TABLE bookmarks(id TEXT PRIMARY KEY,owner TEXT NOT NULL,time REAL NOT NULL,cameras TEXT NOT NULL,note TEXT NOT NULL);INSERT INTO cameras VALUES('old','Old','old','{YYYY}/{MM}/{DD}/{hh}-{mm}-{ss}.mp4','UTC',0,'custom','','partitioned','{YYYY}/{MM}/{DD}','single','_00_','_01_',0,1);INSERT INTO admins VALUES('old','original-hash');INSERT INTO bookmarks VALUES('b','owner',1,'[]','keep');PRAGMA user_version=3;`)
	if e != nil {
		t.Fatal(e)
	}
	// Keep the source connection open so backup is tested with live WAL contents.
	s, e := OpenStore(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	defer db.Close()
	backup, e := sql.Open("sqlite", filepath.Join(dir, "viewer-before-v4.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer backup.Close()
	var note, hash string
	var version int
	if e = backup.QueryRow(`SELECT note FROM bookmarks WHERE id='b'`).Scan(&note); e != nil || note != "keep" {
		t.Fatal(note, e)
	}
	backup.QueryRow(`PRAGMA user_version`).Scan(&version)
	if version != 3 {
		t.Fatal(version)
	}
	s.QueryRow(`SELECT hash FROM admins WHERE username='old'`).Scan(&hash)
	if hash != "original-hash" {
		t.Fatal(hash)
	}
	s.QueryRow(`SELECT note FROM bookmarks WHERE id='b'`).Scan(&note)
	if note != "keep" {
		t.Fatal(note)
	}
	st, _ := os.Stat(filepath.Join(dir, "viewer-before-v4.db"))
	if st.Mode().Perm() != 0600 {
		t.Fatal(st.Mode())
	}
}
func TestSearchRebuildAndRemovedEndpoints(t *testing.T) {
	a := testApp(t)
	c := fixture(t, a, "libx264")
	a.scanDay("2026-10-14")
	cookie, session := login(t, a)
	for _, route := range []string{"/api/bookmarks", "/api/exports", "/api/calendar", "/api/recorded-day", "/api/camera-status"} {
		for _, method := range []string{"GET", "POST", "DELETE"} {
			if w := request(a, method, route, map[string]any{}, cookie, session.CSRF); w.Code != 404 {
				t.Fatal(route, method, w.Code)
			}
		}
	}
	if w := request(a, "GET", "/api/search", nil, nil, ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	type searchPage struct {
		Results []map[string]any `json:"results"`
		Total   int              `json:"total"`
		Offset  int              `json:"offset"`
		Limit   int              `json:"limit"`
		HasMore bool             `json:"hasMore"`
	}
	w := request(a, "GET", "/api/search?q=A&camera="+c.ID+"&minDuration=2", nil, cookie, "")
	var page searchPage
	if e := json.Unmarshal(w.Body.Bytes(), &page); e != nil || len(page.Results) != 1 || page.Total != 1 || page.HasMore {
		t.Fatal(w.Code, w.Body.String(), e)
	}
	if w = request(a, "GET", "/api/search?minDuration=NaN", nil, cookie, ""); w.Code != 400 {
		t.Fatal(w.Code)
	}
	for i := 0; i < 205; i++ {
		_, e := a.db.Exec(`INSERT INTO recordings(id,camera_id,path,start,duration,codec,size,mod) VALUES(?,?,?,?,2,'h264',1,1)`, fmt.Sprintf("page-%03d", i), c.ID, fmt.Sprintf("Testkamera-A/search-%03d.mp4", i), float64(i))
		if e != nil {
			t.Fatal(e)
		}
	}
	for _, tc := range []struct {
		offset, wantCount int
		wantMore          bool
	}{{0, 100, true}, {100, 100, true}, {200, 6, false}} {
		w = request(a, "GET", fmt.Sprintf("/api/search?q=Testkamera-A&offset=%d", tc.offset), nil, cookie, "")
		page = searchPage{}
		if e := json.Unmarshal(w.Body.Bytes(), &page); e != nil || w.Code != 200 || len(page.Results) != tc.wantCount || page.Total != 206 || page.Offset != tc.offset || page.Limit != 100 || page.HasMore != tc.wantMore {
			t.Fatalf("search page at %d: status=%d body=%s err=%v", tc.offset, w.Code, w.Body.String(), e)
		}
	}
	if w = request(a, "GET", "/api/search?offset=-1", nil, cookie, ""); w.Code != 400 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w = request(a, "POST", "/api/index/rebuild", nil, cookie, ""); w.Code != 403 {
		t.Fatal(w.Code)
	}
	a.index.mu.Lock()
	a.index.status.Running = true
	a.index.mu.Unlock()
	if w = request(a, "POST", "/api/index/rebuild", nil, cookie, session.CSRF); w.Code != 409 {
		t.Fatal(w.Code)
	}
	a.index.mu.Lock()
	a.index.status.Running = false
	a.index.mu.Unlock()
	a.db.Exec(`CREATE TABLE bookmarks(id TEXT PRIMARY KEY,owner TEXT,time REAL,cameras TEXT,note TEXT);INSERT INTO bookmarks VALUES('old','owner',1,'[]','keep')`)
	if w = request(a, "POST", "/api/index/rebuild", nil, cookie, session.CSRF); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var n int
	a.db.QueryRow(`SELECT COUNT(*) FROM recordings`).Scan(&n)
	if n != 0 {
		t.Fatal(n)
	}
	a.db.QueryRow(`SELECT COUNT(*) FROM bookmarks`).Scan(&n)
	if n != 1 {
		t.Fatal("retired data deleted")
	}
	if _, e := a.db.Camera(c.ID); e != nil {
		t.Fatal(e)
	}
}
func TestIncrementalScanAndReconciliation(t *testing.T) {
	a := testApp(t)
	c := fixture(t, a, "libx264")
	a.scanDay("2026-10-14")
	rs, _ := a.db.Recordings(c.ID, 0, 2e10)
	r := rs[0]
	old := time.Now().Add(-3 * time.Minute)
	file := filepath.Join(a.cfg.RecordingsDir, r.Path)
	os.Chtimes(file, old, old)
	if !a.scanPartitionMode(c, c.Folder, 0, math.MaxInt64, true) {
		t.Fatal("first incremental failed")
	}
	if !a.scanPartitionMode(c, c.Folder, 0, math.MaxInt64, true) {
		t.Fatal("settling failed")
	}
	var settled int
	a.db.QueryRow(`SELECT settled FROM recordings WHERE id=?`, r.ID).Scan(&settled)
	if settled != 1 {
		t.Fatal(settled)
	}
	os.Remove(file)
	if !a.scanPartitionMode(c, c.Folder, 0, math.MaxInt64, true) {
		t.Fatal("incremental failed")
	}
	if _, e := a.db.Recording(r.ID); e != nil {
		t.Fatal("incremental reconciled deletion")
	}
	a.scanPartition(c, c.Folder, 0, math.MaxInt64)
	if _, e := a.db.Recording(r.ID); e == nil {
		t.Fatal("interactive scan retained deletion")
	}
}
func TestProfilesSpeedTransportAndSessionOwnership(t *testing.T) {
	a := testApp(t)
	c := fixture(t, a, "libx264")
	a.scanDay("2026-10-14")
	rs, _ := a.db.Recordings(c.ID, 0, 2e10)
	cookie, session := login(t, a)
	for _, transport := range []string{"mp4", "hls"} {
		for _, speed := range []float64{1, 4, 8, 16} {
			w := request(a, "POST", "/api/streams", map[string]any{"recording": rs[0].ID, "offset": .1, "profile": "fast", "transport": transport, "speed": speed}, cookie, session.CSRF)
			if w.Code != 201 {
				t.Fatal(transport, speed, w.Code, w.Body.String())
			}
			var stream struct {
				ID, URL string
				Factor  float64
			}
			json.Unmarshal(w.Body.Bytes(), &stream)
			if stream.Factor != speed {
				t.Fatal(stream)
			}
			w = request(a, "GET", stream.URL, nil, cookie, "")
			if w.Code != 200 || w.Body.Len() == 0 {
				t.Fatal(w.Code, w.Body.Len())
			}
			w = request(a, "GET", "/api/playback-sessions/"+stream.ID, nil, cookie, "")
			if w.Code != 200 || strings.Contains(w.Body.String(), a.cfg.DataDir) {
				t.Fatal(w.Body.String())
			}
			request(a, "DELETE", "/api/streams/"+stream.ID, nil, cookie, session.CSRF)
		}
	}
	w := request(a, "POST", "/api/streams", map[string]any{"recording": rs[0].ID, "offset": 0, "profile": "bad"}, cookie, session.CSRF)
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
	a.cfgMu.Lock()
	a.cfg.MaxTranscoders = 0
	a.cfgMu.Unlock()
	for i := 0; i < 100; i++ {
		if !a.acquireEncoder() {
			t.Fatal("unlimited capped at", i)
		}
	}
	for i := 0; i < 100; i++ {
		a.releaseEncoder()
	}
}
func TestLargeSyntheticPartition(t *testing.T) {
	a := testApp(t)
	c := Camera{ID: "large", Name: "Large", Folder: "large", Timezone: "UTC", Format: "automatic", IndexingMode: "full"}
	a.db.SaveCamera(c)
	folder := filepath.Join(a.cfg.RecordingsDir, c.Folder)
	os.MkdirAll(folder, 0700)
	for i := 0; i < 10000; i++ {
		if e := os.WriteFile(filepath.Join(folder, fmt.Sprintf("camera-%05d.mp4", i)), nil, 0600); e != nil {
			t.Fatal(e)
		}
	}
	if a.scanPartition(c, c.Folder, 0, math.MaxInt64) || a.scanStatus().Skipped != 10000 {
		t.Fatal("empty videos must be reported as skipped", a.scanStatus())
	}
}
func TestSchedulePersistsAndIncludesRecursiveCameras(t *testing.T) {
	a := testApp(t)
	a.cfgMu.Lock()
	a.cfg.AutoScan = true
	a.cfg.AutoScanMinutes = 1
	a.cfgMu.Unlock()
	for _, kind := range []string{"full", "partitioned"} {
		a.db.SaveCamera(Camera{ID: kind, Name: kind, Folder: kind, Format: "automatic", IndexingMode: kind, Timezone: "UTC"})
		os.Mkdir(filepath.Join(a.cfg.RecordingsDir, kind), 0700)
	}
	a.runScheduledScan()
	waitIndex(t, a)
	for _, id := range []string{"full", "partitioned"} {
		var next int64
		if e := a.db.QueryRow(`SELECT next_run FROM scan_schedule WHERE camera_id=?`, id).Scan(&next); e != nil || next <= time.Now().Unix() {
			t.Fatal(id, next, e)
		}
	}
}

func TestResourceBudgetAndAdmission(t *testing.T) {
	a := testApp(t)
	a.cfgMu.Lock()
	a.cfg.HLSTempBytes = 1
	a.cfg.MaxTranscoders = 1
	a.cfgMu.Unlock()
	if !a.acquireEncoder() || a.acquireEncoder() {
		t.Fatal("encoder limit not shared")
	}
	a.releaseEncoder()
	dir := filepath.Join(a.cfg.DataDir, "cache/hls-budget")
	os.Mkdir(dir, 0700)
	os.WriteFile(filepath.Join(dir, "segment-000000.ts"), []byte("bytes"), 0600)
	done := make(chan struct{})
	close(done)
	a.mediaMu.Lock()
	a.streams["budget"] = &stream{id: "budget", dir: dir, done: done, cancel: func() {}, created: time.Now(), last: time.Now()}
	a.mediaMu.Unlock()
	a.enforceStreamBudget()
	a.mediaMu.Lock()
	remaining := len(a.streams)
	a.mediaMu.Unlock()
	if remaining != 0 {
		t.Fatal("budget did not evict session")
	}
	if _, e := os.Stat(dir); !os.IsNotExist(e) {
		t.Fatal("temporary files retained", e)
	}
}

func TestScheduleSettingsReloadPreservesFutureRun(t *testing.T) {
	a := testApp(t)
	a.db.SaveCamera(Camera{ID: "scheduled", Name: "Scheduled", Folder: "scheduled", Format: "automatic", IndexingMode: "partitioned", Timezone: "UTC"})
	next := time.Now().Add(time.Hour).Unix()
	a.db.Exec(`INSERT INTO scan_schedule VALUES('scheduled',?)`, next)
	a.db.Exec(`INSERT INTO settings VALUES(1,1,'{"autoScan":true,"autoScanMinutes":60}')`)
	b, e := New(context.Background(), a.config(), fstest.MapFS{"index.html": {Data: []byte("test")}})
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	b.runScheduledScan()
	if b.scanStatus().Running {
		t.Fatal("restart ignored future schedule")
	}
	var actual int64
	b.db.QueryRow(`SELECT next_run FROM scan_schedule WHERE camera_id='scheduled'`).Scan(&actual)
	if actual != next {
		t.Fatal(actual, next)
	}
}

func TestIncrementalDayUsesCameraTimezone(t *testing.T) {
	a := testApp(t)
	c := Camera{ID: "utc-day", Name: "UTC", Folder: "utc-day", Format: "automatic", IndexingMode: "partitioned", DirectoryPattern: "{YYYY}/{MM}/{DD}", Timezone: "UTC"}
	a.db.SaveCamera(c)
	now := time.Now().UTC()
	file := filepath.Join(a.cfg.RecordingsDir, c.Folder, now.Format("2006/01/02"), now.Format("20060102")+"_233000.mp4")
	testVideo(t, file, false)
	a.scanJob(scanJob{date: now.Format("2006-01-02"), camera: c.ID, incremental: true})
	rs, e := a.db.Recordings(c.ID, 0, 2e10)
	if e != nil || len(rs) != 1 {
		t.Fatal("camera-local day was truncated to Berlin bounds", rs, e)
	}
}
