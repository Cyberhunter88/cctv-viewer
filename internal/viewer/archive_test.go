package viewer

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPersonalBookmarks(t *testing.T) {
	a := testApp(t)
	cookie, s := login(t, a)
	c := Camera{ID: "a", Name: "A", Folder: "A", Pattern: "{YYYY}/{MM}/{DD}/{hh}-{mm}-{ss}.mp4", Timezone: "UTC"}
	if e := a.db.SaveCamera(c); e != nil {
		t.Fatal(e)
	}
	if w := request(a, "GET", "/api/bookmarks", nil, nil, ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	input := map[string]any{"time": 1791959400, "cameras": []string{"a"}, "note": "Paket <script>"}
	w := request(a, "POST", "/api/bookmarks", input, cookie, s.CSRF)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var b Bookmark
	json.Unmarshal(w.Body.Bytes(), &b)
	other := httptest.NewRecorder()
	a.newSession(other, Session{Subject: "someone", Name: "Other", Role: "viewer", Kind: "local"})
	otherCookie := other.Result().Cookies()[0]
	// Read the second session through its public endpoint to obtain its CSRF token.
	var values struct {
		CSRF string `json:"csrf"`
	}
	json.Unmarshal(request(a, "GET", "/api/session", nil, otherCookie, "").Body.Bytes(), &values)
	if w = request(a, "GET", "/api/bookmarks", nil, otherCookie, ""); w.Body.String() != "[]\n" {
		t.Fatal(w.Body.String())
	}
	for _, method := range []string{"PUT", "DELETE"} {
		if w = request(a, method, "/api/bookmarks/"+b.ID, input, otherCookie, values.CSRF); w.Code != 404 {
			t.Fatal(method, w.Code, w.Body.String())
		}
	}
	if w = request(a, "PUT", "/api/bookmarks/"+b.ID, input, cookie, ""); w.Code != 403 {
		t.Fatal(w.Code)
	}
	input["note"] = "Bearbeitet"
	if w = request(a, "PUT", "/api/bookmarks/"+b.ID, input, cookie, s.CSRF); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	// Reindexing configuration does not delete personal references.
	c.Offset = 1
	a.db.SaveCamera(c)
	if w = request(a, "GET", "/api/bookmarks", nil, cookie, ""); w.Code != 200 || !strings.Contains(w.Body.String(), "Bearbeitet") {
		t.Fatal(w.Body.String())
	}
	if w = request(a, "DELETE", "/api/bookmarks/"+b.ID, nil, cookie, s.CSRF); w.Code != 200 {
		t.Fatal(w.Code)
	}
	input["cameras"] = []string{"gone"}
	if w = request(a, "POST", "/api/bookmarks", input, cookie, s.CSRF); w.Code != 400 {
		t.Fatal(w.Code)
	}
	if bookmarkOwner(Session{Kind: "local", Subject: "same"}) == bookmarkOwner(Session{Kind: "oidc", Subject: "same"}) || bookmarkOwner(Session{Kind: "oidc", Subject: "issuer1\x00same"}) == bookmarkOwner(Session{Kind: "oidc", Subject: "issuer2\x00same"}) {
		t.Fatal("identity collision")
	}
}
func TestCalendarAndOfflineStatus(t *testing.T) {
	a := testApp(t)
	cookie, _ := login(t, a)
	c := Camera{ID: "a", Name: "A", Folder: "A", Pattern: "{YYYY}/{MM}/{DD}/{hh}-{mm}-{ss}.mp4", Timezone: "Europe/Berlin"}
	a.db.SaveCamera(c)
	file := filepath.Join(a.config().RecordingsDir, "A/2026/10/14/08-30-00.jpg")
	writeJPEG(t, file)
	a.scanDay("2026-10-14")
	a.scanDay("2026-10-15")
	var data struct {
		Days []CalendarDay `json:"days"`
	}
	w := request(a, "GET", "/api/calendar?month=2026-10&cameras=a", nil, cookie, "")
	if w.Code != 200 {
		t.Fatal(w.Body.String())
	}
	json.Unmarshal(w.Body.Bytes(), &data)
	if len(data.Days) != 31 || data.Days[13].State != "recorded" || !data.Days[13].HasRecordings || data.Days[14].State != "empty" || data.Days[29].State != "unknown" {
		t.Fatal(data.Days)
	}
	// Wait for the single bounded calendar scan before taking the source offline.
	for a.scanStatus().Running {
		time.Sleep(time.Millisecond)
	}
	if e := os.Rename(filepath.Join(a.config().RecordingsDir, "A"), filepath.Join(a.config().RecordingsDir, "offline")); e != nil {
		t.Fatal(e)
	}
	a.scanDay("2026-10-14")
	var complete bool
	a.db.QueryRow(`SELECT complete FROM camera_days WHERE camera_id='a' AND date='2026-10-14'`).Scan(&complete)
	if complete {
		t.Fatal("offline day marked complete")
	}
	var count int
	a.db.QueryRow(`SELECT COUNT(*) FROM recordings WHERE camera_id='a'`).Scan(&count)
	if count != 1 {
		t.Fatal("offline index removed", count)
	}
	var status []struct {
		Reachable   bool  `json:"reachable"`
		LastSuccess int64 `json:"lastSuccess"`
		Stale       bool  `json:"stale"`
	}
	w = request(a, "GET", "/api/camera-status", nil, cookie, "")
	json.Unmarshal(w.Body.Bytes(), &status)
	if len(status) != 1 || status[0].Reachable || status[0].LastSuccess == 0 || !status[0].Stale {
		t.Fatal(w.Body.String())
	}
	for _, url := range []string{"/api/calendar?month=2026-13", "/api/calendar?month=no", "/api/recorded-day?date=2026-10-14&direction=bad"} {
		if w = request(a, "GET", url, nil, cookie, ""); w.Code != 400 {
			t.Fatal(url, w.Code)
		}
	}
}
func TestCalendarDSTAndMidnightNavigation(t *testing.T) {
	a := testApp(t)
	cookie, _ := login(t, a)
	c := Camera{ID: "a", Name: "A", Folder: "A", Pattern: "{YYYY}/{MM}/{DD}/{hh}-{mm}-{ss}.mp4", Timezone: "Europe/Berlin"}
	a.db.SaveCamera(c)
	from, to, _ := dayBounds("2026-10-25")
	if to.Sub(from) != 25*time.Hour {
		t.Fatal(to.Sub(from))
	}
	spring, end, _ := dayBounds("2026-03-29")
	if end.Sub(spring) != 23*time.Hour {
		t.Fatal(end.Sub(spring))
	}
	// A long recording starts before midnight and covers both adjacent days.
	_, e := a.db.Exec(`INSERT INTO recordings VALUES('r','a','A/r.mp4',?,7200,'h264',1,1)`, from.Unix()-3600)
	if e != nil {
		t.Fatal(e)
	}
	w := request(a, "GET", "/api/recorded-day?date=2026-10-24&direction=next&cameras=a", nil, cookie, "")
	if !strings.Contains(w.Body.String(), "2026-10-25") {
		t.Fatal(w.Body.String())
	}
	w = request(a, "GET", "/api/recorded-day?date=2026-10-25&direction=previous&cameras=a", nil, cookie, "")
	if !strings.Contains(w.Body.String(), "2026-10-24") {
		t.Fatal(w.Body.String())
	}
	w = request(a, "GET", "/api/recorded-day?date=2026-10-24&direction=next&cameras=missing", nil, cookie, "")
	if !strings.Contains(w.Body.String(), `"date":""`) {
		t.Fatal(w.Body.String())
	}
}
func TestArchiveMigrationPersists(t *testing.T) {
	dir := t.TempDir()
	s, e := OpenStore(dir)
	if e != nil {
		t.Fatal(e)
	}
	c := Camera{ID: "a", Name: "A", Folder: "A", Timezone: "UTC", Pattern: "{YYYY}/{MM}/{DD}/{hh}-{mm}-{ss}.mp4"}
	s.SaveCamera(c)
	s.Exec(`INSERT INTO bookmarks VALUES('b','owner',1,'["a"]','note')`)
	s.Close()
	s, e = OpenStore(dir)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	var n int
	if e = s.QueryRow(`SELECT COUNT(*) FROM bookmarks`).Scan(&n); e != nil || n != 1 {
		t.Fatal(n, e)
	}
	if _, e = s.Camera("a"); e != nil {
		t.Fatal(e)
	}
}
