package viewer

import (
	"context"
	"math"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func viewerDefaults(c Config) Config {
	if c.Balanced.Scale == 0 {
		c.Balanced = StreamProfile{50, 15, 1200}
	}
	if c.Fast.Scale == 0 {
		c.Fast = StreamProfile{30, 8, 450}
	}
	if c.AutoScanMinutes == 0 {
		c.AutoScanMinutes = 60
	}
	if c.HLSTempBytes == 0 {
		c.HLSTempBytes = 256 << 20
	}
	return c
}
func preferenceOwner(s Session) string { return digest(s.Kind + "\x00" + s.Subject) }
func (a *App) registerViewerRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/search", a.protect(false, a.searchRecordings))
	m.HandleFunc("POST /api/scan", a.protect(true, a.requestScan))
	m.HandleFunc("POST /api/index/rebuild", a.protect(true, a.rebuildIndex))
	m.HandleFunc("GET /api/playback-status", a.protect(false, a.playbackStatus))
	m.HandleFunc("GET /api/playback-sessions/{id}", a.protect(false, a.playbackSession))
}
func (a *App) requestScan(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Date   string `json:"date"`
		Camera string `json:"camera"`
		All    bool   `json:"all"`
	}
	if !decode(w, r, &in) {
		return
	}
	if in.Date == "" {
		in.Date = time.Now().In(mustBerlin()).Format("2006-01-02")
	}
	if _, _, e := dayBounds(in.Date); e != nil {
		fail(w, 400, e.Error())
		return
	}
	if in.Camera != "" {
		if _, e := a.db.Camera(in.Camera); e != nil {
			fail(w, 404, "Kamera nicht gefunden")
			return
		}
	}
	if !a.queueScan(scanJob{date: in.Date, camera: in.Camera, all: in.All, manual: true}, true) {
		fail(w, 409, "Indexauftrag bereits aktiv oder Warteschlange voll")
		return
	}
	jsonOut(w, 202, a.scanStatus())
}
func mustBerlin() *time.Location { l, _ := time.LoadLocation("Europe/Berlin"); return l }
func (a *App) rebuildIndex(w http.ResponseWriter, r *http.Request) {
	a.index.mu.Lock()
	defer a.index.mu.Unlock()
	if a.index.status.Running || len(a.index.queue) > 0 {
		fail(w, 409, "Indexierung läuft")
		return
	}
	tx, e := a.db.Begin()
	if e != nil {
		fail(w, 500, "Index nicht verfügbar")
		return
	}
	defer tx.Rollback()
	for _, table := range []string{"recordings", "camera_days", "camera_scans", "scan_schedule"} {
		if _, e = tx.Exec("DELETE FROM " + table); e != nil {
			fail(w, 500, "Index konnte nicht geleert werden")
			return
		}
	}
	if e = tx.Commit(); e != nil {
		fail(w, 500, "Index konnte nicht geleert werden")
		return
	}
	a.index.last = map[string]time.Time{}
	a.cacheMu.Lock()
	entries, _ := os.ReadDir(filepath.Join(a.config().DataDir, "cache"))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".jpg") {
			os.Remove(filepath.Join(a.config().DataDir, "cache", entry.Name()))
		}
	}
	a.cacheMu.Unlock()
	jsonOut(w, 200, map[string]bool{"ok": true})
}
func (a *App) searchRecordings(w http.ResponseWriter, r *http.Request) {
	// Apply the same zoom clock correction as the day API before filtering or sorting.
	relativePath := "lower(substr(r.path,length(c.folder)+2))"
	effectiveStart := "(r.start+CASE WHEN c.view_mode='dual' AND c.zoom_marker!='' AND instr(" + relativePath + ",lower(c.zoom_marker))>0 AND (c.wide_marker='' OR instr(" + relativePath + ",lower(c.wide_marker))=0) THEN c.zoom_offset ELSE 0 END)"

	q := r.URL.Query()
	conditions := []string{"r.codec!='jpeg'"}
	args := []any{}
	if text := q.Get("q"); text != "" {
		if len(text) > 300 {
			fail(w, 400, "Suchtext zu lang")
			return
		}
		conditions = append(conditions, "(r.filename LIKE ? ESCAPE '\\' OR c.name LIKE ? ESCAPE '\\')")
		text = strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_").Replace(text)
		args = append(args, "%"+text+"%", "%"+text+"%")
	}
	if id := q.Get("camera"); id != "" {
		conditions = append(conditions, "r.camera_id=?")
		args = append(args, id)
	}
	for _, key := range []string{"from", "to", "minDuration"} {
		if raw := q.Get(key); raw != "" {
			n, e := strconv.ParseFloat(raw, 64)
			if e != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 {
				fail(w, 400, "Suchfilter ungültig")
				return
			}
			expr := map[string]string{"from": effectiveStart + "+r.duration>=?", "to": effectiveStart + "<=?", "minDuration": "r.duration>=?"}[key]
			conditions = append(conditions, expr)
			args = append(args, n)
			if key == "to" {
				// Keep the original zoom-adjusted predicate, but let the start index
				// discard recordings that cannot possibly precede the upper bound.
				conditions = append(conditions, "r.start<=?+(SELECT COALESCE(MAX(ABS(zoom_offset)),0) FROM cameras)")
				args = append(args, n)
			}
		}
	}
	const limit = 100
	offset := 0
	if raw := q.Get("offset"); raw != "" {
		n, e := strconv.Atoi(raw)
		if e != nil || n < 0 {
			fail(w, 400, "Suchoffset ungültig")
			return
		}
		offset = n
	}
	where := strings.Join(conditions, " AND ")
	var total int
	if e := a.db.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM recordings r JOIN cameras c ON c.id=r.camera_id WHERE `+where, args...).Scan(&total); e != nil {
		fail(w, 500, "Suche nicht verfügbar")
		return
	}
	queryArgs := append(append([]any{}, args...), limit, offset)
	rows, e := a.db.QueryContext(r.Context(), `SELECT r.id,r.camera_id,c.name,r.path,`+effectiveStart+`,r.duration FROM recordings r JOIN cameras c ON c.id=r.camera_id WHERE `+where+` ORDER BY `+effectiveStart+` DESC,r.id LIMIT ? OFFSET ?`, queryArgs...)
	if e != nil {
		fail(w, 500, "Suche nicht verfügbar")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var id, cam, name, file string
		var start, duration float64
		if e = rows.Scan(&id, &cam, &name, &file, &start, &duration); e != nil {
			fail(w, 500, "Suche fehlgeschlagen")
			return
		}
		out = append(out, map[string]any{"id": id, "cameraId": cam, "cameraName": name, "filename": path.Base(file), "start": start, "duration": duration})
	}
	if rows.Err() != nil {
		fail(w, 500, "Suche fehlgeschlagen")
		return
	}
	jsonOut(w, 200, map[string]any{
		"results": out,
		"total":   total,
		"offset":  offset,
		"limit":   limit,
		"hasMore": offset+len(out) < total,
	})
}
func (a *App) playbackStatus(w http.ResponseWriter, r *http.Request) {
	cfg := a.config()
	a.mediaMu.Lock()
	count := len(a.streams)
	active := a.encoderActive
	a.mediaMu.Unlock()
	jsonOut(w, 200, map[string]any{"sessions": count, "encoding": active, "limit": cfg.MaxTranscoders, "temporaryBytes": a.streamUsage(), "temporaryBudget": cfg.HLSTempBytes})
}
func (a *App) playbackSession(w http.ResponseWriter, r *http.Request) {
	a.mediaMu.Lock()
	defer a.mediaMu.Unlock()
	s := a.streams[r.PathValue("id")]
	if s == nil || s.owner != a.streamOwner(r) {
		fail(w, 404, "Sitzung nicht gefunden")
		return
	}
	state := "encoding"
	select {
	case <-s.done:
		state = "complete"
		if s.err != nil {
			state = "failed"
		}
	default:
	}
	jsonOut(w, 200, map[string]any{"id": s.id, "state": state, "created": s.created, "lastAccess": s.last})
}
func (a *App) previewTimestamp(ctx context.Context, c Camera, name, relative string) (time.Time, error) {
	t, e := cameraTimestamp(c, relative)
	if e == nil || cameraFormat(c) != "automatic" {
		return t, e
	}
	if !strings.Contains(e.Error(), "Kein Zeitstempel") {
		return t, e
	}
	f, e := openSource(a.root, name)
	if e != nil {
		return t, e
	}
	defer f.Close()
	_, _, created, e := probeMedia(ctx, f)
	if e != nil {
		return t, e
	}
	if created.IsZero() {
		st, e := f.Stat()
		if e != nil {
			return t, e
		}
		created = st.ModTime()
	}
	return created.Add(time.Duration(c.Offset * float64(time.Second))), nil
}
