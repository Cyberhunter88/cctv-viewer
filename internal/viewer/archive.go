package viewer

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"strings"
	"time"
)

// Include the login kind and (for OIDC) the issuer already present in Subject.
func bookmarkOwner(s Session) string {
	h := sha256.Sum256([]byte(s.Kind + "\x00" + s.Subject))
	return hex.EncodeToString(h[:])
}

type Bookmark struct {
	ID      string   `json:"id"`
	Time    float64  `json:"time"`
	Cameras []string `json:"cameras"`
	Note    string   `json:"note"`
}

func (a *App) registerArchiveRoutes(m *http.ServeMux) {
	m.HandleFunc("GET /api/calendar", a.protect(false, a.calendar))
	m.HandleFunc("GET /api/camera-status", a.protect(false, a.cameraStatus))
	m.HandleFunc("GET /api/bookmarks", a.protect(false, a.listBookmarks))
	m.HandleFunc("POST /api/bookmarks", a.protect(false, a.saveBookmark))
	m.HandleFunc("PUT /api/bookmarks/{id}", a.protect(false, a.saveBookmark))
	m.HandleFunc("DELETE /api/bookmarks/{id}", a.protect(false, a.deleteBookmark))
	m.HandleFunc("GET /api/recorded-day", a.protect(false, a.recordedDay))
}
func (a *App) listBookmarks(w http.ResponseWriter, r *http.Request) {
	rows, e := a.db.Query(`SELECT id,time,cameras,note FROM bookmarks WHERE owner=? ORDER BY time DESC,id`, bookmarkOwner(r.Context().Value(sessionKey).(Session)))
	if e != nil {
		fail(w, 500, "Lesezeichen nicht lesbar")
		return
	}
	defer rows.Close()
	out := []Bookmark{}
	for rows.Next() {
		var b Bookmark
		var cameras string
		if e = rows.Scan(&b.ID, &b.Time, &cameras, &b.Note); e != nil {
			fail(w, 500, "Lesezeichen nicht lesbar")
			return
		}
		if json.Unmarshal([]byte(cameras), &b.Cameras) != nil {
			fail(w, 500, "Lesezeichen nicht lesbar")
			return
		}
		out = append(out, b)
	}
	if rows.Err() != nil {
		fail(w, 500, "Lesezeichen nicht lesbar")
		return
	}
	jsonOut(w, 200, out)
}
func (a *App) saveBookmark(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Time    float64  `json:"time"`
		Cameras []string `json:"cameras"`
		Note    string   `json:"note"`
	}
	if !decode(w, r, &in) {
		return
	}
	in.Note = strings.TrimSpace(in.Note)
	if math.IsNaN(in.Time) || math.IsInf(in.Time, 0) || in.Time < 0 || in.Time >= 7289650800 || len(in.Note) > 1000 || len(in.Cameras) == 0 || len(in.Cameras) > 100 {
		fail(w, 400, "Zeitpunkt, Kameras oder Notiz ungültig")
		return
	}
	seen := map[string]bool{}
	for _, id := range in.Cameras {
		if seen[id] {
			fail(w, 400, "Kamera doppelt ausgewählt")
			return
		}
		seen[id] = true
		if _, e := a.db.Camera(id); e != nil {
			fail(w, 400, "Kamera nicht vorhanden")
			return
		}
	}
	owner := bookmarkOwner(r.Context().Value(sessionKey).(Session))
	cameras, _ := json.Marshal(in.Cameras)
	id := r.PathValue("id")
	if id == "" {
		id = token()
		tx, e := a.db.Begin()
		if e != nil {
			fail(w, 500, "Lesezeichen nicht gespeichert")
			return
		}
		defer tx.Rollback()
		var count int
		e = tx.QueryRow(`SELECT COUNT(*) FROM bookmarks WHERE owner=?`, owner).Scan(&count)
		if e != nil {
			fail(w, 500, "Lesezeichen nicht gespeichert")
			return
		}
		if count >= 1000 {
			fail(w, 409, "Maximal 1000 persönliche Lesezeichen")
			return
		}
		if _, e = tx.Exec(`INSERT INTO bookmarks VALUES(?,?,?,?,?)`, id, owner, in.Time, string(cameras), in.Note); e != nil {
			fail(w, 500, "Lesezeichen nicht gespeichert")
			return
		}
		if e = tx.Commit(); e != nil {
			fail(w, 500, "Lesezeichen nicht gespeichert")
			return
		}
	} else {
		res, e := a.db.Exec(`UPDATE bookmarks SET time=?,cameras=?,note=? WHERE id=? AND owner=?`, in.Time, string(cameras), in.Note, id, owner)
		if e != nil {
			fail(w, 500, "Lesezeichen nicht gespeichert")
			return
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			fail(w, 404, "Lesezeichen nicht vorhanden")
			return
		}
	}
	jsonOut(w, 200, Bookmark{id, in.Time, in.Cameras, in.Note})
}
func (a *App) deleteBookmark(w http.ResponseWriter, r *http.Request) {
	res, e := a.db.Exec(`DELETE FROM bookmarks WHERE id=? AND owner=?`, r.PathValue("id"), bookmarkOwner(r.Context().Value(sessionKey).(Session)))
	if e != nil {
		fail(w, 500, "Lesezeichen nicht gelöscht")
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		fail(w, 404, "Lesezeichen nicht vorhanden")
		return
	}
	jsonOut(w, 200, map[string]bool{"deleted": true})
}
func (a *App) selectedArchiveCameras(r *http.Request) ([]Camera, error) {
	all, e := a.db.Cameras()
	if e != nil {
		return nil, e
	}
	raw, provided := r.URL.Query()["cameras"]
	if !provided {
		return all, nil
	}
	ids := map[string]bool{}
	for _, value := range raw {
		for _, id := range strings.Split(value, ",") {
			ids[id] = true
		}
	}
	out := []Camera{}
	for _, c := range all {
		if ids[c.ID] {
			out = append(out, c)
		}
	}
	return out, nil
}

