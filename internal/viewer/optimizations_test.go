package viewer

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync/atomic"
	"testing"
	"time"
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

func TestReadPoolSeesCommitsAndRejectsWrites(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if store.read == nil || store.reader() == store.DB {
		t.Fatal("read pool missing")
	}
	camera := Camera{ID: "cam", Name: "Kamera", Folder: "cam", Pattern: "{YYYY}{MM}{DD}{hh}{mm}{ss}.mp4", Timezone: "Europe/Berlin"}
	if err = store.SaveCamera(camera); err != nil {
		t.Fatal(err)
	}
	if got, err := store.Camera("cam"); err != nil || got.Name != "Kamera" {
		t.Fatal(got, err)
	}
	// An open write transaction must not block readers, and readers only see committed rows.
	tx, err := store.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(`UPDATE cameras SET name='Neu' WHERE id='cam'`); err != nil {
		t.Fatal(err)
	}
	if got, err := store.Camera("cam"); err != nil || got.Name != "Kamera" {
		t.Fatal("uncommitted row visible or reader blocked", got, err)
	}
	if err = tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if got, err := store.Camera("cam"); err != nil || got.Name != "Neu" {
		t.Fatal(got, err)
	}
	if _, err = store.reader().Exec(`DELETE FROM cameras`); err == nil {
		t.Fatal("read pool accepted a write")
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	if err = store.reader().Ping(); err == nil {
		t.Fatal("read pool still open after Close")
	}
	// Without a pool the store falls back to its single connection.
	if fallback := (&Store{DB: store.DB}); fallback.reader() != store.DB {
		t.Fatal("fallback reader differs")
	}
}

func TestBatchedScanReportsEveryFileOnce(t *testing.T) {
	a := testApp(t)
	camera := Camera{ID: "cam", Name: "Kamera", Folder: "cam", Pattern: "{YYYY}{MM}{DD}{hh}{mm}{ss}.mp4", Timezone: "UTC", Format: "custom"}
	if err := a.db.SaveCamera(camera); err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(a.cfg.RecordingsDir, camera.Folder)
	if err := os.MkdirAll(folder, 0700); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	write := func(name string, data []byte, stamp time.Time) os.FileInfo {
		path := filepath.Join(folder, name)
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
		st, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		return st
	}
	want := []string{}
	// More unreadable files than probe workers, so several rounds run concurrently.
	for i := 0; i < 3*probeWorkers; i++ {
		name := fmt.Sprintf("202603011000%02d.mp4", i)
		write(name, []byte("not a real video"), old)
		want = append(want, "Kamera: "+name+": Video beschädigt oder nicht lesbar")
	}
	write("20260301110000.mp4", nil, old)
	write("20260301120000.mp4", []byte("still growing"), time.Now())
	want = append(want, "Kamera: 20260301110000.mp4: Aufnahme wird noch geschrieben", "Kamera: 20260301120000.mp4: Aufnahme wird noch geschrieben")
	write("unnamed.mp4", []byte("x"), old)
	want = append(want, "Kamera: unnamed.mp4: Dateiname passt nicht zum Muster")
	known := write("20260301130000.mp4", []byte("already indexed"), old)
	if _, err := a.db.Exec(`INSERT INTO recordings(id,camera_id,path,start,duration,codec,size,mod,filename) VALUES('known','cam','cam/20260301130000.mp4',1,30,'h264',?,?,'20260301130000.mp4')`, known.Size(), known.ModTime().UnixNano()); err != nil {
		t.Fatal(err)
	}
	write("notes.txt", []byte("ignored"), old)
	if a.scanPartition(camera, camera.Folder, 0, math.MaxInt64) {
		t.Fatal("scan with unreadable files reported as complete")
	}
	status := a.scanStatus()
	if status.Inspected != len(want)+1 || status.Skipped != len(want) || status.Added != 0 || status.Changed != 0 {
		t.Fatal(status.Inspected, status.Skipped, status.Added, status.Changed)
	}
	got := append([]string{}, status.Warnings...)
	sort.Strings(got)
	sort.Strings(want)
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("warnings\n got %v\nwant %v", got, want)
	}
	var rows int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM recordings`).Scan(&rows); err != nil || rows != 1 {
		t.Fatal(rows, err)
	}
}

func TestParallelProbesIndexEveryFileWithItsOwnResult(t *testing.T) {
	a := testApp(t)
	var running, peak atomic.Int32
	original := scanProbe
	t.Cleanup(func() { scanProbe = original })
	// Each file reports its own size as duration, so mixed-up results are detected.
	scanProbe = func(ctx context.Context, f *os.File) (float64, string, time.Time, error) {
		now := running.Add(1)
		for {
			seen := peak.Load()
			if now <= seen || peak.CompareAndSwap(seen, now) {
				break
			}
		}
		defer running.Add(-1)
		time.Sleep(2 * time.Millisecond)
		st, err := f.Stat()
		if err != nil {
			return 0, "", time.Time{}, err
		}
		return float64(st.Size()), "h264", time.Time{}, nil
	}
	camera := Camera{ID: "cam", Name: "Kamera", Folder: "cam", Pattern: "{YYYY}{MM}{DD}{hh}{mm}{ss}.mp4", Timezone: "UTC", Format: "custom"}
	if err := a.db.SaveCamera(camera); err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(a.cfg.RecordingsDir, camera.Folder)
	if err := os.MkdirAll(folder, 0700); err != nil {
		t.Fatal(err)
	}
	const files = 300 // more than two batches
	old := time.Now().Add(-time.Hour)
	for i := 0; i < files; i++ {
		path := filepath.Join(folder, fmt.Sprintf("20260301%02d%02d00.mp4", i/60, i%60))
		if err := os.WriteFile(path, make([]byte, i+1), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}
	if !a.scanPartition(camera, camera.Folder, 0, math.MaxInt64) {
		t.Fatal("scan failed", a.scanStatus().Warnings)
	}
	if status := a.scanStatus(); status.Inspected != files || status.Added != files || status.Skipped != 0 {
		t.Fatal(status.Inspected, status.Added, status.Skipped)
	}
	if got := peak.Load(); got < 2 || got > probeWorkers {
		t.Fatal("unexpected probe concurrency", got)
	}
	var mismatched, total int
	if err := a.db.QueryRow(`SELECT COUNT(*),COALESCE(SUM(duration!=size),0) FROM recordings WHERE camera_id='cam' AND codec='h264'`).Scan(&total, &mismatched); err != nil || total != files || mismatched != 0 {
		t.Fatal(total, mismatched, err)
	}
	var start float64
	if err := a.db.QueryRow(`SELECT start FROM recordings WHERE filename='20260301010500.mp4'`).Scan(&start); err != nil || start != float64(time.Date(2026, 3, 1, 1, 5, 0, 0, time.UTC).Unix()) {
		t.Fatal(start, err)
	}
	// Unchanged files are not probed again.
	peak.Store(0)
	if !a.scanPartition(camera, camera.Folder, 0, math.MaxInt64) {
		t.Fatal("rescan failed")
	}
	if status := a.scanStatus(); peak.Load() != 0 || status.Inspected != 2*files || status.Added != files || status.Changed != 0 {
		t.Fatal(peak.Load(), status.Inspected, status.Added, status.Changed)
	}
	// A probe failure skips only the affected file.
	changed := filepath.Join(folder, "20260301000000.mp4")
	if err := os.WriteFile(changed, make([]byte, 5000), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(changed, old.Add(time.Minute), old.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	scanProbe = func(ctx context.Context, f *os.File) (float64, string, time.Time, error) {
		return 0, "", time.Time{}, errors.New("unreadable")
	}
	if a.scanPartition(camera, camera.Folder, 0, math.MaxInt64) {
		t.Fatal("failed probe reported as complete")
	}
	if status := a.scanStatus(); status.Skipped != 1 || status.Changed != 0 || len(status.Warnings) != 1 {
		t.Fatal(status.Skipped, status.Changed, status.Warnings)
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
