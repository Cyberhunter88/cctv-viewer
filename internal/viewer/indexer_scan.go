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

func (a *App) scanCamera(job scanJob, c Camera, from, to time.Time) bool {
	date := job.date
	loc, e := cachedLocation(c.Timezone)
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
	type mediaEntry struct {
		name  string
		entry fs.DirEntry
	}
	type fileState struct {
		size, mod int64
		settled   int
	}
	batch := make([]mediaEntry, 0, 128)
	// One file of a batch: plan decides without side effects what has to happen,
	// probes run concurrently, and apply records the outcome in directory order.
	type pending struct {
		name, relative, skip string
		exists, ignore       bool
		settle, fallback     bool
		file                 *os.File
		info                 fs.FileInfo
		start                time.Time
		size, mod            int64
		duration             float64
		codec                string
		created              time.Time
		probeErr, statErr    error
		after                fs.FileInfo
	}
	plan := func(name string, d fs.DirEntry, old fileState, exists bool) *pending {
		item := &pending{name: name, exists: exists}
		st, e := d.Info()
		if e != nil {
			item.skip = "Datei nicht lesbar"
			return item
		}
		item.info = st
		item.relative = strings.TrimPrefix(name, c.Folder+"/")
		start, e := cameraTimestamp(c, item.relative)
		item.start = start
		item.fallback = e != nil && cameraFormat(c) == "automatic" && strings.Contains(e.Error(), "Kein Zeitstempel")
		if e != nil && !item.fallback {
			item.skip = item.relative + ": " + e.Error()
			return item
		}
		if !item.fallback && (start.Unix() >= to || start.Unix() < from-48*3600) {
			item.ignore = true
			return item
		}
		item.size, item.mod = st.Size(), st.ModTime().UnixNano()
		if exists && item.size == old.size && item.mod == old.mod {
			item.ignore = true
			item.settle = incremental && old.settled == 0 && time.Since(st.ModTime()) >= 120*time.Second
			return item
		}
		if time.Since(st.ModTime()) < 30*time.Second || item.size == 0 {
			item.skip = item.relative + ": Aufnahme wird noch geschrieben"
			return item
		}
		file, e := openSource(a.root, name)
		if e != nil {
			item.skip = item.relative + ": Datei nicht lesbar"
			return item
		}
		item.file = file
		return item
	}
	apply := func(item *pending) error {
		inspected++
		a.progress(c, "inspected")
		skip := func(message string) { healthy = false; a.progress(c, "skipped"); a.warn(c.Name + ": " + message) }
		if item.skip != "" {
			skip(item.skip)
			return nil
		}
		if item.ignore {
			if item.settle {
				if _, e := a.db.ExecContext(a.ctx, `UPDATE recordings SET settled=1 WHERE camera_id=? AND path=? AND settled=0`, c.ID, item.name); e != nil {
					skip("Index konnte nicht gespeichert werden")
				}
			}
			return nil
		}
		start := item.start
		if item.fallback {
			created := item.created
			if created.IsZero() {
				created = item.info.ModTime()
			}
			start = created.Add(time.Duration(c.Offset * float64(time.Second)))
		}
		if a.ctx.Err() != nil {
			return a.ctx.Err()
		}
		if item.statErr != nil || item.after.Size() != item.size || item.after.ModTime().UnixNano() != item.mod {
			skip(item.relative + ": Aufnahme wird noch geschrieben")
			return nil
		}
		if item.probeErr != nil {
			skip(item.relative + ": Video beschädigt oder nicht lesbar")
			return nil
		}
		h := sha256.Sum256([]byte(c.ID + "\x00" + item.name))
		id := hex.EncodeToString(h[:16])
		_, e := a.db.ExecContext(a.ctx, `INSERT INTO recordings(id,camera_id,path,start,duration,codec,size,mod,filename) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(camera_id,path) DO UPDATE SET start=excluded.start,duration=excluded.duration,codec=excluded.codec,size=excluded.size,mod=excluded.mod,filename=excluded.filename,settled=0`, id, c.ID, item.name, float64(start.UnixNano())/1e9, item.duration, item.codec, item.size, item.mod, filepath.Base(item.name))
		if e != nil {
			skip("Index konnte nicht gespeichert werden")
			return nil
		}
		updated++
		if item.exists {
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
		rows, err := a.db.reader().QueryContext(a.ctx, `SELECT path,size,mod,settled FROM recordings WHERE camera_id=? AND path IN (`+placeholders+`)`, args...)
		if err != nil {
			return err
		}
		states := make(map[string]fileState, len(batch))
		for rows.Next() {
			var name string
			var state fileState
			if err := rows.Scan(&name, &state.size, &state.mod, &state.settled); err != nil {
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
		planned := make([]*pending, 0, len(batch))
		slots := make(chan struct{}, probeWorkers)
		var probes sync.WaitGroup
		for _, entry := range batch {
			state, exists := states[entry.name]
			item := plan(entry.name, entry.entry, state, exists)
			planned = append(planned, item)
			if item.file == nil {
				continue
			}
			slots <- struct{}{}
			probes.Add(1)
			go func() {
				defer probes.Done()
				defer func() { <-slots }()
				item.duration, item.codec, item.created, item.probeErr = scanProbe(a.ctx, item.file)
				item.after, item.statErr = item.file.Stat()
				item.file.Close()
			}()
		}
		probes.Wait()
		batch = batch[:0]
		for _, item := range planned {
			if err := apply(item); err != nil {
				return err
			}
		}
		return nil
	}
	err := a.walkMedia(p, 0, func(name string, d fs.DirEntry) error {
		count++
		if count > 2000000 {
			return errors.New("mehr als 2000000 Einträge; Tagesordner verwenden")
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
	return true
}

const formats = "mov,matroska,webm,avi,mpegts,h264,hevc,dhav"

// Media files of one camera are inspected by this many ffprobe processes at once.
const probeWorkers = 2

// scanProbe inspects one media file during indexing; tests replace it to cover
// the indexing logic without media tools.
var scanProbe = probeMedia

// Longer media is rejected while indexing. Time-range queries rely on this
// bound to limit how far before a range a recording can start.
const maxRecordingSeconds = 48 * 3600

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
	if codec == "" || math.IsNaN(duration) || math.IsInf(duration, 0) || duration <= 0 || duration > maxRecordingSeconds {
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
