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
