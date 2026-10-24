# CCTV Viewer

Ein selbst gehosteter deutscher Aufnahmeviewer mit synchronisierten Kameras,
gemeinsamer Zeitleiste, Docker Compose und geschützter OIDC-/lokaler Anmeldung.
Die Viewer-Funktionen orientieren sich an einem festen Stand des CCTV Timeline
Viewers und sind eigenständig in Go/TypeScript umgesetzt. Unser Layout bleibt erhalten.

- Datumsauswahl, Aufnahmegrenzen, Übersichtstimeline, Zoom und verstellbare Timeline-Höhe
- Automatische und feste Raster bis 4 × 4, eigene Raster bis 8 × 8 und Kameraseiten
- Kameraauswahl und Reihenfolge per Ziehen; manueller oder automatischer Hotspot
- Original, Ausgewogen und Schnell; Wiedergabe von 0,25× bis 16×
- Tonknopf pro Ansicht; jeweils eine hörbare Quelle, auch bei komprimierter Wiedergabe
- Tastatursteuerung (Leertaste/K für Pause/Play, Pfeiltasten, J/L zum Springen, F für Vollbild) und Timeline-Hover-Uhrzeit
- Aufnahmesuche mit Kamera-, Zeitraum- und Mindestdauerfilter
- Datumspartitionierte oder rekursive Indexierung; optionale automatische Tagesindexierung
- OIDC/SSO, Rollen und lokale Administrator-Wiederherstellung
- Aufnahmequellen nur lesend; SQLite, Vorschaubilder und temporäre Streams unter `/data`

Kein NVR: Der Viewer verwendet vorhandene Videodateien. Optional bietet die
[Liveansicht](docs/LIVE.md) Reolink-Livebilder und eine Streamfreigabe für Home Assistant. Videoanalyse ist ausgeschlossen. Bilder, Clip-Export, Lesezeichen, Zeitlinks,
digitaler Zoom und separate Vollbildbedienung wurden entfernt.

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

In `.env` mindestens `CTV_BASE_URL=https://cctv.example.org` eintragen.
Aufnahmeordner, Datenordner und Host-Adressen direkt in `compose.yaml` anpassen:
`./recordings:/recordings:ro`, `./data:/data` und die beiden Port-Bindings.
Bei bestehenden Installationen die bisher verwendeten Host-Pfade übernehmen,
damit Aufnahmen und gespeicherte Daten erhalten bleiben. Für eine Vorabversion zusätzlich
`CTV_VERSION=0.1.0-beta.1` setzen, da Vorabversionen kein `latest` erhalten.
Die `.env` liefert die Ausgangswerte. Über die Verwaltung gespeicherte
Servereinstellungen haben dauerhaft Vorrang; über „Herkunft und Umgebungswerte“
lassen sie sich einzeln zurücksetzen. Für diese GUI-Einstellungen muss die Compose-Datei nicht bearbeitet werden.
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

OIDC bevorzugt unter **Verwaltung → Anmeldung / OIDC** einrichten.
Optionales `.env`-Beispiel (Discovery-Issuer des jeweiligen Anbieters verwenden):

```dotenv
CTV_OIDC_ISSUER=https://auth.example.org/application/o/cctv/
CTV_OIDC_CLIENT_ID=cctv-viewer
CTV_OIDC_CLIENT_SECRET=<client-secret>
CTV_OIDC_GROUP_CLAIM=groups
CTV_OIDC_ALLOWED_GROUPS=
CTV_OIDC_ADMIN_GROUPS=cctv-admin
CTV_OIDC_ALLOWED_SUBJECTS=
CTV_OIDC_ADMIN_SUBJECTS=
```

Das Client-Secret kann in der GUI gespeichert oder optional über `.env` gesetzt
werden. Die Datei ist in Git ausgeschlossen und sollte nur für dich lesbar sein
(`chmod 600 .env`).
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
`"groups": ["cctv-admin"]` enthält. Anmeldungseinstellungen können alternativ unter **Verwaltung → Anmeldung / OIDC**
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

