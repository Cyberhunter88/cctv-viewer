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
	users        int
	gatewayUsers int
	last         time.Time
	state        string
	cancel       context.CancelFunc
}

func warmWanted(mode string, users int, last, now time.Time) bool {
	return mode == "always" || mode == "idle" && (users > 0 || !last.IsZero() && now.Sub(last) < 60*time.Second)
}
func warmReaderWanted(mode string, users, gatewayUsers int, last, now time.Time) bool {
	return gatewayUsers == 0 && warmWanted(mode, users, last, now)
}
func warmDeadline(mode string, users int, last time.Time) time.Time {
	if mode == "idle" && users == 0 && !last.IsZero() {
		return last.Add(60 * time.Second)
	}
	return time.Time{}
}
func (s *liveService) useSource(id, view, quality string) func() {
	return s.useSourceKind(id, view, quality, false)
}
func (s *liveService) useGatewaySource(id, view, quality string) func() {
	return s.useSourceKind(id, view, quality, true)
}
func (s *liveService) useSourceKind(id, view, quality string, gateway bool) func() {
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
	if gateway {
		entry.gatewayUsers++
	}
	entry.last = time.Now()
	s.mu.Unlock()
	s.wakeWarm()
	var once sync.Once
	return func() {
		once.Do(func() {
			s.mu.Lock()
			entry.users--
			if gateway {
				entry.gatewayUsers--
			}
			entry.last = time.Now()
			s.mu.Unlock()
			s.wakeWarm()
		})
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
		if ctx.Err() != nil {
			return
		}
		cameras, err := s.app.db.Cameras()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			case <-s.warmWake:
			case <-time.After(5 * time.Second):
			}
			continue
		}
		configs := map[string]liveCamera{}
		zoomEnabled := map[string]bool{}
		for _, camera := range cameras {
			zoomEnabled[camera.ID] = camera.ViewMode == "dual"
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
				if view == "zoom" && !zoomEnabled[id] {
					continue
				}
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
		var next time.Time
		for name, e := range s.warm {
			c, exists := configs[e.camera]
			view, quality := liveParts(e.key)
			_, sourceError := c.source(view, quality)
			// An HA route already reads from go2rtc and keeps the camera source
			// connected. A second reader only duplicates RTP processing.
			wanted := exists && c.Enabled && (view != "zoom" || zoomEnabled[e.camera]) && sourceError == nil && s.ready && warmReaderWanted(c.preloadMode(e.key), e.users, e.gatewayUsers, e.last, now)
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
			if !exists || (view == "zoom" && !zoomEnabled[e.camera]) || (!wanted && e.users == 0 && (c.preloadMode(e.key) == "off" || !e.last.IsZero() && now.Sub(e.last) >= 60*time.Second)) {
				delete(s.warm, name)
			} else if deadline := warmDeadline(c.preloadMode(e.key), e.users, e.last); !deadline.IsZero() {
				if next.IsZero() || deadline.Before(next) {
					next = deadline
				}
			}
		}
		s.mu.Unlock()
		if next.IsZero() {
			select {
			case <-ctx.Done():
				return
			case <-s.warmWake:
			}
		} else {
			timer := time.NewTimer(time.Until(next))
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-s.warmWake:
				timer.Stop()
			case <-timer.C:
			}
		}
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
