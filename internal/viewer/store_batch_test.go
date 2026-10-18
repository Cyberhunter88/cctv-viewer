package viewer

import (
	"reflect"
	"testing"
)

func TestDayRecordingsMatchesPerCameraQueries(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	cameras := []Camera{
		{ID: "alpha", Name: "Alpha", Folder: "alpha", Timezone: "Europe/Berlin", ViewMode: "dual", WideMarker: "_wide", ZoomMarker: "_zoom", ZoomOffset: -5},
		{ID: "beta", Name: "Beta", Folder: "beta", Timezone: "Europe/Berlin"},
	}
	for _, camera := range cameras {
		if err := store.SaveCamera(camera); err != nil {
			t.Fatal(err)
		}
	}
	for _, item := range []struct {
		id, camera, path, codec string
		start, duration float64
	}{
		{"wide", "alpha", "alpha/clip_wide.mp4", "h264", 108, 10},
		{"zoom", "alpha", "alpha/clip_zoom.mp4", "h264", 108, 10},
		{"other", "beta", "beta/clip.mp4", "h264", 104, 10},
		{"image", "beta", "beta/image.jpg", "jpeg", 105, 10},
	} {
		if _, err := store.Exec(`INSERT INTO recordings(id,camera_id,path,start,duration,codec,size,mod) VALUES(?,?,?,?,?,?,1,1)`, item.id, item.camera, item.path, item.start, item.duration, item.codec); err != nil {
			t.Fatal(err)
		}
	}
	for _, bounds := range [][2]float64{{100, 110}, {110, 120}, {120, 130}} {
		want := []Recording{}
		for _, camera := range cameras {
			recordings, err := store.Recordings(camera.ID, bounds[0], bounds[1])
			if err != nil {
				t.Fatal(err)
			}
			want = append(want, recordings...)
		}
		got, err := store.DayRecordings(cameras, bounds[0], bounds[1])
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Fatalf("bounds %v: got %+v, want %+v, err %v", bounds, got, want, err)
		}
	}
}