Die linke Navigation bietet unter **Verwaltung** eigene Seiten für Kameras,
Benutzer, Sitzungen, Anmeldung/OIDC und Betrieb. **Einstellungen** enthält
Darstellung, Wiedergabe, das eigene Konto und eigene Sitzungen. Auf schmalen
Bildschirmen öffnet **Menü** die Navigation. Lokale Konten haben die Rolle
„Administrator“ oder „Betrachter“. Bestehende Administratorkonten und Passwort-Hashes
werden übernommen. Benutzer können gesperrt, gelöscht und ihre Passwörter
zurückgesetzt werden. Passwort-, Rollen- und Sperränderungen widerrufen die
zugehörigen Sitzungen. Der letzte aktive lokale Administrator bleibt geschützt.
Eigene lokale Passwörter unter **Einstellungen → Mein Konto** ändern;
SSO-Passwörter beim Provider. Unter **Meine Sitzungen** lassen sich eigene aktive
Anmeldungen einzeln oder gemeinsam beenden; Administratoren können unter
**Verwaltung → Sitzungen** zusätzlich fremde Sitzungen beenden.

Suche und Einstellungen sind feste Seiten mit Direktlinks und Browser-Zurück/
Vorwärts. Beim Verlassen der Aufnahmen pausieren Bild und Ton; Datum, Kameras
und Abspielposition bleiben erhalten. Die Rückkehr startet keine Wiedergabe.
Ungespeicherte Formularänderungen werden beim Seitenwechsel geschützt.
Eine Übersicht der GUI- und Umgebungswerte sowie die Schritte für bestehende
Installationen stehen unter [Konfiguration](docs/CONFIGURATION.md).

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

Manuelle Scans für einzelne oder alle Kameras lesen die gesamten Kameraordner.
Fehlende Dateien bleiben im Index, damit eine ausgefallene Netzwerkquelle keine
Aufnahmen aus dem Archiv entfernt. In der Kameraverwaltung zeigt „Bereinigen“ die
Zahl fehlender Einträge und entfernt sie erst nach Bestätigung. Vorher den Mount
prüfen: Ein leerer, aber lesbarer Ordner kann auch auf einen NAS-Ausfall hinweisen.
„Index neu aufbauen“ entfernt abgeleitete Aufnahmen,
Scanstände und Vorschaubilder; Kameras, Anmeldung und Originalvideos bleiben erhalten.
Die Aktion ist während laufender oder wartender Indexaufträge gesperrt. Eine Quelle
ist auf zwei Millionen Verzeichniseinträge pro Scan begrenzt; für große Archive
Datumspartitionen verwenden.

Wartende und unterbrochene Scanaufträge werden nach einem Neustart erneut
ausgeführt. Bei einem fehlgeschlagenen automatischen Scan folgt nach fünf Minuten
ein neuer Versuch. Die letzten Scanergebnisse und Warnungen stehen in der
Kameraverwaltung; fehlgeschlagene manuelle Scans lassen sich dort erneut starten.

Automatische Tagesindexierung ist standardmäßig ausgeschaltet. Unter
„Verwaltung → Betrieb“ aktivieren; Vorgabe 60 Minuten,
zulässig 1–10080 Minuten. Sie läuft ohne geöffneten Browser, jeweils eine Kamera,
für datumspartitionierte und rekursive Quellen und den heutigen Tag in deren Zeitzone.
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

Die Wiedergabediagnose öffnet sich über **Einstellungen → Wiedergabe → Diagnose**.
Eine gestartete Aufzeichnung läuft beim Schließen des Diagnosedialogs weiter;
zum Beenden und Herunterladen den Dialog erneut öffnen.

