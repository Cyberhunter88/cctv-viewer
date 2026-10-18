package viewer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"path"
	"path/filepath"
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

func sameScan(a, b scanJob) bool {
	return a.date == b.date && a.camera == b.camera && a.batch == b.batch && a.all == b.all && a.incremental == b.incremental
}

type indexer struct {
	current      scanJob
	queue        []scanJob
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
func (a *App) startScan(date string, force bool) bool { return a.queueScan(scanJob{date: date}, force) }
func (a *App) queueScan(job scanJob, force bool) bool {
	a.index.mu.Lock()
	defer a.index.mu.Unlock()
	if a.ctx.Err() != nil {
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
				a.index.queue[i].manual = true
			}
			return false
		}
	}
	if len(a.index.queue) >= 32 {
		return false
	}
	job.id = token()[:16]
	if a.index.status.Running {
		a.index.queue = append(a.index.queue, job)
		return true
	}
	a.index.current = job
	a.index.status = ScanStatus{Running: true, Date: job.date, JobID: job.id, State: "running", Warnings: []string{}}
	a.work.Add(1)
	go func() {
		defer a.work.Done()
		current := job
		for {
			a.scanJob(current)
			if current.incremental && a.ctx.Err() == nil {
				if _, e := a.db.ExecContext(a.ctx, `INSERT INTO scan_schedule VALUES(?,?) ON CONFLICT(camera_id) DO UPDATE SET next_run=excluded.next_run`, current.camera, time.Now().Add(time.Duration(a.config().AutoScanMinutes)*time.Minute).Unix()); e != nil {
					a.warn("Zeitplan konnte nicht gespeichert werden")
				}
			}
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
			if len(a.index.queue) == 0 {
				a.index.status.Running = false
				a.index.status.Finished = time.Now().UTC().Format(time.RFC3339)
				a.index.mu.Unlock()
				return
			}
			current = a.index.nextJob()
			a.index.current = current
			a.index.status = ScanStatus{Running: true, Date: current.date, JobID: current.id, State: "running", Warnings: []string{}}
			a.index.mu.Unlock()
		}
	}()
	return true
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
		a.db.QueryRowContext(a.ctx, `SELECT next_run FROM scan_schedule WHERE camera_id=?`, c.ID).Scan(&next)
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
			a.index.mu.Unlock()
		}(c)
	}
	workers.Wait()
}
func (a *App) scanCamera(job scanJob, c Camera, from, to time.Time) bool {
	date := job.date
	loc, e := time.LoadLocation(c.Timezone)
	if e != nil {
		a.warn(c.Name + ": ungültige Zeitzone")
		return false
	}
	// Include the preceding two local days to find recordings crossing midnight.
	first := from.Add(-time.Duration((c.Offset + math.Max(0, c.ZoomOffset)) * float64(time.Second))).In(loc)
	first = time.Date(first.Year(), first.Month(), first.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -2)
	end := to.Add(-time.Duration((c.Offset + math.Min(0, c.ZoomOffset)) * float64(time.Second))).In(loc)
	if job.incremental {
		localDay, err := time.ParseInLocation("2006-01-02", job.date, loc)
		if err != nil {
			return false
		}
		first = localDay.AddDate(0, 0, -2)
		end = localDay
	}

	complete := true
	seen := map[string]bool{}
	for d := first; !d.After(end); d = d.AddDate(0, 0, 1) {
		p, e := partition(c, d)
		if job.all || c.IndexingMode == "full" {
			p = c.Folder
			e = nil
		}
		if e != nil {
			a.warn(c.Name + ": " + e.Error())
			complete = false
			continue
		}
		if !seen[p] {
			seen[p] = true
			if c.IndexingMode == "full" || job.all || job.incremental {
				complete = a.scanPartitionMode(c, p, 0, math.MaxInt64, job.incremental) && complete
			} else {
				complete = a.scanPartitionMode(c, p, from.Unix()-int64(math.Ceil(math.Abs(c.ZoomOffset))), to.Unix()+int64(math.Ceil(math.Abs(c.ZoomOffset))), job.incremental) && complete
			}
		}
	}
	if a.ctx.Err() == nil {
		now := time.Now().Unix()
		success := int64(0)
		if complete {
			success = now
		}
		_, dayErr := a.db.ExecContext(a.ctx, `INSERT INTO camera_days VALUES(?,?,?,?) ON CONFLICT(camera_id,date) DO UPDATE SET checked=excluded.checked,complete=excluded.complete`, c.ID, date, now, complete)
		_, scanErr := a.db.ExecContext(a.ctx, `INSERT INTO camera_scans(camera_id,attempt,success,complete) VALUES(?,?,?,?) ON CONFLICT(camera_id) DO UPDATE SET attempt=excluded.attempt,complete=excluded.complete,success=CASE WHEN excluded.success>0 THEN excluded.success ELSE camera_scans.success END`, c.ID, now, success, complete)
		if dayErr != nil || scanErr != nil {
			a.warn(c.Name + ": Scanstatus konnte nicht gespeichert werden")
			return false
		}
	}
	return complete && a.ctx.Err() == nil
}

