package viewer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestPreferencesOwnershipValidationAndMerge(t *testing.T) {
	a := testApp(t)
	cookie, s := login(t, a)
	if w := request(a, "GET", "/api/preferences", nil, nil, ""); w.Code != 401 {
		t.Fatal(w.Code)
	}
	if w := request(a, "GET", "/api/preferences", nil, cookie, ""); w.Code != 200 || w.Body.String() != "null\n" {
		t.Fatal(w.Code, w.Body.String())
	}
	patch := func(body any) *httptest.ResponseRecorder {
		return request(a, "PATCH", "/api/preferences", body, cookie, s.CSRF)
	}
	if w := request(a, "PATCH", "/api/preferences", map[string]any{"theme": "dark"}, cookie, ""); w.Code != 403 {
		t.Fatal(w.Code)
	}
	for _, body := range []any{nil, map[string]any{"other": true}, map[string]any{"theme": "purple"}, map[string]any{"rows": 0}, map[string]any{"rows": 1.5}, map[string]any{"shortcuts": "yes"}, map[string]any{"seekStep": 11}, map[string]any{"theme": nil}, map[string]any{"cameras": []any{1}}, map[string]any{"timelineHeight": 999}} {
		if w := patch(body); w.Code != 400 {
			t.Fatalf("accepted %#v: %d %s", body, w.Code, w.Body.String())
		}
	}
	if w := patch(map[string]any{"theme": "dark", "layout": "2x2", "railCollapsed": true}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var wg sync.WaitGroup
	for key, value := range map[string]any{"density": "compact", "seekStep": 30, "railCollapsed": nil} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if w := patch(map[string]any{key: value}); w.Code != 200 {
				t.Errorf("patch: %d %s", w.Code, w.Body.String())
			}
		}()
	}
	wg.Wait()
	var got map[string]any
	w := request(a, "GET", "/api/preferences", nil, cookie, "")
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["theme"] != "dark" || got["layout"] != "2x2" || got["density"] != "compact" || got["seekStep"] != float64(30) {
		t.Fatal(got)
	}
	if _, ok := got["railCollapsed"]; ok {
		t.Fatal("adaptive rail reset not applied", got)
	}
	// The same account in a new session sees durable values; another local
	// subject with the same visible name must not share them.
	for _, identity := range []Session{{Kind: "local", Subject: "other", Name: "test", Role: "viewer"}, {Kind: "local", Subject: "test", Name: "test", Role: "viewer"}} {
		w := httptest.NewRecorder()
		if err := a.newSession(w, identity); err != nil {
			t.Fatal(err)
		}
		c := w.Result().Cookies()[0]
		response := request(a, "GET", "/api/preferences", nil, c, "")
		if response.Code != 200 {
			t.Fatal(response.Code, response.Body.String())
		}
		if identity.Subject == "test" {
			if response.Body.String() == "null\n" {
				t.Fatal("new session lost preferences")
			}
		} else if response.Body.String() != "null\n" {
			t.Fatal("other user received preferences", response.Body.String())
		}
	}
	// Viewer writes are allowed, but cannot choose another account in the body.
	w = httptest.NewRecorder()
	_ = a.newSession(w, Session{Kind: "local", Subject: "viewer", Role: "viewer"})
	c := w.Result().Cookies()[0]
	var viewer Session
	_ = json.Unmarshal(request(a, "GET", "/api/session", nil, c, "").Body.Bytes(), &viewer)
	if response := request(a, "PATCH", "/api/preferences", map[string]any{"theme": "light"}, c, viewer.CSRF); response.Code != 200 {
		t.Fatal(response.Code)
	}
	if response := request(a, "PATCH", "/api/preferences", map[string]any{"owner": "test"}, c, viewer.CSRF); response.Code != 400 {
		t.Fatal(response.Code)
	}
}

func TestPreferencesInitializationAndStoreReopen(t *testing.T) {
	a := testApp(t)
	cookie, s := login(t, a)
	initialize := func(theme string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("PATCH", "/api/preferences", strings.NewReader(`{"theme":"`+theme+`"}`))
		r.AddCookie(cookie)
		r.Header.Set("Origin", a.cfg.BaseURL)
		r.Header.Set("X-CSRF-Token", s.CSRF)
		r.Header.Set("If-None-Match", "*")
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, r)
		return w
	}
	if w := initialize("dark"); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := initialize("light"); w.Code != http.StatusPreconditionFailed {
		t.Fatal(w.Code)
	}
	// Close the writer and open the database again, as after a server restart.
	if err := a.db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := OpenStore(a.cfg.DataDir)
	if err != nil {
		t.Fatal(err)
	}
	a.db = db
	response := request(a, "GET", "/api/preferences", nil, cookie, "")
	if response.Code != 200 || response.Body.String() != "{\"theme\":\"dark\"}\n" {
		t.Fatal(response.Code, response.Body.String())
	}
}

func TestPreferencesOIDCSubjectIsolation(t *testing.T) {
	a := testApp(t)
	// Exercise the handler after authentication, with stable issuer-qualified
	// OIDC subjects. Display names and local usernames cannot choose ownership.
	identities := []Session{{Kind: "oidc", Subject: "https://issuer.example\x00one", Name: "same"}, {Kind: "oidc", Subject: "https://issuer.example\x00two", Name: "same"}, {Kind: "oidc", Subject: "https://other.example\x00one", Name: "same"}, {Kind: "local", Subject: "https://issuer.example\x00one", Name: "same"}}
	for i, s := range identities {
		r := httptest.NewRequest("GET", "/api/preferences", nil).WithContext(context.WithValue(context.Background(), sessionKey, s))
		w := httptest.NewRecorder()
		a.preferences(w, r)
		if w.Code != 200 || w.Body.String() != "null\n" {
			t.Fatal("identity received another account's settings", i, w.Body.String())
		}
		r = httptest.NewRequest("PATCH", "/api/preferences", strings.NewReader(`{"theme":"dark"}`)).WithContext(context.WithValue(context.Background(), sessionKey, s))
		w = httptest.NewRecorder()
		a.preferences(w, r)
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	var owners int
	if err := a.db.QueryRow(`SELECT count(*) FROM user_preferences`).Scan(&owners); err != nil || owners != len(identities) {
		t.Fatal(owners, err)
	}
}

func TestPreferencesRemovedWithLocalAccount(t *testing.T) {
	a := testApp(t)
	cookie, s := login(t, a)
	if err := a.db.CreateAdmin("synthetic-secondary", "a synthetic long password"); err != nil {
		t.Fatal(err)
	}
	owner := preferenceOwner(Session{Kind: "local", Subject: "synthetic-secondary"})
	if _, err := a.db.Exec(`INSERT INTO user_preferences(owner,payload) VALUES(?,?)`, owner, `{"theme":"dark"}`); err != nil {
		t.Fatal(err)
	}
	response := request(a, "DELETE", "/api/admin/users/synthetic-secondary", nil, cookie, s.CSRF)
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	var count int
	if err := a.db.QueryRow(`SELECT count(*) FROM user_preferences WHERE owner=?`, owner).Scan(&count); err != nil || count != 0 {
		t.Fatal(count, err)
	}
}
