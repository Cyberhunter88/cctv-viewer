package viewer

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSavedViewsValidationAndReplacement(t *testing.T) {
	a := testApp(t)
	cookie, s := login(t, a)
	patch := func(views any) *httptest.ResponseRecorder {
		return request(a, "PATCH", "/api/preferences", map[string]any{"views": views}, cookie, s.CSRF)
	}
	view := func(name string) map[string]any {
		return map[string]any{"name": name, "cameras": []string{"front", "garden"}, "layout": "2x2", "columns": 2, "rows": 2}
	}
	with := func(key string, value any) map[string]any {
		entry := view("Außen")
		entry[key] = value
		return entry
	}
	without := func(key string) map[string]any {
		entry := view("Außen")
		delete(entry, key)
		return entry
	}
	many := []any{}
	for i := 0; i < 21; i++ {
		many = append(many, view(fmt.Sprint("Ansicht ", i)))
	}
	tooManyCameras := make([]string, 257)
	for i := range tooManyCameras {
		tooManyCameras[i] = fmt.Sprint("camera-", i)
	}
	invalid := map[string]any{
		"null":            nil,
		"object":          map[string]any{"name": "x"},
		"too many":        many,
		"empty name":      []any{with("name", "")},
		"padded name":     []any{with("name", " Außen")},
		"long name":       []any{with("name", strings.Repeat("ä", 61))},
		"duplicate name":  []any{view("Außen"), view("Außen")},
		"numeric name":    []any{with("name", 5)},
		"unknown layout":  []any{with("layout", "9x9")},
		"zero columns":    []any{with("columns", 0)},
		"fraction rows":   []any{with("rows", 2.5)},
		"too many rows":   []any{with("rows", 9)},
		"empty camera id": []any{with("cameras", []string{""})},
		"camera overflow": []any{with("cameras", tooManyCameras)},
		"null cameras":    []any{with("cameras", nil)},
		"unknown field":   []any{with("script", "x")},
		"missing name":    []any{without("name")},
		"missing cameras": []any{without("cameras")},
		"missing layout":  []any{without("layout")},
		"missing rows":    []any{without("rows")},
	}
	for name, views := range invalid {
		if w := patch(views); w.Code != 400 {
			t.Fatalf("%s accepted: %d %s", name, w.Code, w.Body.String())
		}
	}
	// A name of exactly 60 characters and an empty selection are valid.
	longest := strings.Repeat("ä", 60)
	if w := patch([]any{view("Außen"), with("name", longest), map[string]any{"name": "Leer", "cameras": []string{}, "layout": "custom", "columns": 8, "rows": 1}}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request(a, "PATCH", "/api/preferences", map[string]any{"theme": "dark"}, cookie, s.CSRF); w.Code != 200 {
		t.Fatal(w.Code)
	}
	// Saving a shorter list replaces the stored list instead of merging entries.
	if w := patch([]any{view("Innen")}); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	var stored struct {
		Theme string `json:"theme"`
		Views []struct {
			Name    string   `json:"name"`
			Cameras []string `json:"cameras"`
			Layout  string   `json:"layout"`
			Columns int      `json:"columns"`
			Rows    int      `json:"rows"`
		} `json:"views"`
	}
	w := request(a, "GET", "/api/preferences", nil, cookie, "")
	if err := json.Unmarshal(w.Body.Bytes(), &stored); err != nil {
		t.Fatal(err, w.Body.String())
	}
	if stored.Theme != "dark" || len(stored.Views) != 1 || stored.Views[0].Name != "Innen" || fmt.Sprint(stored.Views[0].Cameras) != "[front garden]" || stored.Views[0].Layout != "2x2" || stored.Views[0].Columns != 2 || stored.Views[0].Rows != 2 {
		t.Fatal(w.Body.String())
	}
	if w := patch([]any{}); w.Code != 200 {
		t.Fatal(w.Code)
	}
	w = request(a, "GET", "/api/preferences", nil, cookie, "")
	if !strings.Contains(w.Body.String(), `"views":[]`) {
		t.Fatal(w.Body.String())
	}
}
