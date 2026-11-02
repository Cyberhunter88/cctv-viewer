package viewer

import (
	"encoding/json"
	"testing"
)

func TestMetricsAreAdminOnlyAndCountLogins(t *testing.T) {
	a := testApp(t)
	if w := request(a, "GET", "/api/admin/metrics", nil, nil, ""); w.Code != 401 {
		t.Fatal("anonymous access must be rejected", w.Code)
	}
	cookie, _ := login(t, a)
	request(a, "POST", "/auth/local", map[string]string{"username": "test", "password": "wrong wrong wrong"}, nil, "")
	w := request(a, "GET", "/api/admin/metrics", nil, cookie, "")
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var m map[string]any
	if e := json.Unmarshal(w.Body.Bytes(), &m); e != nil {
		t.Fatal(e)
	}
	if m["loginSuccess"].(float64) != 1 || m["loginFailures"].(float64) != 1 {
		t.Fatal(m)
	}
}

func TestAccountLockoutAfterRepeatedFailuresAndSuccessReset(t *testing.T) {
	a := testApp(t)
	login(t, a)
	bad := map[string]string{"username": "test", "password": "wrong wrong wrong"}
	for i := 0; i < accountFailureLimit; i++ {
		// Reset the IP counter so only the account lockout is under test.
		a.limits.mu.Lock()
		a.limits.entries = map[string]attempt{}
		a.limits.mu.Unlock()
		if w := request(a, "POST", "/auth/local", bad, nil, ""); w.Code != 401 {
			t.Fatal(i, w.Code)
		}
	}
	a.limits.mu.Lock()
	a.limits.entries = map[string]attempt{}
	a.limits.mu.Unlock()
	good := map[string]string{"username": "test", "password": "a long secure password"}
	if w := request(a, "POST", "/auth/local", good, nil, ""); w.Code != 429 {
		t.Fatal("locked account must be refused", w.Code)
	}
	a.limits.accountSuccess("test")
	if w := request(a, "POST", "/auth/local", good, nil, ""); w.Code != 200 {
		t.Fatal(w.Code)
	}
}

func TestSuccessfulLoginRefundsIPAttempt(t *testing.T) {
	a := testApp(t)
	login(t, a)
	for i := 0; i < 20; i++ {
		good := map[string]string{"username": "test", "password": "a long secure password"}
		if w := request(a, "POST", "/auth/local", good, nil, ""); w.Code != 200 {
			t.Fatal(i, w.Code)
		}
	}
}
