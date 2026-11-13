package viewer

import (
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestTOTPCodesFollowRFC6238(t *testing.T) {
	// RFC 6238 appendix B, SHA-1 key "12345678901234567890", truncated to six digits.
	secret := totpEncoding.EncodeToString([]byte("12345678901234567890"))
	for unix, want := range map[int64]string{59: "287082", 1111111109: "081804", 1111111111: "050471", 1234567890: "005924", 2000000000: "279037", 20000000000: "353130"} {
		got, err := totpCode(secret, unix/totpPeriod)
		if err != nil || got != want {
			t.Fatalf("time %d: got %s, want %s (%v)", unix, got, want, err)
		}
	}
	now := time.Unix(1111111111, 0)
	current := now.Unix() / totpPeriod
	if step, ok := totpStep(secret, "050471", now, 0); !ok || step != current {
		t.Fatal(step, ok)
	}
	if step, ok := totpStep(secret, " 050 471 ", now, 0); !ok || step != current {
		t.Fatal("spaces must be ignored", step, ok)
	}
	// One step of clock drift is tolerated, two are not.
	previous, _ := totpCode(secret, current-1)
	next, _ := totpCode(secret, current+1)
	tooOld, _ := totpCode(secret, current-2)
	if step, ok := totpStep(secret, previous, now, 0); !ok || step != current-1 {
		t.Fatal(step, ok)
	}
	if step, ok := totpStep(secret, next, now, 0); !ok || step != current+1 {
		t.Fatal(step, ok)
	}
	if _, ok := totpStep(secret, tooOld, now, 0); ok {
		t.Fatal("stale code accepted")
	}
	// A used step and everything before it stays rejected.
	if _, ok := totpStep(secret, "050471", now, current); ok {
		t.Fatal("code replayed")
	}
	if _, ok := totpStep(secret, previous, now, current); ok {
		t.Fatal("older code accepted after a newer one")
	}
	for _, code := range []string{"", "05047", "0504711", "abcdef", "050472"} {
		if _, ok := totpStep(secret, code, now, 0); ok {
			t.Fatal("accepted", code)
		}
	}
	if _, err := totpCode("not base32!", 1); err == nil {
		t.Fatal("invalid secret accepted")
	}
	first, _ := newTOTPSecret()
	second, _ := newTOTPSecret()
	if len(first) != 32 || first == second {
		t.Fatal(first, second)
	}
}

func currentCode(t *testing.T, secret string, offset int64) string {
	t.Helper()
	code, err := totpCode(secret, time.Now().Unix()/totpPeriod+offset)
	if err != nil {
		t.Fatal(err)
	}
	return code
}

func TestTOTPSetupLoginAndRemoval(t *testing.T) {
	a := testApp(t)
	cookie, s := login(t, a)
	post := func(url string, body any) *httptest.ResponseRecorder {
		return request(a, "POST", url, body, cookie, s.CSRF)
	}
	signIn := func(code string) *httptest.ResponseRecorder {
		a.limits.mu.Lock()
		a.limits.entries = map[string]attempt{}
		a.limits.mu.Unlock()
		body := map[string]string{"username": "test", "password": "a long secure password"}
		if code != "" {
			body["code"] = code
		}
		return request(a, "POST", "/auth/local", body, nil, "")
	}
	if w := request(a, "GET", "/api/totp", nil, nil, ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := request(a, "GET", "/api/totp", nil, cookie, ""); w.Code != 200 || !strings.Contains(w.Body.String(), `"available":true`) || !strings.Contains(w.Body.String(), `"enabled":false`) {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := post("/api/totp/enable", map[string]string{"code": "000000"}); w.Code != 409 {
		t.Fatal("enable without setup", w.Code)
	}
	if w := request(a, "POST", "/api/totp/setup", nil, cookie, ""); w.Code != 403 {
		t.Fatal("setup without CSRF token", w.Code)
	}
	w := post("/api/totp/setup", nil)
	var setup struct{ Secret, URI string }
	if err := json.Unmarshal(w.Body.Bytes(), &setup); err != nil || w.Code != 200 || len(setup.Secret) != 32 {
		t.Fatal(w.Code, w.Body.String())
	}
	address, err := url.Parse(setup.URI)
	if err != nil || address.Scheme != "otpauth" || address.Host != "totp" || address.Path != "/CCTV Viewer:test" {
		t.Fatal(setup.URI, err)
	}
	if query := address.Query(); query.Get("secret") != setup.Secret || query.Get("issuer") != "CCTV Viewer" || query.Get("digits") != "6" || query.Get("period") != "30" || query.Get("algorithm") != "SHA1" {
		t.Fatal(setup.URI)
	}
	var stored string
	if err := a.db.QueryRow(`SELECT totp_secret FROM admins WHERE username='test'`).Scan(&stored); err != nil || !strings.HasPrefix(stored, secretPrefix) || strings.Contains(stored, setup.Secret) {
		t.Fatal("secret not stored encrypted", err)
	}
	// A pending setup does not affect signing in.
	if w := signIn(""); w.Code != 200 {
		t.Fatal("pending setup required a code", w.Code, w.Body.String())
	}
	if w := post("/api/totp/enable", map[string]string{"code": "000000"}); w.Code != 400 && currentCode(t, setup.Secret, 0) != "000000" {
		t.Fatal("wrong code enabled the second factor", w.Code)
	}
	code := currentCode(t, setup.Secret, 0)
	if w := post("/api/totp/enable", map[string]string{"code": code}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := post("/api/totp/setup", nil); w.Code != 409 {
		t.Fatal("active second factor was replaced", w.Code)
	}
	if w := request(a, "GET", "/api/admin/users", nil, cookie, ""); !strings.Contains(w.Body.String(), `"totp":true`) || strings.Contains(w.Body.String(), setup.Secret) {
		t.Fatal(w.Body.String())
	}
	// Password alone asks for the code without counting as a failed attempt.
	failures := a.metrics.loginFailures.Load()
	w = signIn("")
	if w.Code != 401 || !strings.Contains(w.Body.String(), `"totpRequired":true`) || len(w.Result().Cookies()) != 0 || a.metrics.loginFailures.Load() != failures {
		t.Fatal(w.Code, w.Body.String())
	}
	// The code that enabled the factor cannot be replayed to sign in.
	if w := signIn(code); w.Code != 401 || len(w.Result().Cookies()) != 0 {
		t.Fatal("replayed code accepted", w.Code)
	}
	if a.metrics.loginFailures.Load() != failures+1 {
		t.Fatal("wrong code not counted", a.metrics.loginFailures.Load())
	}
	// A wrong password never reveals whether a second factor exists.
	a.limits.mu.Lock()
	a.limits.entries = map[string]attempt{}
	a.limits.mu.Unlock()
	if w := request(a, "POST", "/auth/local", map[string]string{"username": "test", "password": "wrong wrong wrong wrong", "code": currentCode(t, setup.Secret, 1)}, nil, ""); w.Code != 401 || strings.Contains(w.Body.String(), "totpRequired") {
		t.Fatal(w.Code, w.Body.String())
	}
	next := currentCode(t, setup.Secret, 1)
	w = signIn(next)
	if w.Code != 200 || len(w.Result().Cookies()) == 0 {
		t.Fatal("valid code rejected", w.Code, w.Body.String())
	}
	if w := signIn(next); w.Code != 401 {
		t.Fatal("code accepted twice", w.Code)
	}
	// Removing the factor needs the password.
	a.limits.mu.Lock()
	a.limits.entries = map[string]attempt{}
	a.limits.mu.Unlock()
	if w := post("/api/totp/disable", map[string]string{"password": "not the password at all"}); w.Code != 403 {
		t.Fatal(w.Code)
	}
	if w := post("/api/totp/disable", map[string]string{"password": "a long secure password"}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := signIn(""); w.Code != 200 {
		t.Fatal("removed factor still required", w.Code)
	}
	var secret string
	var enabled bool
	if err := a.db.QueryRow(`SELECT totp_secret,totp_enabled FROM admins WHERE username='test'`).Scan(&secret, &enabled); err != nil || secret != "" || enabled {
		t.Fatal(secret, enabled, err)
	}
	var audited int
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE action IN ('totp.enable','totp.disable') AND ok=1`).Scan(&audited); err != nil || audited != 2 {
		t.Fatal(audited, err)
	}
	if err := a.db.QueryRow(`SELECT COUNT(*) FROM audit_log WHERE target LIKE '%'||?||'%' OR actor LIKE '%'||?||'%'`, setup.Secret, setup.Secret).Scan(&audited); err != nil || audited != 0 {
		t.Fatal("secret in audit log", audited, err)
	}
}

func TestTOTPAdministratorResetAndPermissions(t *testing.T) {
	a := testApp(t)
	cookie, s := login(t, a)
	if err := a.db.CreateAdmin("viewer", "another long password"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`UPDATE admins SET role='viewer' WHERE username='viewer'`); err != nil {
		t.Fatal(err)
	}
	w := request(a, "POST", "/auth/local", map[string]string{"username": "viewer", "password": "another long password"}, nil, "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	viewerCookie := w.Result().Cookies()[0]
	var viewerSession Session
	json.Unmarshal(request(a, "GET", "/api/session", nil, viewerCookie, "").Body.Bytes(), &viewerSession)
	// Viewers manage their own second factor but cannot reset others.
	w = request(a, "POST", "/api/totp/setup", nil, viewerCookie, viewerSession.CSRF)
	var setup struct{ Secret string }
	if err := json.Unmarshal(w.Body.Bytes(), &setup); err != nil || w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request(a, "POST", "/api/totp/enable", map[string]string{"code": currentCode(t, setup.Secret, 0)}, viewerCookie, viewerSession.CSRF); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request(a, "DELETE", "/api/admin/users/test/totp", nil, viewerCookie, viewerSession.CSRF); w.Code != 403 {
		t.Fatal("viewer reset", w.Code)
	}
	if w := request(a, "DELETE", "/api/admin/users/missing/totp", nil, cookie, s.CSRF); w.Code != 404 {
		t.Fatal(w.Code)
	}
	if w := request(a, "DELETE", "/api/admin/users/viewer/totp", nil, cookie, s.CSRF); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	a.limits.mu.Lock()
	a.limits.entries = map[string]attempt{}
	a.limits.mu.Unlock()
	if w := request(a, "POST", "/auth/local", map[string]string{"username": "viewer", "password": "another long password"}, nil, ""); w.Code != 200 {
		t.Fatal("reset account still asks for a code", w.Code, w.Body.String())
	}
	var target string
	if err := a.db.QueryRow(`SELECT target FROM audit_log WHERE action='totp.reset' AND ok=1`).Scan(&target); err != nil || target != "viewer" {
		t.Fatal(target, err)
	}
}

func TestSchemaVersionSevenAddsSecondFactorColumns(t *testing.T) {
	dir := t.TempDir()
	store, err := OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.CreateAdmin("kept", "a long secure password"); err != nil {
		t.Fatal(err)
	}
	// Simulate a version 6 database without the new columns.
	if _, err = store.Exec(`ALTER TABLE admins DROP COLUMN totp_secret; ALTER TABLE admins DROP COLUMN totp_enabled; ALTER TABLE admins DROP COLUMN totp_step; PRAGMA user_version=6`); err != nil {
		t.Fatal(err)
	}
	store.Close()
	store, err = OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	var version int
	var secret string
	var enabled bool
	var step int64
	store.QueryRow(`PRAGMA user_version`).Scan(&version)
	if err = store.QueryRow(`SELECT totp_secret,totp_enabled,totp_step FROM admins WHERE username='kept'`).Scan(&secret, &enabled, &step); err != nil || version != 7 || secret != "" || enabled || step != 0 {
		t.Fatal(version, secret, enabled, step, err)
	}
	store.Close()
	// A database from a newer release is refused instead of being downgraded.
	db, err := sql.Open("sqlite", filepath.Join(dir, "viewer.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`PRAGMA user_version=8`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if _, err = OpenStore(dir); err == nil {
		t.Fatal("newer schema accepted")
	}
}
