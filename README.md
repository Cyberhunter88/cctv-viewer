# CCTV Viewer

Ein schlanker, selbst gehosteter **deutscher Aufnahmeviewer**: mehrere Kameras,
eine gemeinsame Zeitleiste, geschützte Anmeldung und ein Docker-Container.

- Synchronisierte Kameraansichten, Aufnahmekalender, Timeline-Zoom und Vorschaubilder
- Persönliche Lesezeichen, geschützte Zeitlinks, Vollbild und digitaler Zoom
- Exportauswahl direkt auf der Timeline und Touch-Navigation
- Originalwiedergabe mit HTTP-Range und H.265 → H.264-HLS bei Bedarf
- Reolink-Assistent mit Ordnerauswahl und JPG-Einzelbildern auf der Zeitleiste
- MP4-Clip-Export über Dateigrenzen hinweg
- Benutzer-, OIDC- und Betriebseinstellungen in der Weboberfläche
- Aufnahmen ausschließlich lesend; SQLite und Cache in einem Datenverzeichnis
- AMD64 und ARM64; Images automatisch beim Veröffentlichen eines GitHub-Releases

Kein NVR: Der Viewer zeichnet keine Livekameras auf. Er liest bereits vorhandene
Videodateien und JPG-Einzelbilder. KI-Analyse und Home Assistant sind nicht Bestandteil.

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

Betrachter können alle eingerichteten Kameras und Bilder sehen sowie Clips
exportieren; Administratoren zusätzlich Kameras, lokale Benutzer und Einstellungen
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

Scanintervall, Cachebudget, Umwandlungsplätze und Sitzungsdauer werden ohne
Neustart übernommen. Bei weniger Umwandlungsplätzen dürfen laufende Aufträge
fertig werden. Docker-Mounts, Port, Basis-URL und vertrauenswürdige Proxys bleiben
extern konfiguriert und werden nur angezeigt. Gleichzeitig bearbeitete
Einstellungen werden mit Versionskonflikt abgewiesen statt überschrieben.

Client-Secrets werden nie angezeigt oder über die API zurückgeliefert. Die
SQLite-Datenbank enthält jedoch gespeicherte Secrets: Datenordner und Sicherungen
entsprechend schützen. Vor dem ersten Upgrade den Datenordner sichern; neue
Datenbankspalten und die Einstellungstabelle werden automatisch ergänzt.

## Kamera und Zeitmuster

Als Administrator die Kameraeinstellungen öffnen und **Ordner auswählen** nutzen.
Der Assistent zeigt ausschließlich Ordner innerhalb des eingebundenen
Aufnahmeverzeichnisses. Für dein Reolink-Archiv:

| Einstellung | Wert |
|---|---|
| Kameraordner | `Reolink/Eingang` |
| Aufnahmeformat | Reolink |
| Zeitzone | `Europe/Berlin` |

`Reolink/Eingang/2026/10/14/Eingang_00_20261014061217.mp4` wird als Video ab
14.10.2026, 06:12:17 Berliner Ortszeit erkannt. `.jpg` und `.jpeg` werden als
punktuelle Bildereignisse erkannt. Andere numerische Kennungen wie `_01_` sind
zulässig; Ordner- und Dateidatum müssen übereinstimmen. Der Teil des Dateinamens
vor dieser Kennung darf beliebig sein: Die Zuordnung erfolgt über den Kameraordner,
ein Kamerapräfix muss nicht eingestellt werden. Bereits gespeicherte Präfixe werden
ignoriert. Dateien ohne diesen Zeitstempel werden ausgelassen. Der Assistent schlägt das
Reolink-Format aus einer Beispieldatei vor; **Erkennung prüfen** zeigt Zeiten und Dateifehler.
Die begrenzte Suche kann bei großen Archiven einen spezifischeren Ordner benötigen.

JPGs auf der Zeitleiste oder unter **Einzelbilder** anklicken, um sie zu öffnen und
herunterzuladen. Gleichnamige Bilder werden bevorzugt als MP4-Vorschau verwendet.
Einzelbilder haben keine Videodauer und füllen keine Wiedergabe- oder Exportlücken.
Beschädigte Bilder werden mit Warnung ausgelassen; Bildgröße maximal 40 Megapixel.

Für andere Archive weiterhin **Eigenes Dateimuster** wählen, beispielsweise:

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
Aufnahmen dürfen bis zu 48 Stunden dauern; die beiden Vortage werden wegen über Mitternacht
laufender Aufnahmen ebenfalls berücksichtigt. Konfigurationsänderungen an Pfad,
Muster oder Zeitversatz verwerfen nur den abgeleiteten Index der Kamera.

## Wiedergabe und Ressourcen

