package viewer

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func viewPictures() (*image.RGBA, *image.RGBA) {
	wide := image.NewRGBA(image.Rect(0, 0, 160, 90))
	for y := 0; y < 90; y++ {
		for x := 0; x < 160; x++ {
			wide.Set(x, y, color.RGBA{uint8((x*3 + y*7 + x*y/43) % 256), uint8((x*11 + y*5 + x*y/37) % 256), uint8((x*2 + y*13) % 256), 255})
		}
	}
	zoom := image.NewRGBA(image.Rect(0, 0, 160, 90))
	for y := 0; y < 90; y++ {
		for x := 0; x < 160; x++ {
			zoom.Set(x, y, wide.At(40+x/2, 22+y/2))
		}
	}
	return wide, zoom
}

func TestViewCropDirection(t *testing.T) {
	wide, zoom := viewPictures()
	forward, backward := cropScore(wide, zoom), cropScore(zoom, wide)
	if forward < .8 || forward-backward < .16 {
		t.Fatalf("crop scores forward %.3f backward %.3f", forward, backward)
	}
	flat := image.NewUniform(color.Gray{Y: 100})
	if cropScore(flat, flat) >= .8 {
		t.Fatal("flat pictures must not look decisive")
	}
}

func videoFromPicture(t *testing.T, file string, picture image.Image) {
	t.Helper()
	imageFile := filepath.Join(t.TempDir(), "frame.png")
	f, err := os.Create(imageFile)
	if err != nil {
		t.Fatal(err)
	}
	if err = png.Encode(f, picture); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("ffmpeg", "-nostdin", "-v", "error", "-loop", "1", "-framerate", "2", "-i", imageFile, "-t", "1", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-threads", "1", "-y", file)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg: %v %s", err, output)
	}
}

func TestAutoViewSaveAndUnclear(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg unavailable")
	}
	a := testApp(t)
	folder := filepath.Join(a.cfg.RecordingsDir, "pair")
	if err := os.Mkdir(folder, 0700); err != nil {
		t.Fatal(err)
	}
	wide, zoom := viewPictures()
	// The camera's submitted markers are intentionally reversed.
	videoFromPicture(t, filepath.Join(folder, "CAM_00_20261014083000.mp4"), wide)
	videoFromPicture(t, filepath.Join(folder, "CAM_01_20261014083001.mp4"), zoom)
	c := Camera{Name: "Pair", Folder: "pair", Format: "automatic", IndexingMode: "full", Timezone: "Europe/Berlin", ViewMode: "dual", WideMarker: "_01_", ZoomMarker: "_00_"}
	body, _ := json.Marshal(c)
	w := httptest.NewRecorder()
	a.saveCamera(w, httptest.NewRequest("POST", "/api/cameras", bytes.NewReader(body)))
	if w.Code != 200 {
		t.Fatalf("save %d: %s", w.Code, w.Body.String())
	}
	var saved Camera
	if err := json.Unmarshal(w.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Detection == nil || saved.Detection.Status != "swapped" || saved.WideMarker != "_00_" || saved.ZoomMarker != "_01_" {
		t.Fatalf("saved: %+v detection: %+v", saved, saved.Detection)
	}
	persisted, err := a.db.Camera(saved.ID)
	if err != nil || persisted.WideMarker != "_00_" {
		t.Fatalf("persisted: %+v %v", persisted, err)
	}
	if recordingView(persisted, "pair/CAM_00_20261014083000.mp4") != "wide" || recordingView(persisted, "pair/CAM_01_20261014083001.mp4") != "zoom" {
		t.Fatal("wrong classification after save")
	}
	if recordingView(persisted, "pair/CAM_00_01_20261014083002.mp4") != "unknown" || recordingView(persisted, "pair/CAM_20261014083002.mp4") != "unknown" {
		t.Fatal("ambiguous paths must stay unknown")
	}
	videoFromPicture(t, filepath.Join(folder, "CAM_20261014083002.mp4"), wide)
	previewBody, _ := json.Marshal(persisted)
	preview := httptest.NewRecorder()
	a.preview(preview, httptest.NewRequest("POST", "/api/cameras/preview", bytes.NewReader(previewBody)))
	var previewResult struct {
		Examples []struct {
			View string `json:"view"`
		} `json:"examples"`
	}
	if preview.Code != 200 || json.Unmarshal(preview.Body.Bytes(), &previewResult) != nil {
		t.Fatalf("preview: %d %s", preview.Code, preview.Body.String())
	}
	views := map[string]bool{}
	for _, example := range previewResult.Examples {
		views[example.View] = true
	}
	if !views["wide"] || !views["zoom"] || !views["unknown"] {
		t.Fatalf("preview omitted a view: %+v", previewResult.Examples)
	}
	confirmed := detectView(context.Background(), &persisted, a.root)
	if confirmed.Status != "confirmed" || persisted.WideMarker != "_00_" {
		t.Fatalf("confirmed: %+v %+v", confirmed, persisted)
	}
	sameFolder := filepath.Join(a.cfg.RecordingsDir, "same")
	if err := os.Mkdir(sameFolder, 0700); err != nil {
		t.Fatal(err)
	}
	videoFromPicture(t, filepath.Join(sameFolder, "CAM_00_20261014083000.mp4"), wide)
	videoFromPicture(t, filepath.Join(sameFolder, "CAM_01_20261014083001.mp4"), wide)
	same := Camera{Folder: "same", Format: "automatic", Timezone: "Europe/Berlin", ViewMode: "dual", WideMarker: "_00_", ZoomMarker: "_01_"}
	unclear := detectView(context.Background(), &same, a.root)
	if unclear.Status != "unclear" || same.WideMarker != "_00_" {
		t.Fatalf("same picture: %+v %+v", unclear, same)
	}
	if err := os.Mkdir(filepath.Join(a.cfg.RecordingsDir, "empty"), 0700); err != nil {
		t.Fatal(err)
	}
	empty := Camera{Folder: "empty", ViewMode: "dual", WideMarker: "_00_", ZoomMarker: "_01_"}
	result := detectView(context.Background(), &empty, a.root)
	if result.Status != "unclear" || empty.WideMarker != "_00_" {
		t.Fatalf("unclear: %+v %+v", result, empty)
	}
}
