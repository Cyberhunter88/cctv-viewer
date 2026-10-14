package viewer

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type stream struct {
	id, owner, dir string
	cancel         context.CancelFunc
	done           chan struct{}
	last           time.Time
	created        time.Time
	err            error
}

func (a *App) source(id string) (Recording, *os.File, error) {
	r, e := a.db.Recording(id)
	if e != nil {
		return r, nil, e
	}
	f, e := openSource(a.root, r.Path)
	if e != nil {
		return r, nil, e
	}
	st, e := f.Stat()
	if e != nil || st.Size() != r.Size || st.ModTime().UnixNano() != r.Mod {
		f.Close()
		return r, nil, errors.New("Aufnahme wurde geändert; Index bitte aktualisieren")
	}
	return r, f, nil
}
func (a *App) file(w http.ResponseWriter, r *http.Request) {
	record, f, e := a.source(r.PathValue("id"))
	if e != nil {
		fail(w, 404, "Aufnahme nicht verfügbar; Index gegebenenfalls aktualisieren")
		return
	}
	defer f.Close()
	if record.Codec != "h264" && record.Codec != "hevc" {
		fail(w, 415, "Kompatible Wiedergabe auswählen")
		return
	}
	ext := strings.ToLower(filepath.Ext(record.Path))
	if ext != ".mp4" && ext != ".mov" {
		fail(w, 415, "Dieses Format benötigt kompatible Wiedergabe")
		return
	}
	w.Header().Set("Content-Type", "video/mp4")
	http.ServeContent(w, r, "recording.mp4", time.Unix(0, record.Mod), f)
}
func (a *App) thumbnail(w http.ResponseWriter, r *http.Request) {
	record, f, e := a.source(r.PathValue("id"))
	if e != nil {
		fail(w, 404, "Vorschau nicht verfügbar")
		return
	}
	defer f.Close()
	name := filepath.Join(a.cfg.DataDir, "cache", record.ID+"-"+strconv.FormatInt(record.Mod, 10)+".jpg")
	if b, e := os.ReadFile(name); e == nil {
		now := time.Now()
		os.Chtimes(name, now, now)
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(b)
		return
	}
	select {
	case a.thumbSlots <- struct{}{}:
		defer func() { <-a.thumbSlots }()
	default:
		fail(w, 503, "Vorschau wird vorbereitet")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	tmp, e := os.CreateTemp(filepath.Join(a.cfg.DataDir, "cache"), "thumb-*.jpg")
	if e != nil {
		fail(w, 500, "Vorschau fehlgeschlagen")
		return
	}
	tmp.Close()
	defer os.Remove(tmp.Name())
	cmd := exec.CommandContext(ctx, "ffmpeg", "-nostdin", "-v", "error", "-threads", "1", "-protocol_whitelist", "file,pipe,fd", "-format_whitelist", formats, "-i", "/proc/self/fd/3", "-frames:v", "1", "-vf", "scale=320:-2", "-threads", "1", "-y", tmp.Name())
	cmd.ExtraFiles = []*os.File{f}
	if e = cmd.Run(); e != nil {
		fail(w, 422, "Vorschau nicht lesbar")
		return
	}
	if e = os.Rename(tmp.Name(), name); e != nil {
		fail(w, 500, "Vorschau fehlgeschlagen")
		return
	}
	a.pruneCache()
	b, e := os.ReadFile(name)
	if e != nil {
		fail(w, 503, "Vorschaucache ausgelastet")
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Write(b)
}
func (a *App) streamOwner(r *http.Request) string {
	c, e := r.Cookie("ctv_session")
	if e != nil {
		return ""
	}
	return digest(c.Value)
}
func (a *App) createStream(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Recording string  `json:"recording"`
		Offset    float64 `json:"offset"`
	}
	if !decode(w, r, &input) {
		return
	}
	record, f, e := a.source(input.Recording)
	if e != nil {
		fail(w, 404, "Aufnahme nicht verfügbar")
		return
	}
	defer f.Close()
	if math.IsNaN(input.Offset) || math.IsInf(input.Offset, 0) || input.Offset < 0 || input.Offset >= record.Duration {
		fail(w, 400, "Startposition ungültig")
		return
	}
	a.pruneStreams()
	a.pruneCache()
	if a.cacheUsage() >= a.cfg.CacheBytes*9/10 {
		fail(w, 503, "Temporärer Videospeicher ausgelastet")
		return
	}
	select {
	case a.slots <- struct{}{}:
	default:
		fail(w, 429, "Alle Umwandlungsplätze sind belegt. Weniger Kameras oder Originalwiedergabe wählen.")
		return
	}
	id := token()[:24]
	dir := filepath.Join(a.cfg.DataDir, "cache", "hls-"+id)
	if e = os.Mkdir(dir, 0700); e != nil {
		<-a.slots
		fail(w, 500, "Streaming konnte nicht gestartet werden")
		return
	}
	ctx, cancel := context.WithTimeout(a.ctx, 30*time.Minute)
	s := &stream{id: id, owner: a.streamOwner(r), dir: dir, cancel: cancel, done: make(chan struct{}), last: time.Now(), created: time.Now()}
	duration := math.Min(1800, record.Duration-input.Offset)
	cmd := exec.CommandContext(ctx, "ffmpeg", "-nostdin", "-v", "error", "-threads", "1", "-ss", fmt.Sprintf("%.3f", input.Offset), "-protocol_whitelist", "file,pipe,fd", "-format_whitelist", formats, "-i", "/proc/self/fd/3", "-t", fmt.Sprintf("%.3f", duration), "-map", "0:v:0", "-an", "-vf", "scale='min(1280,iw)':-2", "-c:v", "libx264", "-preset", "veryfast", "-threads", "1", "-crf", "25", "-maxrate", "2M", "-bufsize", "4M", "-pix_fmt", "yuv420p", "-force_key_frames", "expr:gte(t,n_forced*2)", "-f", "hls", "-hls_time", "2", "-hls_list_size", "0", "-hls_playlist_type", "event", "-hls_segment_filename", filepath.Join(dir, "segment-%06d.ts"), filepath.Join(dir, "index.m3u8"))
	cmd.ExtraFiles = []*os.File{f}
	if e = cmd.Start(); e != nil {
		cancel()
		os.RemoveAll(dir)
		<-a.slots
		fail(w, 500, "Video-Umwandlung konnte nicht gestartet werden")
		return
	}
	a.mediaMu.Lock()
	a.streams[id] = s
	a.mediaMu.Unlock()
	a.work.Add(1)
	go func() {
		defer a.work.Done()
		e := cmd.Wait()
		cancel()
		a.mediaMu.Lock()
		s.err = e
		a.mediaMu.Unlock()
		<-a.slots
		close(s.done)
	}()
	// Wait for the first complete HLS segment, so clients never receive an empty playlist.
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(20 * time.Second)
	defer timeout.Stop()
	for {
		select {
		case <-r.Context().Done():
			a.removeStream(id)
			return
		case <-timeout.C:
			a.removeStream(id)
			fail(w, 504, "Video-Umwandlung dauert zu lange")
			return
		case <-ticker.C:
			if b, e := os.ReadFile(filepath.Join(dir, "index.m3u8")); e == nil && strings.Contains(string(b), "#EXTINF:") {
				jsonOut(w, 201, map[string]any{"id": id, "url": "/api/streams/" + id + "/index.m3u8", "offset": input.Offset, "duration": duration})
				return
			}
			select {
			case <-s.done:
				a.mediaMu.Lock()
				err := s.err
				a.mediaMu.Unlock()
				if err != nil {
					a.removeStream(id)
					fail(w, 422, "Aufnahme konnte nicht umgewandelt werden")
					return
				}
			default:
			}
		}
	}
}

var hlsFile = regexp.MustCompile(`^(index\.m3u8|segment-[0-9]{6}\.ts)$`)

func (a *App) streamFile(w http.ResponseWriter, r *http.Request) {
	id, name := r.PathValue("id"), r.PathValue("file")
	if !hlsFile.MatchString(name) {
		http.NotFound(w, r)
		return
	}
	a.mediaMu.Lock()
	s := a.streams[id]
	if s == nil || s.owner != a.streamOwner(r) {
		a.mediaMu.Unlock()
		fail(w, 404, "Streaming-Sitzung nicht verfügbar")
		return
	}
	s.last = time.Now()
	dir := s.dir
	a.mediaMu.Unlock()
	f, e := os.Open(filepath.Join(dir, name))
	if e != nil {
		fail(w, 404, "Streaming-Segment nicht verfügbar")
		return
	}
	defer f.Close()
	st, e := f.Stat()
	if e != nil {
		return
	}
	if strings.HasSuffix(name, ".m3u8") {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	} else {
		w.Header().Set("Content-Type", "video/mp2t")
	}
	http.ServeContent(w, r, name, st.ModTime(), f)
}
func (a *App) deleteStream(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	a.mediaMu.Lock()
	s := a.streams[id]
	owned := s != nil && s.owner == a.streamOwner(r)
	a.mediaMu.Unlock()
	if !owned {
		fail(w, 404, "Streaming-Sitzung nicht verfügbar")
		return
	}
	a.removeStream(id)
	jsonOut(w, 200, map[string]bool{"ok": true})
}
func (a *App) removeStream(id string) {
	a.mediaMu.Lock()
	s := a.streams[id]
	if s != nil {
		delete(a.streams, id)
		s.cancel()
	}
	a.mediaMu.Unlock()
	if s != nil {
		<-s.done
		os.RemoveAll(s.dir)
	}
}
func (a *App) pruneStreams() {
	a.mediaMu.Lock()
	ids := []string{}
	for id, s := range a.streams {
		if time.Since(s.last) > 90*time.Second || time.Since(s.created) > 30*time.Minute {
			ids = append(ids, id)
		}
	}
	a.mediaMu.Unlock()
	for _, id := range ids {
		a.removeStream(id)
	}
}
func (a *App) cacheUsage() int64 {
	var total int64
	filepath.WalkDir(filepath.Join(a.cfg.DataDir, "cache"), func(p string, d fs.DirEntry, e error) error {
		if e == nil && !d.IsDir() {
			if s, e := d.Info(); e == nil {
				total += s.Size()
			}
		}
		return nil
	})
	return total
}
func (a *App) pruneCache() {
	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()
	type entry struct {
		path string
		time time.Time
		size int64
	}
	entries := []entry{}
	filepath.WalkDir(filepath.Join(a.cfg.DataDir, "cache"), func(p string, d fs.DirEntry, e error) error {
		if e == nil && !d.IsDir() && strings.HasSuffix(p, ".jpg") {
			if st, e := d.Info(); e == nil {
				entries = append(entries, entry{p, st.ModTime(), st.Size()})
			}
		}
		return nil
	})
	sort.Slice(entries, func(i, j int) bool { return entries[i].time.Before(entries[j].time) })
	used := a.cacheUsage()
	for _, entry := range entries {
		if used < a.cfg.CacheBytes*3/4 {
			break
		}
		if os.Remove(entry.path) == nil {
			used -= entry.size
		}
	}
	if used > a.cfg.CacheBytes {
		a.mediaMu.Lock()
		ids := []string{}
		for id := range a.streams {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool { return a.streams[ids[i]].created.Before(a.streams[ids[j]].created) })
		a.mediaMu.Unlock()
		for _, id := range ids {
			if a.cacheUsage() < a.cfg.CacheBytes*3/4 {
				break
			}
			a.removeStream(id)
		}
	}
}
