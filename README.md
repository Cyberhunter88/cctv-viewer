# CCTV Viewer

Ein schlanker, selbst gehosteter **deutscher Aufnahmeviewer**: mehrere Kameras,
eine gemeinsame Zeitleiste, geschützte Anmeldung und ein Docker-Container.

- Synchronisierte Kameraansichten, Datumsauswahl, Timeline-Zoom und Vorschaubilder
- Originalwiedergabe mit HTTP-Range und H.265 → H.264-HLS bei Bedarf
- OIDC mit Gruppen-/Nutzerfreigaben und abschaltbarem lokalen Administrator
- Aufnahmen ausschließlich lesend; SQLite und Cache in einem Datenverzeichnis
- AMD64 und ARM64; Images automatisch beim Veröffentlichen eines GitHub-Releases

Kein NVR: Der Viewer zeichnet keine Livekameras auf. Er liest bereits vorhandene
Videodateien. Clip-Export, KI-Analyse und Home Assistant sind nicht Bestandteil.

## Mit Docker Compose starten

Voraussetzungen: Docker mit Compose, ein vorhandener HTTPS-Reverse-Proxy und ein
vom Host lesbarer Aufnahmeordner. Netzwerkfreigaben zuerst **auf dem Host** mounten.

```bash
curl -fsSLO https://raw.githubusercontent.com/Cyberhunter88/cctv-viewer/main/compose.yaml
curl -fsSL https://raw.githubusercontent.com/Cyberhunter88/cctv-viewer/main/.env.example -o .env
mkdir -p data secrets
sudo chown 10001:10001 data
sudo chmod 700 data
```

In `.env` mindestens `CTV_BASE_URL=https://cctv.example.org` und
`CTV_RECORDINGS_PATH=/mnt/kameras` eintragen. Für eine Vorabversion zusätzlich
`CTV_VERSION=0.1.0-beta.1` setzen, da Vorabversionen kein `latest` erhalten.
UID/GID lassen sich über `CTV_UID`/`CTV_GID` ändern; der Datenordner muss dann
entsprechend gehören. Der Prozess benötigt Leserechte auf den Kameraordnern.

```bash
docker compose pull
docker compose run --rm cctv-viewer admin --username admin
docker compose up -d
```

Das Admin-Kommando fragt das Passwort verdeckt zweimal ab (mindestens 14 Zeichen).
Ein bereits existierender Benutzername wird nicht überschrieben. Für eine
Wiederherstellung kann ein neuer Adminname über dasselbe CLI angelegt werden.
Keine Benutzerregistrierung, keine Standardzugangsdaten. Für Automation steht
`CTV_ADMIN_PASSWORD_FILE` zur Verfügung. Passwörter nicht als Kommandoargument
übergeben und nicht in Git speichern.

Der Compose-Port ist nur an `127.0.0.1:8080` gebunden. Der Reverse-Proxy auf dem
Host leitet zu dieser Adresse weiter. Bei einem Proxy in einem anderen Container
beide Container in dasselbe Docker-Netzwerk aufnehmen und den Proxy auf
`http://cctv-viewer:8080` richten; dafür ist kein veröffentlichter Port nötig.

Caddy-Beispiel für einen Proxy auf dem Host:

```caddyfile
cctv.example.org {
    reverse_proxy 127.0.0.1:8080
}
```

`CTV_BASE_URL` muss genau der externen HTTPS-Adresse entsprechen (kein Unterpfad).
`CTV_TRUSTED_PROXIES` enthält ausschließlich die konkreten Proxy-IP-Adressen/CIDRs,
damit weitergereichte Client-IP-Adressen für das Login-Limit ausgewertet werden.
Der Proxy muss fremde `X-Forwarded-For`-Werte zuverlässig behandeln. Ohne diese
Konfiguration gilt das Login-Limit gemeinsam für die direkte Proxy-Adresse.

## OIDC einrichten

Einen vertraulichen OIDC-Webclient mit Authorization Code Flow und PKCE S256
anlegen. Callback-URL:

