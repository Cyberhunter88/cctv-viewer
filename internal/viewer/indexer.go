package viewer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type ScanStatus struct {
	Active    []string `json:"active"`
	Completed int      `json:"completed"`
	Total     int      `json:"total"`
	Running   bool     `json:"running"`
	Date      string   `json:"date"`
	Camera    string   `json:"camera"`
	Indexed   int      `json:"indexed"`
	Warnings  []string `json:"warnings"`
	Finished  string   `json:"finished,omitempty"`
}
type scanJob struct {
	date, camera, batch string
	all, incremental    bool
}
type indexer struct {
	current   scanJob
	queue     []scanJob
	scheduled bool
	mu        sync.Mutex
	status    ScanStatus
	last      map[string]time.Time
}

func (a *App) scanStatus() ScanStatus {
	a.index.mu.Lock()
	defer a.index.mu.Unlock()
	s := a.index.status
	s.Active = append([]string{}, s.Active...)
	s.Warnings = append([]string{}, s.Warnings...)
	return s
}
func (a *App) warn(s string) {
	a.index.mu.Lock()
	defer a.index.mu.Unlock()
	if len(a.index.status.Warnings) < 30 {
		a.index.status.Warnings = append(a.index.status.Warnings, s)
	}
}
func (a *App) startScan(date string, force bool) bool { return a.queueScan(scanJob{date: date}, force) }
func (a *App) queueScan(job scanJob, force bool) bool {
	if a.ctx.Err() != nil {
		return false
	}
	a.index.mu.Lock()
	defer a.index.mu.Unlock()
	key := job.date + "/" + job.camera
	if !force && time.Since(a.index.last[key]) < time.Minute {
		return false
	}
	if a.index.status.Running && a.index.current == job {
		return false
	}
	for _, q := range a.index.queue {
		if q == job {
			return false
		}
	}
	if len(a.index.queue) >= 32 {
		return false
	}
	if a.index.status.Running {
		a.index.queue = append(a.index.queue, job)
		return true
	}
	a.index.current = job
	a.index.status = ScanStatus{Running: true, Date: job.date, Warnings: []string{}}
	a.work.Add(1)
	go func() {
		defer a.work.Done()
		current := job
		for {
			a.scanJob(current)
			if current.incremental && a.ctx.Err() == nil {
				a.db.Exec(`INSERT INTO scan_schedule VALUES(?,?) ON CONFLICT(camera_id) DO UPDATE SET next_run=excluded.next_run`, current.camera, time.Now().Add(time.Duration(a.config().AutoScanMinutes)*time.Minute).Unix())
			}
			a.index.mu.Lock()
			if !current.incremental {
				a.index.last[current.date+"/"+current.camera] = time.Now()
			}
			if len(a.index.last) > 1000 {
				for k, v := range a.index.last {
					if time.Since(v) > time.Hour {
						delete(a.index.last, k)
					}
				}
			}
			if len(a.index.queue) == 0 || a.ctx.Err() != nil {
				a.index.status.Running = false
				a.index.status.Finished = time.Now().UTC().Format(time.RFC3339)
				a.index.mu.Unlock()
				return
			}
			current = a.index.queue[0]
			a.index.current = current
			a.index.queue = a.index.queue[1:]
			a.index.status = ScanStatus{Running: true, Date: current.date, Warnings: []string{}}
			a.index.mu.Unlock()
		}
	}()
	return true
}
func (a *App) runScheduledScan() {
	cfg := a.config()
	a.index.mu.Lock()
	busy := a.index.status.Running || len(a.index.queue) > 0
	wasEnabled := a.index.scheduled
	a.index.scheduled = cfg.AutoScan
	a.index.mu.Unlock()
	if !cfg.AutoScan {
		return
	}
	if !wasEnabled {
		a.db.Exec(`DELETE FROM scan_schedule`)
	}
	if busy {
		return
	}
	cams, e := a.db.Cameras()
	if e != nil {
		return
	}
	for _, c := range cams {
		if c.IndexingMode == "full" {
			continue
		}
		var next int64
		a.db.QueryRow(`SELECT next_run FROM scan_schedule WHERE camera_id=?`, c.ID).Scan(&next)
		if next > time.Now().Unix() {
			continue
		}
		loc, e := time.LoadLocation(c.Timezone)
		if e != nil {
			continue
		}
		a.queueScan(scanJob{date: time.Now().In(loc).Format("2006-01-02"), camera: c.ID, incremental: true}, true)
		return
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
			a.index.status.Active = append(a.index.status.Active, c.Name)
			a.index.status.Camera = strings.Join(a.index.status.Active, " · ")
			a.index.mu.Unlock()
			defer func() {
				a.index.mu.Lock()
				for i, name := range a.index.status.Active {
					if name == c.Name {
						a.index.status.Active = append(a.index.status.Active[:i], a.index.status.Active[i+1:]...)
						break
					}
				}
				a.index.status.Completed++
				a.index.status.Camera = strings.Join(a.index.status.Active, " · ")
				a.index.mu.Unlock()
			}()
			a.scanCamera(job, c, from, to)
		}(c)
	}
	workers.Wait()
}
func (a *App) scanCamera(job scanJob, c Camera, from, to time.Time) {
	date := job.date
	loc, e := time.LoadLocation(c.Timezone)
	if e != nil {
		return
	}
	// Include the preceding two local days to find recordings crossing midnight.
	first := from.Add(-time.Duration((c.Offset + math.Max(0, c.ZoomOffset)) * float64(time.Second))).In(loc)
	first = time.Date(first.Year(), first.Month(), first.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -2)
	end := to.Add(-time.Duration((c.Offset + math.Min(0, c.ZoomOffset)) * float64(time.Second))).In(loc)
	if job.incremental {
		first = time.Date(time.Now().In(loc).Year(), time.Now().In(loc).Month(), time.Now().In(loc).Day(), 0, 0, 0, 0, loc)
		end = first
	}
	complete := true
	seen := map[string]bool{}
	for d := first; !d.After(end); d = d.AddDate(0, 0, 1) {
		p, e := partition(c, d)
		if job.all {
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
		a.db.ExecContext(a.ctx, `INSERT INTO camera_days VALUES(?,?,?,?) ON CONFLICT(camera_id,date) DO UPDATE SET checked=excluded.checked,complete=excluded.complete`, c.ID, date, now, complete)
		a.db.ExecContext(a.ctx, `INSERT INTO camera_scans(camera_id,attempt,success,complete) VALUES(?,?,?,?) ON CONFLICT(camera_id) DO UPDATE SET attempt=excluded.attempt,complete=excluded.complete,success=CASE WHEN excluded.success>0 THEN excluded.success ELSE camera_scans.success END`, c.ID, now, success, complete)
	}
}

func (a *App) scanPartition(c Camera, p string, from, to int64) bool {
	return a.scanPartitionMode(c, p, from, to, false)
}
func (a *App) scanPartitionMode(c Camera, p string, from, to int64, incremental bool) bool {
	began := time.Now()
	inspected := 0
	updated := 0
	defer func() {
		slog.Info("Indexierung", "camera", c.ID, "incremental", incremental, "inspected", inspected, "updated", updated, "elapsed", time.Since(began))
	}()
	settled := map[string]bool{}
	if incremental {
		rows, e := a.db.Query(`SELECT path FROM recordings WHERE camera_id=? AND settled=1`, c.ID)
		if e != nil {
			return false
		}
		for rows.Next() {
			var name string
			rows.Scan(&name)
			settled[name] = true
		}
		rows.Close()
	}
	type candidate struct {
		path string
		info fs.FileInfo
	}
	candidates := []candidate{}
	count := 0
	err := fs.WalkDir(a.root.FS(), p, func(name string, d fs.DirEntry, walkErr error) error {
		if a.ctx.Err() != nil {
			return a.ctx.Err()
		}
		if walkErr != nil {
			return walkErr
		}
		count++
		if count > 2000000 {
			return errors.New("mehr als 2000000 Einträge; Tagesordner verwenden")
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !supportedMedia(name) {
			return nil
		}
		if settled[name] {
			candidates = append(candidates, candidate{path: name})
			return nil
		}
		inspected++
		st, e := d.Info()
		if e != nil {
			return e
		}
		candidates = append(candidates, candidate{name, st})
		return nil
	})
	if err != nil {
		if !incremental && errors.Is(err, fs.ErrNotExist) { // A removed partition is reconciled; an offline mount must remain mounted at the host.
			if _, e := a.root.Stat(c.Folder); e == nil {
				return a.reconcile(c.ID, p, map[string]bool{})
			} else {
				a.warn(c.Name + ": Quelle nicht erreichbar")
			}
			return false
		}
		a.warn(c.Name + ": Ordner nicht vollständig lesbar")
		return false
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].path < candidates[j].path })
	healthy := true
	seen := map[string]bool{}
	for _, item := range candidates {
		if a.ctx.Err() != nil {
			return false
		}
		seen[item.path] = true
		relative := strings.TrimPrefix(item.path, c.Folder+"/")
		if item.info == nil {
			continue
		}
		start, e := cameraTimestamp(c, relative)
		fallback := e != nil && cameraFormat(c) == "automatic" && strings.Contains(e.Error(), "Kein Zeitstempel")
		if e != nil && !fallback {
			a.warn(c.Name + ": " + relative + ": " + e.Error())
			continue
		}
		// Avoid probing historic files outside the requested window, except the preceding day.
		if !fallback && (start.Unix() >= to || start.Unix() < from-48*3600) {
			continue
		}
		size, mod := item.info.Size(), item.info.ModTime().UnixNano()
		var oldSize, oldMod int64
		e = a.db.QueryRowContext(a.ctx, `SELECT size,mod FROM recordings WHERE camera_id=? AND path=?`, c.ID, item.path).Scan(&oldSize, &oldMod)
		if e == nil && size == oldSize && mod == oldMod {
			if incremental && time.Since(item.info.ModTime()) >= 120*time.Second {
				a.db.ExecContext(a.ctx, `UPDATE recordings SET settled=1 WHERE camera_id=? AND path=?`, c.ID, item.path)
			}
			continue
		}
		// Growing and recently written files must settle before probing.
		if time.Since(item.info.ModTime()) < 30*time.Second || size == 0 {
			continue
		}
		file, e := openSource(a.root, item.path)
		if e != nil {
			a.warn(c.Name + ": Datei nicht lesbar")
			continue
		}
		duration, codec, created, e := probeMedia(a.ctx, file)
		if fallback {
			if created.IsZero() {
				created = item.info.ModTime()
			}
			start = created.Add(time.Duration(c.Offset * float64(time.Second)))
		}
		after, se := file.Stat()
		file.Close()
		if e != nil || se != nil || after.Size() != size || after.ModTime().UnixNano() != mod {
			a.warn(c.Name + ": " + relative + ": noch unvollständig oder Bild/Video nicht lesbar")
			continue
		}
		h := sha256.Sum256([]byte(c.ID + "\x00" + item.path))
		id := hex.EncodeToString(h[:16])
		_, e = a.db.ExecContext(a.ctx, `INSERT INTO recordings(id,camera_id,path,start,duration,codec,size,mod,filename) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(camera_id,path) DO UPDATE SET start=excluded.start,duration=excluded.duration,codec=excluded.codec,size=excluded.size,mod=excluded.mod,filename=excluded.filename,settled=0`, id, c.ID, item.path, float64(start.UnixNano())/1e9, duration, codec, size, mod, filepath.Base(item.path))
		if e != nil {
			healthy = false
			a.warn(c.Name + ": Index konnte nicht gespeichert werden")
			continue
		}
		a.index.mu.Lock()
		a.index.status.Indexed++
		updated++
		a.index.mu.Unlock()
	}
	if incremental {
		return healthy
	}
	return a.reconcile(c.ID, p, seen) && healthy
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
