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
// including temporary streams, exceeds three quarters of its budget. Directories of
// active streams are measured incrementally instead of being walked file by file.
func (a *App) pruneCache() {
	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()
	type entry struct {
		path string
		time time.Time
		size int64
	}
	active, used := a.measureStreams()
	skip := make(map[string]bool, len(active))
	for s := range active {
		skip[s.dir] = true
	}
	entries := []entry{}
	root := filepath.Join(a.config().DataDir, "cache")
	filepath.WalkDir(root, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return nil
		}
		if d.IsDir() {
			if skip[p] {
				return filepath.SkipDir
			}
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

// measureStreams reports the bytes every active stream occupies and their total.
func (a *App) measureStreams() (map[*stream]int64, int64) {
	a.mediaMu.Lock()
	active := make([]*stream, 0, len(a.streams))
	for _, s := range a.streams {
		active = append(active, s)
	}
	a.mediaMu.Unlock()
	a.usageMu.Lock()
	defer a.usageMu.Unlock()
	sizes := make(map[*stream]int64, len(active))
	var total int64
	for _, s := range active {
		size := s.measure()
		sizes[s] = size
		total += size
	}
	return sizes, total
}

// measure sums the files of the stream directory. Segments other than the newest
// one are complete, so their sizes are remembered and not read again. The caller
// holds App.usageMu.
func (s *stream) measure() int64 {
	entries, e := os.ReadDir(s.dir)
	if e != nil {
		return 0
	}
	if s.sizes == nil {
		s.sizes = map[string]int64{}
	}
	// ReadDir sorts by name, so the last segment is the one FFmpeg may be writing.
	newest := -1
	for i, entry := range entries {
		if isSegment(entry.Name()) {
			newest = i
		}
	}
	var total int64
	for i, entry := range entries {
		name := entry.Name()
		if size, ok := s.sizes[name]; ok {
			total += size
			continue
		}
		if entry.IsDir() {
			continue
		}
		st, e := entry.Info()
		if e != nil {
			continue
		}
		total += st.Size()
		if i != newest && isSegment(name) {
			s.sizes[name] = st.Size()
		}
	}
	return total
}

func isSegment(name string) bool {
	return strings.HasPrefix(name, "segment-") && strings.HasSuffix(name, ".ts")
}

func (a *App) streamUsage() int64 {
	_, total := a.measureStreams()
	return total
}

func (a *App) enforceStreamBudget() {
	sizes, total := a.measureStreams()
	budget := a.config().HLSTempBytes
	if total <= budget {
		return
	}
	ordered := make([]*stream, 0, len(sizes))
	for s := range sizes {
		ordered = append(ordered, s)
	}
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
		if total <= budget {
			break
		}
		a.removeStream(s.id)
		total -= sizes[s]
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
