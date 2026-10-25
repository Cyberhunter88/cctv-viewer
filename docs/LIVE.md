# Liveansicht und Home Assistant (ab 0.10.0)

## Kamera einrichten

Unter **Verwaltung → Kameras** eine vorhandene Kamera wählen und den Reiter
**Live** öffnen. Neue Kameras zuerst speichern. Live aktivieren, Kamerabenutzer und Passwort sowie die RTSP-Adresse des
H.264-Nebenstreams eintragen. RTSP muss in der Reolink-Kamera eingeschaltet sein.
Die Adresse enthält keine Zugangsdaten, beispielsweise
`rtsp://kamera.example.org:554/h264Preview_01_sub`. Der genaue Pfad hängt vom
Modell ab. Hauptstream und separates Zoomobjektiv sind optional. Bei „Eine Ansicht“ sind Zoom-Einstellungen und Zoom-Bedienelemente ausgeblendet. Vorhandene Zoom-Adressen bleiben gespeichert, ihre Streams ruhen bis zum Wechsel auf „Weitwinkel + Zoom“.

**Verbindung testen** zeigt Erreichbarkeit und Codecs, ohne die Eingabe zu
speichern. **Liveeinstellungen speichern** übernimmt die Konfiguration. Ein leeres
Passwortfeld behält das gespeicherte Passwort bei; Entfernen ist ausdrücklich
wählbar. Kamera-Zugangsdaten werden in der Datenbank verschlüsselt gespeichert.
Die Schlüsseldatei muss bei Sicherung und Wiederherstellung mitgeführt werden;
siehe [Datenverschlüsselung](CONFIGURATION.md#verschlüsselte-zugangsdaten-ab-0110).

Der Menüpunkt **Liveansicht** besitzt eigene Kameraauswahl und Raster, unabhängig
von Aufnahmen. Alle/Keine und Kameraschaltflächen speichern automatisch für das
angemeldete Konto. Ton beginnt stumm; ein Kamerabild kann hörbar sein. Das Zoom-Bild wird erst beim Öffnen übertragen; ob seine Kameraquelle bereits
bereitsteht, bestimmt ihre Vorladeeinstellung. Kameras ohne Livequelle zeigen einen Hinweis.

## Kurze Verzögerung

Der Viewer bevorzugt WebRTC, wenn dessen direkter Medienweg erreichbar ist.
Andernfalls wechselt er nach zwei Sekunden zu MSE über WebSocket und die
vorhandene HTTPS-Adresse. In Docker ohne zusätzliche Medienports ist MSE der
übliche Weg. Es ist kein öffentlicher RTSP-Port und kein TURN-Dienst nötig.
WebRTC-Signalisierung und HTTPS-Stream laufen durch die Viewer-Anmeldung.

**Schnell** verwendet den Nebenstream ohne Videotranscodierung. Hohe Qualität
ist manuell wählbar; nicht unterstützte Codecs führen zurück zum schnellen
Stream. Insbesondere H.265 ist browserabhängig. Für zuverlässige Wiedergabe
H.264 verwenden. Browser ohne MediaSource/ManagedMediaSource benötigen einen
funktionierenden WebRTC-Medienweg oder einen neueren Browser. Es wird nicht auf
langsamere HLS-Wiedergabe ausgewichen.

Zielwerte unter stabilen Bedingungen sind unter einer Sekunde bei direktem
WebRTC und ungefähr ein bis zwei Sekunden bei MSE. Kamera, GOP/Keyframe-Intervall,
Netz und Browser beeinflussen Startzeit und tatsächliche Verzögerung. Das ist
keine Latenzgarantie. Wenn die Kamera es unterstützt, ein Keyframe-/I-Frame-Intervall
von ungefähr einer Sekunde einstellen: Lange Intervalle verzögern insbesondere
den ersten Bildaufbau und das Wiederverbinden. Zur Messung eine Uhr gemeinsam mit dem Livebild aufnehmen;
Transport, Codec und Serverlast festhalten.

Reverse-Proxys müssen WebSocket-Upgrades erlauben. Beispiel für einen bestehenden
NGINX-HTTPS-Virtualhost (Ziel an die eigene Installation anpassen):

```nginx
location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_read_timeout 3600s;
    proxy_buffering off;
}
```

## Optional: Home Assistant

1. In **Livequellen und Freigabe** die gewünschten Kameras jeweils für Home
   Assistant freigeben und die Kameraeinstellung speichern.
2. Die globale **RTSP-Freigabe im Heimnetz** aktivieren und speichern. Das erzeugte
   Passwort wird einmal angezeigt. Benutzername ist `homeassistant`.
3. Den Listener für die eigene Installation erreichbar machen. Native Installation:
   `CTV_LIVE_RTSP_ADDRESS=<Heimnetz-IP>:8554`. Docker: In `compose.yaml` beim RTSP-Portmapping
   `127.0.0.1:8554:8554/tcp` die erste Adresse durch die Heimnetz-IP des
   Dockerhosts ersetzen und
   `docker compose up -d` ausführen. Alles läuft mit derselben `compose.yaml`
   in einem Container. Standardmäßig bindet Port 8554 nur an den Dockerhost.
4. Unter **Streamfreigabe** die von Home Assistant erreichbare **Server-URL**
   eintragen, zum Beispiel `rtsp://192.168.1.10:8554`. Sie dient nur den
   kopierbaren Streamadressen und ändert weder Listener noch Docker-Portmapping.
   Ohne Eintrag bleibt der Platzhalter in den angezeigten Adressen erhalten.
5. In Home Assistant **Einstellungen → Geräte & Dienste → Integration hinzufügen →
   Generic Camera** wählen. Die fertige Streamadresse kopieren,
   Benutzername/Passwort separat eintragen und
   **RTSP-Transport TCP** auswählen. Ein separates Standbild ist nicht erforderlich,
   wenn Home Assistants Stream-Integration aktiv ist.

Beispieladresse ohne Zugangsdaten:
`rtsp://viewer.example.org:8554/live/<kamera-id>/wide/fast`.
Die Kamera-ID hält Adressen bei Umbenennungen stabil. Weitere Profile sind
`wide/high`, `zoom/fast` und `zoom/high`, sofern eingerichtet.

Die Freigabe ist ausschließlich lesend und unabhängig von Browseranmeldungen
und der persönlichen Liveauswahl. Kamera und Qualitätsprofil werden zwischen
Betrachtern geteilt. Home Assistant kann den Stream weiter verwenden, wenn im
Viewer keine Kamera geöffnet ist. Erst der letzte Betrachter beendet die Quelle.
Das Zurücksetzen des Freigabepassworts beendet bestehende Freigabeverbindungen.
RTSP ist unverschlüsselt und für das Heimnetz/VPN bestimmt; Port 8554 nicht ins
Internet weiterleiten. Die go2rtc-Verwaltungsoberfläche wird nicht veröffentlicht.

Dokumentation: [Home Assistant Generic Camera](https://www.home-assistant.io/integrations/generic/).
Die Verwendung der generischen Streamquelle umfasst keine Reolink-Ereignisse,
PTZ-Steuerung, automatische Erkennung oder Gegensprechen.

## Installation und Fehlerdiagnose

Das Dockerimage enthält go2rtc 1.9.14 einschließlich MIT-Lizenzhinweis unter
`/usr/share/licenses/go2rtc/LICENSE`. Native Installationen benötigen dessen
passende Binärdatei im PATH oder `CTV_GO2RTC_PATH`. Für Linux kann
`sh scripts/build_go2rtc.sh /gewünschter/pfad/go2rtc` die feste Upstream-Version
über Go-Modulprüfsummen bauen. Dabei werden nur zwei eigenständige C-Generatoren
weggelassen, deren fertige Go-Definitionen Upstream bereits mitliefert.

Der Viewer startet und beendet den Kindprozess selbst, hält dessen API lokal
und startet ihn nach einem Absturz erneut. Fehlt go2rtc oder fällt eine Kamera
aus, bleiben Aufnahmen verfügbar. Streamfehler und Listenerzustand stehen in der
Oberfläche. Nach Abwahl, Navigation oder ausgeblendetem Browsertab werden
Viewer-Verbindungen geschlossen; nach Rückkehr werden aktuelle Bilder geladen.

### Verbindungstest fehlgeschlagen

Der Verbindungstest unterscheidet Kamera-Anmeldung (RTSP 401), fehlende Rechte
(403), ungültigen Stream-Pfad (404), abgelehnte TCP-Verbindung, DNS-/Routingfehler,
Transportfehler und Zeitüberschreitungen. FFprobe-Rohmeldungen werden nicht an den
Browser weitergegeben, weil sie Kamera-Zugangsdaten enthalten können.

Für die Verbindung **Kamera → Viewer** sind die Zugangsdaten der Reolink-Kamera
nötig. Die separat erzeugten Zugangsdaten `homeassistant` gelten ausschließlich
für **Viewer → Home Assistant**. Eine funktionierende HA-Verbindung bestätigt
keine Anmeldung des Viewers. Für „Schnell“ möglichst den H.264-Nebenstream
verwenden; H.265-Hauptstreams benötigen einen passenden Browser.

## Quellen über HTTPS-FLV (ab 0.11.0)

Jede Quelle kann alternativ eine direkte HTTPS-FLV-Adresse verwenden, etwa:

```text
https://kamera.example.org/flv?port=1935&app=bcs&stream=channel0_sub.bcs
```

Bei Reolink den H.264-Stream verwenden. Eine vollständige Adresse mit
`user` und `password` kann eingefügt werden: Die Oberfläche übernimmt diese
Werte in die separaten Zugangsdatenfelder und entfernt sie aus der Adresse.
Abweichende Zugangsdaten werden nur nach Rückfrage übernommen; ein leerer
Passwortparameter löscht kein gespeichertes Passwort. Zugangsdaten gelten für
alle Quellen derselben Kamera. HTTPS-HLS, MJPEG und Kamera-Webseiten sind
nicht Bestandteil dieser Unterstützung.

Der Viewer prüft das HTTPS-Zertifikat einschließlich Hostname oder IP-Adresse.
Für eine Kamera mit selbstsigniertem Zertifikat kann unter **Erweitert** die
Prüfung gezielt für diese Quelle deaktiviert werden. Verbindungstest und
Wiedergabe verwenden dieselbe Einstellung. HTTPS-Weiterleitungen werden nicht
verfolgt. Ein interner Relay überträgt die FLV-Daten ohne Dateispeicherung an
go2rtc; der Browser bekommt die Kameraadresse und ihre Zugangsdaten nicht.

## Verbindung bereithalten (ab 0.11.0)

Jede Quelle hat eine unabhängige Auswahl:

- **Bei Bedarf:** keine zusätzliche Verbindung ohne Nutzung.
- **60 Sekunden Nachlauf:** nach Ende der letzten Viewer- oder
  Home-Assistant-Nutzung noch 60 Sekunden verbunden halten.
- **Dauerhaft bereit:** auch ohne Zuschauer nach Serverstart verbinden.

Neue und bisherige Konfigurationen verwenden standardmäßig den Nachlauf.
Nach einem Neustart werden Nachlaufquellen erst bei tatsächlicher Nutzung
verbunden. Die Einstellung gilt auch für hohe Qualität und Zoom unabhängig
voneinander. Dauerhaftes Bereithalten benötigt laufend Bandbreite und
Kameraverbindungen. Eine zusätzliche WebRTC-Audioumwandlung startet erst bei
Wiedergabe. Es entstehen keine Liveaufnahmen oder Rückspulmöglichkeit.

Die Oberfläche zeigt den Verbindungszustand jeder Quelle. Nach Fehlern folgen
erneute Versuche mit wachsendem Abstand bis 30 Sekunden. Ein bereiter
Kamerastream kann schneller starten; Browseraufbau und das nächste Schlüsselbild
bestimmen weiterhin den tatsächlichen Bildbeginn.

## Streamfreigabe verwalten

Globale Freigabe und Zugangsdaten stehen unter **Verwaltung → Streamfreigabe**.
Dort lassen sich Adressen je Kamera kopieren. Ob eine Kamera freigegeben ist,
wird weiterhin in ihrem Reiter **Live** eingestellt.