Für zwei Objektive im selben Aufnahmeordner muss pro Kamera **Weitwinkel + Zoom**
aktiviert sein, auch wenn zunächst nur Weitwinkel angezeigt werden soll. Bei **Eine Ansicht**
werden alle passenden Dateien gemeinsam abgespielt, einschließlich früher beginnender
Zoom-Dateien. Beide MP4-Dateien liegen im selben Kameraordner oder dessen Unterordnern. Zwei einstellbare Kennungen
(z. B. `_wide`/`_zoom`, `wide/`/`zoom/` oder nach Prüfung `_00_`/`_01_`)
identifizieren die Ansicht anhand des relativen Pfads, ohne Beachtung der
Großschreibung. Beim Einrichten oder Bearbeiten vergleicht der Viewer bis zu
drei zeitlich passende Bildpaare. Wenn der größere Bildausschnitt eindeutig
erkennbar ist, korrigiert er vertauschte Kennungen automatisch. Bei einem
unklaren Vergleich bleiben die Kennungen unverändert und der Grund wird
angezeigt. „Erkennung prüfen“ zeigt Beispiele beider Ansichten, unklare
Dateien und die Aufnahmezeit.
Dateien ohne Kennung oder mit beiden Kennungen bleiben unklar und werden keiner
Ansicht zugeordnet. Eine Kennung wie `_00_` hat keine fest vorgegebene Bedeutung;
vor dem Speichern je eine Datei beider Perspektiven mit „Erkennung prüfen“ kontrollieren.
Unklare Aufnahmen bleiben im Index und in der Erkennungsprüfung erhalten, füllen
aber keine Wiedergabelücken und erscheinen nicht als Vorschaubild oder Sprungziel.
Der Kameraordner bestimmt die Kamera; die Aufnahmezeiten synchronisieren die
Ansichten, auch bei unterschiedlichen Dateianfängen oder Clipgrenzen. Für die
Synchronisierung erfolgt keine Bildanalyse. Unterschiedliche Dateizeitstempel verhindern die gemeinsame Ansicht nicht:
`Reolink Garten_00_20261015061313.mp4` (Weitwinkel) und
`Reolink Garten_01_20261015061312.mp4` (Zoom) gehören bei passenden Kennungen
zu derselben Kamera. Um 06:13:13 läuft Zoom bereits eine Sekunde.
Ein zusätzlicher Zoom-Zeitversatz korrigiert dessen Uhr in
Zehntelsekunden; negative Werte verschieben früher. Zunächst erscheint nur Weitwinkel.
Der Schalter „Zoom anzeigen“ öffnet die zweite Ansicht; „Zoom schließen“
gibt deren Wiedergabe wieder frei. Nach einem Neuladen ist Zoom geschlossen.
Fehlt Weitwinkel, bleibt seine Kachel als Aufnahmelücke sichtbar; Zoom ersetzt sie
nicht automatisch. Bei geöffnetem Zoom steht Weitwinkel links und Zoom rechts;
unter 720 Pixel Kamerabreite stehen beide untereinander. Das Raster zählt Kameras.
Eine fehlende oder fehlerhafte Ansicht stoppt die andere Ansicht nicht.
Doppelklick oder die Vergrößern-Schaltfläche öffnet eine Ansicht groß; Escape
stellt die Rasteransicht wieder her. Browserdecoder können geringfügig abweichen.

Jede Ansicht hat einen Tonknopf. Anfangs sind alle Ansichten stumm; beim Einschalten
wird die bisherige Tonquelle stummgeschaltet. Die Auswahl bleibt bei Clip-, Qualitäts-
und Positionswechseln erhalten, bis die Ansicht ausgeblendet wird. Videos ohne
Audiospur bleiben abspielbar. Ausgewogen und Schnell wandeln die erste vorhandene
Audiospur nach AAC um; bei Geschwindigkeitswechseln bleibt die Tonhöhe erhalten.
Im Original hängt die Audiounterstützung vom Browser und Quellcodec ab.

Kamerafilter per Ziehen umordnen. Gewählte Kameras bleiben über Seitenwechsel
auch in kleinen Rastern erreichbar. Hotspot vergrößert eine Kamera. Im Automatikmodus
gewinnt die zuletzt beginnende aktive Aufnahme; bei gleichen Startzeiten gilt die
Kamerareihenfolge. Eine manuelle Kameraauswahl beendet den Automatikmodus.

Die Pfeile neben Abspielen springen zur vorherigen/nächsten Aufnahmegrenze.
Timeline anklicken zum Springen, Mausrad zum Zoomen, Umschalt+Ziehen zum Verschieben;
Touch unterstützt Verschieben und Zweifinger-Zoom. Die Tagesübersicht verschiebt
den sichtbaren Bereich. Der Trenner darüber verändert die Timeline-Höhe, auch mit
Pfeiltasten. Die Wiedergabesteuerung über den Kamerabildern bleibt beim Scrollen erreichbar.
Die Untermenüs unter „Einstellungen“ öffnen die persönlichen Einstellungen. Die Kameraauswahl bietet
„Alle auswählen“ und „Auswahl aufheben“; Seitenangaben zeigen die sichtbaren Kameras.

Unter „Einstellungen“ können alle Benutzer Farbschema, Oberflächendichte, Raster,
Seitenverhältnis, Vorladen und Hotspot wählen. Kameranamen, Zeitstempel, Codec,
Tagesübersicht und Vorschaubilder lassen sich einzeln ein- und ausblenden.
Ausgeblendete Vorschaubilder werden nicht geladen. Die Timeline-Höhe ist zwischen
100 und 650 Pixeln einstellbar. Tastenkürzel lassen sich abschalten; Pfeiltasten
springen wahlweise 5, 10 oder 30 Sekunden, mit Umschalt immer 60 Sekunden.
Eine Tastenübersicht steht auf der Einstellungsseite. Eingabefelder behalten ihre
normale Tastaturbedienung.

