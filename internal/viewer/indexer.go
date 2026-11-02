package viewer

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"
)

type CameraScanStatus struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	State     string `json:"state"`
	Inspected int    `json:"inspected"`
	Added     int    `json:"added"`
	Changed   int    `json:"changed"`
	Skipped   int    `json:"skipped"`
}
type QueuedScan struct {
	ID        string `json:"id"`
	Date      string `json:"date"`
	Camera    string `json:"camera"`
	Automatic bool   `json:"automatic"`
}
type ScanStatus struct {
	Active     []string           `json:"active"`
	Completed  int                `json:"completed"`
	Total      int                `json:"total"`
	Running    bool               `json:"running"`
	Date       string             `json:"date"`
	Camera     string             `json:"camera"`
	Indexed    int                `json:"indexed"`
	Warnings   []string           `json:"warnings"`
	Finished   string             `json:"finished,omitempty"`
	JobID      string             `json:"jobId"`
	State      string             `json:"state"`
	Queue      []QueuedScan       `json:"queue"`
	Cameras    []CameraScanStatus `json:"cameras"`
	Inspected  int                `json:"inspected"`
	Added      int                `json:"added"`
	Changed    int                `json:"changed"`
	Skipped    int                `json:"skipped"`
	Completion uint64             `json:"completion"`
}
type scanJob struct {
	date, camera, batch, id  string
	all, incremental, manual bool
}

type persistedScanJob struct {
	Date        string `json:"date"`
	Camera      string `json:"camera"`
	Batch       string `json:"batch"`
	All         bool   `json:"all"`
	Incremental bool   `json:"incremental"`
	Manual      bool   `json:"manual"`
}

func (j scanJob) persisted() persistedScanJob {
	return persistedScanJob{j.date, j.camera, j.batch, j.all, j.incremental, j.manual}
}

