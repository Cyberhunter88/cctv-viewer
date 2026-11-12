package viewer

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func assetRequest(a *App, path string, headers map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", path, nil)
	for key, value := range headers {
		r.Header.Set(key, value)
	}
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	return w
}

func TestStaticAssetsAreCompressedAndRevalidated(t *testing.T) {
	a := testApp(t)
	script := strings.Repeat("export const ready = true;\n", 200)
	a.assets = fstest.MapFS{"index.html": {Data: []byte("<!doctype html>")}, "app.js": {Data: []byte(script)}, "favicon.ico": {Data: bytes.Repeat([]byte{0}, 4096)}, "secret.txt": {Data: []byte("x")}}
	plain := assetRequest(a, "/app.js", nil)
	if plain.Code != 200 || plain.Body.String() != script || plain.Header().Get("Content-Encoding") != "" || plain.Header().Get("Cache-Control") != "no-cache" || plain.Header().Get("Vary") != "Accept-Encoding" || plain.Header().Get("ETag") == "" {
		t.Fatal(plain.Code, plain.Header())
	}
	packed := assetRequest(a, "/app.js", map[string]string{"Accept-Encoding": "br, gzip"})
	if packed.Code != 200 || packed.Header().Get("Content-Encoding") != "gzip" || packed.Body.Len() >= len(script) || packed.Header().Get("ETag") == plain.Header().Get("ETag") {
		t.Fatal(packed.Code, packed.Header(), packed.Body.Len())
	}
	reader, err := gzip.NewReader(packed.Body)
	if err != nil {
		t.Fatal(err)
	}
	if body, _ := io.ReadAll(reader); string(body) != script {
		t.Fatal("compressed asset differs")
	}
	if w := assetRequest(a, "/app.js", map[string]string{"Accept-Encoding": "gzip;q=0"}); w.Header().Get("Content-Encoding") != "" {
		t.Fatal("refused coding was used")
	}
	for _, headers := range []map[string]string{
		{"If-None-Match": plain.Header().Get("ETag")},
		{"If-None-Match": "W/" + packed.Header().Get("ETag"), "Accept-Encoding": "gzip"},
	} {
		if w := assetRequest(a, "/app.js", headers); w.Code != http.StatusNotModified || w.Body.Len() != 0 {
			t.Fatal(w.Code, w.Body.Len())
		}
	}
	if w := assetRequest(a, "/app.js", map[string]string{"If-None-Match": `"other"`}); w.Code != 200 {
		t.Fatal(w.Code)
	}
	if w := assetRequest(a, "/favicon.ico", map[string]string{"Accept-Encoding": "gzip"}); w.Code != 200 || w.Header().Get("Content-Encoding") != "" {
		t.Fatal(w.Code, w.Header())
	}
	if w := assetRequest(a, "/", nil); w.Code != 200 || w.Body.String() != "<!doctype html>" || w.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatal(w.Code, w.Header(), w.Body.String())
	}
	for _, path := range []string{"/secret.txt", "/style.css", "/chunks/other.js", "/index.html/"} {
		if w := assetRequest(a, path, nil); w.Code != 404 {
			t.Fatal(path, w.Code)
		}
	}
	// API responses stay uncached.
	cookie, _ := login(t, a)
	if w := request(a, "GET", "/api/session", nil, cookie, ""); w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("ETag") != "" {
		t.Fatal(w.Header())
	}
}

