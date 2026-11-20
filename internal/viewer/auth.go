package viewer

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

type Session struct {
	Subject string `json:"-"`
	Name    string `json:"name"`
	Role    string `json:"role"`
	Kind    string `json:"kind"`
	CSRF    string `json:"csrf"`
}
type contextKey int

const sessionKey contextKey = 1

func token() string {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		panic(e)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func digest(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func HashPassword(password string) (string, error) {
	if utf8.RuneCountInString(password) < 14 || len(password) > 1024 {
		return "", errors.New("Passwort muss 14 bis 1024 Zeichen lang sein")
	}
	salt := make([]byte, 16)
	if _, e := rand.Read(salt); e != nil {
		return "", e
	}
	key := argon2.IDKey([]byte(password), salt, 3, 64*1024, 2, 32)
	return "argon2id$" + base64.RawStdEncoding.EncodeToString(salt) + "$" + base64.RawStdEncoding.EncodeToString(key), nil
}
func checkPassword(hash, password string) bool {
	parts := strings.Split(hash, "$")
	if len(parts) != 3 || parts[0] != "argon2id" {
		return false
	}
	salt, e := base64.RawStdEncoding.DecodeString(parts[1])
	if e != nil || len(salt) != 16 {
		return false
	}
	key, e := base64.RawStdEncoding.DecodeString(parts[2])
	if e != nil || len(key) != 32 {
		return false
	}
	actual := argon2.IDKey([]byte(password), salt, 3, 64*1024, 2, 32)
	return subtle.ConstantTimeCompare(key, actual) == 1
}
func (s *Store) CreateAdmin(name, password string) error {
	return s.CreateAdminRecovery(name, password, false)
}
func (s *Store) CreateAdminRecovery(name, password string, recoverLogin bool) error {
	if strings.TrimSpace(name) == "" || len(name) > 100 {
		return errors.New("Benutzername ungültig")
	}
	hash, e := HashPassword(password)
	if e != nil {
		return e
	}
	tx, e := s.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.Exec(`INSERT INTO admins(username,hash) VALUES(?,?)`, name, hash); e != nil {
		return e
	}
	if recoverLogin {
		var version int
		var payload string
		values := map[string]json.RawMessage{}
		e = tx.QueryRow(`SELECT version,payload FROM settings WHERE id=1`).Scan(&version, &payload)
		if e != nil && e != sql.ErrNoRows {
			return e
		}
		if payload != "" {
			payload, e = s.settingsPayload(payload)
			if e != nil {
				return e
			}
			if e = json.Unmarshal([]byte(payload), &values); e != nil {
				return e
			}
		}
		values["localLogin"] = json.RawMessage(`true`)
		values["oidcIssuer"] = json.RawMessage(`""`)
		b, e := json.Marshal(values)
		if e != nil {
			return e
		}
		sealed, e := s.sealSettings(string(b))
		if e != nil {
			return e
		}
		if _, e = tx.Exec(`INSERT INTO settings(id,version,payload) VALUES(1,?,?) ON CONFLICT(id) DO UPDATE SET version=excluded.version,payload=excluded.payload`, version+1, sealed); e != nil {
			return e
		}
		if _, e = tx.Exec(`DELETE FROM sessions;DELETE FROM oidc_flows;`); e != nil {
			return e
		}
	}
	return tx.Commit()
}

func (a *App) cookie(w http.ResponseWriter, name, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", HttpOnly: true, Secure: !a.config().DevHTTP, SameSite: http.SameSiteLaxMode, MaxAge: maxAge})
}
func (a *App) newSession(w http.ResponseWriter, s Session) error {
	raw := token()
	s.CSRF = token()
	csrf, e := a.db.sealSecret(s.CSRF, secretScope("sessions", digest(raw), "csrf"))
	if e != nil {
		return e
	}
	_, e = a.db.Exec(`INSERT INTO sessions VALUES(?,?,?,?,?,?,?)`, digest(raw), s.Subject, s.Name, s.Role, s.Kind, csrf, time.Now().Add(a.config().SessionTTL).Unix())
	if e == nil {
		a.cookie(w, "ctv_session", raw, int(a.config().SessionTTL.Seconds()))
	}
	return e
}
func (a *App) session(r *http.Request) (Session, error) {
	a.authMu.RLock()
	defer a.authMu.RUnlock()
	var s Session
	c, e := r.Cookie("ctv_session")
	if e != nil {
		return s, e
	}
	e = a.db.reader().QueryRowContext(r.Context(), `SELECT subject,name,role,kind,csrf FROM sessions WHERE hash=? AND expires>?`, digest(c.Value), time.Now().Unix()).Scan(&s.Subject, &s.Name, &s.Role, &s.Kind, &s.CSRF)
	if e != nil {
		return s, e
	}
	s.CSRF, e = a.db.openSecret(s.CSRF, secretScope("sessions", digest(c.Value), "csrf"))
	if e != nil {
		return Session{}, e
	}
	if s.Kind == "oidc" && (a.verifier == nil || !strings.HasPrefix(s.Subject, a.config().OIDCIssuer+"\x00")) {
		return Session{}, errors.New("OIDC-Anmeldung deaktiviert")
	}
	if s.Kind == "local" && !a.config().LocalAdmin {
		return Session{}, errors.New("lokale Anmeldung deaktiviert")
	}
	return s, e
}
func (a *App) protect(admin bool, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s, e := a.session(r)
		if e != nil {
			fail(w, 401, "Anmeldung erforderlich")
			return
		}
		if admin && s.Role != "admin" {
			fail(w, 403, "Administratorrechte erforderlich")
			return
		}
		if r.Method != "GET" && r.Method != "HEAD" {
			if r.Header.Get("Origin") != a.config().BaseURL || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(s.CSRF)) != 1 {
				fail(w, 403, "Ungültige Anfrage")
				return
			}
		}
		h(w, r.WithContext(context.WithValue(r.Context(), sessionKey, s)))
	}
}
func (a *App) logout(w http.ResponseWriter, r *http.Request) {
	c, _ := r.Cookie("ctv_session")
	if c != nil {
		if s, e := a.session(r); e == nil {
			a.recordAudit(r.Context(), s.Name, "logout", "", a.clientIP(r), true)
		}
		a.db.ExecContext(r.Context(), `DELETE FROM sessions WHERE hash=?`, digest(c.Value))
	}
	a.cookie(w, "ctv_session", "", -1)
	jsonOut(w, 200, map[string]bool{"ok": true})
}

