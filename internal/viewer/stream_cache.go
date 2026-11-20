package viewer

import (
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

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
	s.fetched = true
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
		// The playlist grows while FFmpeg encodes. A cacheable or conditional
		// answer would hand the player a stale list and stall it at the end.
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Cache-Control", "no-store")
		http.ServeContent(w, r, name, time.Time{}, f)
		return
	}
	// Finished segments never change, so the player may reuse them when seeking back.
	w.Header().Set("Content-Type", "video/mp2t")
	w.Header().Set("Cache-Control", "private, max-age=300")
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

// unfetchedStreamTTL ends a stream nobody requested after it was created, for example
// because the page went away while the answer was on its way. It frees the encoder slot
// long before the regular 90 s idle limit.
const unfetchedStreamTTL = 15 * time.Second

func (a *App) pruneStreams() {
	a.mediaMu.Lock()
	ids := []string{}
	for id, s := range a.streams {
		if time.Since(s.last) > 90*time.Second || time.Since(s.created) > 30*time.Minute || !s.fetched && time.Since(s.created) > unfetchedStreamTTL {
			ids = append(ids, id)
		}
	}
	a.mediaMu.Unlock()
	for _, id := range ids {
		a.removeStream(id)
	}
}

// pruneCache removes the least recently used previews once the cache directory,
// including temporary streams, exceeds three quarters of its budget.
func (a *App) pruneCache() {
	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()
	type entry struct {
		path string
		time time.Time
		size int64
	}
	entries := []entry{}
	var used int64
	filepath.WalkDir(filepath.Join(a.config().DataDir, "cache"), func(p string, d fs.DirEntry, e error) error {
		if e != nil || d.IsDir() {
			return nil
		}
		st, e := d.Info()
		if e != nil {
			return nil
		}
		used += st.Size()
		if strings.HasSuffix(p, ".jpg") {
			entries = append(entries, entry{p, st.ModTime(), st.Size()})
		}
		return nil
	})
	limit := a.config().CacheBytes * 3 / 4
	if used < limit {
		return
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].time.Before(entries[j].time) })
	for _, entry := range entries {
		if used < limit {
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
