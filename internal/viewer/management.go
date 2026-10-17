package viewer

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"
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
}

func (a *App) registerManagement(m *http.ServeMux) {
	a.registerSessions(m)
	m.HandleFunc("GET /api/admin/users", a.protect(true, a.users))
	m.HandleFunc("POST /api/admin/users", a.protect(true, a.saveUser))
	m.HandleFunc("DELETE /api/admin/users/{name}", a.protect(true, a.deleteUser))
	m.HandleFunc("POST /api/password", a.protect(false, a.changePassword))
	m.HandleFunc("GET /api/admin/settings", a.protect(true, a.getSettings))
	m.HandleFunc("PUT /api/admin/settings", a.protect(true, a.saveSettings))
	m.HandleFunc("GET /auth/oidc/test", a.protect(true, a.startOIDCTest))
	m.HandleFunc("GET /auth/oidc/test/callback", a.protect(true, a.finishOIDCTest))
}
func (a *App) users(w http.ResponseWriter, r *http.Request) {
	rows, e := a.db.QueryContext(r.Context(), `SELECT username,role,disabled FROM admins ORDER BY username`)
	if e != nil {
		fail(w, 500, "Benutzer nicht lesbar")
		return
	}
	defer rows.Close()
	out := []localUser{}
	for rows.Next() {
		var u localUser
		if e = rows.Scan(&u.Username, &u.Role, &u.Disabled); e != nil {
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
	if strings.TrimSpace(in.Username) == "" || len(in.Username) > 100 || (in.Role != "admin" && in.Role != "viewer") {
		fail(w, 400, "Benutzername oder Rolle ungültig")
		return
	}
	hash := ""
	var e error
	if in.Password != "" {
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

type oidcPending struct {
	runtime *App
	owner   string
	version int
	values  map[string]json.RawMessage
	expires time.Time
}
type settingsInput struct {
	Version int                        `json:"version"`
	Values  map[string]json.RawMessage `json:"values"`
	Reset   []string                   `json:"reset"`
}

func settingsValues(c Config) map[string]any {
	return map[string]any{"autoScan": c.AutoScan, "autoScanMinutes": c.AutoScanMinutes, "hlsTempMB": c.HLSTempBytes >> 20, "balanced": c.Balanced, "fast": c.Fast, "localLogin": c.LocalAdmin, "oidcIssuer": c.OIDCIssuer, "oidcClientId": c.OIDCClientID, "oidcGroupClaim": c.OIDCGroupClaim, "allowedSubjects": append([]string{}, c.AllowedUsers...), "allowedGroups": append([]string{}, c.AllowedGroups...), "adminSubjects": append([]string{}, c.AdminUsers...), "adminGroups": append([]string{}, c.AdminGroups...), "scanInterval": int(c.ScanInterval.Seconds()), "cacheMB": c.CacheBytes >> 20, "maxTranscoders": c.MaxTranscoders, "sessionMinutes": int(c.SessionTTL.Minutes())}
}
func configWithValues(c Config, v map[string]json.RawMessage) (Config, error) {
	c = viewerDefaults(c)
	// Unmarshal must not reuse backing arrays from the environment or live config.
	c.AllowedUsers = append([]string(nil), c.AllowedUsers...)
	c.AllowedGroups = append([]string(nil), c.AllowedGroups...)
	c.AdminUsers = append([]string(nil), c.AdminUsers...)
	c.AdminGroups = append([]string(nil), c.AdminGroups...)

	for k, b := range v {
		if strings.TrimSpace(string(b)) == "null" {
			return c, errors.New("Leere Einstellungswerte sind nicht zulässig")
		}
		var target any
		switch k {
		case "autoScan":
			target = &c.AutoScan
		case "balanced":
			target = &c.Balanced
		case "fast":
			target = &c.Fast
		case "localLogin":
			target = &c.LocalAdmin
		case "oidcIssuer":
			target = &c.OIDCIssuer
		case "oidcClientId":
			target = &c.OIDCClientID
		case "oidcSecret":
			target = &c.OIDCSecret
		case "oidcGroupClaim":
			target = &c.OIDCGroupClaim
		case "allowedSubjects":
			target = &c.AllowedUsers
		case "allowedGroups":
			target = &c.AllowedGroups
		case "adminSubjects":
			target = &c.AdminUsers
		case "adminGroups":
			target = &c.AdminGroups
		case "autoScanMinutes", "hlsTempMB", "scanInterval", "cacheMB", "maxTranscoders", "sessionMinutes":
			var n int64
			if e := json.Unmarshal(b, &n); e != nil {
				return c, errors.New("Zahlenwert ungültig")
			}
			switch k {
			case "autoScanMinutes":
				if n < 1 || n > 10080 {
					return c, errors.New("Indexintervall: 1–10080 Minuten")
				}
				c.AutoScanMinutes = int(n)
			case "hlsTempMB":
				if n < 16 || n > 4096 {
					return c, errors.New("Temporärer Videospeicher: 16–4096 MiB")
				}
				c.HLSTempBytes = n << 20
			case "scanInterval":
				if n < 30 || n > 86400 {
					return c, errors.New("Scanintervall: 30–86400 Sekunden")
				}
				c.ScanInterval = time.Duration(n) * time.Second
			case "cacheMB":
				if n < 32 || n > 65536 {
					return c, errors.New("Cache: 32–65536 MB")
				}
				c.CacheBytes = n << 20
			case "maxTranscoders":
				if n < 0 || n > 64 {
					return c, errors.New("Umwandlungsplätze: 0–64")
				}
				c.MaxTranscoders = int(n)
			case "sessionMinutes":
				if n < 15 || n > 1440 {
					return c, errors.New("Sitzungsdauer: 15–1440 Minuten")
				}
				c.SessionTTL = time.Duration(n) * time.Minute
			}
			continue
		default:
			return c, errors.New("Unbekannte Einstellung")
		}
		if e := json.Unmarshal(b, target); e != nil {
			return c, errors.New("Einstellungswert ungültig")
		}
	}
	for _, p := range []StreamProfile{c.Balanced, c.Fast} {
		if p.Scale < 20 || p.Scale > 100 || p.FPS < 2 || p.FPS > 30 || p.Bitrate < 100 || p.Bitrate > 8000 {
			return c, errors.New("Streamingprofil: Skalierung 20–100 %, FPS 2–30, Bitrate 100–8000 kbit/s")
		}
	}
	if !c.LocalAdmin && c.OIDCIssuer == "" {
		return c, errors.New("Mindestens eine Anmeldung erforderlich")
	}
	if c.OIDCIssuer != "" {
		if e := validateOIDCConfig(c); e != nil {
			return c, e
		}
	}
	return c, nil
}
func (a *App) loadSettings() error {
	var payload string
	e := a.db.QueryRow(`SELECT version,payload FROM settings WHERE id=1`).Scan(&a.settingsVersion, &payload)
	if e != nil {
		if !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		return nil
	}
	if e = json.Unmarshal([]byte(payload), &a.overrides); e != nil {
		return e
	}
	a.cfg, e = configWithValues(a.envCfg, a.overrides)
	return e
}
func (a *App) getSettings(w http.ResponseWriter, r *http.Request) {
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	c := a.config()
	source := map[string]string{}
	for k := range settingsValues(c) {
		source[k] = "Umgebung"
		if _, ok := a.overrides[k]; ok {
			source[k] = "Weboberfläche"
		}
	}
	source["oidcSecret"] = "Umgebung"
	if _, ok := a.overrides["oidcSecret"]; ok {
		source["oidcSecret"] = "Weboberfläche"
	}
	jsonOut(w, 200, map[string]any{"version": a.settingsVersion, "values": settingsValues(c), "sources": source, "secretSet": c.OIDCSecret != "", "external": map[string]any{"baseURL": c.BaseURL, "recordingsDir": c.RecordingsDir, "dataDir": c.DataDir, "address": c.Address, "trustedProxies": c.TrustedProxies}})
}
func (a *App) saveSettings(w http.ResponseWriter, r *http.Request) {
	var in settingsInput
	if !decode(w, r, &in) {
		return
	}
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	if in.Version != a.settingsVersion {
		fail(w, 409, "Einstellungen wurden inzwischen geändert. Bitte neu laden.")
		return
	}
	v := map[string]json.RawMessage{}
	for k, b := range a.overrides {
		v[k] = b
	}
	for _, k := range in.Reset {
		if k != "oidcSecret" {
			if _, ok := settingsValues(a.envCfg)[k]; !ok {
				fail(w, 400, "Unbekannte Einstellung")
				return
			}
		}
		delete(v, k)
	}
	for k, b := range in.Values {
		v[k] = b
	}
	c, e := configWithValues(a.envCfg, v)
	if e != nil {
		fail(w, 400, e.Error())
		return
	}
	old := a.config()
	needsTest := c.OIDCIssuer != "" && (oidcChanged(old, c) || old.LocalAdmin && !c.LocalAdmin)
	if needsTest {
		candidate := &App{cfg: c, db: a.db, limits: limiter{entries: map[string]attempt{}, active: make(chan struct{}, 2)}, testOIDC: true}
		if e = candidate.initOIDC(r.Context()); e != nil {
			fail(w, 400, "OIDC-Discovery fehlgeschlagen; bisherige Anmeldung bleibt aktiv")
			return
		}
		// The same callback URL works for normal and test flows; a separate cookie binds the test.
		a.pending = &oidcPending{candidate, a.streamOwner(r), a.settingsVersion, v, time.Now().Add(10 * time.Minute)}
		jsonOut(w, 202, map[string]any{"testRequired": true, "testURL": "/auth/oidc/test"})
		return
	}
	if e = a.activateSettings(c, v, nil); e != nil {
		fail(w, 500, "Einstellungen konnten nicht gespeichert werden")
		return
	}
	jsonOut(w, 200, map[string]bool{"ok": true})
}
func oidcChanged(a, b Config) bool {
	av := settingsValues(a)
	bv := settingsValues(b)
	for _, k := range []string{"oidcIssuer", "oidcClientId", "oidcGroupClaim", "allowedSubjects", "allowedGroups", "adminSubjects", "adminGroups"} {
		x, _ := json.Marshal(av[k])
		y, _ := json.Marshal(bv[k])
		if string(x) != string(y) {
			return true
		}
	}
	return a.OIDCSecret != b.OIDCSecret
}
func (a *App) activateSettings(c Config, v map[string]json.RawMessage, candidate *App) error {
	payload, e := json.Marshal(v)
	if e != nil {
		return e
	}
	a.authMu.Lock()
	defer a.authMu.Unlock()
	tx, e := a.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.Exec(`INSERT INTO settings(id,version,payload) VALUES(1,?,?) ON CONFLICT(id) DO UPDATE SET version=excluded.version,payload=excluded.payload`, a.settingsVersion+1, string(payload)); e != nil {
		return e
	}
	changed := oidcChanged(a.config(), c)
	if changed {
		if _, e = tx.Exec(`DELETE FROM sessions WHERE kind='oidc'`); e != nil {
			return e
		}
		if _, e = tx.Exec(`DELETE FROM oidc_flows`); e != nil {
			return e
		}
	}
	if !c.LocalAdmin {
		if _, e = tx.Exec(`DELETE FROM sessions WHERE kind='local'`); e != nil {
			return e
		}
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	a.cfgMu.Lock()
	a.cfg = c
	a.cfgMu.Unlock()
	if candidate != nil {
		a.verifier = candidate.verifier
		a.oauth = candidate.oauth
	} else if c.OIDCIssuer == "" {
		a.verifier = nil
	}
	a.settingsVersion++
	a.overrides = v
	a.pending = nil
	return nil
}
func (a *App) startOIDCTest(w http.ResponseWriter, r *http.Request) {
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	p := a.pending
	if p == nil || p.owner != a.streamOwner(r) || time.Now().After(p.expires) {
		fail(w, 409, "OIDC-Test abgelaufen. Einstellungen erneut speichern.")
		return
	}
	p.runtime.oidcStart(w, r)
}
func (a *App) finishOIDCTest(w http.ResponseWriter, r *http.Request) {
	a.settingsMu.Lock()
	defer a.settingsMu.Unlock()
	p := a.pending
	if p == nil || p.owner != a.streamOwner(r) || p.version != a.settingsVersion || time.Now().After(p.expires) {
		fail(w, 409, "OIDC-Test abgelaufen; bisherige Anmeldung bleibt aktiv")
		return
	}
	p.runtime.confirmOIDC = func(s Session) error {
		if s.Role != "admin" {
			return errors.New("Testkonto benötigt Administratorrechte; bisherige Anmeldung bleibt aktiv")
		}
		if e := a.activateSettings(p.runtime.config(), p.values, p.runtime); e != nil {
			return e
		}
		return a.newSession(w, s)
	}
	p.runtime.oidcCallback(w, r)
}

func validateOIDCConfig(c Config) error {
	u, e := url.Parse(c.OIDCIssuer)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("OIDC-Issuer muss eine HTTPS-URL sein")
	}
	if c.OIDCClientID == "" || c.OIDCSecret == "" || strings.TrimSpace(c.OIDCGroupClaim) == "" {
		return errors.New("OIDC-Client-ID, Secret und Gruppen-Claim erforderlich")
	}
	return nil
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