func (a *App) restoreScans() error {
	rows, err := a.db.Query(`SELECT id,payload FROM scan_jobs ORDER BY active DESC,queued_at,id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, payload string
		if err := rows.Scan(&id, &payload); err != nil {
			return err
		}
		var p persistedScanJob
		if err := json.Unmarshal([]byte(payload), &p); err != nil {
			return err
		}
		j := scanJob{date: p.Date, camera: p.Camera, batch: p.Batch, id: id, all: p.All, incremental: p.Incremental, manual: p.Manual}
		if _, _, err := dayBounds(j.date); err != nil {
			return err
		}
		a.index.queue = append(a.index.queue, j)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if len(a.index.queue) > 0 {
		a.index.current = a.index.queue[0]
		a.index.queue = a.index.queue[1:]
		if _, err := a.db.Exec(`UPDATE scan_jobs SET active=1 WHERE id=?`, a.index.current.id); err != nil {
			return err
		}
		a.index.status = ScanStatus{Running: true, Date: a.index.current.date, JobID: a.index.current.id, State: "running", Warnings: []string{}}
		a.work.Add(1)
		go a.runScanQueue(a.index.current)
	}
	return nil
}

func sameScan(a, b scanJob) bool {
	return a.date == b.date && a.camera == b.camera && a.batch == b.batch && a.all == b.all && a.incremental == b.incremental
}

type indexer struct {
	current      scanJob
	queue        []scanJob
	cleanup      bool
	scheduled    bool
	mu           sync.Mutex
	status       ScanStatus
	last         map[string]time.Time
	manualStreak int
	completion   uint64
}

func (a *App) scanStatus() ScanStatus {
	a.index.mu.Lock()
	defer a.index.mu.Unlock()
	s := a.index.status
	if s.State == "" {
		s.State = "idle"
	}
	s.Active = append([]string{}, s.Active...)
	s.Warnings = append([]string{}, s.Warnings...)
	s.Cameras = append([]CameraScanStatus{}, s.Cameras...)
	s.Queue = []QueuedScan{}
	for _, q := range a.index.queue {
		s.Queue = append(s.Queue, QueuedScan{q.id, q.date, q.camera, !q.manual})
	}
	s.Completion = a.index.completion
	return s
}
func (a *App) warn(s string) {
	a.index.mu.Lock()
	defer a.index.mu.Unlock()
	if len(a.index.status.Warnings) < 30 {
		a.index.status.Warnings = append(a.index.status.Warnings, s)
	}
}

// Called with the index mutex held; preserve the legacy human-readable fields.
func (x *indexer) updateActive() {
	x.status.Active = []string{}
	for _, c := range x.status.Cameras {
		if c.State == "running" {
			x.status.Active = append(x.status.Active, c.Name)
		}
	}
	x.status.Camera = strings.Join(x.status.Active, " · ")
}
func (a *App) progress(c Camera, kind string) {
	a.index.mu.Lock()
	defer a.index.mu.Unlock()
	for i := range a.index.status.Cameras {
		p := &a.index.status.Cameras[i]
		if p.ID != c.ID {
			continue
		}
		switch kind {
		case "inspected":
			p.Inspected++
		case "added":
			p.Added++
		case "changed":
			p.Changed++
		case "skipped":
			p.Skipped++
		}
		break
	}
	switch kind {
	case "inspected":
		a.index.status.Inspected++
	case "added":
		a.index.status.Added++
		a.index.status.Indexed++
	case "changed":
		a.index.status.Changed++
		a.index.status.Indexed++
	case "skipped":
		a.index.status.Skipped++
	}
}

// Called with the index mutex held. Prefer manual work, but never starve background work.
func (x *indexer) nextJob() scanJob {
	chosen := 0
	manual, automatic := -1, -1
	for i, q := range x.queue {
		if q.manual {
			if manual < 0 {
				manual = i
			}
		} else if automatic < 0 {
			automatic = i
		}
	}
	if automatic >= 0 && (x.manualStreak >= 3 || manual < 0) {
		chosen = automatic
	} else if manual >= 0 {
		chosen = manual
	}
	job := x.queue[chosen]
	x.queue = append(x.queue[:chosen], x.queue[chosen+1:]...)
	return job
}

// A recursive camera scan indexes every day, so a second date does not need a
// duplicate walk while that camera is already being scanned or is queued.
func scanCoversCamera(job scanJob, camera Camera, date string) bool {
	if job.camera != "" && job.camera != camera.ID {
		return false
	}
	if job.batch != "" && !strings.Contains(","+job.batch+",", ","+camera.ID+",") {
		return false
	}
	return job.all || camera.IndexingMode == "full" || job.date == date
}

func scanScopeKey(camera Camera, date string) string {
	if camera.IndexingMode == "full" {
		return "full/" + camera.ID
	}
	return date + "/" + camera.ID
}

func (a *App) startDayScan(date string, cameras []Camera) bool {
	a.index.mu.Lock()
	defer a.index.mu.Unlock()
	ids := make([]string, 0, len(cameras))
	for _, camera := range cameras {
		covered := a.index.status.Running && scanCoversCamera(a.index.current, camera, date)
		for _, queued := range a.index.queue {
			if scanCoversCamera(queued, camera, date) {
				covered = true
				break
			}
		}
		if !covered && time.Since(a.index.last[scanScopeKey(camera, date)]) >= time.Minute {
			ids = append(ids, camera.ID)
		}
	}
	if len(ids) == 0 {
		return false
	}
	return a.queueScanLocked(scanJob{date: date, batch: strings.Join(ids, ",")}, true)
}

func dayBounds(date string) (time.Time, time.Time, error) {
	loc, _ := time.LoadLocation("Europe/Berlin")
	from, e := time.ParseInLocation("2006-01-02", date, loc)
	if e != nil || from.Year() < 1970 || from.Year() > 2200 {
		return time.Time{}, time.Time{}, errors.New("Datum ungültig")
	}
	return from, from.AddDate(0, 0, 1), nil
}
func (a *App) scanDay(date string) { a.scanJob(scanJob{date: date}) }
func (a *App) scanJob(job scanJob) {
	date := job.date
	from, to, e := dayBounds(date)
	if e != nil {
		return
	}
	cams, e := a.db.Cameras()
	if e != nil {
		a.warn("Kameraliste nicht lesbar")
		return
	}
	chosen := []Camera{}
	for _, c := range cams {
		if job.camera != "" && job.camera != c.ID {
			continue
		}
		if job.batch != "" && !strings.Contains(","+job.batch+",", ","+c.ID+",") {
			continue
		}
		chosen = append(chosen, c)
	}
	a.index.mu.Lock()
	a.index.status.Total = len(chosen)
	a.index.status.Cameras = []CameraScanStatus{}
	for _, c := range chosen {
		a.index.status.Cameras = append(a.index.status.Cameras, CameraScanStatus{ID: c.ID, Name: c.Name, State: "queued"})
	}
	a.index.mu.Unlock()
	// Jobs remain serialized, so the same camera cannot be scanned concurrently.
	// Within each job, at most three independent camera sources are probed at once.
	slots := make(chan struct{}, 3)
	var workers sync.WaitGroup
	for _, c := range chosen {
		if a.ctx.Err() != nil {
			break
		}
		select {
		case slots <- struct{}{}:
		case <-a.ctx.Done():
			workers.Wait()
			return
		}
		workers.Add(1)
		go func(c Camera) {
			defer workers.Done()
			defer func() { <-slots }()
			a.index.mu.Lock()
			for i := range a.index.status.Cameras {
				if a.index.status.Cameras[i].ID == c.ID {
					a.index.status.Cameras[i].State = "running"
				}
			}
			a.index.updateActive()
			a.index.mu.Unlock()
			defer func() { a.index.mu.Lock(); a.index.status.Completed++; a.index.updateActive(); a.index.mu.Unlock() }()
			complete := a.scanCamera(job, c, from, to)
			a.index.mu.Lock()
			for i := range a.index.status.Cameras {
				p := &a.index.status.Cameras[i]
				if p.ID == c.ID {
					p.State = "complete"
					if !complete || p.Skipped > 0 {
						p.State = "partial"
						if !complete && p.Inspected == 0 {
							p.State = "failed"
						}
					}
					if a.ctx.Err() != nil {
						p.State = "cancelled"
					}
				}
			}
			a.index.last[scanScopeKey(c, job.date)] = time.Now()
			a.index.mu.Unlock()
		}(c)
	}
	workers.Wait()
}