```text
https://cctv.example.org/auth/oidc/callback
```

Beispiel `.env` (Discovery-Issuer des jeweiligen Anbieters verwenden):

```dotenv
CTV_OIDC_ISSUER=https://auth.example.org/application/o/cctv/
CTV_OIDC_CLIENT_ID=cctv-viewer
CTV_OIDC_CLIENT_SECRET_FILE=/run/secrets/oidc-client-secret
CTV_OIDC_GROUP_CLAIM=groups
CTV_OIDC_ALLOWED_GROUPS=cctv-viewers
CTV_OIDC_ADMIN_GROUPS=cctv-admins
```

Das Client-Secret in `./secrets/oidc-client-secret` ablegen und nur für die
Container-UID lesbar machen. Der Gruppen-Claim muss als Array von Strings **im
ID-Token** enthalten sein; beim Anbieter einen entsprechenden Mapper einrichten.
Angefordert werden `openid profile`. Zusätzliche anbieterspezifische Claims sind
über den Mapper bereitzustellen. Verschachtelte Claim-Pfade werden nicht ausgewertet.

Alternativ/zusätzlich `CTV_OIDC_ALLOWED_SUBJECTS` und `CTV_OIDC_ADMIN_SUBJECTS`
setzen: durch Komma getrennte, exakte `sub`-Werte des konfigurierten Issuers.
E-Mail-Adressen werden nicht als Identität verwendet. Adminfreigaben implizieren
Lesezugriff. Ohne passende Freigabe bleibt der Zugang gesperrt.

Betrachter können alle eingerichteten Kameras sehen; Administratoren zusätzlich
Kameras verwalten und manuelle Scans starten. Es gibt keine Rechte pro Kamera.
Nach erfolgreichem Test kann `CTV_LOCAL_ADMIN=false` die lokale Anmeldung
vollständig abschalten. Bei nicht erreichbarem OIDC-Discovery-Endpunkt schlägt
der Start fehl; der Container startet nach Wiederverfügbarkeit erneut.

Sitzungen laufen nach acht Stunden ab. Logout widerruft die lokale Sitzung, beendet
aber nicht die globale Sitzung beim Identity Provider. OIDC-Sitzungen werden beim
Neustart verworfen, damit geänderte Freigaben sicher wirksam werden. Bei Änderungen
an OIDC-Gruppen den Viewer neu starten oder bestehende Sitzungen ablaufen lassen.

## Kamera und Zeitmuster

Als Administrator links die Kameraeinstellungen öffnen und beispielsweise setzen:

| Einstellung | Wert |
|---|---|
| Name | Eingang |
| Kameraordner | Eingang |
| Muster | `{YYYY}/{MM}/{DD}/{hh}-{mm}-{ss}.mp4` |
| Zeitzone | `Europe/Berlin` |
| Zeitversatz | `0` |

Dies erkennt `/recordings/Eingang/2026/10/14/08-30-00.mp4` als Aufnahme ab
08:30 Uhr Berliner Zeit. „Erkennung prüfen“ zeigt Dateinamen und UTC-Zeit vor dem
Speichern. Positive Zeitversätze verschieben den tatsächlichen Start nach hinten.
Andere feste Muster, Trennzeichen und Dateiendungen sind möglich. Alle sechs
Zeitplatzhalter müssen genau einmal vorkommen. Ein Kameraordner darf kein
absoluter Pfad und nicht der Mount-Wurzelordner selbst sein.

Aufnahmen werden anhand des **Dateinamens**, nicht des Änderungsdatums zugeordnet.
Mehrdeutige Uhrzeiten bei der Winterzeitumstellung und nicht existierende Zeiten
bei der Sommerzeitumstellung werden mit Warnung ausgelassen. Bei solchen Archiven
Kameras möglichst auf UTC-Aufnahmen umstellen und `UTC` als Kamera-Zeitzone nutzen.
Die Oberfläche zeigt grundsätzlich `Europe/Berlin` an; ein Tag kann 23/25 Stunden haben.