Änderungen auf der Einstellungsseite werden mit „Speichern“ übernommen. Kameraauswahl,
Reihenfolge und Wiedergabeoptionen werden direkt gespeichert. Persönliche Einstellungen
einschließlich Kameraauswahl und Reihenfolge sowie das Farbschema werden dauerhaft je Benutzerkonto auf dem Server
gespeichert. Beim Anmelden oder Neuladen stehen sie auch auf anderen Geräten zur
Verfügung; bereits offene Geräte behalten ihre Darstellung. Änderungen verschiedener
Felder werden zusammengeführt, beim selben Feld gilt die zuletzt gespeicherte Änderung.
Bestehende Browserpräferenzen werden einmalig übernommen, sofern das Konto noch keine
Servereinstellungen hat. Der Browser behält eine lokale Kopie; Speicherfehler werden
angezeigt und können auf der Einstellungsseite erneut versucht werden. Vor der Anmeldung
gilt das lokale Farbschema. Geöffnete Zoom-Ansichten, Ton, Vollbild, Datum und Abspielposition
bleiben temporär; nach Neuladen ist Zoom geschlossen. Die persönlichen Einstellungen
liegen mit den übrigen Anwendungsdaten in der SQLite-Datenbank; die zusätzliche Tabelle
wird beim Start automatisch angelegt. Zurücksetzen
erfordert eine Bestätigung und stellt auch die Auswahl aller Kameras wieder her.
Administratoren erreichen Kamera- und Benutzerverwaltung separat über „Verwaltung“.

„Aufnahmen suchen“ durchsucht den bisherigen Index nach Dateiname oder Kameraname.
Kamera, Berliner Zeitraum und Mindestdauer begrenzen die Ergebnisse. Die Suche zeigt
100 Treffer pro Seite; „Weitere Treffer laden“ lädt die nächste Seite. Treffer öffnen Kamera, Tag und Aufnahmezeitpunkt. Noch nicht
indexierte Tage zuerst öffnen oder einen vollständigen manuellen Scan starten.

Original ist die Standardauswahl für Weitwinkel und Zoom. Unterstützte MP4/MOV-Dateien
werden unverändert mit HTTP-Range geliefert. Auflösung,
Bildrate und Bitrate entsprechen dabei der Datei auf dem NAS. Bestehende persönliche
Qualitätseinstellungen werden einmalig auf Original umgestellt; spätere Änderungen
bleiben gespeichert. Bei nicht unterstützten Quellformaten muss ein reduziertes
Profil ausdrücklich ausgewählt werden.
Bei inkompatiblen Codecs oder Containern Ausgewogen oder Schnell wählen:

| Profil | Skalierung | FPS | Maximale Bitrate |
|---|---:|---:|---:|
| Ausgewogen | 50 % | 15 | 1200 kbit/s |
| Schnell | 30 % | 8 | 450 kbit/s |

Die Profile sind administrativ einstellbar: Skalierung 20–100 %, FPS 2–30,
Bitrate 100–8000 kbit/s. Sie erhalten das Seitenverhältnis. Ab 2× wird die
beschleunigte Wiedergabe serverseitig kodiert. Desktopbrowser erhalten fragmentiertes
MP4; Browser mit nativer HLS-Unterstützung erhalten HLS. Bei 4× wartet HLS auf zwei,
bei 8×/16× auf vier Segmente oder auf Clipabschluss. Eine vorhandene Audiospur
wird als AAC übernommen und mit unveränderter Tonhöhe an die Geschwindigkeit angepasst.

Vorladen „Metadaten“ spart im pausierten Zustand Daten; „Automatisch“ erlaubt dem
Browser stärkeres Vorladen. Bei Streamfehlern gibt es höchstens drei automatische
Wiederherstellungsversuche mit einer begrenzten Wiederherstellungsdauer; betroffene
Kameras zeigen anschließend einen Fehler. Gesunde Kameras und die Timeline laufen
weiter. „Erneut versuchen“ im Kamerabild, Pause/Abspielen oder Profilwechsel erlauben einen erneuten Versuch.

Browserdecoder können geringfügig voneinander abweichen; framegenaue forensische
Synchronität wird nicht zugesagt. Bei Aufnahmelücken springt die gemeinsame Uhr
zur nächsten Aufnahme in den sichtbaren Kameras.

## Ressourcen und Diagnose

- `CTV_MAX_TRANSCODERS=0`: unbegrenzte Zahl gleichzeitiger Encoder. UI-Bereich 0–64;
  ein kleineres Limit betrifft neue Aufträge, bestehende Encoder laufen weiter.
  Bei komprimierter Wiedergabe benötigt ein Weitwinkel-/Zoom-Paar bis zu zwei
  Encoderplätze. Originalwiedergabe benötigt keine Encoderplätze.
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