Die gemeinsame Uhr steuert alle sichtbaren Kameras. Lücken werden je Kamera
angezeigt; ohne Aufnahme in allen sichtbaren Kameras springt die Wiedergabe zur
nächsten Aufnahme. Bei überlappenden Clips wird der zuletzt beginnende verwendet.
Browserdecoder können geringfügig voneinander abweichen; Abweichungen über etwa
650 ms werden korrigiert. Es wird keine framegenaue forensische Synchronität zugesagt.

„Automatisch“ bevorzugt H.264 in MP4/MOV sowie browserfähiges H.265. Bei eindeutigen Browser-
Codecfehlern wird automatisch auf kompatible Wiedergabe gewechselt. Bei weiteren
Problemen „Kompatibel“ auswählen. MKV/AVI/TS und inkompatible Codecs benötigen
HLS-Konvertierung. Rohes H.264/H.265 ohne ermittelbare Dauer wird ausgelassen.
Ton lässt sich je Kamera einschalten und in der Lautstärke regeln; jeweils eine
Kamera ist hörbar. Beim Öffnen der Oberfläche bleibt der Ton zunächst stumm. Unterstützte Videoerweiterungen: MP4, MOV, MKV, AVI, TS, H264, H265.
Zusätzlich JPG/JPEG als Einzelbilder. Bei eigenen Videomustern wird für Bilder
dasselbe Zeitmuster mit JPG-/JPEG-Endung verwendet.

- `CTV_MAX_TRANSCODERS=1`: gleichzeitige FFmpeg-Videoumwandlungen; zulässig 1–16.
- `CTV_SCAN_INTERVAL_SECONDS=300`: Hintergrundscan des aktuellen Tages, 30–86400 Sekunden.
- `CTV_CACHE_MB=512`: gemeinsames Budget für Thumbnails und temporäre HLS-Dateien.
- HLS: maximal 1280 Pixel breit, ca. 2 Mbit/s, ein Encoderthread, AAC-Ton, sofern vorhanden.
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

## Aufnahmen finden und prüfen

**Aufnahmekalender** zeigt den gewählten Monat für die ausgewählten Kameras:
`●` bedeutet vorhandene Videos oder JPGs, `✓` einen vollständig geprüften leeren
Tag und `?` einen noch ungeprüften oder unvollständig lesbaren Tag. Bereits
indexierte Aufnahmen können auch an einem mit `?` markierten Tag vorhanden sein.
Solange der Kalender geöffnet ist, wird jeweils ein weiterer Tag asynchron
geprüft. Es bleibt bei einem Scan gleichzeitig und den bestehenden Grenzen pro
Partition. Versuche werden eine Stunde zwischengespeichert; fehlgeschlagene Tage
bleiben als unvollständig markiert. Das Schließen beendet weitere Anfragen,
ein bereits gestarteter Scan darf fertig werden.
**Belegter Tag** springt zur vorherigen/nächsten Aufnahme im bisherigen Index;
noch nicht geprüfte Monate zuerst im Kalender öffnen.

**Lesezeichen** speichert einen Zeitpunkt, die Kameraauswahl und eine kurze Notiz.
Markierungen sind persönlich, über Sitzungen hinweg verfügbar und werden nicht
mit dem Aufnahmeindex gelöscht. Lokale Konten werden über ihren Benutzernamen,
OIDC-Konten über Issuer und Subject zugeordnet. Andere Benutzer, auch
Administratoren, können diese Lesezeichen nicht über die API lesen oder ändern.
Beim Löschen eines lokalen Kontos werden dessen Lesezeichen entfernt. Maximal
1000 Markierungen mit jeweils 1000 UTF-8-Bytes Notiz; fehlende Aufnahmen oder
entfernte Kameras werden beim Öffnen angezeigt. Markierungen schützen keine
Originaldateien vor externem Löschen.

**Zeitlink kopieren** erzeugt einen Link mit Datum, Zeitpunkt und Kameraauswahl.
Er verlangt dieselbe Anmeldung wie der Viewer und gewährt keine zusätzlichen
Rechte. Die Ansicht startet pausiert. Kameraauswahl, Raster, Geschwindigkeit und
Qualitätsmodus werden pro Identität lokal im Browser gespeichert; gültige
Linkparameter haben Vorrang. Browser-Speicher kann deaktiviert sein, dann entfällt
das Merken. Nach einer OIDC-Anmeldung wird ein vorgemerkter Zeitlink im selben
Browser-Tab wiederhergestellt.

