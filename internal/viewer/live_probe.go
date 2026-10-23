package viewer

import (
	"context"
	"errors"
	"os/exec"
	"strings"
)

// Only return fixed messages. FFprobe's stderr can contain the complete source
// URL, including camera credentials; it must never reach responses or logs.
func liveProbeFailure(ctxErr, err error) (code, message string) {
	switch {
	case errors.Is(ctxErr, context.DeadlineExceeded):
		return "timeout", "Zeitüberschreitung nach 8 Sekunden. Die Kamera hat den Verbindungstest nicht rechtzeitig abgeschlossen."
	case errors.Is(ctxErr, context.Canceled):
		return "cancelled", "Verbindungstest abgebrochen."
	case errors.Is(err, exec.ErrNotFound):
		return "probe-unavailable", "FFprobe ist auf dem Viewer-Server nicht verfügbar."
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		return "probe-failed", "FFprobe konnte den Verbindungstest nicht ausführen."
	}
	diagnostic := strings.ToLower(string(exit.Stderr))
	contains := func(values ...string) bool {
		for _, value := range values {
			if strings.Contains(diagnostic, value) {
				return true
			}
		}
		return false
	}
	switch {
	case contains("526"):
		return "tls", "HTTPS-Zertifikat nicht vertrauenswürdig. Zertifikat oder die Ausnahme für diese Quelle prüfen."
	case contains("422"):
		return "stream-error", "Die HTTPS-Adresse liefert keinen FLV-Stream."
	case contains("401 unauthorized", "server returned 401"):
		return "authentication", "Kamera lehnt die Anmeldung ab (401). Kamerabenutzer und Kamerapasswort prüfen."
	case contains("403 forbidden", "server returned 403"):
		return "forbidden", "Kamera verweigert den Zugriff (403). RTSP-Berechtigung des Kamerabenutzers prüfen."
	case contains("404 not found", "server returned 404"):
		return "not-found", "Streampfad nicht gefunden (404). Streamadresse und Kanal prüfen."
	case contains("461 unsupported transport", "server returned 461"):
		return "transport", "Kamera unterstützt die angeforderte RTSP/TCP-Verbindung nicht (RTSP 461)."
	case contains("453 not enough bandwidth", "503 service unavailable", "server returned 453", "server returned 503"):
		return "camera-busy", "Kamera meldet RTSP 453/503. Möglicherweise ausgelastet oder zu viele gleichzeitige Stream-Verbindungen."
	case contains("connection refused"):
		return "connection-refused", "Kamera lehnt die TCP-Verbindung ab. RTSP-Port und aktivierten RTSP-Dienst prüfen."
	case contains("no route to host", "network is unreachable"):
		return "network", "Kamera ist aus dem Netzwerk des Viewer-Containers nicht erreichbar. Routing und Firewall prüfen."
	case contains("failed to resolve hostname", "name or service not known", "temporary failure in name resolution"):
		return "dns", "Der Kamerahostname kann vom Viewer-Server nicht aufgelöst werden."
	case contains("connection timed out", "operation timed out"):
		return "timeout", "Die Verbindung zur Kamera ist zeitlich abgelaufen. Erreichbarkeit aus dem Viewer-Container prüfen."
	case contains("certificate verify failed", "certificate verification failed", "tls handshake", "ssl handshake"):
		return "tls", "Die verschlüsselte RTSP-Verbindung ist fehlgeschlagen. TLS-Einstellungen der Kamera prüfen."
	case contains("unrecognized option", "option not found", "not on whitelist", "protocol not found"):
		return "probe-configuration", "Die FFprobe-Installation unterstützt die benötigten RTSP-Optionen oder Protokolle nicht."
	default:
		return "stream-error", "Die Kamera liefert keinen lesbaren RTSP-Stream. Streamadresse, Kameraberechtigungen und gleichzeitige Verbindungen prüfen."
	}
}
