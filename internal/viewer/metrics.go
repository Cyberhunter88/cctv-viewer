package viewer

import (
	"net/http"
	"runtime"
	"sync/atomic"
	"time"
)

var startedAt = time.Now()

// counters holds process-lifetime operational counters exposed to administrators.
type counters struct {
	streamsStarted atomic.Int64
	streamFailures atomic.Int64
	loginSuccess   atomic.Int64
	loginFailures  atomic.Int64
}

func (a *App) metricsHandler(w http.ResponseWriter, r *http.Request) {
	cfg := a.config()
	a.mediaMu.Lock()
	sessions := len(a.streams)
	encoding := a.encoderActive
	a.mediaMu.Unlock()
	a.index.mu.Lock()
	warnings := len(a.index.status.Warnings)
	a.index.mu.Unlock()
	jsonOut(w, 200, map[string]any{
		"uptimeSeconds":   int64(time.Since(startedAt).Seconds()),
		"goroutines":      runtime.NumGoroutine(),
		"streamSessions":  sessions,
		"encoding":        encoding,
		"encodingLimit":   cfg.MaxTranscoders,
		"temporaryBytes":  a.streamUsage(),
		"temporaryBudget": cfg.HLSTempBytes,
		"streamsStarted":  a.metrics.streamsStarted.Load(),
		"streamFailures":  a.metrics.streamFailures.Load(),
		"loginSuccess":    a.metrics.loginSuccess.Load(),
		"loginFailures":   a.metrics.loginFailures.Load(),
		"scanWarnings":    warnings,
	})
}