func (a *App) localLogin(w http.ResponseWriter, r *http.Request) {
	if !a.config().LocalAdmin {
		fail(w, 404, "Lokale Anmeldung deaktiviert")
		return
	}
	if r.Header.Get("Origin") != a.config().BaseURL {
		fail(w, 403, "Ungültige Herkunft")
		return
	}
	if !a.limits.allow(a.clientIP(r)) {
		w.Header().Set("Retry-After", "900")
		fail(w, 429, "Zu viele Anmeldeversuche. Bitte später erneut versuchen.")
		return
	}
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !decode(w, r, &input) {
		return
	}
	if len(input.Password) > 1024 || len(input.Username) > 100 || len(input.Code) > 16 {
		fail(w, 401, "Anmeldedaten ungültig")
		return
	}
	account := strings.ToLower(strings.TrimSpace(input.Username))
	if a.limits.accountLocked(account) {
		w.Header().Set("Retry-After", "900")
		fail(w, 429, "Zu viele Anmeldeversuche. Bitte später erneut versuchen.")
		return
	}
	// Never hold authentication locks or hashing slots while reading client data.
	a.authMu.RLock()
	defer a.authMu.RUnlock()
	// Settings may have changed while the request body was arriving.
	if !a.config().LocalAdmin {
		fail(w, 404, "Lokale Anmeldung deaktiviert")
		return
	}
	if r.Header.Get("Origin") != a.config().BaseURL {
		fail(w, 403, "Ungültige Herkunft")
		return
	}
	select {
	case a.limits.active <- struct{}{}:
		defer func() { <-a.limits.active }()
	default:
		fail(w, 429, "Anmeldung ausgelastet")
		return
	}
	var hash, role string
	if e := a.db.QueryRowContext(r.Context(), `SELECT hash,role FROM admins WHERE username=? AND disabled=0`, input.Username).Scan(&hash, &role); e != nil {
		hash = a.dummyHash
	}
	if !checkPassword(hash, input.Password) || hash == a.dummyHash {
		a.metrics.loginFailures.Add(1)
		a.limits.accountFailure(account)
		a.recordAudit(r.Context(), input.Username, "login.local", "", a.clientIP(r), false)
		slog.Warn("Anmeldung fehlgeschlagen", "ip", a.clientIP(r))
		fail(w, 401, "Anmeldedaten ungültig")
		return
	}
	// The second factor is only evaluated after the password was correct, so
	// its presence does not reveal anything about unknown accounts.
	if secret, enabled, step, e := a.totpAccount(r, input.Username); e != nil {
		fail(w, 500, "Anmeldung fehlgeschlagen")
		return
	} else if enabled {
		if strings.TrimSpace(input.Code) == "" {
			a.limits.refund(a.clientIP(r))
			jsonOut(w, 401, map[string]any{"error": "Bestätigungscode erforderlich", "totpRequired": true})
			return
		}
		if !a.acceptTOTP(r, input.Username, secret, input.Code, step) {
			a.metrics.loginFailures.Add(1)
			a.limits.accountFailure(account)
			a.recordAudit(r.Context(), input.Username, "login.local", "", a.clientIP(r), false)
			slog.Warn("Anmeldung fehlgeschlagen", "ip", a.clientIP(r))
			jsonOut(w, 401, map[string]any{"error": "Bestätigungscode ungültig", "totpRequired": true})
			return
		}
	}
	if e := a.newLocalSession(w, input.Username, role, hash); e != nil {
		fail(w, 500, "Anmeldung fehlgeschlagen")
		return
	}
	a.metrics.loginSuccess.Add(1)
	a.limits.accountSuccess(account)
	a.recordAudit(r.Context(), input.Username, "login.local", "", a.clientIP(r), true)
	a.limits.refund(a.clientIP(r))
	jsonOut(w, 200, map[string]bool{"ok": true})
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	return decodeLimit(w, r, v, 16*1024)
}
func decodeLimit(w http.ResponseWriter, r *http.Request, v any, limit int64) bool {
	// Bound body reads independently of header/keep-alive timeouts. This does
	// not impose a write timeout on long-running video responses.
	controller := http.NewResponseController(w)
	if e := controller.SetReadDeadline(time.Now().Add(10 * time.Second)); e != nil && !errors.Is(e, http.ErrNotSupported) {
		fail(w, 400, "Anfrage nicht lesbar")
		return false
	}
	// Keep the deadline for net/http's draining of any unread trailing body.
	// The server resets it when it starts the next request on this connection.
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		fail(w, 400, "Ungültige Eingabe")
		return false
	}
	return true
}

// The password and role checked before hashing must still be current when the
// session is inserted. This closes races with password resets and account edits.
func (a *App) newLocalSession(w http.ResponseWriter, name, role, hash string) error {
	raw, csrf := token(), token()
	ttl := a.config().SessionTTL
	sealed, e := a.db.sealSecret(csrf, secretScope("sessions", digest(raw), "csrf"))
	if e != nil {
		return e
	}
	result, e := a.db.Exec(`INSERT INTO sessions(hash,subject,name,role,kind,csrf,expires) SELECT ?,username,username,role,'local',?,? FROM admins WHERE username=? AND hash=? AND role=? AND disabled=0`, digest(raw), sealed, time.Now().Add(ttl).Unix(), name, hash, role)
	if e != nil {
		return e
	}
	n, e := result.RowsAffected()
	if e != nil {
		return e
	}
	if n != 1 {
		return errors.New("Konto wurde inzwischen geändert")
	}
	a.cookie(w, "ctv_session", raw, int(ttl.Seconds()))
	return nil
}
