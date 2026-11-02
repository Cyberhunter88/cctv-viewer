package viewer

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

func (a *App) queueScan(job scanJob, force bool) bool {
	a.index.mu.Lock()
	defer a.index.mu.Unlock()
	return a.queueScanLocked(job, force)
}

// Called with the index mutex held.
func (a *App) queueScanLocked(job scanJob, force bool) bool {
	if a.ctx.Err() != nil {
		return false
	}
	if a.index.cleanup {
		return false
	}
	key := job.date + "/" + job.camera
	if !force && time.Since(a.index.last[key]) < time.Minute {
		return false
	}
	if a.index.status.Running && sameScan(a.index.current, job) {
		return false
	}
	for i, q := range a.index.queue {
		if sameScan(q, job) {
			if job.manual {
				promoted := q
				promoted.manual = true
				payload, _ := json.Marshal(promoted.persisted())
				if _, err := a.db.ExecContext(a.ctx, `UPDATE scan_jobs SET payload=? WHERE id=?`, string(payload), q.id); err == nil {
					a.index.queue[i].manual = true
				}
			}
			return false
		}
	}
	if len(a.index.queue) >= 32 {
		return false
	}
	job.id = token()[:16]
	payload, err := json.Marshal(job.persisted())
	if err != nil {
		return false
	}
	active := !a.index.status.Running
	if _, err = a.db.ExecContext(a.ctx, `INSERT INTO scan_jobs(id,payload,queued_at,active) VALUES(?,?,?,?)`, job.id, string(payload), time.Now().UnixNano(), active); err != nil {
		a.index.status.Warnings = append(a.index.status.Warnings, "Scanauftrag konnte nicht gespeichert werden")
		return false
	}
	if a.index.status.Running {
		a.index.queue = append(a.index.queue, job)
		return true
	}
	a.index.current = job
	a.index.status = ScanStatus{Running: true, Date: job.date, JobID: job.id, State: "running", Warnings: []string{}}
	a.work.Add(1)
	go a.runScanQueue(job)
	return true
}
func (a *App) runScanQueue(job scanJob) {
	defer a.work.Done()
	current := job
	for {
		a.scanJob(current)
		a.index.mu.Lock()
		if current.manual {
			a.index.manualStreak++
		} else {
			a.index.manualStreak = 0
		}
		if !current.incremental {
			a.index.last[current.date+"/"+current.camera] = time.Now()
		}
		for k, v := range a.index.last {
			if time.Since(v) > time.Hour {
				delete(a.index.last, k)
			}
		}
		a.index.completion++
		a.index.status.State = "complete"
		if len(a.index.status.Warnings) > 0 || a.index.status.Skipped > 0 {
			a.index.status.State = "partial"
		}
		for _, c := range a.index.status.Cameras {
			if c.State == "partial" || c.State == "failed" {
				a.index.status.State = "partial"
			}
		}
		failed := 0
		for _, c := range a.index.status.Cameras {
			if c.State == "failed" {
				failed++
			}
		}
		if failed > 0 && failed == len(a.index.status.Cameras) {
			a.index.status.State = "failed"
		}
		if a.ctx.Err() != nil {
			a.index.status.State = "cancelled"
			a.index.queue = nil
			for i := range a.index.status.Cameras {
				if a.index.status.Cameras[i].State == "queued" {
					a.index.status.Cameras[i].State = "cancelled"
				}
			}
		}
		if current.incremental && a.ctx.Err() == nil {
			next := time.Now().Add(time.Duration(a.config().AutoScanMinutes) * time.Minute)
			if a.index.status.State != "complete" {
				next = time.Now().Add(5 * time.Minute)
			}
			if _, e := a.db.ExecContext(a.ctx, `INSERT INTO scan_schedule VALUES(?,?) ON CONFLICT(camera_id) DO UPDATE SET next_run=excluded.next_run`, current.camera, next.Unix()); e != nil {
				a.index.status.Warnings = append(a.index.status.Warnings, "Zeitplan konnte nicht gespeichert werden")
			}
		}
		a.saveScanHistoryLocked(current)
		if a.ctx.Err() == nil {
			if _, err := a.db.ExecContext(a.ctx, `DELETE FROM scan_jobs WHERE id=?`, current.id); err != nil {
				a.index.status.Warnings = append(a.index.status.Warnings, "Scanauftrag konnte nicht abgeschlossen werden")
			}
		}
		if len(a.index.queue) == 0 {
			a.index.status.Running = false
			a.index.status.Finished = time.Now().UTC().Format(time.RFC3339)
			a.index.mu.Unlock()
			return
		}
		current = a.index.nextJob()
		if _, err := a.db.Exec(`UPDATE scan_jobs SET active=1 WHERE id=?`, current.id); err != nil {
			a.index.status.Warnings = append(a.index.status.Warnings, "Aktiver Scanauftrag konnte nicht gespeichert werden")
		}
		a.index.current = current
		a.index.status = ScanStatus{Running: true, Date: current.date, JobID: current.id, State: "running", Warnings: []string{}}
		a.index.mu.Unlock()
	}
}

