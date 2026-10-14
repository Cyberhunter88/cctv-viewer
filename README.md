# CCTV Viewer

Ein selbst gehosteter deutscher Aufnahmeviewer mit synchronisierten Kameras,
gemeinsamer Zeitleiste, Docker Compose und geschützter OIDC-/lokaler Anmeldung.
Die Viewer-Funktionen orientieren sich an einem festen Stand des CCTV Timeline
Viewers und sind eigenständig in Go/TypeScript umgesetzt. Unser Layout bleibt erhalten.

- Datumsauswahl, Aufnahmegrenzen, Übersichtstimeline, Zoom und verstellbare Timeline-Höhe
- Automatische und feste Raster bis 4 × 4, eigene Raster bis 8 × 8 und Kameraseiten
- Kameraauswahl und Reihenfolge per Ziehen; manueller oder automatischer Hotspot
- Original, Ausgewogen und Schnell; Wiedergabe von 0,25× bis 16×
- Aufnahmesuche mit Kamera-, Zeitraum- und Mindestdauerfilter
- Datumspartitionierte oder rekursive Indexierung; optionale automatische Tagesindexierung
- OIDC/SSO, Rollen und lokale Administrator-Wiederherstellung
- Aufnahmequellen nur lesend; SQLite, Vorschaubilder und temporäre Streams unter `/data`

Kein NVR: Der Viewer verwendet vorhandene Videodateien. Home Assistant und
Videoanalyse sind ausgeschlossen. Bilder, Clip-Export, Lesezeichen, Zeitlinks,
digitaler Zoom, separate Vollbild- und Audiobedienung wurden entfernt.

Die vollständige Zuordnung zur Referenz steht in [FEATURE_MATRIX.md](FEATURE_MATRIX.md).

## Mit Docker Compose starten

Voraussetzungen: Docker mit Compose, ein vorhandener HTTPS-Reverse-Proxy und ein
vom Host lesbarer Aufnahmeordner. Netzwerkfreigaben zuerst **auf dem Host** mounten.

```bash
curl -fsSLO https://raw.githubusercontent.com/Cyberhunter88/cctv-viewer/main/compose.yaml
curl -fsSL https://raw.githubusercontent.com/Cyberhunter88/cctv-viewer/main/.env.example -o .env
mkdir -p data
chmod 600 .env
sudo chown 10001:10001 data
sudo chmod 700 data
```

