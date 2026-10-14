package viewer

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type exportPart struct {
	record     Recording
	start, end float64
}
type exportInput struct {
	Camera string  `json:"camera"`
	From   float64 `json:"from"`
	To     float64 `json:"to"`
}
type exportJob struct {
	ID         string  `json:"id"`
	Status     string  `json:"status"`
	Progress   float64 `json:"progress"`
	Error      string  `json:"error,omitempty"`
	owner, dir string
	created    time.Time
	cancel     context.CancelFunc
	done       chan struct{}
	downloads  int
}

func (a *App) registerExports(m *http.ServeMux) {
	m.HandleFunc("POST /api/exports/preview", a.protect(false, a.previewExport))
	m.HandleFunc("POST /api/exports", a.protect(false, a.createExport))
	m.HandleFunc("GET /api/exports", a.protect(false, a.listExports))
	m.HandleFunc("GET /api/exports/{id}", a.protect(false, a.getExport))
	m.HandleFunc("GET /api/exports/{id}/file", a.protect(false, a.downloadExport))
	m.HandleFunc("DELETE /api/exports/{id}", a.protect(false, a.deleteExport))
}
func exportParts(records []Recording, from, to float64) ([]exportPart, [][2]float64) {
	boundaries := []float64{from, to}
	for _, r := range records {
		if r.Kind == "image" || r.Duration <= 0 {
			continue
		}
		if r.Start > from && r.Start < to {
			boundaries = append(boundaries, r.Start)
		}
		end := r.Start + r.Duration
		if end > from && end < to {
			boundaries = append(boundaries, end)
		}
	}
	sort.Float64s(boundaries)
	parts := []exportPart{}
	gaps := [][2]float64{}
	for i := 0; i < len(boundaries)-1; i++ {
		start, end := boundaries[i], boundaries[i+1]
		if end <= start {
			continue
		}
		var best *Recording
		for j := range records {
			r := &records[j]
			if r.Kind == "image" || r.Duration <= 0 || r.Start > start || r.Start+r.Duration <= start {
				continue
			}
			if best == nil || r.Start > best.Start || (r.Start == best.Start && r.ID < best.ID) {
				best = r
			}
		}
		if best == nil {
			if len(gaps) > 0 && gaps[len(gaps)-1][1] == start {
				gaps[len(gaps)-1][1] = end
			} else {
				gaps = append(gaps, [2]float64{start, end})
			}
			continue
		}
		if len(parts) > 0 && parts[len(parts)-1].record.ID == best.ID && parts[len(parts)-1].end == start {
			parts[len(parts)-1].end = end
		} else {
			parts = append(parts, exportPart{*best, start, end})
		}
	}
	return parts, gaps
}
func (a *App) exportPlan(w http.ResponseWriter, r *http.Request) (exportInput, []exportPart, bool) {
	var in exportInput
	if !decode(w, r, &in) {
		return in, nil, false
	}
	if math.IsNaN(in.From) || math.IsNaN(in.To) || math.IsInf(in.From, 0) || math.IsInf(in.To, 0) || in.From < 0 || in.To <= in.From || in.To-in.From > 1800 {
		fail(w, 400, "Zeitraum muss zwischen 0 und 30 Minuten liegen")
		return in, nil, false
	}
	if _, e := a.db.Camera(in.Camera); e != nil {
		fail(w, 404, "Kamera nicht gefunden")
		return in, nil, false
	}
	records, e := a.db.Recordings(in.Camera, in.From, in.To)
	if e != nil {
		fail(w, 500, "Aufnahmen nicht lesbar")
		return in, nil, false
	}
	if len(records) > 2000 {
		fail(w, 400, "Zu viele Aufnahmen. Kürzeren Zeitraum wählen.")
		return in, nil, false
	}
	parts, gaps := exportParts(records, in.From, in.To)
	if len(gaps) > 0 {
		jsonOut(w, 422, map[string]any{"error": "Zeitraum enthält Videolücken. Bitte Start oder Ende anpassen.", "gaps": gaps})
		return in, nil, false
	}
	if len(parts) > 1000 {
		fail(w, 400, "Zu viele Dateigrenzen. Kürzeren Zeitraum wählen.")
		return in, nil, false
	}
	return in, parts, true
}
func (a *App) previewExport(w http.ResponseWriter, r *http.Request) {
	_, parts, ok := a.exportPlan(w, r)
	if ok {
		jsonOut(w, 200, map[string]any{"ok": true, "parts": len(parts)})
	}
}
func (a *App) createExport(w http.ResponseWriter, r *http.Request) {
	in, parts, ok := a.exportPlan(w, r)
	if !ok {
		return
	}
	a.pruneExports()
	a.pruneCache()
	if a.cacheUsage() >= a.config().CacheBytes*9/10 {
		fail(w, 503, "Temporärer Videospeicher ausgelastet")
		return
	}
	if !a.acquireEncoder() {
		fail(w, 429, "Alle Umwandlungsplätze belegt. Wiedergabe pausieren oder später versuchen.")
		return
	}
	id := token()[:24]
	dir := filepath.Join(a.config().DataDir, "cache", "export-"+id)
	if e := os.Mkdir(dir, 0700); e != nil {
		<-a.slots
		fail(w, 500, "Export konnte nicht gestartet werden")
		return
	}
	ctx, cancel := context.WithTimeout(a.ctx, time.Hour)
	job := &exportJob{ID: id, Status: "running", owner: a.streamOwner(r), dir: dir, created: time.Now(), cancel: cancel, done: make(chan struct{})}
	a.exportMu.Lock()
	a.exports[id] = job
	a.exportMu.Unlock()
	a.work.Add(1)
	go func() {
		defer a.work.Done()
		defer close(job.done)
		defer func() { <-a.slots; cancel() }()
		e := a.encodeExport(ctx, job, parts, in.To-in.From)
		a.exportMu.Lock()
		if e != nil {
			job.Status = "failed"
			job.Error = "Export fehlgeschlagen, abgebrochen oder Speicherbudget überschritten."
		} else {
			job.Status = "ready"
			job.Progress = 100
		}
		a.exportMu.Unlock()
		if e != nil {
			os.RemoveAll(dir)
		}
	}()
	jsonOut(w, 202, map[string]any{"id": id, "status": "running", "progress": 0})
}
func jobView(j *exportJob) map[string]any {
	return map[string]any{"id": j.ID, "status": j.Status, "progress": j.Progress, "error": j.Error}
}
func (a *App) listExports(w http.ResponseWriter, r *http.Request) {
	a.exportMu.Lock()
	defer a.exportMu.Unlock()
	out := []map[string]any{}
	for _, j := range a.exports {
		if j.owner == a.streamOwner(r) {
			out = append(out, jobView(j))
		}
	}
	jsonOut(w, 200, out)
}
func (a *App) getExport(w http.ResponseWriter, r *http.Request) {
	a.exportMu.Lock()
	defer a.exportMu.Unlock()
	j := a.exports[r.PathValue("id")]
	if j == nil || j.owner != a.streamOwner(r) {
		fail(w, 404, "Export nicht verfügbar")
		return
	}
	jsonOut(w, 200, jobView(j))
}
func (a *App) downloadExport(w http.ResponseWriter, r *http.Request) {
	a.exportMu.Lock()
	j := a.exports[r.PathValue("id")]
	if j == nil || j.owner != a.streamOwner(r) || j.Status != "ready" {
		a.exportMu.Unlock()
		fail(w, 404, "Export nicht verfügbar")
		return
	}
	j.downloads++
	a.exportMu.Unlock()
	defer func() { a.exportMu.Lock(); j.downloads--; a.exportMu.Unlock() }()
	f, e := os.Open(filepath.Join(j.dir, "clip.mp4"))
	if e != nil {
		fail(w, 404, "Export nicht verfügbar")
		return
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil {
		fail(w, 500, "Export nicht lesbar")
		return
	}
	w.Header().Set("Content-Type", "video/mp4")
	w.Header().Set("Content-Disposition", `attachment; filename="cctv-clip.mp4"`)
	http.ServeContent(w, r, "cctv-clip.mp4", st.ModTime(), f)
}
func (a *App) deleteExport(w http.ResponseWriter, r *http.Request) {
	a.exportMu.Lock()
	j := a.exports[r.PathValue("id")]
	if j == nil || j.owner != a.streamOwner(r) {
		a.exportMu.Unlock()
		fail(w, 404, "Export nicht verfügbar")
		return
	}
	if j.downloads > 0 {
		a.exportMu.Unlock()
		fail(w, 409, "Download läuft noch")
		return
	}
	delete(a.exports, j.ID)
	j.cancel()
	a.exportMu.Unlock()
	<-j.done
	os.RemoveAll(j.dir)
	jsonOut(w, 200, map[string]bool{"ok": true})
}
func (a *App) pruneExports() {
	a.exportMu.Lock()
	var jobs []*exportJob
	for id, j := range a.exports {
		if time.Since(j.created) > time.Hour && j.downloads == 0 {
			delete(a.exports, id)
			j.cancel()
			jobs = append(jobs, j)
		}
	}
	a.exportMu.Unlock()
	for _, j := range jobs {
		<-j.done
		os.RemoveAll(j.dir)
	}
}
func (a *App) trimExports() {
	a.exportMu.Lock()
	jobs := []*exportJob{}
	for _, j := range a.exports {
		if j.downloads == 0 && j.Status != "failed" {
			jobs = append(jobs, j)
		}
	}
	a.exportMu.Unlock()
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].created.Before(jobs[j].created) })
	for _, j := range jobs {
		if a.cacheUsage() < a.config().CacheBytes*3/4 {
			break
		}
		a.exportMu.Lock()
		if j.downloads > 0 || j.Status == "failed" {
			a.exportMu.Unlock()
			continue
		}
		j.Status = "failed"
		j.Error = "Export wegen ausgeschöpftem Cachebudget entfernt."
		j.cancel()
		a.exportMu.Unlock()
		<-j.done
		os.RemoveAll(j.dir)
		a.exportMu.Lock()
		j.Status = "failed"
		j.Error = "Export wegen ausgeschöpftem Cachebudget entfernt."
		a.exportMu.Unlock()
	}
}
func exportDimensions(ctx context.Context, f *os.File) (int, int, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-protocol_whitelist", "file,pipe,fd", "-format_whitelist", formats, "-select_streams", "v:0", "-show_entries", "stream=width,height", "-of", "json", "/proc/self/fd/3")
	cmd.ExtraFiles = []*os.File{f}
	out, e := cmd.Output()
	if e != nil {
		return 0, 0, e
	}
	var info struct{ Streams []struct{ Width, Height int } }
	if e = json.Unmarshal(out, &info); e != nil || len(info.Streams) != 1 {
		return 0, 0, errors.New("Videogröße fehlt")
	}
	w, h := info.Streams[0].Width, info.Streams[0].Height
	if w < 2 || h < 2 || w > 16384 || h > 16384 {
		return 0, 0, errors.New("Videogröße ungültig")
	}
	return (w + 1) / 2 * 2, (h + 1) / 2 * 2, nil
}
func (a *App) encodeExport(ctx context.Context, j *exportJob, parts []exportPart, total float64) error {
	width, height := 0, 0
	elapsed := 0.0
	var list strings.Builder
	for i, p := range parts {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		_, f, e := a.source(p.record.ID)
		if e != nil {
			return e
		}
		if i == 0 {
			width, height, e = exportDimensions(ctx, f)
			if e != nil {
				f.Close()
				return e
			}
			f.Seek(0, 0)
		}
		filename := fmt.Sprintf("part-%04d.ts", i)
		output := filepath.Join(j.dir, filename)
		filter := fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease,pad=%d:%d:(ow-iw)/2:(oh-ih)/2,setsar=1,fps=30", width, height, width, height)
		cmd := exec.CommandContext(ctx, "ffmpeg", "-nostdin", "-v", "error", "-threads", "1", "-ss", fmt.Sprintf("%.6f", p.start-p.record.Start), "-protocol_whitelist", "file,pipe,fd", "-format_whitelist", formats, "-i", "/proc/self/fd/3", "-t", fmt.Sprintf("%.6f", p.end-p.start), "-map", "0:v:0", "-an", "-vf", filter, "-c:v", "libx264", "-preset", "veryfast", "-crf", "18", "-pix_fmt", "yuv420p", "-threads", "1", "-progress", "pipe:1", "-f", "mpegts", output)
		cmd.ExtraFiles = []*os.File{f}
		pipe, e := cmd.StdoutPipe()
		if e != nil {
			f.Close()
			return e
		}
		if e = cmd.Start(); e != nil {
			f.Close()
			return e
		}
		scan := bufio.NewScanner(pipe)
		for scan.Scan() {
			line := scan.Text()
			if strings.HasPrefix(line, "out_time_us=") {
				n, _ := strconv.ParseFloat(strings.TrimPrefix(line, "out_time_us="), 64)
				a.exportMu.Lock()
				j.Progress = math.Min(95, (elapsed+math.Min(n/1e6, p.end-p.start))/total*95)
				a.exportMu.Unlock()
			}
		}
		e = cmd.Wait()
		f.Close()
		if e != nil {
			return e
		}
		elapsed += p.end - p.start
		fmt.Fprintf(&list, "file '%s'\n", filename)
		if a.cacheUsage() >= a.config().CacheBytes {
			return errors.New("Cachebudget überschritten")
		}
	}
	if e := os.WriteFile(filepath.Join(j.dir, "parts.txt"), []byte(list.String()), 0600); e != nil {
		return e
	}
	cmd := exec.CommandContext(ctx, "ffmpeg", "-nostdin", "-v", "error", "-protocol_whitelist", "file,pipe", "-f", "concat", "-safe", "1", "-i", filepath.Join(j.dir, "parts.txt"), "-map", "0:v:0", "-an", "-c", "copy", "-movflags", "+faststart", filepath.Join(j.dir, "clip.mp4"))
	if e := cmd.Run(); e != nil {
		return e
	}
	for i := range parts {
		os.Remove(filepath.Join(j.dir, fmt.Sprintf("part-%04d.ts", i)))
	}
	os.Remove(filepath.Join(j.dir, "parts.txt"))
	if a.cacheUsage() >= a.config().CacheBytes {
		return errors.New("Cachebudget überschritten")
	}
	return nil
}
