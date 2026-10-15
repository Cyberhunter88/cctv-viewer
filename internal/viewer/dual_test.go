package viewer

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDualViewClassificationAndPersistence(t *testing.T) {
	db, e := OpenStore(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	c := Camera{ID: "dual", Name: "Dual", Folder: "Zoom Kamera", Format: "automatic", Timezone: "Europe/Berlin", ViewMode: "dual", WideMarker: "_00_", ZoomMarker: "_01_", ZoomOffset: -2.5}
	if e = db.SaveCamera(c); e != nil {
		t.Fatal(e)
	}
	saved, e := db.Camera(c.ID)
	if e != nil || saved.ZoomOffset != -2.5 || saved.ViewMode != "dual" {
		t.Fatalf("roundtrip: %+v %v", saved, e)
	}
	for filename, want := range map[string]string{"Zoom Kamera/Eingang_00_20261014010548.mp4": "wide", "Zoom Kamera/Eingang_01_20261014120659.mp4": "zoom", "Zoom Kamera/CAM_00_01_20261014083000.mp4": "unknown", "Zoom Kamera/20261014083000.mp4": "unknown"} {
		if got := recordingView(saved, filename); got != want {
			t.Fatalf("%s: %s", filename, got)
		}
	}
	saved.WideMarker = "wide/"
	saved.ZoomMarker = "zoom/"
	if recordingView(saved, "Zoom Kamera/ZOOM/video.mp4") != "zoom" {
		t.Fatal("folder marker")
	}
	_, e = db.Exec(`INSERT INTO recordings(id,camera_id,path,start,duration,codec,size,mod) VALUES('z','dual','Zoom Kamera/CAM_01_video.mp4',101,2,'h264',1,1)`)
	if e != nil {
		t.Fatal(e)
	}
	rs, e := db.Recordings(c.ID, 98, 100)
	if e != nil || len(rs) != 1 || rs[0].Start != 98.5 || rs[0].View != "zoom" {
		t.Fatalf("shifted day boundary: %+v %v", rs, e)
	}
}

func TestBatchAtomicAndParallelCameraScan(t *testing.T) {
	a := testApp(t)
	tools := t.TempDir()
	script := filepath.Join(tools, "ffprobe")
	if e := os.WriteFile(script, []byte("#!/bin/sh\nsleep 0.4\nprintf '%s' '{\"format\":{\"duration\":\"10\"},\"streams\":[{\"codec_type\":\"video\",\"codec_name\":\"h264\"}]}'\n"), 0700); e != nil {
		t.Fatal(e)
	}
	t.Setenv("PATH", tools+":"+os.Getenv("PATH"))
	cameras := []Camera{}
	for _, name := range []string{"One", "Two", "Three", "Four"} {
		if e := a.root.Mkdir(name, 0700); e != nil {
			t.Fatal(e)
		}
		f, e := a.root.Create(name + "/CAM_00_20261014083000.mp4")
		if e != nil {
			t.Fatal(e)
		}
		f.WriteString("synthetic")
		f.Close()
		old := time.Now().Add(-2 * time.Minute)
		if e := os.Chtimes(filepath.Join(a.cfg.RecordingsDir, name, "CAM_00_20261014083000.mp4"), old, old); e != nil {
			t.Fatal(e)
		}
		cameras = append(cameras, Camera{Name: name, Folder: name, Timezone: "Europe/Berlin", Format: "automatic", IndexingMode: "full"})
	}
	send := func(c []Camera) *httptest.ResponseRecorder {
		payload, _ := json.Marshal(map[string]any{"cameras": c, "date": "2026-10-14"})
		w := httptest.NewRecorder()
		a.saveCameraBatch(w, httptest.NewRequest("POST", "/api/cameras/batch", strings.NewReader(string(payload))))
		return w
	}
	bad := append([]Camera{}, cameras...)
	bad[3].Folder = "missing"
	if w := send(bad); w.Code != 400 {
		t.Fatalf("bad batch: %d %s", w.Code, w.Body.String())
	}
	list, _ := a.db.Cameras()
	if len(list) != 0 {
		t.Fatal("partial batch committed")
	}
	if w := send(cameras); w.Code != 201 {
		t.Fatalf("batch: %d %s", w.Code, w.Body.String())
	}
	maxActive := 0
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		s := a.scanStatus()
		if len(s.Active) > maxActive {
			maxActive = len(s.Active)
		}
		if len(s.Active) > 3 {
			t.Fatal("too many parallel probes")
		}
		if !s.Running {
			if s.Completed != 4 || s.Total != 4 || s.Indexed != 4 {
				t.Fatalf("status: %+v", s)
			}
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if a.scanStatus().Running || maxActive != 3 {
		t.Fatalf("parallel scan: max=%d status=%+v", maxActive, a.scanStatus())
	}
}

// The two perspectives belong to the same camera even with unequal timestamps.
func TestReolinkGardenUnequalStarts(t *testing.T) {
	a := testApp(t)
	c := Camera{ID: "garden", IndexingMode: "full", Name: "Reolink Garten", Folder: "Reolink Garten", Format: "automatic", Timezone: "Europe/Berlin", ViewMode: "dual", WideMarker: "_00_", ZoomMarker: "_01_"}
	if err := a.db.SaveCamera(c); err != nil {
		t.Fatal(err)
	}
	names := []string{"Reolink Garten_00_20261015061313.mp4", "Reolink Garten_01_20261015061312.mp4"}
	for i, name := range names {
		want := []string{"wide", "zoom"}[i]
		full := c.Folder + "/" + name
		if got := recordingView(c, full); got != want {
			t.Fatalf("%s: %s", name, got)
		}
		testVideo(t, filepath.Join(a.cfg.RecordingsDir, full), false)
	}
	a.scanDay("2026-10-15")
	records, err := a.db.Recordings(c.ID, 0, 2e10)
	if err != nil || len(records) != 2 {
		t.Fatalf("%+v %v", records, err)
	}
	starts := map[string]float64{}
	for _, r := range records {
		starts[r.View] = r.Start
	}
	if starts["wide"]-starts["zoom"] != 1 {
		t.Fatalf("timestamps changed: %+v", starts)
	}
	cookie, session := login(t, a)
	dayResponse := request(a, "GET", "/api/day?date=2026-10-15", nil, cookie, session.CSRF)
	var day struct {
		Recordings []Recording `json:"recordings"`
	}
	if dayResponse.Code != 200 {
		t.Fatal(dayResponse.Code, dayResponse.Body.String())
	}
	if err := json.Unmarshal(dayResponse.Body.Bytes(), &day); err != nil {
		t.Fatal(err)
	}
	if len(day.Recordings) != 2 {
		t.Fatal(dayResponse.Body.String())
	}
	for _, r := range day.Recordings {
		if r.Start != starts[r.View] {
			t.Fatal(dayResponse.Body.String())
		}
	}
	c.ZoomOffset = 1
	if err := a.db.SaveCamera(c); err != nil {
		t.Fatal(err)
	}
	records, err = a.db.Recordings(c.ID, 0, 2e10)
	if err != nil || len(records) != 2 || records[0].Start != records[1].Start {
		t.Fatalf("clock correction: %+v %v", records, err)
	}
	w := request(a, "GET", fmt.Sprintf("/api/search?camera=garden&from=%.0f&to=%.0f", starts["wide"], starts["wide"]), nil, cookie, session.CSRF)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var search struct {
		Results []struct {
			Start float64 `json:"start"`
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &search); err != nil {
		t.Fatal(err)
	}
	if len(search.Results) != 2 {
		t.Fatal(w.Body.String())
	}
	for _, r := range search.Results {
		if r.Start != starts["wide"] {
			t.Fatal(w.Body.String())
		}
	}
	w = request(a, "POST", "/api/cameras/preview", c, cookie, session.CSRF)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var preview struct {
		Examples []struct {
			View string `json:"view"`
			Time string `json:"time"`
		}
	}
	if err := json.Unmarshal(w.Body.Bytes(), &preview); err != nil {
		t.Fatal(err)
	}
	if len(preview.Examples) != 2 || preview.Examples[0].Time != preview.Examples[1].Time {
		t.Fatal(w.Body.String())
	}
}

func TestDualViewMidnightIntervals(t *testing.T) {
	a := testApp(t)
	c := Camera{ID: "midnight-pair", Name: "Midnight pair", Folder: "pair", Timezone: "Europe/Berlin", ViewMode: "dual", WideMarker: "_00_", ZoomMarker: "_01_"}
	if err := a.db.SaveCamera(c); err != nil {
		t.Fatal(err)
	}
	boundary, _, err := dayBounds("2026-10-16")
	if err != nil {
		t.Fatal(err)
	}
	for _, clip := range []struct {
		id, path string
		start    float64
	}{{"wide", "pair/CAM_00_20261015235959.mp4", float64(boundary.Unix() - 1)}, {"zoom", "pair/CAM_01_20261016000001.mp4", float64(boundary.Unix() + 1)}} {
		if _, err := a.db.Exec(`INSERT INTO recordings(id,camera_id,path,start,duration,codec,size,mod) VALUES(?,?,?,?,3,'h264',1,1)`, clip.id, c.ID, clip.path, clip.start); err != nil {
			t.Fatal(err)
		}
	}
	for _, date := range []string{"2026-10-15", "2026-10-16"} {
		from, to, err := dayBounds(date)
		if err != nil {
			t.Fatal(err)
		}
		recordings, err := a.db.Recordings(c.ID, float64(from.Unix()), float64(to.Unix()))
		want := 1
		if date == "2026-10-16" {
			want = 2
		}
		if err != nil || len(recordings) != want {
			t.Fatalf("%s: %+v %v", date, recordings, err)
		}
	}
}
