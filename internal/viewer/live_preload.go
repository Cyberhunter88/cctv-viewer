package viewer

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/bluenviron/gortsplib/v4"
	"github.com/bluenviron/gortsplib/v4/pkg/base"
)

type liveWarm struct {
	camera, key string
	users       int
	last        time.Time
	state       string
	cancel      context.CancelFunc
}

func warmWanted(mode string, users int, last, now time.Time) bool {
	return mode == "always" || mode == "idle" && (users > 0 || !last.IsZero() && now.Sub(last) < 60*time.Second)
}
func (s *liveService) useSource(id, view, quality string) func() {
	name := liveStreamName(id, view, quality)
	s.mu.Lock()
	if s.warm == nil {
		s.warm = map[string]*liveWarm{}
	}
	entry := s.warm[name]
	if entry == nil {
		entry = &liveWarm{camera: id, key: liveKey(view, quality), state: "disconnected"}
		s.warm[name] = entry
	}
	entry.users++
	entry.last = time.Now()
	s.mu.Unlock()
	s.wakeWarm()
	var once sync.Once
	return func() {
		once.Do(func() { s.mu.Lock(); entry.users--; entry.last = time.Now(); s.mu.Unlock(); s.wakeWarm() })
	}
}
func (s *liveService) wakeWarm() {
	select {
	case s.warmWake <- struct{}{}:
	default:
	}
}
func (s *liveService) runWarm(ctx context.Context) {
	defer close(s.warmDone)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	defer func() {
		s.mu.Lock()
		for _, e := range s.warm {
			if e.cancel != nil {
				e.cancel()
			}
		}
		s.mu.Unlock()
		s.warmWorkers.Wait()
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		case <-s.warmWake:
		}
		cameras, err := s.app.db.Cameras()
		if err != nil {
			continue
		}
		configs := map[string]liveCamera{}
		for _, camera := range cameras {
			if c, e := s.app.db.liveCamera(camera.ID); e == nil {
				configs[camera.ID] = c
			}
		}
		s.mu.Lock()
		if s.warm == nil {
			s.warm = map[string]*liveWarm{}
		}
		for id, c := range configs {
			if !c.Enabled {
				continue
			}
			for _, key := range []string{"fast", "high", "zoomFast", "zoomHigh"} {
				view, quality := liveParts(key)
				if _, e := c.source(view, quality); e != nil || c.preloadMode(key) != "always" {
					continue
				}
				name := liveStreamName(id, view, quality)
				if s.warm[name] == nil {
					s.warm[name] = &liveWarm{camera: id, key: key, state: "disconnected"}
				}
			}
		}
		now := time.Now()
		for name, e := range s.warm {
			c, exists := configs[e.camera]
			view, quality := liveParts(e.key)
			_, sourceError := c.source(view, quality)
			wanted := exists && c.Enabled && sourceError == nil && s.ready && warmWanted(c.preloadMode(e.key), e.users, e.last, now)
			if wanted && e.cancel == nil {
				workerCtx, cancel := context.WithCancel(ctx)
				e.cancel = cancel
				e.state = "connecting"
				s.warmWorkers.Add(1)
				go func() { defer s.warmWorkers.Done(); s.keepSource(workerCtx, e, view, quality) }()
			} else if !wanted && e.cancel != nil {
				e.cancel()
				e.cancel = nil
				e.state = "disconnected"
			}
			if !exists || !wanted && e.users == 0 && (c.preloadMode(e.key) == "off" || !e.last.IsZero() && now.Sub(e.last) >= 60*time.Second) {
				delete(s.warm, name)
			}
		}
		s.mu.Unlock()
	}
}
func (s *liveService) keepSource(ctx context.Context, e *liveWarm, view, quality string) {
	delay := time.Second
	for ctx.Err() == nil {
		s.mu.Lock()
		if ctx.Err() != nil {
			s.mu.Unlock()
			return
		}
		e.state = "connecting"
		s.mu.Unlock()
		name, err := s.ensureSource(ctx, e.camera, view, quality, false)
		if err == nil {
			s.mu.Lock()
			address := s.rtsp
			s.mu.Unlock()
			u, parseErr := base.ParseURL("rtsp://" + address + "/" + name)
			if parseErr == nil {
				tcp := gortsplib.TransportTCP
				client := &gortsplib.Client{Transport: &tcp, ReadTimeout: 8 * time.Second, WriteTimeout: 5 * time.Second}
				err = client.Start(u.Scheme, u.Host)
				if err == nil {
					stopped := make(chan struct{})
					go func() {
						select {
						case <-ctx.Done():
							client.Close()
						case <-stopped:
						}
					}()
					desc, _, startErr := client.Describe(u)
					if startErr == nil {
						startErr = client.SetupAll(desc.BaseURL, desc.Medias)
					}
					if startErr == nil {
						_, startErr = client.Play(nil)
					}
					if startErr == nil {
						s.mu.Lock()
						if ctx.Err() == nil {
							e.state = "ready"
						}
						s.mu.Unlock()
						delay = time.Second
						_ = client.Wait()
					}
					close(stopped)
				}
				client.Close()
			}
		}
		if ctx.Err() != nil {
			return
		}
		s.mu.Lock()
		e.state = "error"
		s.mu.Unlock()
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		delay = min(delay*2, 30*time.Second)
	}
}
func (a *App) liveSourceStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, err := a.db.Camera(id); err != nil {
		fail(w, 404, "Kamera nicht gefunden")
		return
	}
	c, err := a.db.liveCamera(id)
	if err != nil {
		fail(w, 500, "Livekonfiguration nicht lesbar")
		return
	}
	result := map[string]string{}
	a.live.mu.Lock()
	for _, key := range []string{"fast", "high", "zoomFast", "zoomHigh"} {
		view, quality := liveParts(key)
		state := "disconnected"
		if e := a.live.warm[liveStreamName(id, view, quality)]; e != nil {
			state = e.state
			if e.users > 0 && state == "disconnected" {
				state = "active"
			}
			if state == "ready" && e.users == 0 && c.preloadMode(key) == "idle" {
				state = "idle"
			}
		}
		result[key] = state
	}
	a.live.mu.Unlock()
	w.Header().Set("Cache-Control", "no-store")
	jsonOut(w, 200, result)
}
