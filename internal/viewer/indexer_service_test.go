package viewer

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func waitIndex(t *testing.T, a *App) ScanStatus {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		s := a.scanStatus()
		if !s.Running {
			return s
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("indexer did not finish")
	return ScanStatus{}
}
func TestIndexQueueDedupeAndFairness(t *testing.T) {
	a := testApp(t)
	a.index.mu.Lock()
	a.index.status.Running = true
	a.index.current = scanJob{date: "2026-10-14", camera: "active"}
	a.index.mu.Unlock()
	job := scanJob{date: "2026-10-14", camera: "one", incremental: true}
	if !a.queueScan(job, true) || a.queueScan(job, true) || a.queueScan(scanJob{date: "2026-10-14", camera: "active"}, true) {
		t.Fatal("duplicate accepted")
	}
	if !a.queueScan(scanJob{date: "2026-10-15", camera: "one", incremental: true}, true) {
		t.Fatal("distinct date rejected")
	}
	status := a.scanStatus()
	if len(status.Queue) != 2 || status.Queue[0].ID == "" {
		t.Fatal(status)
	}
	status.Queue[0].Camera = "changed"
	if a.scanStatus().Queue[0].Camera != "one" {
		t.Fatal("status aliases live queue")
	}
	x := indexer{queue: []scanJob{{camera: "auto"}, {camera: "m1", manual: true}, {camera: "m2", manual: true}, {camera: "m3", manual: true}, {camera: "m4", manual: true}}}
	for _, want := range []string{"m1", "m2", "m3", "auto", "m4"} {
		got := x.nextJob()
		if got.camera != want {
			t.Fatalf("got %s want %s", got.camera, want)
		}
		if got.manual {
			x.manualStreak++
		} else {
			x.manualStreak = 0
		}
	}
}
func TestStableFilesAreRecheckedWithoutReprobingUnchangedVideos(t *testing.T) {
	a := testApp(t)
	c := fixture(t, a, "libx264")
	a.scanDay("2026-10-14")
	rs, _ := a.db.Recordings(c.ID, 0, 2e10)
	r := rs[0]
	file := filepath.Join(a.cfg.RecordingsDir, r.Path)
	old := time.Now().Add(-4 * time.Minute)
	os.Chtimes(file, old, old)
	for range 2 {
		if !a.scanPartitionMode(c, c.Folder, 0, math.MaxInt64, true) {
			t.Fatal("settling failed")
		}
	}
	data, _ := os.ReadFile(file)
	os.WriteFile(file, append(data, 0), 0600)
	os.Chtimes(file, old, old)
	if !a.scanPartitionMode(c, c.Folder, 0, math.MaxInt64, true) {
		t.Fatal("changed stable file skipped")
	}
	got, e := a.db.Recording(r.ID)
	if e != nil || got.Size != int64(len(data)+1) {
		t.Fatal(got, e)
	}
	// No ffprobe on PATH: an unchanged recording must still scan successfully.
	t.Setenv("PATH", t.TempDir())
	if !a.scanPartitionMode(c, c.Folder, 0, math.MaxInt64, true) {
		t.Fatal("unchanged video was reprobed")
	}
}
func TestPartialAndUnavailableSourcesPreserveExistingIndex(t *testing.T) {
	a := testApp(t)
	c := fixture(t, a, "libx264")
	a.scanDay("2026-10-14")
	rs, _ := a.db.Recordings(c.ID, 0, 2e10)
	r := rs[0]
	file := filepath.Join(a.cfg.RecordingsDir, r.Path)
	os.Remove(file)
	bad := filepath.Join(filepath.Dir(file), "08-31-00.mp4")
	os.WriteFile(bad, []byte("broken video"), 0600)
	old := time.Now().Add(-time.Minute)
	os.Chtimes(bad, old, old)
	if a.scanPartition(c, c.Folder, 0, math.MaxInt64) {
		t.Fatal("damaged video reported success")
	}
	if _, e := a.db.Recording(r.ID); e != nil {
		t.Fatal("partial scan removed index")
	}
	os.Rename(filepath.Join(a.cfg.RecordingsDir, c.Folder), filepath.Join(a.cfg.RecordingsDir, "offline"))
	if a.scanPartition(c, c.Folder, 0, math.MaxInt64) {
		t.Fatal("offline source reported success")
	}
	if _, e := a.db.Recording(r.ID); e != nil {
		t.Fatal("offline source removed index")
	}
	if !strings.Contains(strings.Join(a.scanStatus().Warnings, " "), "Quelle nicht erreichbar") {
		t.Fatal(a.scanStatus())
	}
}
func TestIncrementalLookbackAcrossMidnightAndDST(t *testing.T) {
	for _, date := range []string{"2026-10-15", "2026-03-30", "2026-10-26"} {
		t.Run(date, func(t *testing.T) {
			a := testApp(t)
			c := Camera{ID: "lookback", Name: "Lookback", Folder: "lookback", Format: "automatic", Timezone: "Europe/Berlin", IndexingMode: "partitioned", DirectoryPattern: "{YYYY}/{MM}/{DD}"}
			a.db.SaveCamera(c)
			loc, _ := time.LoadLocation(c.Timezone)
			day, _ := time.ParseInLocation("2006-01-02", date, loc)
			for offset := 0; offset < 3; offset++ {
				d := day.AddDate(0, 0, -offset)
				testVideo(t, filepath.Join(a.cfg.RecordingsDir, c.Folder, d.Format("2006/01/02"), d.Format("20060102")+"_233000.mp4"), false)
			}
			a.scanJob(scanJob{date: date, camera: c.ID, incremental: true})
			rs, e := a.db.Recordings(c.ID, 0, 2e10)
			if e != nil || len(rs) != 3 {
				t.Fatal(len(rs), e, a.scanStatus())
			}
		})
	}
}
func TestSixSourcesStatusAndShutdown(t *testing.T) {
	a := testApp(t)
	for i := 0; i < 6; i++ {
		id := fmt.Sprintf("source-%d", i)
		c := Camera{ID: id, Name: id, Folder: id, Format: "automatic", IndexingMode: "full", Timezone: "UTC"}
		a.db.SaveCamera(c)
		testVideo(t, filepath.Join(a.cfg.RecordingsDir, id, "deep/20261014083000.mp4"), false)
	}
	if !a.queueScan(scanJob{date: "2026-10-14", manual: true}, true) {
		t.Fatal("not queued")
	}
	s := waitIndex(t, a)
	if s.State != "complete" || s.Total != 6 || s.Completed != 6 || s.Added != 6 || s.Inspected != 6 || len(s.Cameras) != 6 || s.JobID == "" || s.Completion != 1 {
		t.Fatal(s)
	}
	for _, c := range s.Cameras {
		if c.State != "complete" || c.Added != 1 {
			t.Fatal(c)
		}
	}
	if !a.queueScan(scanJob{date: "2026-10-14", manual: true}, true) {
		t.Fatal("rescan not queued")
	}
	a.cancel()
	s = waitIndex(t, a)
	if s.State != "cancelled" || len(s.Queue) != 0 {
		t.Fatal(s)
	}
	if a.queueScan(scanJob{date: "2026-10-15"}, true) {
		t.Fatal("accepted after shutdown")
	}
}
