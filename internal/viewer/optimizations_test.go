package viewer

import (
	"fmt"
	"testing"
)

func TestDayQueryBoundsKeepLongAndMidnightRecordings(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	camera := Camera{ID: "cam", Name: "Kamera", Folder: "cam", Pattern: "{YYYY}{MM}{DD}{hh}{mm}{ss}.mp4", Timezone: "Europe/Berlin"}
	if err = store.SaveCamera(camera); err != nil {
		t.Fatal(err)
	}
	const from, to = 1_000_000.0, 1_086_400.0
	rows := []struct {
		id              string
		start, duration float64
	}{
		{"history", from - 30*86400, 60},                       // old history must be skipped
		{"ended-before", from - 3600, 3599},                    // ends just before the day
		{"across-midnight", from - 600, 1200},                  // starts before, ends inside
		{"longest", from - maxRecordingSeconds + 10, 172790.5}, // longest accepted media still overlaps
		{"inside", from + 100, 30},
		{"after", to + 1, 30},
	}
	for _, row := range rows {
		if _, err = store.Exec(`INSERT INTO recordings(id,camera_id,path,start,duration,codec,size,mod,filename) VALUES(?,?,?,?,?,'h264',1,1,?)`, row.id, camera.ID, "cam/"+row.id+".mp4", row.start, row.duration, row.id+".mp4"); err != nil {
			t.Fatal(err)
		}
	}
	want := "[longest across-midnight inside]"
	day, err := store.DayRecordings([]Camera{camera}, from, to)
	if err != nil {
		t.Fatal(err)
	}
	single, err := store.Recordings(camera.ID, from, to)
	if err != nil {
		t.Fatal(err)
	}
	for name, list := range map[string][]Recording{"day": day, "camera": single} {
		ids := []string{}
		for _, recording := range list {
			ids = append(ids, recording.ID)
		}
		if fmt.Sprint(ids) != want {
			t.Fatalf("%s: got %v, want %s", name, ids, want)
		}
	}
	var detail string
	if err = store.QueryRow(`EXPLAIN QUERY PLAN SELECT id FROM recordings WHERE start<? AND start>? AND start+duration>? AND codec!='jpeg' ORDER BY start,id`, to, from-maxRecordingSeconds, from).Scan(new(int), new(int), new(int), &detail); err != nil {
		t.Fatal(err)
	}
	if want := "start>? AND start<?"; !containsText(detail, want) {
		t.Fatalf("day query does not use a bounded start range: %s", detail)
	}
}

func containsText(text, part string) bool {
	for i := 0; i+len(part) <= len(text); i++ {
		if text[i:i+len(part)] == part {
			return true
		}
	}
	return false
}

func TestTimestampCachesReturnStableResults(t *testing.T) {
	const pattern = "{YYYY}/{MM}/{DD}/clip_{hh}{mm}{ss}.mp4"
	first, err := ParseTimestamp(pattern, "2026/03/01/clip_101112.mp4", "Europe/Berlin", 0)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		again, err := ParseTimestamp(pattern, "2026/03/01/clip_101112.mp4", "Europe/Berlin", 0)
		if err != nil || !again.Equal(first) {
			t.Fatal(again, err)
		}
	}
	if _, ok := patternCache.Load(pattern); !ok {
		t.Fatal("compiled pattern was not cached")
	}
	if _, err = ParseTimestamp("{YYYY}.mp4", "2026.mp4", "Europe/Berlin", 0); err == nil {
		t.Fatal("invalid pattern accepted")
	}
	if _, ok := patternCache.Load("{YYYY}.mp4"); ok {
		t.Fatal("invalid pattern was cached")
	}
	if _, err = cachedLocation("Nowhere/Invalid"); err == nil {
		t.Fatal("invalid zone accepted")
	}
	if _, ok := locationCache.Load("Nowhere/Invalid"); ok {
		t.Fatal("invalid zone was cached")
	}
	reolink := Camera{Format: "reolink", Timezone: "Europe/Berlin"}
	if _, err = cameraTimestamp(reolink, "2026/03/01/Cam_00_20260301101112.mp4"); err != nil {
		t.Fatal(err)
	}
	if _, err = cameraTimestamp(reolink, "2026/03/02/Cam_00_20260301101112.mp4"); err == nil {
		t.Fatal("mismatching Reolink folder accepted")
	}
}
