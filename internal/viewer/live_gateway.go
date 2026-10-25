package viewer

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bluenviron/gortsplib/v4"
	"github.com/bluenviron/gortsplib/v4/pkg/base"
	"github.com/bluenviron/gortsplib/v4/pkg/description"
	"github.com/bluenviron/gortsplib/v4/pkg/format"
	"github.com/bluenviron/gortsplib/v4/pkg/liberrors"
	"github.com/pion/rtp"
)

type liveShareSettings struct {
	Enabled   bool   `json:"enabled"`
	Username  string `json:"username"`
	Password  string `json:"-"`
	ServerURL string `json:"serverUrl"`
}

func (s *Store) liveShare() (liveShareSettings, error) {
	var payload string
	e := s.QueryRow(`SELECT payload FROM live_share WHERE id=1`).Scan(&payload)
	if errors.Is(e, sql.ErrNoRows) {
		return liveShareSettings{Username: "homeassistant"}, nil
	}
	if e != nil {
		return liveShareSettings{}, e
	}
	payload, e = s.openSecret(payload, secretScope("live_share", "1", "payload"))
	if e != nil {
		return liveShareSettings{}, e
	}
	var stored struct {
		Enabled   bool   `json:"enabled"`
		Username  string `json:"username"`
		Secret    string `json:"secret"`
		ServerURL string `json:"serverUrl"`
	}
	e = json.Unmarshal([]byte(payload), &stored)
	return liveShareSettings{Enabled: stored.Enabled, Username: stored.Username, Password: stored.Secret, ServerURL: stored.ServerURL}, e
}
func validShareServerURL(raw string) bool {
	if raw == "" {
		return true
	}
	if len(raw) > 255 || strings.ContainsAny(raw, "\r\n\x00 \t?#") {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "rtsp" || u.User != nil || u.Hostname() == "" || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return false
	}
	port, err := strconv.Atoi(u.Port())
	return err == nil && port >= 1 && port <= 65535
}
func (a *App) liveShare(w http.ResponseWriter, r *http.Request) {
	settings, e := a.db.liveShare()
	if e != nil {
		fail(w, 500, "Freigabe nicht lesbar")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	var generated string
	if r.Method == http.MethodPut {
		var in struct {
			Enabled       bool    `json:"enabled"`
			ResetPassword bool    `json:"resetPassword"`
			ServerURL     *string `json:"serverUrl"`
		}
		if !decode(w, r, &in) {
			return
		}
		if in.ServerURL != nil {
			settings.ServerURL = strings.TrimSpace(*in.ServerURL)
			if !validShareServerURL(settings.ServerURL) {
				fail(w, 400, "RTSP-Serveradresse muss rtsp://Host:Port ohne Pfad oder Zugangsdaten sein")
				return
			}
		}
		reload := settings.Enabled != in.Enabled || in.ResetPassword || settings.Password == ""
		settings.Enabled = in.Enabled
		if in.ResetPassword || settings.Password == "" {
			generated = token()
			settings.Password = generated
		}
		payload, _ := json.Marshal(map[string]any{"enabled": settings.Enabled, "username": settings.Username, "secret": settings.Password, "serverUrl": settings.ServerURL})
		sealed, sealErr := a.db.sealSecret(string(payload), secretScope("live_share", "1", "payload"))
		if sealErr != nil {
			fail(w, 500, "Freigabe nicht gespeichert")
			return
		}
		if _, e = a.db.Exec(`INSERT INTO live_share(id,payload) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload`, sealed); e != nil {
			fail(w, 500, "Freigabe nicht gespeichert")
			return
		}
		if reload {
			a.live.reloadGateway()
		}
	}
	address := a.config().LiveRTSPAddress
	if address == "" {
		address = "127.0.0.1:8554"
	}
	a.live.mu.Lock()
	listening := a.live.gateway != nil
	a.live.mu.Unlock()
	jsonOut(w, 200, map[string]any{"enabled": settings.Enabled, "username": settings.Username, "password": generated, "hasPassword": settings.Password != "", "address": address, "listening": listening, "serverUrl": settings.ServerURL})
}

type gatewayRoute struct {
	client  *gortsplib.Client
	stream  *gortsplib.ServerStream
	used    time.Time
	readers map[*gortsplib.ServerSession]bool
	failed  chan struct{}
}

type gatewayBuild struct {
	done  chan struct{}
	route *gatewayRoute
	err   error
}

func (r *gatewayRoute) close() { r.client.Close(); r.stream.Close() }

type liveGateway struct {
	live     *liveService
	server   *gortsplib.Server
	mu       sync.Mutex
	routes   map[string]*gatewayRoute
	builds   map[string]*gatewayBuild
	sessions map[*gortsplib.ServerSession]string
	releases map[*gortsplib.ServerSession]func()
	cancel   context.CancelFunc
	ctx      context.Context
	done     chan struct{}
}

func startLiveGateway(s *liveService, address string) (*liveGateway, error) {
	ctx, cancel := context.WithCancel(s.app.ctx)
	g := &liveGateway{live: s, ctx: ctx, routes: map[string]*gatewayRoute{}, builds: map[string]*gatewayBuild{}, sessions: map[*gortsplib.ServerSession]string{}, cancel: cancel, done: make(chan struct{})}
	// No UDP listeners: SETUP can only negotiate interleaved RTP over TCP.
	g.server = &gortsplib.Server{Handler: g, RTSPAddress: address, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second}
	if e := g.server.Start(); e != nil {
		cancel()
		return nil, e
	}
	go func() {
		defer close(g.done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				g.prune()
			}
		}
	}()
	return g, nil
}
func (g *liveGateway) close() {
	g.cancel()
	<-g.done
	g.server.Close()
	g.mu.Lock()
	for session, release := range g.releases {
		release()
		delete(g.releases, session)
	}
	routes := g.routes
	g.routes = map[string]*gatewayRoute{}
	g.mu.Unlock()
	for _, r := range routes {
		r.close()
	}
}
func (g *liveGateway) prune() {
	g.mu.Lock()
	expired := []*gatewayRoute{}
	for name, r := range g.routes {
		failed := false
		select {
		case <-r.failed:
			failed = true
		default:
		}
		if failed || len(r.readers) == 0 && time.Since(r.used) > 10*time.Second {
			delete(g.routes, name)
			expired = append(expired, r)
		}
	}
	g.mu.Unlock()
	for _, r := range expired {
		r.close()
	}
}
func (g *liveGateway) authorize(conn *gortsplib.ServerConn, request *base.Request) bool {
	share, e := g.live.app.db.liveShare()
	return e == nil && share.Enabled && share.Password != "" && conn.VerifyCredentials(request, share.Username, share.Password)
}
func parseLivePath(path string) (id, view, quality string, ok bool) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 4 || parts[0] != "live" {
		return
	}
	id, view, quality = parts[1], parts[2], parts[3]
	ok = id != "" && (view == "wide" || view == "zoom") && (quality == "fast" || quality == "high")
	return
}
func (g *liveGateway) getRoute(path string) (*gatewayRoute, error) {
	id, view, quality, ok := parseLivePath(path)
	if !ok {
		return nil, errors.New("invalid path")
	}
	c, e := g.live.app.db.liveCamera(id)
	if e != nil || !c.Shared {
		return nil, errors.New("not shared")
	}
	return g.route(path, func() (*gatewayRoute, error) { return g.buildRoute(id, view, quality) })
}

