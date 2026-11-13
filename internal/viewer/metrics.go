package viewer

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"runtime"
	"strings"
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
	alertsSent       atomic.Int64
	alertFailures    atomic.Int64
	encoderFallbacks atomic.Int64
}

// metric describes one value for both the JSON and the Prometheus view.
type metric struct {
	key, name, help string
	counter         bool
	value           int64
}

func (a *App) metricValues() []metric {
	cfg := a.config()
	a.mediaMu.Lock()
	sessions := len(a.streams)
	encoding := a.encoderActive
	a.mediaMu.Unlock()
	a.index.mu.Lock()
	warnings := len(a.index.status.Warnings)
	a.index.mu.Unlock()
	return []metric{
		{"uptimeSeconds", "cctv_uptime_seconds", "Sekunden seit dem Start", false, int64(time.Since(startedAt).Seconds())},
		{"goroutines", "cctv_goroutines", "Aktive Goroutinen", false, int64(runtime.NumGoroutine())},
		{"streamSessions", "cctv_stream_sessions", "Offene Wiedergabesitzungen", false, int64(sessions)},
		{"encoding", "cctv_encoders_active", "Laufende Umwandlungen", false, int64(encoding)},
		{"encodingLimit", "cctv_encoders_limit", "Erlaubte gleichzeitige Umwandlungen (0 = unbegrenzt)", false, int64(cfg.MaxTranscoders)},
		{"temporaryBytes", "cctv_temporary_bytes", "Belegter temporärer Videospeicher", false, a.streamUsage()},
		{"temporaryBudget", "cctv_temporary_budget_bytes", "Budget für temporären Videospeicher", false, cfg.HLSTempBytes},
		{"streamsStarted", "cctv_streams_started_total", "Gestartete Umwandlungen", true, a.metrics.streamsStarted.Load()},
		{"streamFailures", "cctv_stream_failures_total", "Fehlgeschlagene Umwandlungen", true, a.metrics.streamFailures.Load()},
		{"encoderFallbacks", "cctv_encoder_fallbacks_total", "Rückfälle von Hardware- auf Software-Encoding", true, a.metrics.encoderFallbacks.Load()},
		{"loginSuccess", "cctv_login_success_total", "Erfolgreiche Anmeldungen", true, a.metrics.loginSuccess.Load()},
		{"loginFailures", "cctv_login_failures_total", "Fehlgeschlagene Anmeldungen", true, a.metrics.loginFailures.Load()},
		{"alertsSent", "cctv_alerts_sent_total", "Zugestellte Warnmeldungen", true, a.metrics.alertsSent.Load()},
		{"alertFailures", "cctv_alert_failures_total", "Nicht zugestellte Warnmeldungen", true, a.metrics.alertFailures.Load()},
		{"scanWarnings", "cctv_scan_warnings", "Warnungen des letzten Indexlaufs", false, int64(warnings)},
	}
}

func (a *App) metricsHandler(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{}
	for _, m := range a.metricValues() {
		out[m.key] = m.value
	}
	jsonOut(w, 200, out)
}

// prometheusMetrics serves the same values in the Prometheus text format. It is
// only reachable with the bearer token from CTV_METRICS_TOKEN; without a
// configured token the endpoint does not exist.
func (a *App) prometheusMetrics(w http.ResponseWriter, r *http.Request) {
	token := a.config().MetricsToken
	if token == "" {
		http.NotFound(w, r)
		return
	}
	given, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || subtle.ConstantTimeCompare([]byte(given), []byte(token)) != 1 {
		w.Header().Set("WWW-Authenticate", `Bearer realm="metrics"`)
		fail(w, 401, "Metrik-Token erforderlich")
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	var b strings.Builder
	for _, m := range a.metricValues() {
		kind := "gauge"
		if m.counter {
			kind = "counter"
		}
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s %s\n%s %d\n", m.name, m.help, m.name, kind, m.name, m.value)
	}
	w.Write([]byte(b.String()))
}
