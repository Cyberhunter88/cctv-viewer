package viewer

import (
	"encoding/json"
	"net/http"
	"time"
)

const backupFormat = 1

type backupUser struct {
	Username string `json:"username"`
	Role     string `json:"role"`
	Disabled bool   `json:"disabled"`
}

// configBackup is the portable configuration. It deliberately omits password
// hashes, OIDC secrets, live credentials and recordings.
type configBackup struct {
	Format     int                        `json:"format"`
	ExportedAt int64                      `json:"exportedAt"`
	Cameras    []Camera                   `json:"cameras"`
	Settings   map[string]json.RawMessage `json:"settings"`
	Users      []backupUser               `json:"users"`
}

// runtimeFree strips fields that describe indexing state rather than configuration.
func runtimeFree(c Camera) Camera {
	c.SourceStatus, c.LastScan, c.LastRecording, c.IndexedVideos, c.Detection = "", 0, 0, 0, nil
	return c
}

func (a *App) backupExport(w http.ResponseWriter, r *http.Request) {
	cams, e := a.db.Cameras()
	if e != nil {
		fail(w, 500, "Kameras nicht lesbar")
		return
	}
	out := configBackup{Format: backupFormat, ExportedAt: time.Now().Unix(), Cameras: []Camera{}, Settings: map[string]json.RawMessage{}, Users: []backupUser{}}
	for _, c := range cams {
		out.Cameras = append(out.Cameras, runtimeFree(c))
	}
	a.settingsMu.Lock()
	for k, v := range a.overrides {
		if k != "oidcSecret" {
			out.Settings[k] = v
		}
	}
	a.settingsMu.Unlock()
	rows, e := a.db.QueryContext(r.Context(), `SELECT username,role,disabled FROM admins ORDER BY username`)
	if e != nil {
		fail(w, 500, "Benutzer nicht lesbar")
		return
	}
	defer rows.Close()
	for rows.Next() {
		var u backupUser
		if e = rows.Scan(&u.Username, &u.Role, &u.Disabled); e != nil {
			fail(w, 500, "Benutzer nicht lesbar")
			return
		}
		out.Users = append(out.Users, u)
	}
	w.Header().Set("Content-Disposition", `attachment; filename="cctv-viewer-konfiguration.json"`)
	jsonOut(w, 200, out)
}

// backupImport restores camera definitions. Settings and users are exported for
// reference only, because restoring them could lock administrators out.
func (a *App) backupImport(w http.ResponseWriter, r *http.Request) {
	var in configBackup
	if !decodeLimit(w, r, &in, 1<<20) {
		return
	}
	if in.Format != backupFormat {
		fail(w, 400, "Unbekanntes Sicherungsformat")
		return
	}
	if len(in.Cameras) > 256 {
		fail(w, 400, "Zu viele Kameras")
		return
	}
	seen := map[string]bool{}
	for i := range in.Cameras {
		c := runtimeFree(in.Cameras[i])
		if c.ID == "" || seen[c.ID] {
			fail(w, 400, "Kamera-ID fehlt oder doppelt")
			return
		}
		seen[c.ID] = true
		if e := validateCamera(c, a.root); e != nil {
			fail(w, 400, c.Name+": "+e.Error())
			return
		}
		in.Cameras[i] = c
	}
	a.index.mu.Lock()
	defer a.index.mu.Unlock()
	if a.index.status.Running {
		fail(w, 409, "Bitte laufende Indexierung abwarten")
		return
	}
	for _, c := range in.Cameras {
		if e := a.db.SaveCamera(c); e != nil {
			fail(w, 500, "Kamera konnte nicht gespeichert werden")
			return
		}
		setAuditTarget(r, c.ID)
	}
	a.index.last = map[string]time.Time{}
	jsonOut(w, 200, map[string]int{"cameras": len(in.Cameras)})
}