func safeScanWarning(message string) string {
	lower := strings.ToLower(message)
	if strings.ContainsAny(message, `/\`) || strings.Contains(lower, ".mp4") || strings.Contains(lower, ".mkv") || strings.Contains(lower, ".dav") || strings.Contains(lower, ".avi") || strings.Contains(lower, ".mov") {
		return "Datei oder Ordner konnte nicht indexiert werden"
	}
	if len(message) > 240 {
		return message[:240]
	}
	return message
}

func (a *App) saveScanHistoryLocked(job scanJob) {
	status := a.index.status
	globalWarnings := []string{}
	for _, warning := range status.Warnings {
		assigned := false
		for _, camera := range status.Cameras {
			if strings.HasPrefix(warning, camera.Name+": ") {
				assigned = true
				break
			}
		}
		if !assigned {
			globalWarnings = append(globalWarnings, safeScanWarning(warning))
		}
	}
	for _, camera := range status.Cameras {
		warnings := []string{}
		for _, warning := range status.Warnings {
			if strings.HasPrefix(warning, camera.Name+": ") {
				warnings = append(warnings, safeScanWarning(strings.TrimPrefix(warning, camera.Name+": ")))
			}
		}
		encoded, _ := json.Marshal(warnings)
		state := camera.State
		if a.ctx.Err() != nil {
			state = "interrupted"
		}
		_, err := a.db.Exec(`INSERT INTO scan_history(id,camera_id,camera_name,scan_date,manual,finished,state,inspected,added,changed,skipped,warnings) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, job.id+"/"+camera.ID+"/"+fmt.Sprint(time.Now().UnixNano()), camera.ID, camera.Name, job.date, job.manual, time.Now().Unix(), state, camera.Inspected, camera.Added, camera.Changed, camera.Skipped, string(encoded))
		if err != nil {
			a.index.status.Warnings = append(a.index.status.Warnings, "Scanverlauf konnte nicht gespeichert werden")
			return
		}
	}
	if len(globalWarnings) > 0 || len(status.Cameras) == 0 {
		encoded, _ := json.Marshal(globalWarnings)
		state := status.State
		if a.ctx.Err() != nil {
			state = "interrupted"
		}
		if _, err := a.db.Exec(`INSERT INTO scan_history(id,camera_id,camera_name,scan_date,manual,finished,state,inspected,added,changed,skipped,warnings) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, job.id+"/system/"+fmt.Sprint(time.Now().UnixNano()), "", "System", job.date, job.manual, time.Now().Unix(), state, status.Inspected, status.Added, status.Changed, status.Skipped, string(encoded)); err != nil {
			a.index.status.Warnings = append(a.index.status.Warnings, "Scanverlauf konnte nicht gespeichert werden")
			return
		}
	}
	if _, err := a.db.Exec(`DELETE FROM scan_history WHERE id IN (SELECT id FROM scan_history ORDER BY finished DESC,id DESC LIMIT -1 OFFSET 200)`); err != nil {
		a.index.status.Warnings = append(a.index.status.Warnings, "Scanverlauf konnte nicht bereinigt werden")
	}
}
func (a *App) runScheduledScan() {
	cfg := a.config()
	a.index.mu.Lock()
	wasEnabled := a.index.scheduled
	a.index.scheduled = cfg.AutoScan
	a.index.mu.Unlock()
	if !cfg.AutoScan {
		return
	}
	if !wasEnabled {
		if _, e := a.db.ExecContext(a.ctx, `DELETE FROM scan_schedule`); e != nil {
			a.warn("Zeitplan nicht lesbar")
			return
		}
	}
	cams, e := a.db.Cameras()
	if e != nil {
		return
	}
	// Queue all due cameras, including recursive sources. Dedupe limits repeated ticks.
	for _, c := range cams {
		var next int64
		if err := a.db.QueryRowContext(a.ctx, `SELECT next_run FROM scan_schedule WHERE camera_id=?`, c.ID).Scan(&next); err != nil && !errors.Is(err, sql.ErrNoRows) {
			a.warn("Zeitplan nicht lesbar")
			return
		}
		if next > time.Now().Unix() {
			continue
		}
		loc, e := time.LoadLocation(c.Timezone)
		if e != nil {
			continue
		}
		a.queueScan(scanJob{date: time.Now().In(loc).Format("2006-01-02"), camera: c.ID, incremental: true}, true)
	}
}
