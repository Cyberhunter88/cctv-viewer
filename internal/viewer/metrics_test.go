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
