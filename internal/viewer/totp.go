package viewer

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Time-based one-time passwords for local accounts (RFC 6238: SHA-1, 30 seconds,
// six digits). The secret is stored encrypted; the last accepted time step is
// stored so a code cannot be used twice.
const totpPeriod = 30

var totpEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

func newTOTPSecret() (string, error) {
	raw := make([]byte, 20)
	if _, e := rand.Read(raw); e != nil {
		return "", e
	}
	return totpEncoding.EncodeToString(raw), nil
}

func totpCode(secret string, step int64) (string, error) {
	key, e := totpEncoding.DecodeString(secret)
	if e != nil || len(key) < 10 {
		return "", fmt.Errorf("ungültiges TOTP-Geheimnis")
	}
	var counter [8]byte
	binary.BigEndian.PutUint64(counter[:], uint64(step))
	mac := hmac.New(sha1.New, key)
	mac.Write(counter[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	value := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", value%1000000), nil
}

// totpStep returns the time step matching code, allowing one step of clock
// drift in either direction. Steps up to and including last are rejected.
func totpStep(secret, code string, now time.Time, last int64) (int64, bool) {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if len(code) != 6 {
		return 0, false
	}
	current := now.Unix() / totpPeriod
	var matched int64
	found := 0
	for step := current - 1; step <= current+1; step++ {
		expected, e := totpCode(secret, step)
		if e != nil {
			return 0, false
		}
		// Check every candidate so timing does not reveal which step matched.
		if subtle.ConstantTimeCompare([]byte(expected), []byte(code)) == 1 && step > last && found == 0 {
			matched, found = step, 1
		}
	}
	return matched, found == 1
}

func totpScope(username string) string { return secretScope("admins", username, "totp_secret") }

// totpAccount loads the second factor of a local account.
func (a *App) totpAccount(r *http.Request, username string) (secret string, enabled bool, step int64, e error) {
	var sealed string
	e = a.db.QueryRowContext(r.Context(), `SELECT totp_secret,totp_enabled,totp_step FROM admins WHERE username=? AND disabled=0`, username).Scan(&sealed, &enabled, &step)
	if e != nil || sealed == "" {
		return "", false, 0, e
	}
	secret, e = a.db.openSecret(sealed, totpScope(username))
	return secret, enabled, step, e
}

// acceptTOTP verifies a code and records its time step. The conditional update
// makes concurrent requests with the same code succeed at most once.
func (a *App) acceptTOTP(r *http.Request, username, secret, code string, last int64) bool {
	step, ok := totpStep(secret, code, time.Now(), last)
	if !ok {
		return false
	}
	result, e := a.db.ExecContext(r.Context(), `UPDATE admins SET totp_step=? WHERE username=? AND totp_step<?`, step, username, step)
	if e != nil {
		return false
	}
	n, _ := result.RowsAffected()
	return n == 1
}

func (a *App) registerTOTP(m *http.ServeMux) {
	m.HandleFunc("GET /api/totp", a.protect(false, a.totpStatus))
	m.HandleFunc("POST /api/totp/setup", a.protect(false, a.totpSetup))
	m.HandleFunc("POST /api/totp/enable", a.protect(false, a.audit("totp.enable", a.totpEnable)))
	m.HandleFunc("POST /api/totp/disable", a.protect(false, a.audit("totp.disable", a.totpDisable)))
	m.HandleFunc("DELETE /api/admin/users/{name}/totp", a.protect(true, a.audit("totp.reset", a.totpReset)))
}

// localAccount returns the session of a local account or answers the request.
func localAccount(w http.ResponseWriter, r *http.Request) (Session, bool) {
	s := r.Context().Value(sessionKey).(Session)
	if s.Kind != "local" {
		fail(w, 400, "Zweiten Faktor beim Identity Provider verwalten")
		return s, false
	}
	return s, true
}

func (a *App) totpStatus(w http.ResponseWriter, r *http.Request) {
	s := r.Context().Value(sessionKey).(Session)
	enabled := false
	if s.Kind == "local" {
		_, enabled, _, _ = a.totpAccount(r, s.Subject)
	}
	jsonOut(w, 200, map[string]bool{"available": s.Kind == "local", "enabled": enabled})
}

// totpSetup creates a pending secret. It only takes effect once totpEnable has
// confirmed that the authenticator produces matching codes.
func (a *App) totpSetup(w http.ResponseWriter, r *http.Request) {
	s, ok := localAccount(w, r)
	if !ok {
		return
	}
	if _, enabled, _, _ := a.totpAccount(r, s.Subject); enabled {
		fail(w, 409, "Zweiter Faktor ist bereits aktiv")
		return
	}
	secret, e := newTOTPSecret()
	if e != nil {
		fail(w, 500, "Einrichtung fehlgeschlagen")
		return
	}
	sealed, e := a.db.sealSecret(secret, totpScope(s.Subject))
	if e != nil {
		fail(w, 500, "Einrichtung fehlgeschlagen")
		return
	}
	result, e := a.db.ExecContext(r.Context(), `UPDATE admins SET totp_secret=?,totp_step=0 WHERE username=? AND disabled=0 AND totp_enabled=0`, sealed, s.Subject)
	if e != nil {
		fail(w, 500, "Einrichtung fehlgeschlagen")
		return
	}
	if n, _ := result.RowsAffected(); n != 1 {
		fail(w, 409, "Einrichtung nicht möglich")
		return
	}
	issuer := "CCTV Viewer"
	parameters := url.Values{"issuer": {issuer}, "algorithm": {"SHA1"}, "digits": {"6"}, "period": {"30"}}
	parameters.Set("secret", secret)
	uri := "otpauth://totp/" + url.PathEscape(issuer+":"+s.Subject) + "?" + parameters.Encode()
	jsonOut(w, 200, map[string]string{"secret": secret, "uri": uri})
}

func (a *App) totpEnable(w http.ResponseWriter, r *http.Request) {
	s, ok := localAccount(w, r)
	if !ok {
		return
	}
	var in struct {
		Code string `json:"code"`
	}
	if !decode(w, r, &in) {
		return
	}
	if !a.limits.allow(a.clientIP(r)) {
		fail(w, 429, "Zu viele Versuche")
		return
	}
	secret, enabled, step, e := a.totpAccount(r, s.Subject)
	if e != nil || secret == "" || enabled {
		fail(w, 409, "Keine offene Einrichtung")
		return
	}
	if !a.acceptTOTP(r, s.Subject, secret, in.Code, step) {
		fail(w, 400, "Bestätigungscode ungültig")
		return
	}
	if _, e = a.db.ExecContext(r.Context(), `UPDATE admins SET totp_enabled=1 WHERE username=? AND totp_secret!=''`, s.Subject); e != nil {
		fail(w, 500, "Aktivierung fehlgeschlagen")
		return
	}
	a.limits.refund(a.clientIP(r))
	jsonOut(w, 200, map[string]bool{"ok": true})
}

// totpDisable requires the password, so an unattended session cannot remove
// the second factor.
func (a *App) totpDisable(w http.ResponseWriter, r *http.Request) {
	s, ok := localAccount(w, r)
	if !ok {
		return
	}
	var in struct {
		Password string `json:"password"`
	}
	if !decode(w, r, &in) {
		return
	}
	if len(in.Password) > 1024 || !a.limits.allow(a.clientIP(r)) {
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
	var hash string
	if e := a.db.QueryRowContext(r.Context(), `SELECT hash FROM admins WHERE username=? AND disabled=0`, s.Subject).Scan(&hash); e != nil || !checkPassword(hash, in.Password) {
		fail(w, 403, "Passwort ungültig")
		return
	}
	if _, e := a.db.ExecContext(r.Context(), `UPDATE admins SET totp_secret='',totp_enabled=0,totp_step=0 WHERE username=?`, s.Subject); e != nil {
		fail(w, 500, "Änderung fehlgeschlagen")
		return
	}
	a.limits.refund(a.clientIP(r))
	jsonOut(w, 200, map[string]bool{"ok": true})
}

// totpReset lets an administrator remove the second factor of an account whose
// authenticator was lost.
func (a *App) totpReset(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	setAuditTarget(r, name)
	result, e := a.db.ExecContext(r.Context(), `UPDATE admins SET totp_secret='',totp_enabled=0,totp_step=0 WHERE username=?`, name)
	if e != nil {
		fail(w, 500, "Änderung fehlgeschlagen")
		return
	}
	if n, _ := result.RowsAffected(); n != 1 {
		fail(w, 404, "Benutzer nicht gefunden")
		return
	}
	jsonOut(w, 200, map[string]bool{"ok": true})
}
