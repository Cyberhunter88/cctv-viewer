package viewer

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

func (a *App) initOIDC(ctx context.Context) error {
	if a.config().OIDCIssuer == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	provider, e := oidc.NewProvider(ctx, a.config().OIDCIssuer)
	if e != nil {
		return fmt.Errorf("OIDC Discovery: %w", e)
	}
	var discovery struct {
		JWKS string `json:"jwks_uri"`
	}
	if e = provider.Claims(&discovery); e != nil {
		return e
	}
	for _, endpoint := range []string{provider.Endpoint().AuthURL, provider.Endpoint().TokenURL, discovery.JWKS} {
		u, e := url.Parse(endpoint)
		if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
			return errors.New("OIDC-Endpunkte müssen HTTPS verwenden")
		}
	}
	a.verifier = provider.Verifier(&oidc.Config{ClientID: a.config().OIDCClientID})
	a.oauth = oauth2.Config{ClientID: a.config().OIDCClientID, ClientSecret: a.config().OIDCSecret, Endpoint: provider.Endpoint(), RedirectURL: a.config().BaseURL + "/auth/oidc/callback", Scopes: []string{oidc.ScopeOpenID, "profile", "groups"}}
	return nil
}
func (a *App) oidcStart(w http.ResponseWriter, r *http.Request) {
	a.authMu.RLock()
	defer a.authMu.RUnlock()
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
	sealedNonce, e := a.db.sealSecret(nonce, secretScope("oidc_flows", digest(state), "nonce"))
	if e != nil {
		fail(w, 500, "Anmeldung fehlgeschlagen")
		return
	}
	sealedVerifier, e := a.db.sealSecret(verifier, secretScope("oidc_flows", digest(state), "verifier"))
	if e != nil {
		fail(w, 500, "Anmeldung fehlgeschlagen")
		return
	}
	_, e = a.db.ExecContext(r.Context(), `INSERT INTO oidc_flows VALUES(?,?,?,?)`, digest(state), sealedNonce, sealedVerifier, time.Now().Add(5*time.Minute).Unix())
	if e != nil {
		fail(w, 500, "Anmeldung fehlgeschlagen")
		return
	}
	a.cookie(w, a.oidcCookie(), state, 300)
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
	if raw, ok := claims[a.config().OIDCGroupClaim].([]any); ok {
		for _, v := range raw {
			if g, ok := v.(string); ok {
				groups = append(groups, g)
			}
		}
	}
	admin := contains(a.config().AdminUsers, subject)
	allowed := contains(a.config().AllowedUsers, subject)
	for _, g := range groups {
		admin = admin || contains(a.config().AdminGroups, g)
		allowed = allowed || contains(a.config().AllowedGroups, g)
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
	a.authMu.RLock()
	defer a.authMu.RUnlock()
	if a.verifier == nil {
		fail(w, 503, "OIDC nicht verfügbar")
		return
	}
	cookie, e := r.Cookie(a.oidcCookie())
	state := r.URL.Query().Get("state")
	if e != nil || state == "" || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(state)) != 1 {
		fail(w, 400, "OIDC-Anfrage ungültig")
		return
	}
	a.cookie(w, a.oidcCookie(), "", -1)
	var nonce, verifier string
	e = a.db.QueryRowContext(r.Context(), `DELETE FROM oidc_flows WHERE hash=? AND expires>? RETURNING nonce,verifier`, digest(state), time.Now().Unix()).Scan(&nonce, &verifier)
	if e != nil {
		fail(w, 400, "Anmeldung abgelaufen oder bereits verwendet")
		return
	}
	nonce, e = a.db.openSecret(nonce, secretScope("oidc_flows", digest(state), "nonce"))
	if e != nil {
		fail(w, 400, "Anmeldung ungültig")
		return
	}
	verifier, e = a.db.openSecret(verifier, secretScope("oidc_flows", digest(state), "verifier"))
	if e != nil {
		fail(w, 400, "Anmeldung ungültig")
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
		a.recordAudit(r.Context(), id.Subject, "login.oidc", "", a.clientIP(r), false)
		fail(w, 403, "Dieses Konto ist nicht freigegeben")
		return
	}
	name, _ := claims["name"].(string)
	if name == "" {
		name = id.Subject
	}
	s := Session{Subject: a.config().OIDCIssuer + "\x00" + id.Subject, Name: name, Role: role, Kind: "oidc"}
	if a.confirmOIDC != nil {
		if e = a.confirmOIDC(s); e != nil {
			fail(w, 403, e.Error())
			return
		}
		http.Redirect(w, r, "/?settings=confirmed", http.StatusSeeOther)
		return
	}
	if e = a.newSession(w, s); e != nil {
		fail(w, 500, "Anmeldung fehlgeschlagen")
		return
	}
	a.recordAudit(r.Context(), s.Name, "login.oidc", "", a.clientIP(r), true)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
func (a *App) oidcCookie() string {
	if a.testOIDC {
		return "ctv_oidc_test"
	}
	return "ctv_oidc"
}
