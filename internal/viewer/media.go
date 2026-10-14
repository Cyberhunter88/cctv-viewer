package viewer

import (
	"context"
	"errors"
	"fmt"
	"io"
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
	name := filepath.Join(a.config().DataDir, "cache", record.ID+"-"+strconv.FormatInt(record.Mod, 10)+".jpg")
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
	tmp, e := os.CreateTemp(filepath.Join(a.config().DataDir, "cache"), "thumb-*.jpg")
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
		Profile   string  `json:"profile"`
		Speed     float64 `json:"speed"`
		Transport string  `json:"transport"`
	}
	if !decode(w, r, &input) {
		return
	}
	if input.Profile == "" {
		input.Profile = "balanced"
	}
	if input.Speed == 0 {
		input.Speed = 1
	}
	if input.Transport == "" {
		input.Transport = "hls"
	}
	if (input.Profile != "balanced" && input.Profile != "fast") || (input.Transport != "hls" && input.Transport != "mp4") || !contains([]string{"0.25", "0.5", "1", "2", "4", "8", "16"}, strconv.FormatFloat(input.Speed, 'f', -1, 64)) {
		fail(w, 400, "Streamingprofil, Geschwindigkeit oder Transport ungültig")
		return
	}
	record, f, e := a.source(input.Recording)
	if e != nil {
		fail(w, 404, "Aufnahme nicht verfügbar")
		return
	}
	defer f.Close()
	if record.Kind == "image" {
		fail(w, 400, "Einzelbilder sind keine Videos")
		return
	}
	if math.IsNaN(input.Offset) || math.IsInf(input.Offset, 0) || input.Offset < 0 || input.Offset >= record.Duration {
		fail(w, 400, "Startposition ungültig")
		return
	}
	a.pruneStreams()
	a.pruneCache()
	if a.streamUsage() >= a.config().HLSTempBytes*9/10 {
		fail(w, 503, "Temporärer Videospeicher ausgelastet")
		return
	}
	if !a.acquireEncoder() {
		fail(w, 429, "Alle Umwandlungsplätze sind belegt. Weniger Kameras oder Originalwiedergabe wählen.")
		return
	}
	id := token()[:24]
	dir := filepath.Join(a.config().DataDir, "cache", "hls-"+id)
	if e = os.Mkdir(dir, 0700); e != nil {
		a.releaseEncoder()
		fail(w, 500, "Streaming konnte nicht gestartet werden")
		return
	}
	ctx, cancel := context.WithTimeout(a.ctx, 30*time.Minute)
	s := &stream{id: id, owner: a.streamOwner(r), dir: dir, cancel: cancel, done: make(chan struct{}), last: time.Now(), created: time.Now()}
	duration := math.Min(1800, record.Duration-input.Offset)
	profile := a.config().Balanced
	if input.Profile == "fast" {
		profile = a.config().Fast
	}
	factor := math.Max(1, input.Speed)
	vf := fmt.Sprintf("setpts=(PTS-STARTPTS)/%.3f,scale=trunc(iw*%d/100/2)*2:trunc(ih*%d/100/2)*2,fps=%d", factor, profile.Scale, profile.Scale, profile.FPS)
	args := []string{"-nostdin", "-v", "error", "-threads", "1", "-ss", fmt.Sprintf("%.3f", input.Offset), "-protocol_whitelist", "file,pipe,fd", "-format_whitelist", formats, "-i", "/proc/self/fd/3", "-t", fmt.Sprintf("%.3f", duration/factor), "-map", "0:v:0", "-an", "-vf", vf, "-c:v", "libx264", "-preset", "veryfast", "-threads", "1", "-crf", "25", "-maxrate", fmt.Sprintf("%dk", profile.Bitrate), "-bufsize", fmt.Sprintf("%dk", profile.Bitrate*2), "-pix_fmt", "yuv420p", "-force_key_frames", "expr:gte(t,n_forced*1)"}
	output := "index.m3u8"
	if input.Transport == "hls" {
		args = append(args, "-f", "hls", "-hls_time", "1", "-hls_list_size", "0", "-hls_playlist_type", "event", "-hls_segment_filename", filepath.Join(dir, "segment-%06d.ts"))
	} else {
		output = "video.mp4"
		args = append(args, "-f", "mp4", "-movflags", "+frag_keyframe+empty_moov+default_base_moof")
	}
	if input.Speed >= 8 && input.Offset == 0 {
		for i, arg := range args {
			if arg == "-i" {
				args = append(args[:i], append([]string{"-skip_frame", "nokey"}, args[i:]...)...)
				break
			}
		}
	}
	args = append(args, filepath.Join(dir, output))
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)

	cmd.ExtraFiles = []*os.File{f}
	if e = cmd.Start(); e != nil {
		cancel()
		os.RemoveAll(dir)
		a.releaseEncoder()
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
		a.releaseEncoder()
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
			ready := false
			if input.Transport == "hls" {
				if b, e := os.ReadFile(filepath.Join(dir, output)); e == nil {
					ready = strings.Count(string(b), "#EXTINF:") >= neededSegments(input.Speed)
				}
			} else {
				if st, e := os.Stat(filepath.Join(dir, output)); e == nil {
					ready = st.Size() > 4096
				}
			}
			select {
			case <-s.done:
				a.mediaMu.Lock()
				encodeErr := s.err
				a.mediaMu.Unlock()
				if encodeErr != nil {
					a.removeStream(id)
					fail(w, 422, "Aufnahme konnte nicht umgewandelt werden")
					return
				}
				if input.Transport == "hls" {
					if b, e := os.ReadFile(filepath.Join(dir, output)); e == nil {
						ready = strings.Contains(string(b), "#EXTINF:")
					}
				} else if st, e := os.Stat(filepath.Join(dir, output)); e == nil && st.Size() > 0 {
					ready = true
				}
			default:
			}
			if ready {
				jsonOut(w, 201, map[string]any{"id": id, "url": "/api/streams/" + id + "/" + output, "offset": input.Offset, "duration": duration, "factor": factor})
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

var hlsFile = regexp.MustCompile(`^(index\.m3u8|segment-[0-9]{6}\.ts|video\.mp4)$`)

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
	if name == "video.mp4" {
		w.Header().Set("Content-Type", "video/mp4")
		select {
		case <-s.done:
			http.ServeContent(w, r, name, st.ModTime(), f)
			return
		default:
		}
		// Tail fragmented MP4 while FFmpeg encodes. Seeks outside the buffer start a new stream.
		w.WriteHeader(200)
		if r.Method == "HEAD" {
			return
		}
		buffer := make([]byte, 64*1024)
		ticker := time.NewTicker(50 * time.Millisecond)
		defer ticker.Stop()
		for {
			n, e := f.Read(buffer)
			if n > 0 {
				if _, writeErr := w.Write(buffer[:n]); writeErr != nil {
					return
				}
				if flusher, ok := w.(http.Flusher); ok {
					flusher.Flush()
				}
			}
			if e != nil && e != io.EOF {
				return
			}
			if e == io.EOF {
				select {
				case <-r.Context().Done():
					return
				case <-s.done:
					io.Copy(w, f)
					return
				case <-ticker.C:
				}
			}
		}
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
	filepath.WalkDir(filepath.Join(a.config().DataDir, "cache"), func(p string, d fs.DirEntry, e error) error {
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
	filepath.WalkDir(filepath.Join(a.config().DataDir, "cache"), func(p string, d fs.DirEntry, e error) error {
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
		if used < a.config().CacheBytes*3/4 {
			break
		}
		if os.Remove(entry.path) == nil {
			used -= entry.size
		}
	}

}

func (a *App) streamUsage() int64 {
	a.mediaMu.Lock()
	dirs := []string{}
	for _, s := range a.streams {
		dirs = append(dirs, s.dir)
	}
	a.mediaMu.Unlock()
	var total int64
	for _, dir := range dirs {
		filepath.WalkDir(dir, func(p string, d fs.DirEntry, e error) error {
			if e == nil && !d.IsDir() {
				if st, e := d.Info(); e == nil {
					total += st.Size()
				}
			}
			return nil
		})
	}
	return total
}
func (a *App) enforceStreamBudget() {
	if a.streamUsage() <= a.config().HLSTempBytes {
		return
	}
	a.mediaMu.Lock()
	ordered := []*stream{}
	for _, s := range a.streams {
		ordered = append(ordered, s)
	}
	a.mediaMu.Unlock()
	sort.Slice(ordered, func(i, j int) bool {
		select {
		case <-ordered[i].done:
			select {
			case <-ordered[j].done:
			default:
				return true
			}
		default:
			select {
			case <-ordered[j].done:
				return false
			default:
			}
		}
		return ordered[i].created.Before(ordered[j].created)
	})
	for _, s := range ordered {
		if a.streamUsage() <= a.config().HLSTempBytes {
			break
		}
		a.removeStream(s.id)
	}
}

func neededSegments(speed float64) int {
	if speed >= 8 {
		return 4
	}
	if speed >= 4 {
		return 2
	}
	return 1
}
