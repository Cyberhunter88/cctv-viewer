package viewer

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/crypto/argon2"
	"golang.org/x/oauth2"
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
	if len(password) < 14 || len(password) > 1024 {
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
	if len(name) < 1 || len(name) > 100 {
		return errors.New("Benutzername ungültig")
	}
	hash, e := HashPassword(password)
	if e != nil {
		return e
	}
	_, e = s.Exec(`INSERT INTO admins(username,hash) VALUES(?,?)`, name, hash)
	return e
}
func (a *App) cookie(w http.ResponseWriter, name, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", HttpOnly: true, Secure: !a.cfg.DevHTTP, SameSite: http.SameSiteLaxMode, MaxAge: maxAge})
}
func (a *App) newSession(w http.ResponseWriter, s Session) error {
	raw := token()
	s.CSRF = token()
	_, e := a.db.Exec(`INSERT INTO sessions VALUES(?,?,?,?,?,?,?)`, digest(raw), s.Subject, s.Name, s.Role, s.Kind, s.CSRF, time.Now().Add(a.cfg.SessionTTL).Unix())
	if e == nil {
		a.cookie(w, "ctv_session", raw, int(a.cfg.SessionTTL.Seconds()))
	}
	return e
}
func (a *App) session(r *http.Request) (Session, error) {
	var s Session
	c, e := r.Cookie("ctv_session")
	if e != nil {
		return s, e
	}
	e = a.db.QueryRowContext(r.Context(), `SELECT subject,name,role,kind,csrf FROM sessions WHERE hash=? AND expires>?`, digest(c.Value), time.Now().Unix()).Scan(&s.Subject, &s.Name, &s.Role, &s.Kind, &s.CSRF)
	if s.Kind == "oidc" && (a.verifier == nil || !strings.HasPrefix(s.Subject, a.cfg.OIDCIssuer+"\x00")) {
		return Session{}, errors.New("OIDC-Anmeldung deaktiviert")
	}
	if s.Kind == "local" && !a.cfg.LocalAdmin {
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
			if r.Header.Get("Origin") != a.cfg.BaseURL || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(s.CSRF)) != 1 {
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
		a.db.ExecContext(r.Context(), `DELETE FROM sessions WHERE hash=?`, digest(c.Value))
	}
	a.cookie(w, "ctv_session", "", -1)
	jsonOut(w, 200, map[string]bool{"ok": true})
}

type attempt struct {
	start time.Time
	count int
}
type limiter struct {
	mu      sync.Mutex
	entries map[string]attempt
	active  chan struct{}
}

