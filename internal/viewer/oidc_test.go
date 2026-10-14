package viewer

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	jose "github.com/go-jose/go-jose/v4"
)

func TestOIDCFlow(t *testing.T) {
	key, e := rsa.GenerateKey(rand.Reader, 2048)
	if e != nil {
		t.Fatal(e)
	}
	signer, e := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithType("JWT").WithHeader("kid", "test-key"))
	if e != nil {
		t.Fatal(e)
	}
	a := testApp(t)
	var issuer, nonce, challenge, mode string
	provider := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			jsonOut(w, 200, map[string]any{"issuer": issuer, "authorization_endpoint": issuer + "/authorize", "token_endpoint": issuer + "/token", "jwks_uri": issuer + "/keys", "response_types_supported": []string{"code"}, "subject_types_supported": []string{"public"}, "id_token_signing_alg_values_supported": []string{"RS256"}})
		case "/keys":
			jsonOut(w, 200, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "test-key", Algorithm: "RS256", Use: "sig"}}})
		case "/token":
			r.ParseForm()
			sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
			if base64.RawURLEncoding.EncodeToString(sum[:]) != challenge || r.Form.Get("code") != "valid-code" {
				http.Error(w, "bad PKCE", 400)
				return
			}
			claims := map[string]any{"iss": issuer, "sub": "user-123", "aud": "cctv", "exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(), "nonce": nonce, "name": "OIDC Nutzer", "groups": []string{"cctv-viewers"}}
			switch mode {
			case "expired":
				claims["exp"] = time.Now().Add(-time.Hour).Unix()
			case "audience":
				claims["aud"] = "wrong"
			case "issuer":
				claims["iss"] = "https://other.example"
			case "nonce":
				claims["nonce"] = "wrong"
			case "denied":
				claims["groups"] = []string{"other"}
			case "admin":
				claims["groups"] = []string{"cctv-admin"}
			case "missing-groups":
				delete(claims, "groups")
			case "string-group":
				claims["groups"] = "cctv-admin"
			}
			payload, _ := json.Marshal(claims)
			signed, _ := signer.Sign(payload)
			jwt, _ := signed.CompactSerialize()
			if mode == "tampered" {
				parts := strings.Split(jwt, ".")
				parts[1] = base64.RawURLEncoding.EncodeToString([]byte(`{"sub":"attacker"}`))
				jwt = strings.Join(parts, ".")
			}
			jsonOut(w, 200, map[string]any{"access_token": "unused", "token_type": "Bearer", "id_token": jwt, "expires_in": 3600})
		default:
			http.NotFound(w, r)
		}
	}))
	defer provider.Close()
	issuer = provider.URL
	a.cfg.OIDCIssuer = issuer
	a.cfg.OIDCClientID = "cctv"
	a.cfg.OIDCSecret = "secret"
	a.cfg.OIDCGroupClaim = "groups"
	a.cfg.AllowedGroups = []string{"cctv-viewers"}
	a.cfg.AdminGroups = []string{"cctv-admin"}
	ctx := oidc.ClientContext(context.Background(), provider.Client())
	if e = a.initOIDC(ctx); e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		mode string
		want int
	}{{"valid", 303}, {"expired", 401}, {"audience", 401}, {"issuer", 401}, {"nonce", 401}, {"denied", 403}, {"admin", 303}, {"missing-groups", 403}, {"string-group", 403}, {"tampered", 401}} {
		t.Run(tc.mode, func(t *testing.T) {
			mode = tc.mode
			// Use a unique client IP so each case tests OIDC rather than the shared login limiter.
			start := httptest.NewRequest("GET", "/auth/oidc", nil)
			start.RemoteAddr = "192.0.2." + string(rune('1'+len(tc.mode))) + ":1234"
			w := httptest.NewRecorder()
			a.Handler().ServeHTTP(w, start)
			if w.Code != 302 {
				t.Fatal(w.Code, w.Body.String())
			}
			location, e := url.Parse(w.Header().Get("Location"))
			if e != nil {
				t.Fatal(e)
			}
			q := location.Query()
			if q.Get("scope") != "openid profile groups" {
				t.Fatal("group membership must be requested", q.Get("scope"))
			}
			state := q.Get("state")
			nonce = q.Get("nonce")
			challenge = q.Get("code_challenge")
			if nonce == "" || state == "" || q.Get("code_challenge_method") != "S256" || challenge == "" {
				t.Fatal("missing OIDC protections", q)
			}
			cookie := w.Result().Cookies()[0]
			callback := func() *httptest.ResponseRecorder {
				r := httptest.NewRequest("GET", "/auth/oidc/callback?state="+url.QueryEscape(state)+"&code=valid-code", nil).WithContext(ctx)
				r.AddCookie(cookie)
				out := httptest.NewRecorder()
				a.Handler().ServeHTTP(out, r)
				return out
			}
			w = callback()
			if w.Code != tc.want {
				t.Fatal(w.Code, w.Body.String())
			}
			if tc.want == 303 {
				var sessionCookie *http.Cookie
				for _, c := range w.Result().Cookies() {
					if c.Name == "ctv_session" {
						sessionCookie = c
					}
				}
				if sessionCookie == nil {
					t.Fatal("missing session")
				}
				out := request(a, "GET", "/api/session", nil, sessionCookie, "")
				if out.Code != 200 {
					t.Fatal(out.Code, out.Body.String())
				}
				var s Session
				json.Unmarshal(out.Body.Bytes(), &s)
				if tc.mode == "admin" && s.Role != "admin" {
					t.Fatal(s)
				}
				if tc.mode == "valid" && s.Role != "viewer" {
					t.Fatal(s)
				}
			}
			if w = callback(); w.Code != 400 {
				t.Fatal("callback replay accepted", w.Code)
			}
		})
	}
	w := request(a, "GET", "/auth/oidc/callback?state=wrong&code=x", nil, nil, "")
	if w.Code != 400 {
		t.Fatal("unbound callback accepted")
	}
	t.Run("web settings require administrator test before activation", func(t *testing.T) {
		cookie, session := login(t, a)
		send := func(method, target string, body any, cookies ...*http.Cookie) *httptest.ResponseRecorder {
			var b bytes.Buffer
			if body != nil {
				json.NewEncoder(&b).Encode(body)
			}
			r := httptest.NewRequest(method, target, &b).WithContext(ctx)
			r.RemoteAddr = "192.0.2.200:1234"
			r.Header.Set("Origin", a.config().BaseURL)
			r.Header.Set("X-CSRF-Token", session.CSRF)
			for _, c := range cookies {
				r.AddCookie(c)
			}
			w := httptest.NewRecorder()
			a.Handler().ServeHTTP(w, r)
			return w
		}
		values := map[string]any{"oidcIssuer": issuer, "oidcClientId": "cctv", "oidcSecret": "changed secret", "oidcGroupClaim": "groups", "allowedGroups": []string{"cctv-viewers"}, "adminGroups": []string{"cctv-admin"}, "localLogin": false}
		w := send("PUT", "/api/admin/settings", map[string]any{"version": 0, "values": values}, cookie)
		if w.Code != 202 {
			t.Fatal(w.Code, w.Body.String())
		}
		if !a.config().LocalAdmin || a.config().OIDCSecret == "changed secret" {
			t.Fatal("unconfirmed settings activated")
		}
		for _, testMode := range []string{"valid", "admin"} {
			mode = testMode
			w = send("GET", "/auth/oidc/test", nil, cookie)
			if w.Code != 302 {
				t.Fatal(w.Code, w.Body.String())
			}
			location, _ := url.Parse(w.Header().Get("Location"))
			q := location.Query()
			nonce = q.Get("nonce")
			challenge = q.Get("code_challenge")
			state := q.Get("state")
			testCookie := w.Result().Cookies()[0]
			w = send("GET", "/auth/oidc/callback?state="+url.QueryEscape(state)+"&code=valid-code", nil, cookie, testCookie)
			if testMode == "valid" {
				if w.Code != 403 || !a.config().LocalAdmin {
					t.Fatal("viewer activated settings", w.Code, w.Body.String())
				}
				continue
			}
			if w.Code != 303 || a.config().LocalAdmin || a.config().OIDCSecret != "changed secret" {
				t.Fatal("administrator activation failed", w.Code, w.Body.String())
			}
			if out := request(a, "GET", "/api/session", nil, cookie, ""); out.Code != 401 {
				t.Fatal("local session not revoked")
			}
			var newCookie *http.Cookie
			for _, c := range w.Result().Cookies() {
				if c.Name == "ctv_session" {
					newCookie = c
				}
			}
			if newCookie == nil || request(a, "GET", "/api/session", nil, newCookie, "").Code != 200 {
				t.Fatal("confirmed admin session missing")
			}
			if w = send("GET", "/auth/oidc/callback?state="+url.QueryEscape(state)+"&code=valid-code", nil, cookie, testCookie); w.Code != 401 {
				t.Fatal("test replay accepted", w.Code)
			}
		}
	})

}
func TestOIDCAccessDefaults(t *testing.T) {
	a := testApp(t)
	if a.oidcRole("user", map[string]any{}) != "" {
		t.Fatal("default must deny")
	}
	a.cfg.AllowedUsers = []string{"subject-id"}
	if a.oidcRole("subject-id", map[string]any{"email": "spoof@example.org"}) != "viewer" {
		t.Fatal("subject allowlist")
	}
	a.cfg.OIDCGroupClaim = "roles"
	a.cfg.AdminGroups = []string{"admins"}
	if a.oidcRole("other", map[string]any{"roles": []any{"admins"}}) != "admin" {
		t.Fatal("group claim")
	}
}

func TestOIDCAdminGroupOnly(t *testing.T) {
	a := testApp(t)
	a.cfg.OIDCGroupClaim = "groups"
	a.cfg.AdminGroups = []string{"cctv-admin"}
	for _, tc := range []struct {
		name   string
		claims map[string]any
		want   string
	}{
		{"member", map[string]any{"groups": []any{"other", "cctv-admin"}}, "admin"},
		{"nonmember", map[string]any{"groups": []any{"cctv-viewers", "cctv-admins"}}, ""},
		{"missing", map[string]any{}, ""},
		{"wrong-case", map[string]any{"groups": []any{"CCTV-Admin"}}, ""},
		{"malformed", map[string]any{"groups": "cctv-admin"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := a.oidcRole("user", tc.claims); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
