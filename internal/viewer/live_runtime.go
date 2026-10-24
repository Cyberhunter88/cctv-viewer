package viewer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
)

type probeSlot chan struct{}

func (s probeSlot) TryAcquire() bool {
	select {
	case s <- struct{}{}:
		return true
	default:
		return false
	}
}
func (s probeSlot) Release() { <-s }

type liveService struct {
	sourceMu           sync.RWMutex
	sourceLocks        sync.Map
	ctx                context.Context
	warm               map[string]*liveWarm
	warmWake           chan struct{}
	warmDone           chan struct{}
	warmWorkers        sync.WaitGroup
	relays             map[string]func()
	app                *App
	mu                 sync.Mutex
	api, rtsp, message string
	ready              bool
	secret             string
	cancel             context.CancelFunc
	done               chan struct{}
	clients            map[string]liveConnection
	sources            map[string]bool
	probes             probeSlot
	gateway            *liveGateway
}
type liveConnection struct {
	camera string
	cancel context.CancelFunc
}

func newLiveService(a *App) *liveService {
	ctx, cancel := context.WithCancel(a.ctx)
	s := &liveService{app: a, secret: token(), cancel: cancel, done: make(chan struct{}), clients: map[string]liveConnection{}, sources: map[string]bool{}, probes: make(probeSlot, 1), message: "Live-Dienst wird gestartet …"}
	s.ctx = ctx
	s.warmWake = make(chan struct{}, 1)
	s.warmDone = make(chan struct{})
	s.relays = map[string]func(){}
	go s.runWarm(ctx)
	go s.run(ctx)
	return s
}
func (s *liveService) status() (bool, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ready, s.message
}
func (s *liveService) close() { s.cancel(); <-s.done; <-s.warmDone; s.invalidate("") }
func freeLocalAddress() (string, error) {
	ln, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		return "", e
	}
	address := ln.Addr().String()
	ln.Close()
	return address, nil
}
func (s *liveService) run(ctx context.Context) {
	defer close(s.done)
	binary := s.app.cfg.Go2RTCPath
	if binary == "" {
		binary = "go2rtc"
	}
	if _, e := exec.LookPath(binary); e != nil {
		s.mu.Lock()
		s.message = "Live-Dienst fehlt: go2rtc installieren oder CTV_GO2RTC_PATH setzen."
		s.mu.Unlock()
		return
	}
	for ctx.Err() == nil {
		_ = s.runProcess(ctx, binary)
		s.mu.Lock()
		s.ready = false
		s.message = "Live-Dienst nicht verfügbar. Neustart wird versucht …"
		s.mu.Unlock()
		s.invalidate("")
		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}
	}
}
func (s *liveService) runProcess(ctx context.Context, binary string) error {
	api, e := freeLocalAddress()
	if e != nil {
		return e
	}
	rtsp, e := freeLocalAddress()
	if e != nil {
		return e
	}
	// Only control and local restream listeners are exposed by the child. No defaults
	// for discovery, RTMP or public management listeners are inherited.
	config := map[string]any{"modules": []string{"api", "ws", "http", "rtsp", "webrtc", "mp4", "exec", "ffmpeg"}, "api": map[string]any{"listen": api, "username": "viewer", "password": s.secret, "local_auth": true}, "rtsp": map[string]any{"listen": rtsp}, "webrtc": map[string]any{"listen": "", "ice_servers": []any{}}, "log": map[string]any{"level": "error"}}
	b, _ := json.Marshal(config)
	file, e := os.CreateTemp(filepath.Join(s.app.cfg.DataDir, "cache"), "live-runtime-*.json")
	if e != nil {
		return e
	}
	name := file.Name()
	defer os.Remove(name)
	if _, e = file.Write(b); e != nil {
		file.Close()
		return e
	}
	if e = file.Close(); e != nil {
		return e
	}
	// Upstream's stream API also patches ConfigPath. Direct those writes to
	// the OS null device; only the protected second input supplies startup settings.
	// Camera URLs therefore never become a second persistent credentials file.
	cmd := exec.CommandContext(ctx, binary, "-config", os.DevNull, "-config", name)
	defer configureLiveProcess(cmd)()
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if e = cmd.Start(); e != nil {
		return e
	}
	processDone := make(chan error, 1)
	go func() { processDone <- cmd.Wait() }()
	defer func() { _ = cmd.Process.Kill() }()
	s.mu.Lock()
	s.api = "http://" + api
	s.rtsp = rtsp
	s.mu.Unlock()
	deadline := time.NewTimer(8 * time.Second)
	defer deadline.Stop()
	for {
		if e = s.call(ctx, http.MethodGet, "/api", nil, nil); e == nil {
			break
		}
		select {
		case e = <-processDone:
			return e
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return errors.New("live startup timeout")
		case <-time.After(100 * time.Millisecond):
		}
	}
	// The child has read its configuration. Do not retain even its API secret on disk.
	_ = os.Remove(name)
	s.mu.Lock()
	s.ready = true
	s.message = ""
	s.mu.Unlock()
	s.refreshGateway()
	s.wakeWarm()
	select {
	case e = <-processDone:
		return e
	case <-ctx.Done():
		_ = cmd.Process.Kill()
		return <-processDone
	}
}
func (s *liveService) call(ctx context.Context, method, path string, body io.Reader, out any) error {
	s.mu.Lock()
	api := s.api
	s.mu.Unlock()
	if api == "" {
		return errors.New("live unavailable")
	}
	request, e := http.NewRequestWithContext(ctx, method, api+path, body)
	if e != nil {
		return e
	}
	request.SetBasicAuth("viewer", s.secret)
	client := http.Client{Timeout: 5 * time.Second}
	response, e := client.Do(request)
	if e != nil {
		return errors.New("live unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode >= 300 {
		return errors.New("live request failed")
	}
	if out != nil {
		return json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(out)
	}
	return nil
}
func liveStreamName(id, view, quality string) string {
	return "live_" + digest(id)[:24] + "_" + view + "_" + quality
}
func (s *liveService) ensureSource(ctx context.Context, id, view, quality string, rtc bool) (string, error) {
	s.sourceMu.RLock()
	defer s.sourceMu.RUnlock()
	name := liveStreamName(id, view, quality)
	lock, _ := s.sourceLocks.LoadOrStore(name, &sync.Mutex{})
	lock.(*sync.Mutex).Lock()
	defer lock.(*sync.Mutex).Unlock()
	camera, e := s.app.db.Camera(id)
	if e != nil {
		return "", e
	}
	if view == "zoom" && camera.ViewMode != "dual" {
		return "", errors.New("Zoom ist für diese Kamera deaktiviert")
	}
	c, e := s.app.db.liveCamera(id)
	if e != nil {
		return "", e
	}
	raw, e := c.source(view, quality)
	if e != nil {
		return "", e
	}
	s.mu.Lock()
	ready, known := s.ready, s.sources[name]
	s.mu.Unlock()
	if !ready {
		return "", errors.New("Live-Dienst nicht verfügbar")
	}
	if !known {
		// All command/source fragments are generated here; administrator input is a
		// validated RTSP URL without '#' and never an ffmpeg/exec source expression.
		source := raw + "#backchannel=0#transport=tcp"
		var closeRelay func()
		if strings.HasPrefix(raw, "https://") {
			source, closeRelay, e = liveHTTPSRelay(s.ctx, raw, c.SkipTLSVerify[liveKey(view, quality)])
			if e != nil {
				return "", e
			}
		}
		endpoint := "/api/streams?name=" + url.QueryEscape(name) + "&src=" + url.QueryEscape(source)
		if e = s.call(ctx, http.MethodPut, endpoint, nil, nil); e != nil {
			if closeRelay != nil {
				closeRelay()
			}
			return "", e
		}
		s.mu.Lock()
		if closeRelay != nil {
			s.relays[name] = closeRelay
		}
		s.sources[name] = true
		s.mu.Unlock()
	}
	if rtc {
		rtcName := name + "_rtc"
		s.mu.Lock()
		known = s.sources[rtcName]
		s.mu.Unlock()
		if !known {
			s.mu.Lock()
			address := s.rtsp
			s.mu.Unlock()
			// Keep video on the direct restream path. FFmpeg is only a fallback for
			// audio codecs (such as Reolink AAC) that WebRTC cannot negotiate.
			sources := url.Values{"name": {rtcName}, "src": {"rtsp://" + address + "/" + name, "ffmpeg:" + name + "#audio=opus"}}
			if e = s.call(ctx, http.MethodPut, "/api/streams?"+sources.Encode(), nil, nil); e != nil {
				return "", e
			}
			s.mu.Lock()
			s.sources[rtcName] = true
			s.mu.Unlock()
		}
		name = rtcName
	}
	return name, nil
}
func (s *liveService) invalidate(id string) {
	s.sourceMu.Lock()
	s.mu.Lock()
	for name, e := range s.warm {
		if id == "" || e.camera == id {
			if e.cancel != nil {
				e.cancel()
			}
			delete(s.warm, name)
		}
	}
	for _, c := range s.clients {
		if id == "" || c.camera == id {
			c.cancel()
		}
	}
	g := s.gateway
	s.gateway = nil
	names := []string{}
	for name := range s.sources {
		if id == "" || strings.HasPrefix(name, "live_"+digest(id)[:24]+"_") {
			names = append(names, name)
			delete(s.sources, name)
			if closeRelay := s.relays[name]; closeRelay != nil {
				closeRelay()
				delete(s.relays, name)
			}
		}
	}
	s.mu.Unlock()
	for _, name := range names {
		_ = s.call(s.app.ctx, http.MethodDelete, "/api/streams?src="+url.QueryEscape(name), nil, nil)
	}
	s.sourceMu.Unlock()
	if g != nil {
		g.close()
	}
	if s.app.ctx.Err() == nil {
		s.refreshGateway()
		s.wakeWarm()
	}
}
func (a *App) liveWebSocket(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != a.config().BaseURL {
		fail(w, 403, "Ungültiger Ursprung")
		return
	}
	id := r.PathValue("id")
	if _, e := a.db.Camera(id); e != nil {
		fail(w, 404, "Kamera nicht gefunden")
		return
	}
	view, quality, mode := r.URL.Query().Get("view"), r.URL.Query().Get("quality"), r.URL.Query().Get("mode")
	if view == "" {
		view = "wide"
	}
	if quality == "" {
		quality = "fast"
	}
	if mode != "webrtc" && mode != "mse" {
		fail(w, 400, "Ungültige Übertragung")
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	key := token()
	s := a.live
	s.mu.Lock()
	if len(s.clients) >= 128 {
		s.mu.Unlock()
		fail(w, 429, "Zu viele Liveverbindungen")
		return
	}
	s.clients[key] = liveConnection{id, cancel}
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.clients, key); s.mu.Unlock() }()
	name, e := s.ensureSource(ctx, id, view, quality, mode == "webrtc")
	if e != nil {
		fail(w, 503, "Livequelle nicht verfügbar")
		return
	}
	releaseSource := s.useSource(id, view, quality)
	defer releaseSource()
	s.mu.Lock()
	api := s.api
	s.mu.Unlock()
	headers := http.Header{}
	headers.Set("Authorization", "Basic "+basicAuth("viewer", s.secret))
	upstream, _, e := websocket.Dial(ctx, strings.Replace(api, "http://", "ws://", 1)+"/api/ws?src="+url.QueryEscape(name), &websocket.DialOptions{HTTPHeader: headers})
	if e != nil {
		fail(w, 503, "Livequelle nicht verfügbar")
		return
	}
	defer upstream.CloseNow()
	upstream.SetReadLimit(4 << 20)
	// Origin is checked against the configured external URL above, including behind proxies.
	downstream, e := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if e != nil {
		return
	}
	defer downstream.CloseNow()
	downstream.SetReadLimit(65536)
	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := a.session(r); err != nil {
					cancel()
					return
				}
			}
		}
	}()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		defer cancel()
		for {
			kind, b, err := upstream.Read(ctx)
			if err != nil {
				return
			}
			if kind == websocket.MessageText {
				var msg struct {
					Type string `json:"type"`
				}
				if json.Unmarshal(b, &msg) != nil {
					return
				}
				switch msg.Type {
				case "mse", "webrtc/answer", "webrtc/candidate":
				default:
					b = []byte(`{"type":"error","value":"Livequelle oder Codec nicht verfügbar"}`)
				}
			}
			if downstream.Write(ctx, kind, b) != nil {
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		defer cancel()
		for {
			kind, b, err := downstream.Read(ctx)
			if err != nil {
				return
			}
			if kind != websocket.MessageText {
				return
			}
			var msg struct {
				Type  string `json:"type"`
				Value string `json:"value"`
			}
			if json.Unmarshal(b, &msg) != nil {
				return
			}
			if mode == "mse" && msg.Type != "mse" || mode == "webrtc" && msg.Type != "webrtc/offer" && msg.Type != "webrtc/candidate" {
				return
			}
			if upstream.Write(ctx, kind, b) != nil {
				return
			}
		}
	}()
	wg.Wait()
}
func basicAuth(user, password string) string {
	request, _ := http.NewRequest("GET", "http://localhost", bytes.NewReader(nil))
	request.SetBasicAuth(user, password)
	return strings.TrimPrefix(request.Header.Get("Authorization"), "Basic ")
}

// Export settings and credentials do not interrupt authenticated viewer streams.
func (s *liveService) reloadGateway() {
	s.mu.Lock()
	gateway := s.gateway
	s.gateway = nil
	s.mu.Unlock()
	if gateway != nil {
		gateway.close()
	}
	s.refreshGateway()
}
func (s *liveService) refreshGateway() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gateway != nil || !s.ready || s.app.ctx.Err() != nil {
		return
	}
	share, e := s.app.db.liveShare()
	if e != nil || !share.Enabled {
		if e == nil {
			s.message = ""
		}
		return
	}
	address := s.app.cfg.LiveRTSPAddress
	if address == "" {
		address = "127.0.0.1:8554"
	}
	g, e := startLiveGateway(s, address)
	if e != nil {
		s.message = fmt.Sprintf("RTSP-Freigabe konnte nicht auf %s starten", address)
		return
	}
	s.gateway = g
	s.message = ""
}