In `.env` mindestens `CTV_BASE_URL=https://cctv.example.org` und
`CTV_RECORDINGS_PATH=/mnt/kameras` eintragen. Für eine Vorabversion zusätzlich
`CTV_VERSION=0.1.0-beta.1` setzen, da Vorabversionen kein `latest` erhalten.
Die `.env` liefert die Ausgangswerte. Im Browser gespeicherte Einstellungen
haben dauerhaft Vorrang; über „Herkunft und Umgebungswerte“ lassen sie sich
einzeln zurücksetzen. Die Compose-Datei
muss dafür nicht bearbeitet werden. `CTV_DATA_PATH`, `CTV_BIND_ADDRESS` und
`CTV_HTTP_PORT` steuern Datenordner und Portbindung.
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
Bei ausgesperrtem oder nicht erreichbarem OIDC zusätzlich `--recover` verwenden:
Container stoppen, `docker compose run --rm cctv-viewer admin --username rettung --recover`
ausführen und den Container wieder starten. Das aktiviert die lokale Anmeldung,
deaktiviert OIDC und widerruft vorhandene Sitzungen; übrige Webeinstellungen bleiben erhalten.
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
CTV_OIDC_CLIENT_SECRET=dein-client-secret
CTV_OIDC_GROUP_CLAIM=groups
CTV_OIDC_ALLOWED_GROUPS=
CTV_OIDC_ADMIN_GROUPS=cctv-admin
CTV_OIDC_ALLOWED_SUBJECTS=
CTV_OIDC_ADMIN_SUBJECTS=
```

Das Client-Secret wird direkt in `.env` eingetragen; die Datei ist in Git
ausgeschlossen und sollte nur für dich lesbar sein (`chmod 600 .env`).
Optional bleibt `CTV_OIDC_CLIENT_SECRET_FILE` unterstützt; dafür die Secret-Datei
zusätzlich schreibgeschützt in den Container mounten und die direkte Variable
weglassen. Beide Varianten gleichzeitig werden abgelehnt.

Der Gruppen-Claim muss als Array von Strings **im
ID-Token** enthalten sein; beim Anbieter einen entsprechenden Mapper einrichten.
Angefordert werden `openid profile groups`. Zusätzliche anbieterspezifische Claims sind
über den Mapper bereitzustellen. Verschachtelte Claim-Pfade werden nicht ausgewertet.

Für Pocket-ID den Benutzer zur Gruppe mit dem exakten Namen `cctv-admin`
hinzufügen (nicht nur zum Anzeigenamen). Im OIDC-Client unter **Allowed User
Groups** ebenfalls ausschließlich `cctv-admin` auswählen. Mit der obigen
Konfiguration erhalten nur Mitglieder dieser Gruppe SSO-Zugang und die
Administratorrolle; weitere Gruppen- und Subject-Freigaben bleiben leer.
Pocket-ID liefert die Gruppen über den angeforderten Scope `groups` automatisch.
Über **OIDC Data Preview** lässt sich prüfen, ob das ID-Token
`"groups": ["cctv-admin"]` enthält. Anmeldungseinstellungen können alternativ unter **Einstellungen → Anmeldung/OIDC**
geändert werden. OIDC zuerst speichern, dann die angebotene Testanmeldung mit
einem freigegebenen Administrator abschließen. Bis dahin bleibt die bisherige
Konfiguration aktiv. Der Test nutzt dieselbe Callback-URL. Eine alleinige
Betrachteranmeldung aktiviert nichts; der Testentwurf verfällt nach zehn Minuten.

Nach Änderungen an `.env` den Container mit
`docker compose up -d --force-recreate cctv-viewer` neu erstellen und erneut anmelden.

Alternativ/zusätzlich `CTV_OIDC_ALLOWED_SUBJECTS` und `CTV_OIDC_ADMIN_SUBJECTS`
setzen: durch Komma getrennte, exakte `sub`-Werte des konfigurierten Issuers.
E-Mail-Adressen werden nicht als Identität verwendet. Adminfreigaben implizieren
Lesezugriff. Ohne passende Freigabe bleibt der Zugang gesperrt.

Betrachter können alle eingerichteten Kameras ansehen, durchsuchen und synchronisiert abspielen; Administratoren zusätzlich Kameras, lokale Benutzer und Einstellungen
verwalten und manuelle Scans starten. Es gibt keine Rechte pro Kamera.
Nach erfolgreichem Test kann `CTV_LOCAL_ADMIN=false` die lokale Anmeldung
vollständig abschalten, sofern kein Webwert diese Vorgabe überschreibt. Im Browser
ist das Abschalten erst nach erfolgreicher OIDC-Administrator-Testanmeldung möglich.
Bei nicht erreichbarem OIDC-Discovery-Endpunkt schlägt
der Start fehl; der Container startet nach Wiederverfügbarkeit erneut.

Sitzungen laufen standardmäßig nach acht Stunden ab. Die Webeinstellung
„Sitzungsdauer“ beziehungsweise `CTV_SESSION_MINUTES` gilt für neue Sitzungen
(15–1440 Minuten). Logout widerruft die lokale Sitzung, beendet
aber nicht die globale Sitzung beim Identity Provider. OIDC-Sitzungen werden beim
Neustart verworfen, damit geänderte Freigaben sicher wirksam werden. Im Browser aktivierte OIDC-Änderungen widerrufen bestehende OIDC-Sitzungen sofort.
Bei Gruppenänderungen ausschließlich beim Identity Provider den Viewer neu
starten oder bestehende Sitzungen ablaufen lassen.

## Benutzer und Webeinstellungen

Über das Zahnrad die Kameraeinrichtung öffnen; **Benutzer · Anmeldung · Betrieb**
öffnet die weiteren Einstellungsbereiche. Lokale Konten haben die Rolle
„Administrator“ oder „Betrachter“. Bestehende Administratorkonten und Passwort-Hashes
werden übernommen. Benutzer können gesperrt, gelöscht und ihre Passwörter
zurückgesetzt werden. Passwort-, Rollen- und Sperränderungen widerrufen die
zugehörigen Sitzungen. Der letzte aktive lokale Administrator bleibt geschützt.
Eigene lokale Passwörter über „Passwort ändern“ ändern; SSO-Passwörter beim Provider.

Indexintervall, Streamingprofile, Ressourcenlimits und Sitzungsdauer werden ohne
Neustart übernommen. Bei einem kleineren Streamlimit dürfen laufende Aufträge
fertig werden. Docker-Mounts, Port, Basis-URL und vertrauenswürdige Proxys bleiben
extern konfiguriert und werden nur angezeigt. Gleichzeitig bearbeitete
Einstellungen werden mit Versionskonflikt abgewiesen statt überschrieben.

Client-Secrets werden nie angezeigt oder über die API zurückgeliefert. Die
SQLite-Datenbank enthält jedoch gespeicherte Secrets: Datenordner und Sicherungen
entsprechend schützen. Vor dem ersten Upgrade den Datenordner sichern; neue
Datenbankspalten und die Einstellungstabelle werden automatisch ergänzt.

## Kameraquellen und Indexierung

Über **Mehrere Kameras hinzufügen** lassen sich bis zu 64 Kameraordner gesammelt
auswählen und ihre Anzeigenamen bearbeiten. Das Kameraformular gibt Zeitzone,
Indexierungsart und Ansichten für die Auswahl vor. Alle Quellen werden vor dem
Speichern geprüft; ein ungültiger Ordner verhindert die gesamte Anlage. Anschließend
werden die Aufnahmen des geöffneten Tages eingelesen. Tages- und vollständige
Scans verarbeiten bis zu drei Kameras parallel; Aufträge bleiben untereinander
geordnet. Der Fortschritt zeigt aktive und abgeschlossene Kameras sowie neue Videos.

In „Kameras verwalten“ einen relativen Ordner innerhalb des eingebundenen
Aufnahmeverzeichnisses auswählen. SMB/NFS vorher auf dem Host mounten; der Viewer
speichert keine Freigabe-Zugangsdaten. Symlinks werden beim Indexieren übersprungen.
Ein nicht erreichbarer Kameraordner erhält seinen bisherigen Index. Ein leer
erscheinendes, nicht mehr gemountetes NAS lässt sich von echten Löschungen nicht
unterscheiden: Host-Mounts vor Containerstart prüfen.

Neue Kameras verwenden „Automatische Erkennung“. Unterstützte Videoendungen:
MP4, MOV, MKV, AVI, TS, H264, H265 und DAV. Die tatsächliche Wiedergabefähigkeit
hängt von Codec und gültiger Videodauer ab. JPG/JPEG werden ignoriert.

Zeitstempel werden zuerst aus Dateinamen wie `CAM_00_20261014083005.mp4`,
`20261014_083005.mp4` oder `camera_2026-10-14_08-30-05.mp4` gelesen. Ohne
Dateizeitstempel folgen Medien-Erstellungszeit und Dateiänderungszeit.
Dateinamen werden in der Kamera-Zeitzone interpretiert. Mehrdeutige oder nicht
existierende lokale Uhrzeiten bei der Zeitumstellung werden mit Warnung ausgelassen.
Die Timeline zeigt Europe/Berlin und berücksichtigt Tage mit 23 beziehungsweise 25 Stunden.

„Nach Datum“ nutzt beispielsweise `{YYYY}/{MM}/{DD}`; `{YY}` ist ebenfalls
möglich. Der geöffnete Tag und angrenzende Partitionen für über Mitternacht
laufende Videos werden eingelesen. „Gesamter Ordner“ durchsucht rekursiv alle
Unterordner. Bestehende eigene Dateimuster und Reolink-Konfigurationen werden intern
weiter unterstützt und im Bearbeitungsformular erhalten.

Zeitversatz unterstützt Zehntelsekunden. `-5,1` verschiebt den tatsächlichen Start
um 5,1 Sekunden nach vorne. Änderungen verschieben vorhandene Indexeinträge,
ohne Videos erneut zu untersuchen. Änderungen an Quelle, Muster oder Zeitzone
verwerfen den abgeleiteten Kameraindex.

Manuelle Scans für einzelne oder alle Kameras lesen die gesamten Kameraordner und
gleichen entfernte Dateien ab. „Index neu aufbauen“ entfernt abgeleitete Aufnahmen,
Scanstände und Vorschaubilder; Kameras, Anmeldung und Originalvideos bleiben erhalten.
Die Aktion ist während laufender oder wartender Indexaufträge gesperrt. Eine Quelle
ist auf zwei Millionen Verzeichniseinträge pro Scan begrenzt; für große Archive
Datumspartitionen verwenden.

Automatische Tagesindexierung ist standardmäßig ausgeschaltet. Unter
„Benutzer · Anmeldung · Betrieb → Betrieb“ aktivieren; Vorgabe 60 Minuten,
zulässig 1–10080 Minuten. Sie läuft ohne geöffneten Browser, jeweils eine Kamera,
ausschließlich für datumspartitionierte Quellen und den heutigen Tag in deren Zeitzone.
Vergangene Tage werden nicht automatisch nachgeholt. Erfolgreiche und fehlgeschlagene
Versuche warten vor dem nächsten Lauf das eingestellte Intervall. Der nächste
Laufzeitpunkt bleibt in SQLite erhalten; ein unterbrochener Auftrag wird erneut fällig.
Interaktive Aufträge werden vor dem nächsten Hintergrundscan bearbeitet.

Der Hintergrundscan listet Tagesdateien, prüft aber nur neue oder noch nicht stabile
Dateien näher. Erst nach wiederholter Beobachtung unveränderter Größe/Änderungszeit,
gültiger Dauer und mindestens zwei Minuten Alter gilt eine Datei als stabil.
Der Hintergrundscan gleicht keine Löschungen oder Ersetzungen stabiler Dateien ab;
dafür den Tag öffnen oder manuell scannen. Neu geschriebene Dateien müssen vor dem
Probe-Lauf mindestens 30 Sekunden alt sein. Die Stabilitätserkennung ist eine Heuristik.

## Ansicht, Suche und Wiedergabe

Pro Kamera kann **Weitwinkel + Zoom** aktiviert werden. Beide MP4-Dateien liegen
im selben Kameraordner oder dessen Unterordnern. Zwei einstellbare Kennungen
(z. B. `_wide`/`_zoom`, `wide/`/`zoom/` oder nach Prüfung `_00_`/`_01_`)
identifizieren die Ansicht anhand des relativen Pfads, ohne Beachtung der
Großschreibung. „Erkennung prüfen“ zeigt die Zuordnung und Aufnahmezeit.
Dateien ohne Kennung oder mit beiden Kennungen bleiben unklar und werden keiner
Ansicht zugeordnet. Eine Kennung wie `_00_` hat keine fest vorgegebene Bedeutung.
Der Kameraordner bestimmt die Kamera; die Aufnahmezeiten synchronisieren die
Ansichten, auch bei unterschiedlichen Dateianfängen oder Clipgrenzen. Es erfolgt
keine Bildanalyse. Ein zusätzlicher Zoom-Zeitversatz korrigiert dessen Uhr in
Zehntelsekunden; negative Werte verschieben früher. Fehlt eine Ansicht, zeigt sie
eine Aufnahmelücke. Weitwinkel steht links, Zoom rechts; das Raster zählt Kamerapaare.
Doppelklick oder die Vergrößern-Schaltfläche öffnet eine Ansicht groß; Escape
stellt die Rasteransicht wieder her. Browserdecoder können geringfügig abweichen.

Kamerafilter per Ziehen umordnen. Gewählte Kameras bleiben über Seitenwechsel
auch in kleinen Rastern erreichbar. Hotspot vergrößert eine Kamera. Im Automatikmodus
gewinnt die zuletzt beginnende aktive Aufnahme; bei gleichen Startzeiten gilt die
Kamerareihenfolge. Eine manuelle Kameraauswahl beendet den Automatikmodus.

Die Pfeile neben Abspielen springen zur vorherigen/nächsten Aufnahmegrenze.
Timeline anklicken zum Springen, Mausrad zum Zoomen, Umschalt+Ziehen zum Verschieben;
Touch unterstützt Verschieben und Zweifinger-Zoom. Die Tagesübersicht verschiebt
den sichtbaren Bereich. Der Trenner darüber verändert die Timeline-Höhe, auch mit
Pfeiltasten. „Ansicht einklappen“ lässt Wiedergabesteuerung weiterhin erreichbar.
Raster, Filter, Reihenfolge, Vorladen und Timeline-Höhe werden je Benutzer im Browser gespeichert.

„Aufnahmen suchen“ durchsucht den bisherigen Index nach Dateiname oder Kameraname.
Kamera, Berliner Zeitraum und Mindestdauer begrenzen die Ergebnisse (maximal 100
über die Oberfläche). Treffer öffnen Kamera, Tag und Aufnahmezeitpunkt. Noch nicht
indexierte Tage zuerst öffnen oder einen vollständigen manuellen Scan starten.

Original liefert unterstützte MP4/MOV-Dateien unverändert mit HTTP-Range.
Bei inkompatiblen Codecs oder Containern Ausgewogen oder Schnell wählen:

| Profil | Skalierung | FPS | Maximale Bitrate |
|---|---:|---:|---:|
| Ausgewogen | 50 % | 15 | 1200 kbit/s |
| Schnell | 30 % | 8 | 450 kbit/s |

Die Profile sind administrativ einstellbar: Skalierung 20–100 %, FPS 2–30,
Bitrate 100–8000 kbit/s. Sie erhalten das Seitenverhältnis. Ab 2× wird die
beschleunigte Wiedergabe serverseitig kodiert. Desktopbrowser erhalten fragmentiertes
MP4; Browser mit nativer HLS-Unterstützung erhalten HLS. Bei 4× wartet HLS auf zwei,
bei 8×/16× auf vier Segmente oder auf Clipabschluss. Ton bleibt stumm.

Vorladen „Metadaten“ spart im pausierten Zustand Daten; „Automatisch“ erlaubt dem
Browser stärkeres Vorladen. Bei Streamfehlern gibt es höchstens drei automatische
Wiederherstellungsversuche mit einer begrenzten Wiederherstellungsdauer; betroffene
Kameras zeigen anschließend einen Fehler. Gesunde Kameras und die Timeline laufen
weiter. Pause/Abspielen oder Profilwechsel erlauben einen erneuten Versuch.

Browserdecoder können geringfügig voneinander abweichen; framegenaue forensische
Synchronität wird nicht zugesagt. Bei Aufnahmelücken springt die gemeinsame Uhr
zur nächsten Aufnahme in den sichtbaren Kameras.

## Ressourcen und Diagnose

- `CTV_MAX_TRANSCODERS=0`: unbegrenzte Zahl gleichzeitiger Encoder. UI-Bereich 0–64;
  ein kleineres Limit betrifft neue Aufträge, bestehende Encoder laufen weiter.
- `CTV_HLS_TEMP_MB=256`: gemeinsames Budget für temporäre komprimierte Streams;
  in der Oberfläche 16–4096 MiB. Ein Watchdog prüft alle 500 ms und entfernt ganze
  Sitzungen, bevorzugt ältere abgeschlossene. Kurzzeitige Überschreitungen sind möglich.
- `CTV_CACHE_MB=512`: Vorschaubild-Cachebudget. Vorschaubilder können neu erzeugt werden.
- Automatische Indexierung wird über die Weboberfläche konfiguriert;
  `CTV_SCAN_INTERVAL_SECONDS` bleibt ausschließlich zum Lesen alter Konfigurationen unterstützt.

Bei Pause werden unvollständige Streams nach zwei Sekunden freigegeben; fertige
MP4-Puffer können weiterverwendet werden. Seitenwechsel/Verbergen gibt Streams frei
und setzt die Wiedergabe nicht automatisch fort. Serverseitige Zeitlimits schützen
vor verlorenen Abbruchanfragen. Lange Clips werden in bis zu 30-minütigen Teilen abgespielt.

„Wiedergabediagnose“ startet eine begrenzte Aufzeichnung mit maximal 20.000 Einträgen.
Nach Stopp als JSON herunterladen. Der Bericht enthält Wiedergabeereignisse und
Ressourcenzähler, keine Aufnahmeordner, Passwörter, Tokens oder Client-Secrets.
Geschützte APIs: `/api/playback-status` und `/api/playback-sessions/{id}`;
Sitzungsdetails sind nur für den Eigentümer erreichbar.

## Upgrade und Rückkehr

Vor dem Umbau wurde der Arbeitsstand als lokales Archiv gesichert:
`.sync-conflicts/pre-replacement-20261014-120741.tar.gz` (bewusst nicht in Git).
Beim ersten Öffnen einer vorhandenen Datenbank vor Schema 4 erzeugt die Anwendung
`/data/viewer-before-v4.db` mit SQLite `VACUUM INTO`, einschließlich committed WAL-Daten.
Die Sicherung wird nicht überschrieben. Konten, Passwort-Hashes, OIDC-Einstellungen,
Kameras und alte Dateimuster bleiben erhalten. Alte Lesezeichendaten werden nicht gelöscht,
sind aber über die Anwendung nicht mehr zugänglich. Neue Installationen erzeugen
keine Lesezeichentabelle. Bestehende gespeicherte oder externe Ressourcenlimits gelten
weiter; bei einem Upgrade ohne explizites Encoderlimit bleibt die frühere Vorgabe 1 erhalten.

Für eine Rückkehr Container stoppen, den bisherigen Image-Tag wählen und die
gesicherte Datenbank als `viewer.db` wiederherstellen. Dabei vorhandene `viewer.db-wal`
und `viewer.db-shm` zusammen mit der aktuellen Datenbank separat sichern und entfernen,
bevor die Sicherung eingesetzt wird. Eine vollständige Datenverzeichnis-Sicherung
ist zusätzlich empfohlen. Neue Einstellungen seit dem Upgrade sind in der alten
Sicherung nicht enthalten. Originalaufnahmen werden nicht verändert.

## Entwicklung und Prüfungen

```bash
npm ci
npm run typecheck
npm test
npm run build
go vet ./...
go test -race ./...
go build -o bin/cctv ./cmd/cctv
npx playwright install chrome webkit
npm run test:browser
CTV_TEST_ENGINE=webkit npm run test:browser
python3 -m unittest discover -s scripts -p 'test_*.py'
docker build -t cctv-viewer:test .
./scripts/smoke.sh cctv-viewer:test
```

Die Browser- und Medientests verwenden ausschließlich synthetische Videos und
temporäre SQLite-Datenbanken. `CTV_TEST_BROWSER` erlaubt einen installierten
Chromium-Browser für lokale Tests. Die CI prüft Containerstart, Anmeldung,
Neustart sowie schreibgeschützte Aufnahme-Mounts und erstellt AMD64/ARM64-Images.