func (a *App) scanPartition(c Camera, p string, from, to int64) bool {
	return a.scanPartitionMode(c, p, from, to, false)
}

// Read directories in bounded chunks. Unlike fs.WalkDir this never reads and sorts
// an entire large archive directory before processing the first video.
func (a *App) walkMedia(name string, depth int, visit func(string, fs.DirEntry) error) error {
	if a.ctx.Err() != nil {
		return a.ctx.Err()
	}
	if depth > 128 {
		return errors.New("Verzeichnisstruktur zu tief")
	}
	dir, e := a.root.Open(name)
	if e != nil {
		return e
	}
	defer dir.Close()
	for {
		entries, err := dir.ReadDir(128)
		for _, entry := range entries {
			if a.ctx.Err() != nil {
				return a.ctx.Err()
			}
			if entry.Type()&os.ModeSymlink != 0 {
				continue
			}
			child := path.Join(name, entry.Name())
			if e := visit(child, entry); e != nil {
				return e
			}
			if entry.IsDir() {
				if e := a.walkMedia(child, depth+1, visit); e != nil {
					return e
				}
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}
func (a *App) scanPartitionMode(c Camera, p string, from, to int64, incremental bool) bool {
	began := time.Now()
	inspected, updated, count := 0, 0, 0
	defer func() {
		slog.Info("Indexierung", "camera", c.ID, "incremental", incremental, "inspected", inspected, "updated", updated, "elapsed", time.Since(began))
	}()
	healthy := true
	seen := map[string]bool{}
	type mediaEntry struct {
		name string
		entry fs.DirEntry
	}
	type fileState struct{ size, mod int64 }
	batch := make([]mediaEntry, 0, 128)
	process := func(name string, d fs.DirEntry, old fileState, exists bool) error {
		inspected++
		a.progress(c, "inspected")
		skip := func(message string) { healthy = false; a.progress(c, "skipped"); a.warn(c.Name + ": " + message) }
		st, e := d.Info()
		if e != nil {
			skip("Datei nicht lesbar")
			return nil
		}
		relative := strings.TrimPrefix(name, c.Folder+"/")
		start, e := cameraTimestamp(c, relative)
		fallback := e != nil && cameraFormat(c) == "automatic" && strings.Contains(e.Error(), "Kein Zeitstempel")
		if e != nil && !fallback {
			skip(relative + ": " + e.Error())
			return nil
		}
		if !fallback && (start.Unix() >= to || start.Unix() < from-48*3600) {
			return nil
		}
		size, mod := st.Size(), st.ModTime().UnixNano()
		if exists && size == old.size && mod == old.mod {
			if incremental && time.Since(st.ModTime()) >= 120*time.Second {
				if _, e := a.db.ExecContext(a.ctx, `UPDATE recordings SET settled=1 WHERE camera_id=? AND path=? AND settled=0`, c.ID, name); e != nil {
					skip("Index konnte nicht gespeichert werden")
				}
			}
			return nil
		}
		if time.Since(st.ModTime()) < 30*time.Second || size == 0 {
			skip(relative + ": Aufnahme wird noch geschrieben")
			return nil
		}
		file, e := openSource(a.root, name)
		if e != nil {
			skip(relative + ": Datei nicht lesbar")
			return nil
		}
		duration, codec, created, probeErr := probeMedia(a.ctx, file)
		if fallback {
			if created.IsZero() {
				created = st.ModTime()
			}
			start = created.Add(time.Duration(c.Offset * float64(time.Second)))
		}
		after, statErr := file.Stat()
		file.Close()
		if a.ctx.Err() != nil {
			return a.ctx.Err()
		}
		if statErr != nil || after.Size() != size || after.ModTime().UnixNano() != mod {
			skip(relative + ": Aufnahme wird noch geschrieben")
			return nil
		}
		if probeErr != nil {
			skip(relative + ": Video beschädigt oder nicht lesbar")
			return nil
		}
		h := sha256.Sum256([]byte(c.ID + "\x00" + name))
		id := hex.EncodeToString(h[:16])
		_, e = a.db.ExecContext(a.ctx, `INSERT INTO recordings(id,camera_id,path,start,duration,codec,size,mod,filename) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(camera_id,path) DO UPDATE SET start=excluded.start,duration=excluded.duration,codec=excluded.codec,size=excluded.size,mod=excluded.mod,filename=excluded.filename,settled=0`, id, c.ID, name, float64(start.UnixNano())/1e9, duration, codec, size, mod, filepath.Base(name))
		if e != nil {
			skip("Index konnte nicht gespeichert werden")
			return nil
		}
		updated++
		if exists {
			a.progress(c, "changed")
		} else {
			a.progress(c, "added")
		}
		return nil
	}
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		placeholders := strings.TrimSuffix(strings.Repeat("?,", len(batch)), ",")
		args := make([]any, 0, len(batch)+1)
		args = append(args, c.ID)
		for _, item := range batch {
			args = append(args, item.name)
		}
		rows, err := a.db.QueryContext(a.ctx, `SELECT path,size,mod FROM recordings WHERE camera_id=? AND path IN (`+placeholders+`)`, args...)
		if err != nil {
			return err
		}
		states := make(map[string]fileState, len(batch))
		for rows.Next() {
			var name string
			var state fileState
			if err := rows.Scan(&name, &state.size, &state.mod); err != nil {
				rows.Close()
				return err
			}
			states[name] = state
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, item := range batch {
			state, exists := states[item.name]
			if err := process(item.name, item.entry, state, exists); err != nil {
				return err
			}
		}
		batch = batch[:0]
		return nil
	}
	err := a.walkMedia(p, 0, func(name string, d fs.DirEntry) error {
		count++
		if count > 2000000 {
			return errors.New("mehr als 2000000 Einträge; Tagesordner verwenden")
		}
		if !incremental {
			seen[name] = true
		}
		if d.IsDir() || !supportedMedia(name) {
			return nil
		}
		batch = append(batch, mediaEntry{name, d})
		if len(batch) == cap(batch) {
			return flush()
		}
		return nil
	})
	if err == nil {
		err = flush()
	}
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			if _, e := a.root.Stat(c.Folder); e == nil {
				return true
			}
			a.warn(c.Name + ": Quelle nicht erreichbar")
		} else {
			a.warn(c.Name + ": Ordner nicht vollständig lesbar")
		}
		return false
	}
	if !healthy || a.ctx.Err() != nil {
		return false
	}
	if incremental {
		return true
	}
	if !a.reconcile(c.ID, p, seen) {
		a.warn(c.Name + ": Index konnte nicht bereinigt werden")
		return false
	}
	return true
}

func (a *App) reconcile(camera, p string, seen map[string]bool) bool {
	rows, e := a.db.QueryContext(a.ctx, `SELECT id,path FROM recordings WHERE camera_id=? AND path>=? AND path<?`, camera, p+"/", p+"0")
	if e != nil {
		return false
	}
	ids := []string{}
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			rows.Close()
			return false
		}
		if (strings.HasPrefix(name, p+"/")) && !seen[name] {
			ids = append(ids, id)
		}
	}
	readErr := rows.Err()
	rows.Close()
	if readErr != nil {
		return false
	}
	for _, id := range ids {
		if _, e := a.db.ExecContext(a.ctx, `DELETE FROM recordings WHERE id=?`, id); e != nil {
			return false
		}
	}
	return true
}