type CalendarDay struct {
	Date          string `json:"date"`
	State         string `json:"state"`
	HasRecordings bool   `json:"hasRecordings"`
}

func (a *App) calendar(w http.ResponseWriter, r *http.Request) {
	month := r.URL.Query().Get("month")
	first, _, e := dayBounds(month + "-01")
	if e != nil {
		fail(w, 400, "Monat ungültig")
		return
	}
	cams, e := a.selectedArchiveCameras(r)
	if e != nil {
		fail(w, 500, "Kameras nicht lesbar")
		return
	}
	days := []CalendarDay{}
	next := ""
	now := time.Now().Unix()
	for d := first; d.Month() == first.Month(); d = d.AddDate(0, 0, 1) {
		date := d.Format("2006-01-02")
		item := CalendarDay{Date: date, State: "empty"}
		end := d.AddDate(0, 0, 1)
		for _, c := range cams {
			var checked int64
			var complete bool
			e = a.db.QueryRow(`SELECT checked,complete FROM camera_days WHERE camera_id=? AND date=?`, c.ID, date).Scan(&checked, &complete)
			if e != nil && e != sql.ErrNoRows {
				fail(w, 500, "Kalender nicht lesbar")
				return
			}
			if e != nil || !complete {
				item.State = "unknown"
			}
			if (e != nil || checked < now-3600) && next == "" {
				next = date
			}
			var count int
			if e = a.db.QueryRow(`SELECT COUNT(*) FROM recordings WHERE camera_id=? AND start<? AND (start+duration>? OR (codec='jpeg' AND start>=?))`, c.ID, end.Unix(), d.Unix(), d.Unix()).Scan(&count); e != nil {
				fail(w, 500, "Kalender nicht lesbar")
				return
			}
			item.HasRecordings = item.HasRecordings || count > 0
		}
		if item.HasRecordings && item.State == "empty" {
			item.State = "recorded"
		}
		days = append(days, item)
	}
	if next != "" {
		a.startScan(next, false)
	}
	jsonOut(w, 200, map[string]any{"days": days, "scan": a.scanStatus(), "pending": next != ""})
}

// Navigation uses only the derived index. Unchecked history is never called empty.
func (a *App) recordedDay(w http.ResponseWriter, r *http.Request) {
	from, to, e := dayBounds(r.URL.Query().Get("date"))
	if e != nil {
		fail(w, 400, "Datum ungültig")
		return
	}
	direction := r.URL.Query().Get("direction")
	if direction != "previous" && direction != "next" {
		fail(w, 400, "Richtung ungültig")
		return
	}
	cams, e := a.selectedArchiveCameras(r)
	if e != nil {
		fail(w, 500, "Kameras nicht lesbar")
		return
	}
	var best float64
	for _, c := range cams {
		var t float64
		if direction == "next" {
			e = a.db.QueryRow(`SELECT MIN(MAX(start,?)) FROM recordings WHERE camera_id=? AND (start+duration>? OR (codec='jpeg' AND start>=?))`, to.Unix(), c.ID, to.Unix(), to.Unix()).Scan(&t)
			if e == nil && (best == 0 || t < best) {
				best = t
			}
		} else {
			e = a.db.QueryRow(`SELECT MAX(CASE WHEN codec='jpeg' THEN start ELSE MIN(start+duration-0.001,?) END) FROM recordings WHERE camera_id=? AND start<?`, float64(from.Unix())-.001, c.ID, from.Unix()).Scan(&t)
			if e == nil && (best == 0 || t > best) {
				best = t
			}
		}
	}
	date := ""
	if best != 0 {
		date = time.Unix(int64(best), 0).In(from.Location()).Format("2006-01-02")
	}
	jsonOut(w, 200, map[string]string{"date": date})
}
func (a *App) cameraStatus(w http.ResponseWriter, r *http.Request) {
	cams, e := a.db.Cameras()
	if e != nil {
		fail(w, 500, "Kameras nicht lesbar")
		return
	}
	scan := a.scanStatus()
	out := []map[string]any{}
	for _, c := range cams {
		dir, err := a.root.Open(c.Folder)
		if err == nil {
			_, err = dir.Readdirnames(1)
			dir.Close()
			if err == io.EOF {
				err = nil
			}
		}
		var latest float64
		var attempt, success int64
		var complete bool
		if e = a.db.QueryRow(`SELECT COALESCE(MAX(start),0) FROM recordings WHERE camera_id=?`, c.ID).Scan(&latest); e != nil {
			fail(w, 500, "Kamerastatus nicht lesbar")
			return
		}
		if e = a.db.QueryRow(`SELECT attempt,success,complete FROM camera_scans WHERE camera_id=?`, c.ID).Scan(&attempt, &success, &complete); e != nil && e != sql.ErrNoRows {
			fail(w, 500, "Kamerastatus nicht lesbar")
			return
		}
		out = append(out, map[string]any{"id": c.ID, "name": c.Name, "reachable": err == nil, "lastRecording": latest, "lastAttempt": attempt, "lastSuccess": success, "stale": err != nil || !complete || success == 0 || attempt > success || time.Now().Unix()-success > int64(a.config().ScanInterval.Seconds())+60, "scanning": scan.Running && scan.Camera == c.Name})
	}
	jsonOut(w, 200, out)
}
