package viewer

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func insertTestRecording(t *testing.T, a *App, id string, start float64) {
	t.Helper()
	if _, err := a.db.Exec(`INSERT INTO recordings(id,camera_id,path,start,duration,codec,size,mod,filename) VALUES(?,'camera',?,?,30,'h264',1,1,?)`, id, "camera/"+id+".mp4", start, id+".mp4"); err != nil {
		t.Fatal(err)
	}
}

func cameraList(t *testing.T, a *App, cookie *http.Cookie) []Camera {
	t.Helper()
	w := request(a, "GET", "/api/cameras", nil, cookie, "")
	var cameras []Camera
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &cameras) != nil || len(cameras) == 0 {
		t.Fatal(w.Code, w.Body.String())
	}
	return cameras
}

// Counting the index is reused until recordings change, and changes made
// through the application invalidate it.
func TestCameraStatsAreCachedUntilRecordingsChange(t *testing.T) {
	a := testApp(t)
	if err := a.db.SaveCamera(Camera{ID: "camera", Name: "Camera", Folder: "camera", Timezone: "UTC"}); err != nil {
		t.Fatal(err)
	}
	cookie, _ := login(t, a)
	insertTestRecording(t, a, "first", 1000)
	a.db.RecordingsChanged()
	if got := cameraList(t, a, cookie)[0]; got.IndexedVideos != 1 || got.LastRecording != 1030 {
		t.Fatal("initial statistics", got.IndexedVideos, got.LastRecording)
	}
	// A write that bypasses the application stays unseen while the cache is valid.
	insertTestRecording(t, a, "second", 2000)
	if got := cameraList(t, a, cookie)[0]; got.IndexedVideos != 1 {
		t.Fatal("statistics not cached", got.IndexedVideos)
	}
	a.db.RecordingsChanged()
	if got := cameraList(t, a, cookie)[0]; got.IndexedVideos != 2 || got.LastRecording != 2030 {
		t.Fatal("statistics not refreshed after a change", got.IndexedVideos, got.LastRecording)
	}
	// Saving a camera with a new offset shifts its recordings.
	camera, err := a.db.Camera("camera")
	if err != nil {
		t.Fatal(err)
	}
	camera.Offset = 10
	if err := a.db.SaveCamera(camera); err != nil {
		t.Fatal(err)
	}
	if got := cameraList(t, a, cookie)[0]; got.LastRecording != 2040 {
		t.Fatal("camera save did not refresh statistics", got.LastRecording)
	}
}

func TestCameraStatsExpire(t *testing.T) {
	a := testApp(t)
	if err := a.db.SaveCamera(Camera{ID: "camera", Name: "Camera", Folder: "camera", Timezone: "UTC"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.CameraRecordingStats(context.Background(), time.Hour); err != nil {
		t.Fatal(err)
	}
	insertTestRecording(t, a, "late", 1000)
	stats, err := a.db.CameraRecordingStats(context.Background(), time.Hour)
	if err != nil || stats["camera"].Videos != 0 {
		t.Fatal("cache not reused", stats, err)
	}
	stats, err = a.db.CameraRecordingStats(context.Background(), 0)
	if err != nil || stats["camera"].Videos != 1 {
		t.Fatal("expired cache reused", stats, err)
	}
}

// A finished camera scan and a deleted camera are visible at once.
func TestCameraStatsFollowScansAndDeletion(t *testing.T) {
	a := testApp(t)
	c := Camera{ID: "camera", Name: "Camera", Folder: "camera", Format: "automatic", IndexingMode: "partitioned", DirectoryPattern: "{YYYY}/{MM}/{DD}", Timezone: "UTC"}
	if err := a.db.SaveCamera(c); err != nil {
		t.Fatal(err)
	}
	cookie, session := login(t, a)
	if got := cameraList(t, a, cookie)[0]; got.IndexedVideos != 0 {
		t.Fatal("unexpected videos", got.IndexedVideos)
	}
	insertTestRecording(t, a, "scanned", 1000)
	a.scanCamera(scanJob{date: "2026-03-01", camera: c.ID}, c, time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 3, 2, 0, 0, 0, 0, time.UTC))
	if got := cameraList(t, a, cookie)[0]; got.IndexedVideos != 1 {
		t.Fatal("scan did not refresh statistics", got.IndexedVideos)
	}
	if w := request(a, "DELETE", "/api/cameras/camera", nil, cookie, session.CSRF); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	stats, err := a.db.CameraRecordingStats(context.Background(), time.Hour)
	if err != nil || len(stats) != 0 {
		t.Fatal("deleted camera still counted", stats, err)
	}
}
