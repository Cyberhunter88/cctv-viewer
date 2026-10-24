package viewer

import (
	"context"
	"fmt"
	"math"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
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

func TestDayScanReusesActiveAndQueuedCameraWork(t *testing.T) {
	a := testApp(t)
	cameras := []Camera{
		{ID: "recursive", Name: "Recursive", Folder: "recursive", Format: "automatic", IndexingMode: "full", Timezone: "UTC"},
		{ID: "dated", Name: "Dated", Folder: "dated", Format: "automatic", IndexingMode: "partitioned", DirectoryPattern: "{YYYY}/{MM}/{DD}", Timezone: "UTC"},
	}
	a.index.mu.Lock()
	a.index.status.Running = true
	a.index.current = scanJob{date: "2026-10-14", camera: "recursive"}
	a.index.queue = []scanJob{{date: "2026-10-15", camera: "dated"}}
	a.index.mu.Unlock()
	if a.startDayScan("2026-10-15", cameras) {
		t.Fatal("queued work was duplicated")
	}
	if !a.startDayScan("2026-10-16", cameras) {
		t.Fatal("new dated partition was not queued")
	}
	status := a.scanStatus()
	if len(status.Queue) != 2 {
		t.Fatal(status.Queue)
	}
	job := a.index.queue[1]
	if job.batch != "dated" || job.camera != "" || job.date != "2026-10-16" {
		t.Fatal(job)
	}
	if a.startDayScan("2026-10-16", cameras) {
		t.Fatal("same partition was queued twice")
	}
}

func TestDayRefreshDoesNotStartAnotherScan(t *testing.T) {
	a := testApp(t)
	camera := Camera{ID: "refresh", Name: "Refresh", Folder: "refresh", Format: "automatic", IndexingMode: "full", Timezone: "UTC"}
	if err := a.db.SaveCamera(camera); err != nil {
		t.Fatal(err)
	}
	refresh := httptest.NewRecorder()
	a.day(refresh, httptest.NewRequest("GET", "/api/day?date=2026-10-14&refresh=1", nil))
	if refresh.Code != 200 || a.scanStatus().Running || a.scanStatus().Completion != 0 {
		t.Fatal(refresh.Code, a.scanStatus())
	}
	open := httptest.NewRecorder()
	a.day(open, httptest.NewRequest("GET", "/api/day?date=2026-10-14", nil))
	if open.Code != 200 {
		t.Fatal(open.Code, open.Body.String())
	}
	if status := waitIndex(t, a); status.Completion != 1 {
		t.Fatal(status)
	}
	reopen := httptest.NewRecorder()
	a.day(reopen, httptest.NewRequest("GET", "/api/day?date=2026-10-14", nil))
	if reopen.Code != 200 {
		t.Fatal(reopen.Code, reopen.Body.String())
	}
	if status := waitIndex(t, a); status.Completion != 2 {
		t.Fatal("reopening the day did not check the source again", status)
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
func TestMissingRecordingRequiresConfirmedCleanup(t *testing.T) {
	a:=testApp(t)
	c:=fixture(t,a,"libx264")
	a.scanDay("2026-10-14")
	items,err:=a.db.Recordings(c.ID,0,2e10)
	if err!=nil || len(items)!=1 { t.Fatal(items,err) }
	if err:=os.Remove(filepath.Join(a.cfg.RecordingsDir,items[0].Path));err!=nil { t.Fatal(err) }
	if !a.scanPartition(c,c.Folder,0,math.MaxInt64) { t.Fatal("empty source scan failed") }
	if _,err:=a.db.Recording(items[0].ID);err!=nil { t.Fatal("ordinary scan deleted recording",err) }
	ids,digest,err:=a.cleanupCandidates(c.ID)
	if err!=nil || len(ids)!=1 || digest=="" { t.Fatal(ids,digest,err) }
	cookie,session:=login(t,a)
	preview:=request(a,"GET","/api/index/cleanup?camera="+c.ID,nil,cookie,"")
	if preview.Code!=200 { t.Fatal(preview.Code,preview.Body.String()) }
	commit:=request(a,"POST","/api/index/cleanup",map[string]string{"camera":c.ID,"digest":"stale"},cookie,session.CSRF)
	if commit.Code!=409 { t.Fatal(commit.Code,commit.Body.String()) }
	commit=request(a,"POST","/api/index/cleanup",map[string]string{"camera":c.ID,"digest":digest},cookie,session.CSRF)
	if commit.Code!=200 { t.Fatal(commit.Code,commit.Body.String()) }
	if _,err:=a.db.Recording(items[0].ID);err==nil { t.Fatal("confirmed cleanup kept stale recording") }
}

func TestScanJobPersistedUntilCompletion(t *testing.T) {
	a:=testApp(t)
	a.index.mu.Lock()
	a.index.status.Running=true
	a.index.current=scanJob{date:"2026-10-14",camera:"active"}
	a.index.mu.Unlock()
	if !a.queueScan(scanJob{date:"2026-10-14",camera:"pending",manual:true},true) { t.Fatal("queue rejected") }
	var payload string
	if err:=a.db.QueryRow(`SELECT payload FROM scan_jobs`).Scan(&payload);err!=nil || !strings.Contains(payload,`"camera":"pending"`) { t.Fatal(payload,err) }
}
func TestPendingScanResumesAfterRestart(t *testing.T) {
	dir:=t.TempDir()
	config:=Config{DataDir:filepath.Join(dir,"data"),RecordingsDir:filepath.Join(dir,"recordings"),BaseURL:"http://localhost:8080",LocalAdmin:true,SessionTTL:time.Hour,ScanInterval:time.Hour,DevHTTP:true}
	if err:=os.MkdirAll(config.RecordingsDir,0700);err!=nil { t.Fatal(err) }
	assets:=fstest.MapFS{"index.html":{Data:[]byte("test")}}
	first,err:=New(context.Background(),config,assets)
	if err!=nil { t.Fatal(err) }
	first.index.mu.Lock();first.index.status.Running=true;first.index.current=scanJob{date:"2026-10-14",camera:"active"};first.index.mu.Unlock()
	if !first.queueScan(scanJob{date:"2026-10-14",manual:true},true) { t.Fatal("queue rejected") }
	first.Close()
	second,err:=New(context.Background(),config,assets)
	if err!=nil { t.Fatal(err) }
	defer second.Close()
	status:=waitIndex(t,second)
	if status.Completion!=1 { t.Fatal(status) }
	var remaining int
	if err:=second.db.QueryRow(`SELECT COUNT(*) FROM scan_jobs`).Scan(&remaining);err!=nil || remaining!=0 { t.Fatal(remaining,err) }
}
func TestFailedAutomaticScanRetriesSoon(t *testing.T) {
	a:=testApp(t)
	c:=Camera{ID:"offline-auto",Name:"Offline",Folder:"missing",Format:"automatic",Timezone:"Europe/Berlin",IndexingMode:"full"}
	if err:=a.db.SaveCamera(c);err!=nil { t.Fatal(err) }
	if !a.queueScan(scanJob{date:"2026-10-14",camera:c.ID,incremental:true},true) { t.Fatal("queue rejected") }
	status:=waitIndex(t,a)
	if status.State!="failed" { t.Fatal(status) }
	var next int64
	if err:=a.db.QueryRow(`SELECT next_run FROM scan_schedule WHERE camera_id=?`,c.ID).Scan(&next);err!=nil { t.Fatal(err) }
	if delay:=next-time.Now().Unix();delay<295 || delay>305 { t.Fatal("unexpected retry delay",delay) }
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