func thumbnailFixture(t *testing.T, a *App) (Recording, string) {
	t.Helper()
	folder := filepath.Join(a.cfg.RecordingsDir, "cam")
	if err := os.MkdirAll(folder, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(folder, "clip.mp4")
	if err := os.WriteFile(file, []byte("not a real video"), 0600); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.db.SaveCamera(Camera{ID: "cam", Name: "Kamera", Folder: "cam", Pattern: "{YYYY}{MM}{DD}{hh}{mm}{ss}.mp4", Timezone: "Europe/Berlin"}); err != nil {
		t.Fatal(err)
	}
	record := Recording{ID: "clip", Duration: 60, Mod: st.ModTime().UnixNano()}
	if _, err = a.db.Exec(`INSERT INTO recordings(id,camera_id,path,start,duration,codec,size,mod,filename) VALUES(?,'cam','cam/clip.mp4',100,?,'h264',?,?,'clip.mp4')`, record.ID, record.Duration, st.Size(), record.Mod); err != nil {
		t.Fatal(err)
	}
	return record, filepath.Join(a.cfg.DataDir, "cache")
}

func TestThumbnailCacheRevalidationAndPosition(t *testing.T) {
	a := testApp(t)
	cookie, _ := login(t, a)
	record, cache := thumbnailFixture(t, a)
	base := record.ID + "-" + strconv.FormatInt(record.Mod, 10)
	image := []byte{0xff, 0xd8, 1, 2, 3}
	for _, name := range []string{base + ".jpg", base + "-20.jpg"} {
		if err := os.WriteFile(filepath.Join(cache, name), append(append([]byte{}, image...), name...), 0600); err != nil {
			t.Fatal(err)
		}
	}
	get := func(query, match string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("GET", "/api/recordings/clip/thumbnail"+query, nil)
		r.AddCookie(cookie)
		if match != "" {
			r.Header.Set("If-None-Match", match)
		}
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, r)
		return w
	}
	first := get("", "")
	if first.Code != 200 || !bytes.HasPrefix(first.Body.Bytes(), image) || first.Header().Get("Cache-Control") != "private, no-cache" || first.Header().Get("ETag") != `"`+base+`"` {
		t.Fatal(first.Code, first.Header(), first.Body.String())
	}
	if w := get("", first.Header().Get("ETag")); w.Code != http.StatusNotModified || w.Body.Len() != 0 {
		t.Fatal(w.Code, w.Body.Len())
	}
	// Positions share ten-second steps and never reuse the start image.
	for _, query := range []string{"?t=20", "?t=29.9"} {
		if w := get(query, ""); w.Code != 200 || !strings.HasSuffix(w.Body.String(), "-20.jpg") || w.Header().Get("ETag") != `"`+base+`-20"` {
			t.Fatal(query, w.Code, w.Header(), w.Body.String())
		}
	}
	if w := get("?t=9", ""); w.Code != 200 || w.Header().Get("ETag") != `"`+base+`"` {
		t.Fatal(w.Code, w.Header())
	}
	for _, query := range []string{"?t=-1", "?t=60", "?t=NaN", "?t=abc", "?t=1e400"} {
		if w := get(query, ""); w.Code != 400 {
			t.Fatal(query, w.Code)
		}
	}
	r := httptest.NewRequest("GET", "/api/recordings/clip/thumbnail", nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("unauthenticated thumbnail", w.Code)
	}
}

func TestThumbnailRequestsWaitForFreeSlot(t *testing.T) {
	a := testApp(t)
	cookie, _ := login(t, a)
	record, cache := thumbnailFixture(t, a)
	for i := 0; i < cap(a.thumbSlots); i++ {
		a.thumbSlots <- struct{}{}
	}
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		r := httptest.NewRequest("GET", "/api/recordings/clip/thumbnail", nil)
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		a.Handler().ServeHTTP(w, r)
		done <- w
	}()
	select {
	case w := <-done:
		t.Fatal("request did not wait for a slot", w.Code)
	case <-time.After(150 * time.Millisecond):
	}
	// Another request finished the same preview while this one was queued.
	name := filepath.Join(cache, record.ID+"-"+strconv.FormatInt(record.Mod, 10)+".jpg")
	if err := os.WriteFile(name, []byte{0xff, 0xd8, 9}, 0600); err != nil {
		t.Fatal(err)
	}
	<-a.thumbSlots
	select {
	case w := <-done:
		if w.Code != 200 || !bytes.Equal(w.Body.Bytes(), []byte{0xff, 0xd8, 9}) {
			t.Fatal(w.Code, w.Body.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("queued request never completed")
	}
	if len(a.thumbSlots) != cap(a.thumbSlots)-1 {
		t.Fatal("slot was not released", len(a.thumbSlots))
	}
	<-a.thumbSlots
}

func TestCachePruneRemovesOldestPreviewsOnlyOverBudget(t *testing.T) {
	a := testApp(t)
	cache := filepath.Join(a.cfg.DataDir, "cache")
	a.cfgMu.Lock()
	a.cfg.CacheBytes = 4000
	a.cfgMu.Unlock()
	write := func(name string, size int, age time.Duration) {
		path := filepath.Join(cache, name)
		if err := os.WriteFile(path, make([]byte, size), 0600); err != nil {
			t.Fatal(err)
		}
		stamp := time.Now().Add(-age)
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	write("old.jpg", 1000, 3*time.Hour)
	write("new.jpg", 1000, time.Hour)
	a.pruneCache()
	for _, name := range []string{"old.jpg", "new.jpg"} {
		if _, err := os.Stat(filepath.Join(cache, name)); err != nil {
			t.Fatal("pruned below budget", name)
		}
	}
	write("middle.jpg", 1000, 2*time.Hour)
	write("stream.ts", 500, 4*time.Hour)
	a.pruneCache()
	if _, err := os.Stat(filepath.Join(cache, "old.jpg")); !os.IsNotExist(err) {
		t.Fatal("oldest preview kept", err)
	}
	for _, name := range []string{"middle.jpg", "new.jpg", "stream.ts"} {
		if _, err := os.Stat(filepath.Join(cache, name)); err != nil {
			t.Fatal("unexpectedly removed", name)
		}
	}
}