func (l *limiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	for k, v := range l.entries {
		if now.Sub(v.start) > 15*time.Minute {
			delete(l.entries, k)
		}
	}
	v := l.entries[ip]
	if v.start.IsZero() {
		v.start = now
	}
	v.count++
	if len(l.entries) >= 10000 && v.count == 1 {
		return false
	}
	l.entries[ip] = v
	return v.count <= 8
}
func (a *App) clientIP(r *http.Request) string {
	host, _, e := net.SplitHostPort(r.RemoteAddr)
	if e != nil {
		host = r.RemoteAddr
	}
	ip, e := netip.ParseAddr(host)
	if e != nil {
		return "unknown"
	}
	trusted := func(ip netip.Addr) bool {
		for _, p := range a.cfg.TrustedProxies {
			if p.Contains(ip) {
				return true
			}
		}
		return false
	}
	if trusted(ip) {
		parts := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
		for i := len(parts) - 1; i >= 0; i-- {
			candidate, e := netip.ParseAddr(strings.TrimSpace(parts[i]))
			if e != nil {
				return ip.String()
			}
			ip = candidate
			if !trusted(ip) {
				break
			}
		}
	}
	return ip.String()
}
func (a *App) localLogin(w http.ResponseWriter, r *http.Request) {
	if !a.cfg.LocalAdmin {
		fail(w, 404, "Lokale Anmeldung deaktiviert")
		return
	}
	if r.Header.Get("Origin") != a.cfg.BaseURL {
		fail(w, 403, "Ungültige Herkunft")
		return
	}
	if !a.limits.allow(a.clientIP(r)) {
		w.Header().Set("Retry-After", "900")
		fail(w, 429, "Zu viele Anmeldeversuche. Bitte später erneut versuchen.")
		return
	}
	select {
	case a.limits.active <- struct{}{}:
		defer func() { <-a.limits.active }()
	default:
		fail(w, 429, "Anmeldung ausgelastet")
		return
	}
	var input struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decode(w, r, &input) {
		return
	}
	if len(input.Password) > 1024 || len(input.Username) > 100 {
		fail(w, 401, "Anmeldedaten ungültig")
		return
	}
	var hash string
	if e := a.db.QueryRowContext(r.Context(), `SELECT hash FROM admins WHERE username=?`, input.Username).Scan(&hash); e != nil {
		hash = a.dummyHash
	}
	if !checkPassword(hash, input.Password) || hash == a.dummyHash {
		fail(w, 401, "Anmeldedaten ungültig")
		return
	}
	if e := a.newSession(w, Session{Subject: input.Username, Name: input.Username, Role: "admin", Kind: "local"}); e != nil {
		fail(w, 500, "Anmeldung fehlgeschlagen")
		return
	}
	jsonOut(w, 200, map[string]bool{"ok": true})
}
func (a *App) initOIDC(ctx context.Context) error {
	if a.cfg.OIDCIssuer == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	provider, e := oidc.NewProvider(ctx, a.cfg.OIDCIssuer)
	if e != nil {
		return fmt.Errorf("OIDC Discovery: %w", e)
	}
	a.verifier = provider.Verifier(&oidc.Config{ClientID: a.cfg.OIDCClientID})
	a.oauth = oauth2.Config{ClientID: a.cfg.OIDCClientID, ClientSecret: a.cfg.OIDCSecret, Endpoint: provider.Endpoint(), RedirectURL: a.cfg.BaseURL + "/auth/oidc/callback", Scopes: []string{oidc.ScopeOpenID, "profile"}}
	return nil
}
func (a *App) oidcStart(w http.ResponseWriter, r *http.Request) {
	if a.verifier == nil {
		fail(w, 503, "OIDC nicht verfügbar")
		return
	}
	if !a.limits.allow(a.clientIP(r)) {
		fail(w, 429, "Zu viele Anmeldeversuche")
		return
	}
	state, nonce, verifier := token(), token(), oauth2.GenerateVerifier()
	a.db.Cleanup(r.Context())
	_, e := a.db.ExecContext(r.Context(), `INSERT INTO oidc_flows VALUES(?,?,?,?)`, digest(state), nonce, verifier, time.Now().Add(5*time.Minute).Unix())
	if e != nil {
		fail(w, 500, "Anmeldung fehlgeschlagen")
		return
	}
	a.cookie(w, "ctv_oidc", state, 300)
	http.Redirect(w, r, a.oauth.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), http.StatusFound)
}
func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
func (a *App) oidcRole(subject string, claims map[string]any) string {
	var groups []string
	if raw, ok := claims[a.cfg.OIDCGroupClaim].([]any); ok {
		for _, v := range raw {
			if g, ok := v.(string); ok {
				groups = append(groups, g)
			}
		}
	}
	admin := contains(a.cfg.AdminUsers, subject)
	allowed := contains(a.cfg.AllowedUsers, subject)
	for _, g := range groups {
		admin = admin || contains(a.cfg.AdminGroups, g)
		allowed = allowed || contains(a.cfg.AllowedGroups, g)
	}
	if admin {
		return "admin"
	}
	if allowed {
		return "viewer"
	}
	return ""
}
func (a *App) oidcCallback(w http.ResponseWriter, r *http.Request) {
	if a.verifier == nil {
		fail(w, 503, "OIDC nicht verfügbar")
		return
	}
	cookie, e := r.Cookie("ctv_oidc")
	state := r.URL.Query().Get("state")
	if e != nil || state == "" || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(state)) != 1 {
		fail(w, 400, "OIDC-Anfrage ungültig")
		return
	}
	a.cookie(w, "ctv_oidc", "", -1)
	var nonce, verifier string
	e = a.db.QueryRowContext(r.Context(), `DELETE FROM oidc_flows WHERE hash=? AND expires>? RETURNING nonce,verifier`, digest(state), time.Now().Unix()).Scan(&nonce, &verifier)
	if e != nil {
		fail(w, 400, "Anmeldung abgelaufen oder bereits verwendet")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	tok, e := a.oauth.Exchange(ctx, r.URL.Query().Get("code"), oauth2.VerifierOption(verifier))
	if e != nil {
		fail(w, 401, "OIDC-Anmeldung fehlgeschlagen")
		return
	}
	raw, ok := tok.Extra("id_token").(string)
	if !ok {
		fail(w, 401, "ID-Token fehlt")
		return
	}
	id, e := a.verifier.Verify(ctx, raw)
	if e != nil || id.Nonce != nonce || id.Subject == "" {
		fail(w, 401, "ID-Token ungültig")
		return
	}
	claims := map[string]any{}
	if e = id.Claims(&claims); e != nil {
		fail(w, 401, "ID-Token ungültig")
		return
	}
	role := a.oidcRole(id.Subject, claims)
	if role == "" {
		fail(w, 403, "Dieses Konto ist nicht freigegeben")
		return
	}
	name, _ := claims["name"].(string)
	if name == "" {
		name = id.Subject
	}
	if e = a.newSession(w, Session{Subject: a.cfg.OIDCIssuer + "\x00" + id.Subject, Name: name, Role: role, Kind: "oidc"}); e != nil {
		fail(w, 500, "Anmeldung fehlgeschlagen")
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 16*1024)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		fail(w, 400, "Ungültige Eingabe")
		return false
	}
	return true
}
