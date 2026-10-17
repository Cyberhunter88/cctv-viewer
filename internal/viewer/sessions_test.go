package viewer

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSessionManagement(t *testing.T) {
	a := testApp(t)
	admin, as := login(t, a)
	// Session fixtures cover both authentication kinds, including identical subjects.
	makeSession := func(subject, kind, role string) (string, Session) {
		t.Helper()
		w := httptest.NewRecorder()
		s := Session{Subject: subject, Name: subject, Kind: kind, Role: role}
		if err := a.newSession(w, s); err != nil {
			t.Fatal(err)
		}
		raw := w.Result().Cookies()[0].Value
		return raw, s
	}
	readerRaw, _ := makeSession("reader", "local", "viewer")
	reader := *admin
	reader.Value = readerRaw
	w := request(a, "GET", "/api/session", nil, &reader, "")
	var rs Session
	if err := json.Unmarshal(w.Body.Bytes(), &rs); err != nil {
		t.Fatal(err)
	}
	otherReaderRaw, _ := makeSession("reader", "local", "viewer")
	oidcRaw, _ := makeSession("reader", "oidc", "viewer")
	expiredRaw, _ := makeSession("expired", "local", "viewer")
	if _, err := a.db.Exec(`UPDATE sessions SET expires=? WHERE hash=?`, time.Now().Unix()-1, digest(expiredRaw)); err != nil {
		t.Fatal(err)
	}
	list := func(url string, raw string) []managedSession {
		t.Helper()
		cookie := *admin
		cookie.Value = raw
		w := request(a, "GET", url, nil, &cookie, "")
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		for _, secret := range []string{raw, digest(raw), rs.CSRF, as.CSRF} {
			if secret != "" && strings.Contains(w.Body.String(), secret) {
				t.Fatal("session response leaked authentication material")
			}
		}
		var out []managedSession
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	own := list("/api/sessions", readerRaw)
	if len(own) != 2 {
		t.Fatal("wrong ownership filtering", own)
	}
	current := 0
	for _, s := range own {
		if s.Current {
			current++
		}
		if s.Kind != "local" {
			t.Fatal("cross-kind visibility")
		}
	}
	if current != 1 {
		t.Fatal("current session identification")
	}
	all := list("/api/admin/sessions", admin.Value)
	if len(all) != 4 {
		t.Fatal("expired session visible", all)
	}
	oidcID := sessionManagementID(digest(oidcRaw))
	if w = request(a, "DELETE", "/api/sessions/"+oidcID, nil, &reader, rs.CSRF); w.Code != 404 {
		t.Fatal("cross-kind revoke", w.Code)
	}
	if w = request(a, "GET", "/api/admin/sessions", nil, &reader, ""); w.Code != 403 {
		t.Fatal("viewer listed admin sessions", w.Code)
	}
	if w = request(a, "DELETE", "/api/admin/sessions/"+oidcID, nil, &reader, rs.CSRF); w.Code != 403 {
		t.Fatal("viewer revoked admin sessions", w.Code)
	}
	otherID := sessionManagementID(digest(otherReaderRaw))
	if w = request(a, "DELETE", "/api/sessions/"+otherID, nil, &reader, ""); w.Code != 403 {
		t.Fatal("missing CSRF accepted", w.Code)
	}
	if w = request(a, "DELETE", "/api/sessions/"+otherID, nil, &reader, rs.CSRF); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w = request(a, "DELETE", "/api/sessions/"+otherID, nil, &reader, rs.CSRF); w.Code != 404 {
		t.Fatal("stale ID accepted", w.Code)
	}
	if w = request(a, "DELETE", "/api/sessions/"+sessionManagementID(digest(expiredRaw)), nil, admin, as.CSRF); w.Code != 404 {
		t.Fatal("expired ID accepted", w.Code)
	}
	if w = request(a, "DELETE", "/api/admin/sessions/"+oidcID, nil, admin, as.CSRF); w.Code != 200 {
		t.Fatal("admin revoke", w.Code)
	}
	if w = request(a, "DELETE", "/api/sessions", nil, &reader, rs.CSRF); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w = request(a, "GET", "/api/session", nil, &reader, ""); w.Code != 401 {
		t.Fatal("revoked current session survived")
	}
	if len(list("/api/sessions", admin.Value)) != 1 {
		t.Fatal("own revoke affected other account")
	}
	if w = request(a, "DELETE", "/api/sessions/"+sessionManagementID(digest(admin.Value)), nil, admin, as.CSRF); w.Code != 200 {
		t.Fatal("current revoke", w.Code)
	}
	if len(w.Result().Cookies()) != 1 || w.Result().Cookies()[0].MaxAge != -1 {
		t.Fatal("cookie not cleared")
	}
	if w = request(a, "GET", "/api/sessions", nil, admin, ""); w.Code != 401 {
		t.Fatal("anonymous listing allowed")
	}
}