Beim Öffnen eines Tages wird dessen Index asynchron aufgefrischt (höchstens einmal
pro Minute); der aktuelle Tag zusätzlich alle fünf Minuten. Vorschaubilder entstehen
bei Bedarf. Es läuft ein Scan gleichzeitig. Die UI zeigt Fortschritt und begrenzte
Warnungen. Die Vorschau der Einrichtung ist auf 10.000 Einträge beschränkt; eine
Tagespartition auf 200.000 Einträge. Tagesordner sparen bei großen Archiven Arbeit.
Symlinks werden beim Indexieren nicht verfolgt. Entfernte Dateien verschwinden
beim nächsten erfolgreichen Scan; ein nicht erreichbarer Kameraordner erhält den
vorhandenen Index. Ein **leer erscheinendes, nicht mehr gemountetes NAS** ist nicht
von echten Löschungen unterscheidbar: Host-Mounts vor Containerstart sicherstellen.

Dateien müssen mindestens 30 Sekunden unverändert alt sein und eine gültige Dauer
haben. Nach dem Probe-Lauf werden Größe und Änderungszeit erneut geprüft. Das ist
eine Stabilitätsheuristik; ungewöhnliche Schreiber können später weiter schreiben.
Aufnahmen dürfen bis zu 48 Stunden dauern; der Vortag wird wegen über Mitternacht
laufender Aufnahmen ebenfalls berücksichtigt. Konfigurationsänderungen an Pfad,
Muster oder Zeitversatz verwerfen nur den abgeleiteten Index der Kamera.

## Wiedergabe und Ressourcen

Die gemeinsame Uhr steuert alle sichtbaren Kameras. Lücken werden je Kamera
angezeigt; ohne Aufnahme in allen sichtbaren Kameras springt die Wiedergabe zur
nächsten Aufnahme. Bei überlappenden Clips wird der zuletzt beginnende verwendet.
Browserdecoder können geringfügig voneinander abweichen; Abweichungen über etwa
650 ms werden korrigiert. Es wird keine framegenaue forensische Synchronität zugesagt.

„Automatisch“ bevorzugt H.264 in MP4/MOV sowie browserfähiges H.265. Bei Browser-
Codecfehlern „Kompatibel“ auswählen. MKV/AVI/TS und inkompatible Codecs benötigen
HLS-Konvertierung. Rohes H.264/H.265 ohne ermittelbare Dauer wird ausgelassen.
Keine Audioausgabe. Unterstützte Erweiterungen: MP4, MOV, MKV, AVI, TS, H264, H265.

- `CTV_MAX_TRANSCODERS=1`: gleichzeitige FFmpeg-Videoumwandlungen; zulässig 1–16.
- `CTV_CACHE_MB=512`: gemeinsames Budget für Thumbnails und temporäre HLS-Dateien.
- HLS: maximal 1280 Pixel breit, ca. 2 Mbit/s, ein Encoderthread, kein Audio.
- Nach 90 Sekunden ohne Segmentabruf oder nach 30 Minuten werden Streams entfernt.
  Lange Clips werden in bis zu 30-minütigen Teilen wiedergegeben.
- Ein Watchdog prüft den Cache alle zwei Sekunden, entfernt alte Vorschaubilder und
  beendet bei Überschreitung Streams. Kurzzeitige Überschreitungen sind möglich;
  für eine harte Grenze zusätzlich ein Dateisystem-/Volume-Quota verwenden.
- Pause hält den Stream kurz für erneute Wiedergabe bereit. Verlassen/Verbergen
  der Seite pausiert und gibt Streams frei. Neue Streams können an Kapazitätsgrenzen
  abgewiesen werden; Originalwiedergabe hat dieses Encoderlimit nicht.

