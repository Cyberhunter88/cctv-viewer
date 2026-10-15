package viewer

import (
	"net/http"
	"strings"
	"time"
)

// Validate every source before committing; a failed batch adds no cameras.
func (a *App) saveCameraBatch(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Cameras []Camera `json:"cameras"`
		Date    string   `json:"date"`
	}
	if !decodeLimit(w, r, &input, 256*1024) {
		return
	}
	if len(input.Cameras) < 1 || len(input.Cameras) > 64 {
		fail(w, 400, "1 bis 64 Kameras auswählen")
		return
	}
	if _, _, e := dayBounds(input.Date); e != nil {
		fail(w, 400, e.Error())
		return
	}
	a.index.mu.Lock()
	if a.index.status.Running {
		a.index.mu.Unlock()
		fail(w, 409, "Bitte laufende Indexierung abwarten")
		return
	}
	existing, e := a.db.Cameras()
	if e != nil {
		a.index.mu.Unlock()
		fail(w, 500, "Kameraliste nicht lesbar")
		return
	}
	folders := map[string]bool{}
	for _, c := range existing {
		folders[c.Folder] = true
	}
	ids := []string{}
	for i := range input.Cameras {
		c := &input.Cameras[i]
		if c.ID != "" || folders[c.Folder] {
			a.index.mu.Unlock()
			fail(w, 400, "Ordner bereits eingerichtet oder doppelt ausgewählt")
			return
		}
		if c.Format == "" {
			c.Format = "automatic"
		}
		if c.ViewMode == "" {
			c.ViewMode = "single"
		}
		if c.IndexingMode == "" {
			c.IndexingMode = "partitioned"
		}
		if c.DirectoryPattern == "" {
			c.DirectoryPattern = "{YYYY}/{MM}/{DD}"
		}
		if e := validateCamera(*c, a.root); e != nil {
			a.index.mu.Unlock()
			fail(w, 400, c.Name+": "+e.Error())
			return
		}
		folders[c.Folder] = true
		c.ID = token()[:16]
		ids = append(ids, c.ID)
	}
	tx, e := a.db.Begin()
	if e == nil {
		defer tx.Rollback()
		for _, c := range input.Cameras {
			_, e = tx.Exec(`INSERT INTO cameras(id,name,folder,pattern,timezone,offset,format,prefix,indexing_mode,directory_pattern,view_mode,wide_marker,zoom_marker,zoom_offset) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, c.ID, c.Name, c.Folder, c.Pattern, c.Timezone, c.Offset, c.Format, c.Prefix, c.IndexingMode, c.DirectoryPattern, c.ViewMode, c.WideMarker, c.ZoomMarker, c.ZoomOffset)
			if e != nil {
				break
			}
		}
		if e == nil {
			e = tx.Commit()
		} else {
			tx.Rollback()
		}
	}
	if e != nil {
		a.index.mu.Unlock()
		fail(w, 500, "Kameras konnten nicht gespeichert werden")
		return
	}
	a.index.last = map[string]time.Time{}
	a.index.mu.Unlock()
	a.queueScan(scanJob{date: input.Date, batch: strings.Join(ids, ","), manual: true}, true)
	jsonOut(w, 201, input.Cameras)
}
