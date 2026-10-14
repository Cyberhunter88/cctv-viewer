package viewer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path"
	"sort"
	"strings"
	"sync"
	"time"
)

type ScanStatus struct {
	Running  bool     `json:"running"`
	Date     string   `json:"date"`
	Camera   string   `json:"camera"`
	Indexed  int      `json:"indexed"`
	Warnings []string `json:"warnings"`
	Finished string   `json:"finished,omitempty"`
}
type indexer struct {
	mu     sync.Mutex
	status ScanStatus
	last   map[string]time.Time
}

func (a *App) scanStatus() ScanStatus {
	a.index.mu.Lock()
	defer a.index.mu.Unlock()
	s := a.index.status
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
func (a *App) startScan(date string, force bool) bool {
	if a.ctx.Err() != nil {
		return false
	}
	a.index.mu.Lock()
	defer a.index.mu.Unlock()
	if a.index.status.Running || (!force && time.Since(a.index.last[date]) < time.Minute) {
		return false
	}
	a.index.status = ScanStatus{Running: true, Date: date, Warnings: []string{}}
	a.work.Add(1)
	go func() {
		defer a.work.Done()
		a.scanDay(date)
		a.index.mu.Lock()
		a.index.status.Running = false
		a.index.status.Finished = time.Now().UTC().Format(time.RFC3339)
		a.index.last[date] = time.Now()
		if len(a.index.last) > 100 {
			for k, v := range a.index.last {
				if time.Since(v) > time.Hour {
					delete(a.index.last, k)
				}
			}
		}
		a.index.mu.Unlock()
	}()
	return true
}
func dayBounds(date string) (time.Time, time.Time, error) {
	loc, _ := time.LoadLocation("Europe/Berlin")
	from, e := time.ParseInLocation("2006-01-02", date, loc)
	if e != nil || from.Year() < 1970 || from.Year() > 2200 {
		return time.Time{}, time.Time{}, errors.New("Datum ungültig")
	}
	return from, from.AddDate(0, 0, 1), nil
}
func (a *App) scanDay(date string) {
	from, to, e := dayBounds(date)
	if e != nil {
		return
	}
	cams, e := a.db.Cameras()
	if e != nil {
		a.warn("Kameraliste nicht lesbar")
		return
	}
	for _, c := range cams {
		if a.ctx.Err() != nil {
			return
		}
		a.index.mu.Lock()
		a.index.status.Camera = c.Name
		a.index.mu.Unlock()
		loc, e := time.LoadLocation(c.Timezone)
		if e != nil {
			continue
		}
		// Include the preceding two local days to find recordings crossing midnight.
		first := from.Add(-time.Duration(c.Offset) * time.Second).In(loc)
		first = time.Date(first.Year(), first.Month(), first.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -2)
		end := to.Add(-time.Duration(c.Offset) * time.Second).In(loc)
		seen := map[string]bool{}
		for d := first; !d.After(end); d = d.AddDate(0, 0, 1) {
			p, e := partition(c, d)
			if e != nil {
				a.warn(c.Name + ": " + e.Error())
				continue
			}
			if !seen[p] {
				seen[p] = true
				a.scanPartition(c, p, from.Unix(), to.Unix())
			}
		}
	}
}
func (a *App) scanPartition(c Camera, p string, from, to int64) {
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
		if count > 200000 {
			return errors.New("mehr als 200000 Einträge; Tagesordner verwenden")
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
		st, e := d.Info()
		if e != nil {
			return e
		}
		candidates = append(candidates, candidate{name, st})
		return nil
	})
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) { // A removed partition is reconciled; an offline mount must remain mounted at the host.
			if _, e := a.root.Stat(c.Folder); e == nil {
				a.reconcile(c.ID, p, map[string]bool{})
			} else {
				a.warn(c.Name + ": Quelle nicht erreichbar")
			}
			return
		}
		a.warn(c.Name + ": Ordner nicht vollständig lesbar")
		return
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].path < candidates[j].path })
	seen := map[string]bool{}
	for _, item := range candidates {
		if a.ctx.Err() != nil {
			return
		}
		seen[item.path] = true
		relative := strings.TrimPrefix(item.path, c.Folder+"/")
		start, e := cameraTimestamp(c, relative)
		if e != nil {
			a.warn(c.Name + ": " + relative + ": " + e.Error())
			continue
		}
		// Avoid probing historic files outside the requested window, except the preceding day.
		if start.Unix() >= to || start.Unix() < from-48*3600 {
			continue
		}
		size, mod := item.info.Size(), item.info.ModTime().UnixNano()
		var oldSize, oldMod int64
		e = a.db.QueryRowContext(a.ctx, `SELECT size,mod FROM recordings WHERE camera_id=? AND path=?`, c.ID, item.path).Scan(&oldSize, &oldMod)
		if e == nil && size == oldSize && mod == oldMod {
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
		var duration float64
		var codec string
		if strings.EqualFold(path.Ext(item.path), ".jpg") || strings.EqualFold(path.Ext(item.path), ".jpeg") {
			e = validateJPEG(file)
			codec = "jpeg"
		} else {
			duration, codec, e = probe(a.ctx, file)
		}
		after, se := file.Stat()
		file.Close()
		if e != nil || se != nil || after.Size() != size || after.ModTime().UnixNano() != mod {
			a.warn(c.Name + ": " + relative + ": noch unvollständig oder Bild/Video nicht lesbar")
			continue
		}
		h := sha256.Sum256([]byte(c.ID + "\x00" + item.path))
		id := hex.EncodeToString(h[:16])
		_, e = a.db.ExecContext(a.ctx, `INSERT INTO recordings VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(camera_id,path) DO UPDATE SET start=excluded.start,duration=excluded.duration,codec=excluded.codec,size=excluded.size,mod=excluded.mod`, id, c.ID, item.path, float64(start.Unix()), duration, codec, size, mod)
		if e != nil {
			a.warn(c.Name + ": Index konnte nicht gespeichert werden")
			continue
		}
		a.index.mu.Lock()
		a.index.status.Indexed++
		a.index.mu.Unlock()
	}
	a.reconcile(c.ID, p, seen)
}
func (a *App) reconcile(camera, p string, seen map[string]bool) {
	rows, e := a.db.QueryContext(a.ctx, `SELECT id,path FROM recordings WHERE camera_id=?`, camera)
	if e != nil {
		return
	}
	ids := []string{}
	for rows.Next() {
		var id, name string
		if rows.Scan(&id, &name) == nil && (strings.HasPrefix(name, p+"/")) && !seen[name] {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		a.db.ExecContext(a.ctx, `DELETE FROM recordings WHERE id=?`, id)
	}
}

const formats = "mov,matroska,webm,avi,mpegts,h264,hevc"

func probe(ctx context.Context, f *os.File) (float64, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-protocol_whitelist", "file,pipe,fd", "-format_whitelist", formats, "-show_entries", "format=duration:stream=codec_type,codec_name,duration", "-of", "json", "/proc/self/fd/3")
	cmd.ExtraFiles = []*os.File{f}
	out, e := cmd.Output()
	if e != nil {
		return 0, "", e
	}
	var info struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
		Streams []struct {
			CodecType string `json:"codec_type"`
			CodecName string `json:"codec_name"`
			Duration  string `json:"duration"`
		} `json:"streams"`
	}
	if e = json.Unmarshal(out, &info); e != nil {
		return 0, "", e
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
		return 0, "", errors.New("ungültige Videodauer oder fehlende Videospur")
	}
	return duration, codec, nil
}