## Commit-Images und Releases

Der vollständige Ablauf steht in [CI/CD und manuelle Releases](docs/CI-CD.md).
PRs gegen `main` führen Code- und Browser-Prüfungen aus. Nach dem manuellen Merge
prüft die CI den tatsächlichen Main-Commit erneut und veröffentlicht nach nativen
AMD64-/ARM64-Smoke-Tests ausschließlich `ghcr.io/cyberhunter88/cctv-viewer-builds:sha-<Commit-SHA>`.
Der geschützte Statuscheck bleibt „Anwendung prüfen“.

Geprüfte Main-Builds und interne Tags liegen im separaten GHCR-Paket `cctv-viewer-builds`. Das Installationspaket `cctv-viewer` enthält neue Veröffentlichungen mit Versions-Tags und stabilen Aliases; die Compose-Adresse bleibt unverändert. Historische interne Einträge lassen sich mit dem manuellen Vorschau-/Bereinigungsworkflow gezielt prüfen. Details stehen in [docs/CI-CD.md](docs/CI-CD.md).

Offizielle Releases und Docker-Versionstags entstehen ausschließlich über
**Actions → Release und Docker veröffentlichen → Run workflow** auf Branch `main`.
Pflichtfeld `version` enthält zum Beispiel `v0.6.0`, das optionale Feld `commit`
einen vollständigen Main-SHA; leer bedeutet den aktuellen HEAD von `main`.
Die Version muss bereits im getesteten Commit in `package.json`, Lockdatei und
Image übereinstimmen. Der Workflow verändert `main` nicht und baut kein Image neu.
Ein grüner PR-Lauf genügt nicht: Die vollständige Push-CI des exakten Commits,
beide Architektur-Digests und deren Buildnachweis werden geprüft.

Ältere Releases und Vorabversionen verschieben `latest` nicht. Wiederholungen
mit derselben Version und demselben SHA verwenden korrekte Artefakte weiter;
widersprüchliche Tags werden abgelehnt. Release Notes werden automatisch generiert;
Compose, Beispielkonfiguration und `release-proof.json` werden als Assets angehängt.
Ein in der GitHub-Releases-Oberfläche manuell angelegtes Release löst keine
Docker-Promotion aus. Die Paket-Sichtbarkeit für anonyme GHCR-Pulls muss einmalig
auf **Public** gestellt werden. Laufende Server werden nicht automatisch aktualisiert.

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

## Farbschema und Darstellung mit mehreren Kameras

Über „Einstellungen → Darstellung“ stehen System, Hell und Dunkel zur Verfügung;
auch die Anmeldung bietet eine Farbschema-Auswahl. Die Auswahl wird
lokal gespeichert. Die linke Seitenleiste lässt sich ein- und ausklappen. Das
Kontomenü mit Passwortänderung und Abmelden befindet sich unten in der Seitenleiste;
auf Mobilgeräten ist es in der horizontalen Navigation erreichbar. Der Vollbildknopf,
ein Doppelklick auf die Aufnahme oder die Taste F öffnen echtes Browser-Vollbild;
Escape beendet es. Falls der Browser dies nicht unterstützt, wird die Aufnahme
innerhalb der Seite vergrößert. Neue Ansichten verwenden Originalwiedergabe und das natürliche
Seitenverhältnis; gespeicherte Einstellungen bleiben erhalten. Originalvideos werden
ohne zusätzliche Kompression übertragen und vollständig in die Kachel eingepasst.

Kameraraster passen sich der verfügbaren Breite an. Auf schmalen Displays stehen
Weitwinkel und Zoom untereinander; weitere Kameras sind über Kameraseiten erreichbar.
Unter „Indexierung · Details“ erscheinen Dateizähler und Status pro Kamera sowie die
Warteschlange. Automatische Scans berücksichtigen auch rekursive Quellen und bei
Tagesordnern die beiden vorherigen lokalen Tage.

Funktionsumfang und technische Prüfgrenzen: [Funktionsabgleich](FEATURE_MATRIX.md).

## Liveansicht und Home Assistant

Ab 0.10.0: Livekameras in der Kameraverwaltung einrichten und unter **Liveansicht**
unabhängig von den Aufnahmen auswählen. Einrichtung, geringe Verzögerung und
optionale Heimnetz-Streamfreigabe: [docs/LIVE.md](docs/LIVE.md).