Jede Kamera bietet **Vergrößern**, Zoomtasten und **Zoom zurücksetzen**. Das
Vollbild funktioniert auch als bildschirmfüllende Ansicht ohne Fullscreen-API.
Mit Mausrad oder zwei Fingern zoomen, das vergrößerte Bild mit Maus/Finger
verschieben. Tastatur: `+`/`-` zoomen, Pfeiltasten verschieben, `0` setzt den Zoom
zurück und Escape schließt die vergrößerte Ansicht. Die Anzeigevergrößerung
verändert weder Originaldateien noch Exportauflösung.

Auf der Timeline springt ein Fingertipp zur Uhrzeit; Ziehen verschiebt den
sichtbaren Zeitbereich, zwei Finger zoomen. **Exportbereich auswählen** aktiviert
einen eigenen Modus: A/B-Griffe ziehen oder fokussieren und mit Pfeiltasten um
jeweils eine Sekunde verschieben, mit Umschalt um zehn Sekunden. **Auswahl
exportieren** übernimmt den Bereich in den Exportdialog. Die Auswahl ist auf
30 Minuten begrenzt; für Zeiträume über Mitternacht die Datumseingaben im Dialog
nutzen. Escape oder **Auswahl beenden** kehrt zur Navigation zurück.

**Kameraübersicht** zeigt Erreichbarkeit, letzte indexierte Aufnahme und letzten
erfolgreichen Scan. Unvollständige/alte Scanstände werden markiert. Ein
nicht erreichbarer Ordner behält seinen Index. Ein leerer, aber erreichbarer
NAS-Mount bleibt weiterhin nicht von einem tatsächlich leeren Archiv unterscheidbar.

Im Wiedergabemodus **Automatisch** führt ein eindeutiger Decoder-/Formatfehler
einmal je Versuch zur kompatiblen HLS-Wiedergabe, mit Hinweis und erhaltenem
Zeitpunkt beziehungsweise Wiedergabestatus. Netzwerkfehler starten keine
Konvertierung; ausgelastete Umwandlungsplätze werden als Fehler angezeigt.
**Original** erzwingt weiterhin die Originalwiedergabe. Keine Ereignisfilter oder
zusätzliche KI-Analyse.

## Clips exportieren

**Clip exportieren** öffnen, eine Kamera und Start/Ende in `Europe/Berlin` wählen.
Die Wiedergabeposition wird als Start vorgeschlagen. **Zeitraum prüfen** zeigt
Videolücken; **Export starten** erzeugt asynchron eine MP4-Datei mit Fortschritt,
Abbruch und Download. Administratoren und Betrachter dürfen exportieren.
Noch nicht indexierte Tage des Zeitraums werden automatisch eingelesen; der
Dialog zeigt die Vorbereitung und setzt danach die Prüfung oder Erstellung fort.
Bei nicht erreichbaren Quellen wird kein vermeintlich lückenloser Export zugesagt.

- Maximal 30 Minuten, über Dateigrenzen und Mitternacht hinweg; JPGs füllen keine Lücken.
- H.264 mit vorhandener Audiospur als AAC (48 kHz, Stereo), erneute Komprimierung mit 30 Bildern pro Sekunde. Auflösung des
  ersten Abschnitts; abweichende Auflösungen werden mit Randflächen angepasst.
  Ungerade Bildabmessungen werden für H.264 auf gerade Pixelzahlen aufgerundet.
- Bei überlappenden Videos gilt dieselbe Auswahl wie bei der Wiedergabe.
- Bei gemischten Dateien mit und ohne Ton bleibt der Ton an den richtigen
  Zeitpunkten: Abschnitte ohne Audiospur werden mit Stille ergänzt. Enthält der
  gesamte Zeitraum keinen Ton, bleibt der Export ohne Audiospur.
- Umwandlungsplätze und Cachebudget werden mit der Wiedergabe geteilt. Bei
  Auslastung Wiedergabe pausieren und erneut versuchen. Sehr viele Dateigrenzen
  erfordern einen kürzeren Exportzeitraum.
- Exporte gehören zur aktuellen Sitzung. Abmelden oder Sitzungswiderruf beendet
  den Zugriff; ein erneutes Login übernimmt die alten Aufträge nicht.
- Temporäre Exporte verfallen eine Stunde nach Auftragserstellung und werden bei
  Neustart entfernt. Unter Speicherdruck können sie früher entfernt werden.
  Aktive Downloads werden vor Bereinigung geschützt. Für eine harte Speichergrenze
  weiterhin Dateisystem-/Volume-Quota verwenden.
- Mehrdeutige oder nicht existierende lokale Exportzeiten bei einer Zeitumstellung
  werden abgewiesen. Es ist kein framegenauer forensischer Export zugesagt.

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
npx playwright install chrome
# Optional vorhandenen Chromium-Browser verwenden:
# CTV_TEST_BROWSER=/pfad/zum/browser npm run test:browser
npm run test:browser
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
