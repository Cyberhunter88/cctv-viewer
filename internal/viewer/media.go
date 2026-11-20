package viewer

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// errRecordingChanged reports that the file differs from its index entry, which
// is normal while a recorder still writes it.
var errRecordingChanged = errors.New("Aufnahme wird gerade geschrieben oder wurde geändert")

// sourceFailure answers a failed source lookup: a recording that is still
// being written is a temporary condition the client retries, not a missing file.
func sourceFailure(w http.ResponseWriter, e error, missing string) {
	if errors.Is(e, errRecordingChanged) {
		w.Header().Set("Retry-After", "5")
		fail(w, http.StatusConflict, e.Error())
		return
	}
	fail(w, 404, missing)
}

type stream struct {
	id, owner, dir string
	cancel         context.CancelFunc
	done           chan struct{}
	last           time.Time
	created        time.Time
	// fetched turns true with the first playlist or video request. A stream whose
	// creating request was abandoned after the answer is never fetched.
	fetched bool
	err     error
	// sourceChanged is set when the recording was modified while ffmpeg read it,
	// so the output may end before the real end of the file.
	sourceChanged bool
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
		if e == nil {
			a.reindexSettled(r, st)
		}
		return r, nil, errRecordingChanged
	}
	return r, f, nil
}

// reindexSettled queues a rescan of the recording's day once the recorder has
// left the file alone long enough, so a finished clip becomes playable without
// waiting for the next scheduled scan. Files still being written are skipped.
func (a *App) reindexSettled(r Recording, st os.FileInfo) {
	if time.Since(st.ModTime()) < 30*time.Second || st.Size() == 0 {
		return
	}
	c, e := a.db.Camera(r.CameraID)
	if e != nil {
		return
	}
	loc, e := cachedLocation(c.Timezone)
	if e != nil {
		return
	}
	date := time.Unix(0, int64(r.Start*1e9)).In(loc).Format("2006-01-02")
	a.queueScan(scanJob{date: date, camera: c.ID, incremental: true}, false)
}

// sourceChangedSince reports whether the file no longer matches the index
// entry it was verified against when the stream started, for example because a
// recorder kept appending while the encoder read it.
func (a *App) sourceChangedSince(r Recording) bool {
	f, e := openSource(a.root, r.Path)
	if e != nil {
		return true
	}
	defer f.Close()
	st, e := f.Stat()
	return e != nil || st.Size() != r.Size || st.ModTime().UnixNano() != r.Mod
}
func (a *App) file(w http.ResponseWriter, r *http.Request) {
	record, f, e := a.source(r.PathValue("id"))
	if e != nil {
		sourceFailure(w, e, "Aufnahme nicht verfügbar; Index gegebenenfalls aktualisieren")
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
	w.Header().Set("Cache-Control", "private, no-cache")
	http.ServeContent(w, r, "recording.mp4", time.Unix(0, record.Mod), f)
}
func (a *App) thumbnail(w http.ResponseWriter, r *http.Request) {
	record, f, e := a.source(r.PathValue("id"))
	if e != nil {
		sourceFailure(w, e, "Vorschau nicht verfügbar")
		return
	}
	defer f.Close()
	// Optional position inside the recording, rounded down to ten-second steps
	// so hovering a timeline reuses a bounded number of cached images.
	offset := 0
	if raw := r.URL.Query().Get("t"); raw != "" {
		position, e := strconv.ParseFloat(raw, 64)
		if e != nil || math.IsNaN(position) || math.IsInf(position, 0) || position < 0 || position >= record.Duration {
			fail(w, 400, "Vorschauposition ungültig")
			return
		}
		offset = int(position/10) * 10
	}
	key := record.ID + "-" + strconv.FormatInt(record.Mod, 10)
	if offset > 0 {
		key += "-" + strconv.Itoa(offset)
	}
	name := filepath.Join(a.config().DataDir, "cache", key+".jpg")
	etag := `"` + key + `"`
	// The session is checked on every request; the browser may only reuse its
	// private copy after revalidating it here.
	serveCached := func() bool {
		if _, e := os.Stat(name); e != nil {
			return false
		}
		w.Header().Set("ETag", etag)
		w.Header().Set("Cache-Control", "private, no-cache")
		now := time.Now()
		os.Chtimes(name, now, now)
		if etagMatches(r.Header.Get("If-None-Match"), etag) {
			w.WriteHeader(http.StatusNotModified)
			return true
		}
		b, e := os.ReadFile(name)
		if e != nil {
			w.Header().Del("ETag")
			w.Header().Set("Cache-Control", "no-store")
			return false
		}
		w.Header().Set("Content-Type", "image/jpeg")
		w.Write(b)
		return true
	}
	if serveCached() {
		return
	}
	// Browsers request several previews at once. Queue them instead of failing
	// all but the first.
	wait := time.NewTimer(15 * time.Second)
	defer wait.Stop()
	select {
	case a.thumbSlots <- struct{}{}:
		defer func() { <-a.thumbSlots }()
	case <-r.Context().Done():
		return
	case <-a.ctx.Done():
		fail(w, 503, "Vorschau wird vorbereitet")
		return
	case <-wait.C:
		fail(w, 503, "Vorschau wird vorbereitet")
		return
	}
	if serveCached() {
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
	args := []string{"-nostdin", "-v", "error", "-threads", "1"}
	if offset > 0 {
		args = append(args, "-ss", strconv.Itoa(offset))
	}
	args = append(args, "-protocol_whitelist", "file,pipe,fd", "-format_whitelist", formats, "-i", "/proc/self/fd/3", "-frames:v", "1", "-vf", "scale=320:-2", "-threads", "1", "-y", tmp.Name())
	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
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
	if !serveCached() {
		fail(w, 503, "Vorschaucache ausgelastet")
	}
}

// etagMatches implements the weak comparison used by If-None-Match.
func etagMatches(header, etag string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimPrefix(strings.TrimSpace(candidate), "W/")
		if candidate == "*" || candidate == etag {
			return true
		}
	}
	return false
}

// Use factors no greater than two to preserve pitch and avoid dropping samples.
// Slower playback is handled by the browser, just like the video track.
func audioTempo(factor float64) string {
	filters := []string{"asetpts=PTS-STARTPTS"}
	for factor > 2 {
		filters = append(filters, "atempo=2")
		factor /= 2
	}
	filters = append(filters, fmt.Sprintf("atempo=%.3f", factor))
	return strings.Join(filters, ",")
}