func (g *liveGateway) route(path string, create func() (*gatewayRoute, error)) (*gatewayRoute, error) {
	g.mu.Lock()
	if g.ctx.Err() != nil {
		g.mu.Unlock()
		return nil, context.Canceled
	}
	if r := g.routes[path]; r != nil {
		r.used = time.Now()
		g.mu.Unlock()
		return r, nil
	}
	if build := g.builds[path]; build != nil {
		g.mu.Unlock()
		select {
		case <-build.done:
			return build.route, build.err
		case <-g.ctx.Done():
			return nil, g.ctx.Err()
		}
	}
	if len(g.routes)+len(g.builds) >= 64 {
		g.mu.Unlock()
		return nil, errors.New("too many streams")
	}
	build := &gatewayBuild{done: make(chan struct{})}
	g.builds[path] = build
	g.mu.Unlock()
	route, err := create()
	g.mu.Lock()
	if g.ctx.Err() != nil && route != nil {
		route.close()
		route, err = nil, g.ctx.Err()
	}
	if err == nil {
		g.routes[path] = route
	}
	build.route, build.err = route, err
	delete(g.builds, path)
	close(build.done)
	g.mu.Unlock()
	return route, err
}

func (g *liveGateway) buildRoute(id, view, quality string) (*gatewayRoute, error) {
	name, e := g.live.ensureSource(g.ctx, id, view, quality, false)
	if e != nil {
		return nil, e
	}
	g.live.mu.Lock()
	address := g.live.rtsp
	g.live.mu.Unlock()
	u, _ := base.ParseURL("rtsp://" + address + "/" + name)
	tcp := gortsplib.TransportTCP
	client := &gortsplib.Client{Transport: &tcp, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second}
	if e = client.Start(u.Scheme, u.Host); e != nil {
		return nil, e
	}
	success := false
	defer func() {
		if !success {
			client.Close()
		}
	}()
	desc, _, e := client.Describe(u)
	if e != nil {
		return nil, e
	}
	if e = client.SetupAll(desc.BaseURL, desc.Medias); e != nil {
		return nil, e
	}
	stream := &gortsplib.ServerStream{Server: g.server, Desc: desc}
	if e = stream.Initialize(); e != nil {
		return nil, e
	}
	r := &gatewayRoute{client: client, stream: stream, used: time.Now(), readers: map[*gortsplib.ServerSession]bool{}, failed: make(chan struct{})}
	client.OnPacketRTPAny(func(media *description.Media, _ format.Format, packet *rtp.Packet) {
		_ = stream.WritePacketRTP(media, packet)
	})
	if _, e = client.Play(nil); e != nil {
		stream.Close()
		return nil, e
	}
	success = true
	go func() { _ = client.Wait(); close(r.failed) }()
	return r, nil
}
func (g *liveGateway) OnDescribe(ctx *gortsplib.ServerHandlerOnDescribeCtx) (*base.Response, *gortsplib.ServerStream, error) {
	if !g.authorize(ctx.Conn, ctx.Request) {
		return &base.Response{StatusCode: base.StatusUnauthorized}, nil, liberrors.ErrServerAuth{}
	}
	route, e := g.getRoute(ctx.Path)
	if e != nil {
		return &base.Response{StatusCode: base.StatusNotFound}, nil, nil
	}
	return &base.Response{StatusCode: base.StatusOK}, route.stream, nil
}
func (g *liveGateway) OnSetup(ctx *gortsplib.ServerHandlerOnSetupCtx) (*base.Response, *gortsplib.ServerStream, error) {
	if !g.authorize(ctx.Conn, ctx.Request) {
		return &base.Response{StatusCode: base.StatusUnauthorized}, nil, liberrors.ErrServerAuth{}
	}
	if ctx.Transport != gortsplib.TransportTCP {
		return &base.Response{StatusCode: base.StatusUnsupportedTransport}, nil, nil
	}
	route, e := g.getRoute(ctx.Path)
	if e != nil {
		return &base.Response{StatusCode: base.StatusNotFound}, nil, nil
	}
	g.mu.Lock()
	if g.releases == nil {
		g.releases = map[*gortsplib.ServerSession]func(){}
	}
	if g.releases[ctx.Session] == nil {
		id, view, quality, _ := parseLivePath(ctx.Path)
		g.releases[ctx.Session] = g.live.useGatewaySource(id, view, quality)
	}
	route.readers[ctx.Session] = true
	g.sessions[ctx.Session] = ctx.Path
	g.mu.Unlock()
	return &base.Response{StatusCode: base.StatusOK}, route.stream, nil
}
func (g *liveGateway) OnPlay(ctx *gortsplib.ServerHandlerOnPlayCtx) (*base.Response, error) {
	if !g.authorize(ctx.Conn, ctx.Request) {
		return &base.Response{StatusCode: base.StatusUnauthorized}, liberrors.ErrServerAuth{}
	}
	return &base.Response{StatusCode: base.StatusOK}, nil
}
func (g *liveGateway) OnSessionClose(ctx *gortsplib.ServerHandlerOnSessionCloseCtx) {
	g.mu.Lock()
	defer g.mu.Unlock()
	path := g.sessions[ctx.Session]
	delete(g.sessions, ctx.Session)
	if release := g.releases[ctx.Session]; release != nil {
		release()
		delete(g.releases, ctx.Session)
	}
	if route := g.routes[path]; route != nil {
		delete(route.readers, ctx.Session)
		route.used = time.Now()
	}
}

// Publishing and talkback are deliberately absent; gortsplib rejects unsupported
// ANNOUNCE/RECORD methods and no backchannel media is advertised by our sources.
