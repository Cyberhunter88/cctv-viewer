package viewer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"
)

const alertInterval = 5 * time.Minute

// alertStatus is the last state that was reported successfully for a camera.
type alertStatus struct{ offline, stale bool }

type alertMessage struct {
	Event         string `json:"event"`
	Message       string `json:"message"`
	CameraID      string `json:"cameraId,omitempty"`
	Camera        string `json:"camera,omitempty"`
	Time          string `json:"time"`
	LastRecording string `json:"lastRecording,omitempty"`
}

func validAlertURL(raw string) error {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 500 || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Fragment != "" {
		return errors.New("Webhook-Adresse muss eine http- oder https-URL sein")
	}
	return nil
}

// Redirects are not followed: the configured address is the only destination.
var alertClient = &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

func (a *App) sendAlert(ctx context.Context, target string, message alertMessage) error {
	message.Time = time.Now().UTC().Format(time.RFC3339)
	body, err := json.Marshal(message)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json; charset=utf-8")
	request.Header.Set("User-Agent", "cctv-viewer")
	response, err := alertClient.Do(request)
	if err != nil {
		a.metrics.alertFailures.Add(1)
		// The address may carry a token, so the transport error is not logged verbatim.
		return errors.New("Webhook nicht erreichbar")
	}
	response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode > 299 {
		a.metrics.alertFailures.Add(1)
		return fmt.Errorf("Webhook antwortet mit Status %d", response.StatusCode)
	}
	a.metrics.alertsSent.Add(1)
	return nil
}

// checkAlerts compares every camera with the state reported last and sends one
// message per change. A failed delivery keeps the old state, so the change is
// reported again with the next check.
func (a *App) checkAlerts() {
	cfg := a.config()
	a.alertMu.Lock()
	defer a.alertMu.Unlock()
	if cfg.AlertWebhookURL == "" {
		a.alertState = nil
		return
	}
	cameras, err := a.db.Cameras()
	if err != nil {
		return
	}
	if a.alertState == nil {
		a.alertState = map[string]alertStatus{}
	}
	known := map[string]bool{}
	now := time.Now()
	for _, camera := range cameras {
		if a.ctx.Err() != nil {
			return
		}
		known[camera.ID] = true
		previous := a.alertState[camera.ID]
		current := alertStatus{}
		if st, e := a.root.Stat(camera.Folder); e != nil || !st.IsDir() {
			current.offline = true
		}
		var last float64
		if e := a.db.reader().QueryRowContext(a.ctx, `SELECT COALESCE(MAX(start),0) FROM recordings WHERE camera_id=?`, camera.ID).Scan(&last); e != nil {
			continue
		}
		// An unreachable source explains missing recordings; report only the cause.
		current.stale = !current.offline && cfg.AlertStaleHours > 0 && last > 0 && now.Sub(time.Unix(int64(last), 0)) > time.Duration(cfg.AlertStaleHours)*time.Hour
		if current.offline {
			current.stale = previous.stale
		}
		message := alertMessage{CameraID: camera.ID, Camera: camera.Name}
		if last > 0 {
			message.LastRecording = time.Unix(int64(last), 0).UTC().Format(time.RFC3339)
		}
		reported := previous
		send := func(event, text string) bool {
			message.Event, message.Message = event, "Kamera „"+camera.Name+"“: "+text
			if e := a.sendAlert(a.ctx, cfg.AlertWebhookURL, message); e != nil {
				slog.Warn("Warnmeldung nicht zugestellt", "camera", camera.ID, "event", event, "error", e)
				return false
			}
			return true
		}
		if current.offline != previous.offline {
			if current.offline && send("source_offline", "Aufnahmequelle nicht erreichbar") || !current.offline && send("source_online", "Aufnahmequelle wieder erreichbar") {
				reported.offline = current.offline
			}
		}
		if current.stale != previous.stale {
			if current.stale && send("recordings_stale", fmt.Sprintf("seit mehr als %d Stunden keine neue Aufnahme", cfg.AlertStaleHours)) || !current.stale && send("recordings_resumed", "es werden wieder Aufnahmen indexiert") {
				reported.stale = current.stale
			}
		}
		a.alertState[camera.ID] = reported
	}
	for id := range a.alertState {
		if !known[id] {
			delete(a.alertState, id)
		}
	}
}

// startAlertCheck runs one check in the background unless one is still running.
func (a *App) startAlertCheck() {
	if !a.alertRunning.CompareAndSwap(false, true) {
		return
	}
	a.work.Add(1)
	go func() {
		defer a.work.Done()
		defer a.alertRunning.Store(false)
		a.checkAlerts()
	}()
}

func (a *App) testAlert(w http.ResponseWriter, r *http.Request) {
	target := a.config().AlertWebhookURL
	if target == "" {
		fail(w, 400, "Keine Webhook-Adresse gespeichert")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if err := a.sendAlert(ctx, target, alertMessage{Event: "test", Message: "Testmeldung des CCTV Viewers"}); err != nil {
		fail(w, 502, "Testmeldung nicht zugestellt: "+err.Error())
		return
	}
	jsonOut(w, 200, map[string]bool{"ok": true})
}
