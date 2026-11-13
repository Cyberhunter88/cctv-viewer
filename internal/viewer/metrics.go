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
	streamsStarted   atomic.Int64
	streamFailures   atomic.Int64
	loginSuccess     atomic.Int64
	loginFailures    atomic.Int64
	encoderFallbacks atomic.Int64
	liveDrops        atomic.Int64
}

func (a *App) metricValues() map[string]int64 {
	cfg := a.config()
	a.mediaMu.Lock()
	sessions := len(a.streams)
	encoding := a.encoderActive
	a.mediaMu.Unlock()
	a.index.mu.Lock()
	warnings := len(a.index.status.Warnings)
	a.index.mu.Unlock()
	return map[string]int64{
		"uptimeSeconds":    int64(time.Since(startedAt).Seconds()),
		"goroutines":       int64(runtime.NumGoroutine()),
		"streamSessions":   int64(sessions),
		"encoding":         int64(encoding),
		"encodingLimit":    int64(cfg.MaxTranscoders),
		"temporaryBytes":   a.streamUsage(),
		"temporaryBudget":  cfg.HLSTempBytes,
		"streamsStarted":   a.metrics.streamsStarted.Load(),
		"streamFailures":   a.metrics.streamFailures.Load(),
		"encoderFallbacks": a.metrics.encoderFallbacks.Load(),
		"loginSuccess":     a.metrics.loginSuccess.Load(),
		"loginFailures":    a.metrics.loginFailures.Load(),
		// Live packets the RTSP share could not hand to a reader.
		"liveGatewayDrops": a.metrics.liveDrops.Load(),
		"scanWarnings":     int64(warnings),
	}
}

func (a *App) metricsHandler(w http.ResponseWriter, r *http.Request) {
	jsonOut(w, 200, a.metricValues())
}