Für mehrere gleichzeitige H.265-Kameras mehr Encoderplätze konfigurieren, sofern
CPU und Arbeitsspeicher ausreichen. Hardwarebeschleunigung ist nicht integriert.
Die App kopiert keine Originalvideos. Die Größe und der Speicherverbrauch des
Images werden in den Release-/CI-Prüfungen ausgegeben; Laufzeitlast hängt stark
von Auflösung, Codec, Kamerazahl und Transcoding ab.

## Releases und Updates

In GitHub **Releases → Draft a new release** öffnen, z. B. Tag `v1.0.0` auf `main`
anlegen und **Publish release** wählen. Der Workflow „Docker veröffentlichen“:

1. Prüft Tag, TypeScript, Go, Authentifizierung, Video-Fixtures und Dockerstart.
2. Baut und startet das Image auf nativen AMD64- und ARM64-Runnern.
3. Veröffentlicht ein Multiarch-Image mit SBOM und Build-Provenienz in GHCR.
4. Hängt Compose und eine zur Version passende Konfigurationsvorlage ans Release.
5. Prüft anonymes Herunterladen und erneuten Containerstart.

Image: `ghcr.io/cyberhunter88/cctv-viewer:1.0.0`. Stabile Versionen aktualisieren
passende `1.0`-/`1`-Zeiger; nur die höchste veröffentlichte stabile Version erhält
`latest`. `v1.1.0-beta.1` muss als GitHub-Vorabversion markiert sein und erhält nur
`1.1.0-beta.1`. Entwürfe veröffentlichen nichts. Bestehende Versions-Tags werden
nicht überschrieben; ein Retry muss auf denselben Commit zeigen. Alle fremden
Actions sind auf Commit-SHAs fixiert. Ein Registry-Passwort ist nicht erforderlich;
Actions verwendet das kurzlebige `GITHUB_TOKEN`.

**Einmalig beim ersten Image:** GitHub legt neue GHCR-Pakete zunächst privat an.
Unter Profil → Packages → cctv-viewer → Package settings → Change visibility
„Public“ wählen. Die Repository-Sichtbarkeit allein genügt nicht. Danach den
fehlgeschlagenen Prüfjob erneut ausführen oder den Workflow über „Run workflow“
mit dem vorhandenen Release-Tag starten. Die Ersteinrichtung wird bei Übergabe
soweit möglich bereits durchgeführt.

Updates auf dem Server:

```bash
docker compose pull
docker compose up -d
```

Mit `CTV_VERSION=1.0.0` eine feste Version wählen; mit `latest` beim obigen Befehl
auf die neueste stabile Version wechseln. Keine automatischen Serverupdates.
Vor Updates `/data` sichern: Container stoppen und den Datenordner inklusive
SQLite-Dateien kopieren; danach wieder starten. Zurückrollen durch Auswahl einer
älteren Version und Wiederherstellung ihrer passenden Datensicherung.

## Lokal entwickeln und prüfen

Go 1.27, Node 24, FFmpeg/FFprobe und Linux (sichere FD-Übergabe über `/proc`):

```bash
npm ci
npm run typecheck
npm test
npm run build
go vet ./...
go test -race ./...
python3 -m unittest discover -s scripts -p 'test_*.py'
go build -o bin/cctv ./cmd/cctv
mkdir -p data recordings
CTV_DATA_DIR=./data bin/cctv admin --username admin
CTV_DEV_HTTP=true CTV_BASE_URL=http://localhost:8080 CTV_DATA_DIR=./data CTV_RECORDINGS_DIR=./recordings bin/cctv
```

`CTV_DEV_HTTP=true` ist ausschließlich für lokale Entwicklung vorgesehen;
Sitzungscookies sind dann ohne Secure-Flag. Produktiv HTTPS verwenden.

```bash
docker build -t cctv-viewer:test .
./scripts/smoke.sh cctv-viewer:test
```

Die Tests verwenden ausschließlich temporäre Daten und synthetische Videos,
keine echten Überwachungsaufnahmen oder produktiven OIDC-Zugangsdaten.

## Lizenz

Eigener Anwendungscode: MIT. Drittanbieterhinweise: [THIRD_PARTY.md](THIRD_PARTY.md).
