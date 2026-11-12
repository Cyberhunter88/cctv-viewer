package viewer

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPrometheusMetricsRequireConfiguredToken(t *testing.T) {
	a := testApp(t)
	get := func(authorization string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/metrics", nil)
		if authorization != "" {
			r.Header.Set("Authorization", authorization)
		}
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, r)
		return w
	}
	// Without a token the endpoint does not exist, even for administrators.
	cookie, _ := login(t, a)
	if w := request(a, "GET", "/metrics", nil, cookie, ""); w.Code != 404 {
		t.Fatal(w.Code)
	}
	if w := get("Bearer anything"); w.Code != 404 {
		t.Fatal(w.Code)
	}
	const token = "synthetic-metrics-token-0001"
	a.cfgMu.Lock()
	a.cfg.MetricsToken = token
	a.cfgMu.Unlock()
	for _, authorization := range []string{"", "Bearer wrong", "Bearer " + token + "x", "Basic " + token, token, "bearer " + token} {
		if w := get(authorization); w.Code != 401 || w.Header().Get("WWW-Authenticate") == "" || strings.Contains(w.Body.String(), "cctv_") {
			t.Fatal(authorization, w.Code, w.Body.String())
		}
	}
	// A session cookie is not a substitute for the token.
	if w := request(a, "GET", "/metrics", nil, cookie, ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	w := get("Bearer " + token)
	if w.Code != 200 || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/plain; version=0.0.4") {
		t.Fatal(w.Code, w.Header())
	}
	body := w.Body.String()
	for _, line := range []string{
		"# TYPE cctv_login_success_total counter\ncctv_login_success_total 1\n",
		"# TYPE cctv_login_failures_total counter\ncctv_login_failures_total 0\n",
		"# TYPE cctv_encoders_limit gauge\ncctv_encoders_limit 1\n",
		"# TYPE cctv_stream_sessions gauge\ncctv_stream_sessions 0\n",
		"# HELP cctv_uptime_seconds ",
	} {
		if !strings.Contains(body, line) {
			t.Fatalf("missing %q in\n%s", line, body)
		}
	}
	for _, secret := range []string{token, "test", a.cfg.DataDir} {
		if strings.Contains(body, secret) {
			t.Fatal("metrics expose", secret)
		}
	}
}

func TestMetricsTokenConfiguration(t *testing.T) {
	for _, key := range []string{"CTV_DEV_HTTP", "CTV_LOCAL_ADMIN", "CTV_OIDC_ISSUER", "CTV_OIDC_CLIENT_ID", "CTV_OIDC_CLIENT_SECRET", "CTV_OIDC_CLIENT_SECRET_FILE", "CTV_MAX_TRANSCODERS", "CTV_CACHE_MB", "CTV_SCAN_INTERVAL_SECONDS", "CTV_TRUSTED_PROXIES", "CTV_SESSION_MINUTES", "CTV_METRICS_TOKEN", "CTV_METRICS_TOKEN_FILE"} {
		t.Setenv(key, "")
	}
	t.Setenv("CTV_BASE_URL", "https://cctv.example.org")
	if c, err := LoadConfig(); err != nil || c.MetricsToken != "" {
		t.Fatal(c.MetricsToken, err)
	}
	t.Setenv("CTV_METRICS_TOKEN", "too-short")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("short metrics token accepted")
	}
	t.Setenv("CTV_METRICS_TOKEN", "synthetic-metrics-token-0001")
	if c, err := LoadConfig(); err != nil || c.MetricsToken != "synthetic-metrics-token-0001" {
		t.Fatal(c.MetricsToken, err)
	}
}