const formats = "mov,matroska,webm,avi,mpegts,h264,hevc,dhav"

func probe(ctx context.Context, f *os.File) (float64, string, error) {
	duration, codec, _, e := probeMedia(ctx, f)
	return duration, codec, e
}
func probeMedia(ctx context.Context, f *os.File) (float64, string, time.Time, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-protocol_whitelist", "file,pipe,fd", "-format_whitelist", formats, "-show_entries", "format=duration:format_tags=creation_time:stream=codec_type,codec_name,duration:stream_tags=creation_time", "-of", "json", "/proc/self/fd/3")
	cmd.ExtraFiles = []*os.File{f}
	out, e := cmd.Output()
	if e != nil {
		return 0, "", time.Time{}, e
	}
	var info struct {
		Format struct {
			Duration string            `json:"duration"`
			Tags     map[string]string `json:"tags"`
		} `json:"format"`
		Streams []struct {
			CodecType string            `json:"codec_type"`
			CodecName string            `json:"codec_name"`
			Duration  string            `json:"duration"`
			Tags      map[string]string `json:"tags"`
		} `json:"streams"`
	}
	if e = json.Unmarshal(out, &info); e != nil {
		return 0, "", time.Time{}, e
	}
	var duration float64
	fmt.Sscan(info.Format.Duration, &duration)
	codec := ""
	for _, s := range info.Streams {
		if s.CodecType == "video" {
			codec = s.CodecName
			if duration <= 0 {
				fmt.Sscan(s.Duration, &duration)
			}
			break
		}
	}
	if codec == "" || math.IsNaN(duration) || math.IsInf(duration, 0) || duration <= 0 || duration > 48*3600 {
		return 0, "", time.Time{}, errors.New("ungültige Videodauer oder fehlende Videospur")
	}
	created, _ := time.Parse(time.RFC3339Nano, info.Format.Tags["creation_time"])
	if created.IsZero() {
		for _, stream := range info.Streams {
			if t, e := time.Parse(time.RFC3339Nano, stream.Tags["creation_time"]); e == nil {
				created = t
				break
			}
		}
	}
	return duration, codec, created, nil
}
