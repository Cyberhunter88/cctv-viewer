package viewer

import (
	"errors"
	"io/fs"
	"net/http"
	"path"
	"sort"
	"strings"
)

func (a *App) config() Config { a.cfgMu.RLock(); defer a.cfgMu.RUnlock(); return a.cfg }
func (a *App) oidcEnabled() bool {
	a.authMu.RLock()
	defer a.authMu.RUnlock()
	return a.verifier != nil
}

type localUser struct {
	Username string `json:"username"`
	Role     string `json:"role"`
	Disabled bool   `json:"disabled"`
	TOTP     bool   `json:"totp"`
}

func (a *App) registerManagement(m *http.ServeMux) {
	a.registerSessions(m)
	a.registerTOTP(m)
	m.HandleFunc("GET /api/admin/users", a.protect(true, a.users))
	m.HandleFunc("POST /api/admin/users", a.protect(true, a.audit("user.save", a.saveUser)))
	m.HandleFunc("DELETE /api/admin/users/{name}", a.protect(true, a.audit("user.delete", a.deleteUser)))
	m.HandleFunc("GET /api/admin/audit", a.protect(true, a.auditList))
	m.HandleFunc("GET /api/admin/backup", a.protect(true, a.audit("backup.export", a.backupExport)))
	m.HandleFunc("POST /api/admin/backup", a.protect(true, a.audit("backup.import", a.backupImport)))
	m.HandleFunc("GET /api/admin/metrics", a.protect(true, a.metricsHandler))
	m.HandleFunc("GET /api/admin/storage", a.protect(true, a.storageOverview))
	m.HandleFunc("POST /api/password", a.protect(false, a.audit("password.change", a.changePassword)))
	m.HandleFunc("GET /api/admin/settings", a.protect(true, a.getSettings))
	m.HandleFunc("PUT /api/admin/settings", a.protect(true, a.audit("settings.save", a.saveSettings)))
	m.HandleFunc("GET /auth/oidc/test", a.protect(true, a.startOIDCTest))
	m.HandleFunc("GET /auth/oidc/test/callback", a.protect(true, a.finishOIDCTest))
}
func (a *App) users(w http.ResponseWriter, r *http.Request) {
	rows, e := a.db.QueryContext(r.Context(), `SELECT username,role,disabled,totp_enabled FROM admins ORDER BY username`)
	if e != nil {
		fail(w, 500, "Benutzer nicht lesbar")
		return
	}
	defer rows.Close()
	out := []localUser{}
	for rows.Next() {
		var u localUser
		if e = rows.Scan(&u.Username, &u.Role, &u.Disabled, &u.TOTP); e != nil {
			fail(w, 500, "Benutzer nicht lesbar")
			return
		}
		out = append(out, u)
	}
	jsonOut(w, 200, out)
}
func (a *App) saveUser(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username string `json:"username"`
		Role     string `json:"role"`
		Disabled bool   `json:"disabled"`
		Password string `json:"password"`
		Create   bool   `json:"create"`
	}
	if !decode(w, r, &in) {
		return
	}
	setAuditTarget(r, in.Username)
	if strings.TrimSpace(in.Username) == "" || len(in.Username) > 100 || (in.Role != "admin" && in.Role != "viewer") {
		fail(w, 400, "Benutzername oder Rolle ungültig")
		return
	}
	hash := ""
	var e error
	if in.Password != "" {
		select {
		case a.limits.active <- struct{}{}:
			defer func() { <-a.limits.active }()
		default:
			fail(w, 429, "Passwortprüfung ausgelastet")
			return
		}
		hash, e = HashPassword(in.Password)
		if e != nil {
			fail(w, 400, e.Error())
			return
		}
	}
	tx, e := a.db.BeginTx(r.Context(), nil)
	if e != nil {
		fail(w, 500, "Speichern fehlgeschlagen")
		return
	}
	defer tx.Rollback()
	var oldRole string
	var disabled bool
	e = tx.QueryRow(`SELECT role,disabled FROM admins WHERE username=?`, in.Username).Scan(&oldRole, &disabled)
	if in.Create {
		if e == nil {
			fail(w, 409, "Benutzer existiert bereits")
			return
		}
		if hash == "" {
			fail(w, 400, "Passwort erforderlich")
			return
		}
		_, e = tx.Exec(`INSERT INTO admins(username,hash,role,disabled) VALUES(?,?,?,?)`, in.Username, hash, in.Role, in.Disabled)
	} else {
		if e != nil {
			fail(w, 404, "Benutzer nicht gefunden")
			return
		}
		if oldRole == "admin" && !disabled && (in.Disabled || in.Role != "admin") {
			var n int
			tx.QueryRow(`SELECT COUNT(*) FROM admins WHERE role='admin' AND disabled=0`).Scan(&n)
			if n <= 1 {
				fail(w, 409, "Der letzte aktive lokale Administrator muss erhalten bleiben")
				return
			}
		}
		_, e = tx.Exec(`UPDATE admins SET role=?,disabled=?,hash=CASE WHEN ?='' THEN hash ELSE ? END WHERE username=?`, in.Role, in.Disabled, hash, hash, in.Username)
		if e == nil && (hash != "" || oldRole != in.Role || disabled != in.Disabled) {
			_, e = tx.Exec(`DELETE FROM sessions WHERE kind='local' AND subject=?`, in.Username)
		}
	}
	if e != nil || tx.Commit() != nil {
		fail(w, 500, "Benutzer konnte nicht gespeichert werden")
		return
	}
	jsonOut(w, 200, map[string]bool{"ok": true})
}
func (a *App) deleteUser(w http.ResponseWriter, r *http.Request) {
	tx, e := a.db.BeginTx(r.Context(), nil)
	if e != nil {
		fail(w, 500, "Löschen fehlgeschlagen")
		return
	}
	defer tx.Rollback()
	name := r.PathValue("name")
	var role string
	var disabled bool
	if e = tx.QueryRow(`SELECT role,disabled FROM admins WHERE username=?`, name).Scan(&role, &disabled); e != nil {
		fail(w, 404, "Benutzer nicht gefunden")
		return
	}
	if role == "admin" && !disabled {
		var n int
		tx.QueryRow(`SELECT COUNT(*) FROM admins WHERE role='admin' AND disabled=0`).Scan(&n)
		if n <= 1 {
			fail(w, 409, "Der letzte aktive lokale Administrator muss erhalten bleiben")
			return
		}
	}
	if _, e = tx.Exec(`DELETE FROM sessions WHERE kind='local' AND subject=?`, name); e == nil {
		_, e = tx.Exec(`DELETE FROM admins WHERE username=?`, name)
	}
	if e == nil {
		_, e = tx.Exec(`DELETE FROM user_preferences WHERE owner=?`, preferenceOwner(Session{Kind: "local", Subject: name}))
	}
	if e != nil || tx.Commit() != nil {
		fail(w, 500, "Löschen fehlgeschlagen")
		return
	}
	jsonOut(w, 200, map[string]bool{"ok": true})
}
func (a *App) changePassword(w http.ResponseWriter, r *http.Request) {
	s := r.Context().Value(sessionKey).(Session)
	if s.Kind != "local" {
		fail(w, 400, "Passwort beim Identity Provider ändern")
		return
	}
	var in struct {
		Current  string `json:"current"`
		Password string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}
	if len(in.Current) > 1024 || !a.limits.allow(a.clientIP(r)) {
		fail(w, 429, "Zu viele Versuche")
		return
	}
	select {
	case a.limits.active <- struct{}{}:
		defer func() { <-a.limits.active }()
	default:
		fail(w, 429, "Passwortprüfung ausgelastet")
		return
	}
	var old string
	if e := a.db.QueryRow(`SELECT hash FROM admins WHERE username=? AND disabled=0`, s.Subject).Scan(&old); e != nil || !checkPassword(old, in.Current) {
		fail(w, 403, "Bisheriges Passwort ungültig")
		return
	}
	hash, e := HashPassword(in.Password)
	if e != nil {
		fail(w, 400, e.Error())
		return
	}
	tx, e := a.db.BeginTx(r.Context(), nil)
	if e != nil {
		fail(w, 500, "Änderung fehlgeschlagen")
		return
	}
	defer tx.Rollback()
	result, e := tx.Exec(`UPDATE admins SET hash=? WHERE username=? AND hash=?`, hash, s.Subject, old)
	if e != nil {
		fail(w, 500, "Änderung fehlgeschlagen")
		return
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		fail(w, 409, "Konto wurde inzwischen geändert")
		return
	}
	if _, e = tx.Exec(`DELETE FROM sessions WHERE kind='local' AND subject=?`, s.Subject); e != nil || tx.Commit() != nil {
		fail(w, 500, "Änderung fehlgeschlagen")
		return
	}
	a.cookie(w, "ctv_session", "", -1)
	jsonOut(w, 200, map[string]bool{"ok": true})
}
func (a *App) cameraFolders(w http.ResponseWriter, r *http.Request) {
	folder := r.URL.Query().Get("path")
	if folder == "" {
		folder = "."
	}
	if folder != "." && !validRelative(folder) {
		fail(w, 400, "Ordner ungültig")
		return
	}
	entries, e := fs.ReadDir(a.root.FS(), folder)
	if e != nil {
		fail(w, 400, "Ordner nicht lesbar")
		return
	}
	dirs := []string{}
	for _, d := range entries {
		if d.IsDir() && d.Type()&fs.ModeSymlink == 0 {
			dirs = append(dirs, path.Join(folder, d.Name()))
			if len(dirs) >= 500 {
				break
			}
		}
	}
	sort.Strings(dirs)
	// Bound discovery work; only names of files beneath the selected folder are returned.
	samples := []string{}
	count := 0
	stop := errors.New("stop")
	fs.WalkDir(a.root.FS(), folder, func(p string, d fs.DirEntry, e error) error {
		count++
		if count > 10000 || len(samples) >= 8 {
			return stop
		}
		if e != nil {
			return fs.SkipDir
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		if !d.IsDir() && supportedMedia(p) {
			samples = append(samples, p)
		}
		return nil
	})
	jsonOut(w, 200, map[string]any{"path": folder, "folders": dirs, "samples": samples, "limited": count > 10000 || len(dirs) >= 500})
}

func (a *App) acquireEncoder() bool {
	limit := a.config().MaxTranscoders
	a.mediaMu.Lock()
	defer a.mediaMu.Unlock()
	if limit > 0 && a.encoderActive >= limit {
		return false
	}
	a.encoderActive++
	return true
}
func (a *App) releaseEncoder() { a.mediaMu.Lock(); a.encoderActive--; a.mediaMu.Unlock() }
