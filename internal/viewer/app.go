package viewer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type App struct {
	cfg             Config
	cfgMu           sync.RWMutex
	authMu          sync.RWMutex
	settingsMu      sync.Mutex
	envCfg          Config
	settingsVersion int
	overrides       map[string]json.RawMessage
	pending         *oidcPending
	testOIDC        bool
	confirmOIDC     func(Session) error
	exportMu        sync.Mutex
	exports         map[string]*exportJob
	db              *Store
	root            *os.Root
	ctx             context.Context
	cancel          context.CancelFunc
	work            sync.WaitGroup
	index           indexer
	limits          limiter
	dummyHash       string
	verifier        *oidc.IDTokenVerifier
	oauth           oauth2.Config
	mediaMu         sync.Mutex
	streams         map[string]*stream
	slots           chan struct{}
	cacheMu         sync.Mutex
	thumbSlots      chan struct{}
	assets          fs.FS
}

func New(ctx context.Context, cfg Config, assets fs.FS) (*App, error) {
	for _, bin := range []string{"ffprobe", "ffmpeg"} {
		if _, e := exec.LookPath(bin); e != nil {
			return nil, e
		}
	}
	root, e := os.OpenRoot(cfg.RecordingsDir)
	if e != nil {
		return nil, e
	}
	db, e := OpenStore(cfg.DataDir)
	if e != nil {
		root.Close()
		return nil, e
	}
	ctx, cancel := context.WithCancel(ctx)
	a := &App{cfg: cfg, db: db, root: root, ctx: ctx, cancel: cancel, index: indexer{last: map[string]time.Time{}, status: ScanStatus{Warnings: []string{}}}, limits: limiter{entries: map[string]attempt{}, active: make(chan struct{}, 2)}, streams: map[string]*stream{}, slots: make(chan struct{}, 16), thumbSlots: make(chan struct{}, 1), assets: assets}
	a.envCfg = cfg
	a.overrides = map[string]json.RawMessage{}
	a.exports = map[string]*exportJob{}
	if e = a.loadSettings(); e != nil {
		a.Close()
		return nil, e
	}
	a.dummyHash, _ = HashPassword(token())
	if e = os.MkdirAll(path.Join(cfg.DataDir, "cache"), 0700); e != nil {
		a.Close()
		return nil, e
	}
	// Old HLS jobs cannot survive process restart.
	entries, _ := os.ReadDir(path.Join(cfg.DataDir, "cache"))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "hls-") || strings.HasPrefix(entry.Name(), "export-") {
			os.RemoveAll(path.Join(cfg.DataDir, "cache", entry.Name()))
		}
	}
	if e = a.initOIDC(ctx); e != nil {
		a.Close()
		return nil, e
	}
	a.db.Exec(`DELETE FROM sessions WHERE kind='oidc'`)
	a.work.Add(1)
	go a.background()
	return a, nil
}
func (a *App) Close() {
	a.cancel()
	a.mediaMu.Lock()
	for _, s := range a.streams {
		s.cancel()
	}
	a.mediaMu.Unlock()
	a.work.Wait()
	a.root.Close()
	a.db.Close()
}
func (a *App) background() {
	defer a.work.Done()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	lastScan := time.Time{}
	for {
		select {
		case <-a.ctx.Done():
			return
		case <-ticker.C:
			a.db.Cleanup(a.ctx)
			a.pruneStreams()
			a.pruneExports()
			a.pruneCache()
			if time.Since(lastScan) >= a.config().ScanInterval {
				loc, _ := time.LoadLocation("Europe/Berlin")
				a.startScan(time.Now().In(loc).Format("2006-01-02"), false)
				lastScan = time.Now()
			}
		}
	}
}
func jsonOut(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, message string) {
	jsonOut(w, status, map[string]string{"error": message})
}
func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		if e := a.db.PingContext(r.Context()); e != nil {
			fail(w, 503, "Nicht bereit")
			return
		}
		jsonOut(w, 200, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /auth/methods", func(w http.ResponseWriter, r *http.Request) {
		jsonOut(w, 200, map[string]bool{"local": a.config().LocalAdmin, "oidc": a.oidcEnabled()})
	})
	mux.HandleFunc("POST /auth/local", a.localLogin)
	mux.HandleFunc("GET /auth/oidc", a.oidcStart)
	mux.HandleFunc("GET /auth/oidc/callback", func(w http.ResponseWriter, r *http.Request) {
		if cookie, e := r.Cookie("ctv_oidc_test"); e == nil && r.URL.Query().Get("state") != "" && cookie.Value == r.URL.Query().Get("state") {
			a.protect(true, a.finishOIDCTest)(w, r)
			return
		}
		a.oidcCallback(w, r)
	})
	mux.HandleFunc("GET /api/session", a.protect(false, func(w http.ResponseWriter, r *http.Request) { jsonOut(w, 200, r.Context().Value(sessionKey)) }))
	mux.HandleFunc("POST /api/logout", a.protect(false, a.logout))
	mux.HandleFunc("GET /api/cameras", a.protect(false, func(w http.ResponseWriter, r *http.Request) {
		cams, e := a.db.Cameras()
		if e != nil {
			fail(w, 500, "Kameras nicht lesbar")
			return
		}
		if r.Context().Value(sessionKey).(Session).Role != "admin" {
			for i := range cams {
				cams[i].Folder = ""
				cams[i].Pattern = ""
			}
		}
		jsonOut(w, 200, cams)
	}))
	mux.HandleFunc("POST /api/cameras", a.protect(true, a.saveCamera))
	mux.HandleFunc("POST /api/cameras/preview", a.protect(true, a.preview))
	mux.HandleFunc("DELETE /api/cameras/{id}", a.protect(true, a.deleteCamera))
	a.registerManagement(mux)
	a.registerExports(mux)
	mux.HandleFunc("GET /api/cameras/folders", a.protect(true, a.cameraFolders))
	mux.HandleFunc("GET /api/day", a.protect(false, a.day))
	mux.HandleFunc("GET /api/scan", a.protect(false, func(w http.ResponseWriter, r *http.Request) { jsonOut(w, 200, a.scanStatus()) }))
	mux.HandleFunc("POST /api/scan", a.protect(true, func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Date string `json:"date"`
		}
		if !decode(w, r, &in) {
			return
		}
		if _, _, e := dayBounds(in.Date); e != nil {
			fail(w, 400, e.Error())
			return
		}
		if !a.startScan(in.Date, true) {
			fail(w, 409, "Indexierung läuft bereits")
			return
		}
		jsonOut(w, 202, a.scanStatus())
	}))
	mux.HandleFunc("GET /api/recordings/{id}/file", a.protect(false, a.file))
	mux.HandleFunc("GET /api/recordings/{id}/thumbnail", a.protect(false, a.thumbnail))
	mux.HandleFunc("POST /api/streams", a.protect(false, a.createStream))
	mux.HandleFunc("DELETE /api/streams/{id}", a.protect(false, a.deleteStream))
	mux.HandleFunc("GET /api/streams/{id}/{file}", a.protect(false, a.streamFile))
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" && r.URL.Path != "/app.js" && r.URL.Path != "/style.css" {
			http.NotFound(w, r)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/")
		if name == "" {
			name = "index.html"
		}
		b, e := fs.ReadFile(a.assets, name)
		if e != nil {
			http.NotFound(w, r)
			return
		}
		switch path.Ext(name) {
		case ".html":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
		case ".js":
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		case ".css":
			w.Header().Set("Content-Type", "text/css; charset=utf-8")
		}
		w.Write(b)
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; media-src 'self' blob:; connect-src 'self'; worker-src 'self' blob:; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		if !a.config().DevHTTP {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		}
		defer func() {
			if e := recover(); e != nil {
				slog.Error("HTTP-Anfrage fehlgeschlagen", "panic", fmt.Sprint(e))
				fail(w, 500, "Interner Fehler")
			}
		}()
		mux.ServeHTTP(w, r)
	})
}
func (a *App) saveCamera(w http.ResponseWriter, r *http.Request) {
	a.index.mu.Lock()
	defer a.index.mu.Unlock()
	if a.index.status.Running {
		fail(w, 409, "Bitte laufende Indexierung abwarten")
		return
	}
	var c Camera
	if !decode(w, r, &c) {
		return
	}
	if e := validateCamera(c, a.root); e != nil {
		fail(w, 400, e.Error())
		return
	}
	if c.ID == "" {
		c.ID = token()[:16]
	} else if _, e := a.db.Camera(c.ID); e != nil {
		fail(w, 404, "Kamera nicht gefunden")
		return
	}
	if e := a.db.SaveCamera(c); e != nil {
		fail(w, 500, "Kamera konnte nicht gespeichert werden")
		return
	}
	a.index.last = map[string]time.Time{}
	jsonOut(w, 200, c)
}
func (a *App) deleteCamera(w http.ResponseWriter, r *http.Request) {
	a.index.mu.Lock()
	defer a.index.mu.Unlock()
	if a.index.status.Running {
		fail(w, 409, "Bitte Indexierung abwarten")
		return
	}
	if _, e := a.db.ExecContext(r.Context(), `DELETE FROM cameras WHERE id=?`, r.PathValue("id")); e != nil {
		fail(w, 500, "Löschen fehlgeschlagen")
		return
	}
	jsonOut(w, 200, map[string]bool{"ok": true})
}
func (a *App) preview(w http.ResponseWriter, r *http.Request) {
	var c Camera
	if !decode(w, r, &c) {
		return
	}
	if e := validateCamera(c, a.root); e != nil {
		fail(w, 400, e.Error())
		return
	}
	type item struct {
		File  string `json:"file"`
		Time  string `json:"time,omitempty"`
		Kind  string `json:"kind"`
		Error string `json:"error,omitempty"`
	}
	out := []item{}
	count := 0
	stop := errors.New("enough")
	e := fs.WalkDir(a.root.FS(), c.Folder, func(name string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		count++
		if count > 10000 {
			return stop
		}
		if d.IsDir() || d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if !supportedMedia(name) {
			return nil
		}
		relative := strings.TrimPrefix(name, c.Folder+"/")
		t, e := cameraTimestamp(c, relative)
		v := item{File: relative, Kind: "video"}
		if strings.EqualFold(path.Ext(relative), ".jpg") || strings.EqualFold(path.Ext(relative), ".jpeg") {
			v.Kind = "image"
		}
		if e != nil {
			v.Error = e.Error()
		} else {
			v.Time = t.Format(time.RFC3339)
			file, openErr := openSource(a.root, name)
			if openErr != nil {
				v.Error = "Datei nicht lesbar"
			} else {
				if v.Kind == "image" {
					openErr = validateJPEG(file)
				} else {
					_, _, openErr = probe(r.Context(), file)
				}
				file.Close()
				if openErr != nil {
					v.Error = "Bild oder Video beschädigt oder nicht lesbar"
				}
			}
		}
		out = append(out, v)
		if len(out) >= 8 {
			return stop
		}
		return nil
	})
	if e != nil && !errors.Is(e, stop) {
		fail(w, 400, "Ordner konnte nicht gelesen werden")
		return
	}
	jsonOut(w, 200, map[string]any{"examples": out, "limited": count > 10000})
}
func (a *App) day(w http.ResponseWriter, r *http.Request) {
	date := r.URL.Query().Get("date")
	from, to, e := dayBounds(date)
	if e != nil {
		fail(w, 400, e.Error())
		return
	}
	cams, e := a.db.Cameras()
	if e != nil {
		fail(w, 500, "Index nicht lesbar")
		return
	}
	out := []Recording{}
	for _, c := range cams {
		rs, e := a.db.Recordings(c.ID, float64(from.Unix()), float64(to.Unix()))
		if e != nil {
			fail(w, 500, "Index nicht lesbar")
			return
		}
		out = append(out, rs...)
	}
	a.startScan(date, false)
	jsonOut(w, 200, map[string]any{"recordings": out, "from": from.Unix(), "to": to.Unix(), "scan": a.scanStatus()})
}
