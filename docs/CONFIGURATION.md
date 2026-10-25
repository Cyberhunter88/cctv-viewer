# Einstellungen über die Weboberfläche

Nach der einmaligen Erstellung des ersten Administrators per CLI werden Konten
und Servereinstellungen links unter **Verwaltung** gepflegt. Es ist keine
Bearbeitung der `.env` für weitere Benutzer erforderlich.

| Bereich | Einstellungen |
| --- | --- |
| Einstellungen → Darstellung | Farbschema und Oberflächendichte des eigenen Kontos |
| Einstellungen → Wiedergabe | Kamerabilder, Raster, Vorladen, Zeitleiste, Tastenkürzel und Diagnose |
| Einstellungen → Mein Konto / Meine Sitzungen | Eigenes lokales Passwort ändern; eigene aktive Sitzungen einzeln oder gemeinsam beenden |
| Verwaltung → Kameras | Reiter Allgemein, Aufnahmen und Live; Aufnahmeordner, Dateizuordnung, Livequellen und Vorladen |
| Verwaltung → Streamfreigabe | Globale RTSP-Freigabe, Home-Assistant-Zugangsdaten und kopierbare Server-URL |
| Verwaltung → Benutzer | Lokale Konten anlegen, suchen, bearbeiten, sperren, entsperren und löschen; Passwörter setzen |
| Verwaltung → Sitzungen | Aktive lokale und SSO-Sitzungen aller Konten anzeigen und einzeln beenden |
| Verwaltung → Anmeldung / OIDC | Lokale Anmeldung, Issuer, Client-ID, Client-Secret, Gruppen-Claim sowie Gruppen-/Subject-Freigaben |
| Verwaltung → Betrieb | Automatische Indexierung und Intervall, Cache, Umwandlungen, temporärer Videospeicher, Streamingprofile und Sitzungsdauer |

Administratoren und Betrachter behalten die bisherigen Rechte. SSO-Konten,
Gruppenzugehörigkeiten und SSO-Passwörter werden beim Identity Provider verwaltet.
Das Beenden einer Viewer-Sitzung beendet keine globale Sitzung beim Anbieter;
eine erneute Anmeldung bleibt möglich. Geräte und IP-Adressen werden nicht erfasst.

## Was extern bleibt

Die `.env.example` enthält nur die Grundeinstellungen für Docker Compose:

| Werte | Zweck |
| --- | --- |
| `CTV_BASE_URL` | Öffentliche URL für Cookies, Origin-Prüfung und OIDC-Callback |
| `compose.yaml`: `volumes` | Aufnahmeordner und Datenordner direkt eintragen; Vorgaben `./recordings` und `./data` |
| `compose.yaml`: `ports` | Host-Adressen und Ports direkt eintragen; standardmäßig nur `127.0.0.1` |
| `CTV_UID`, `CTV_GID` | Container-Benutzer und Dateizugriff |
| `CTV_VERSION` | Ausgewähltes Container-Image |
| `CTV_TRUSTED_PROXIES` | Vertrauenswürdige Proxy-Adressen für Client-IP-Ermittlung |

Diese Werte werden durch die GUI nicht verändert. Bei Betrieb ohne Compose
bleiben entsprechend `CTV_ADDRESS`, `CTV_RECORDINGS_DIR` und `CTV_DATA_DIR`
extern. Werkzeuge, Dateirechte und Mounts richten sich weiterhin nach der Installation.

## Optionale Ausgangswerte und bestehende Installationen

Bestehende Umgebungsvariablen bleiben unterstützt. Explizit gespeicherte GUI-Werte
haben Vorrang, liegen im persistenten Datenverzeichnis und überstehen Neustarts.
Unter **Herkunft und Umgebungswerte** kann ein Wert wieder auf die Umgebung
zurückgesetzt werden. Ohne gesetzte Variable gilt dann der Programmstandard.

| Optionale Variable | Entsprechung in der Verwaltung |
| --- | --- |
| `CTV_LOCAL_ADMIN` | Lokale Anmeldung aktiv |
| `CTV_SESSION_MINUTES` | Sitzungsdauer für neue Anmeldungen |
| `CTV_OIDC_ISSUER`, `CTV_OIDC_CLIENT_ID`, `CTV_OIDC_CLIENT_SECRET` | OIDC-Anbieter und Client |
| `CTV_OIDC_GROUP_CLAIM` | Gruppen-Claim |
| `CTV_OIDC_ALLOWED_GROUPS`, `CTV_OIDC_ADMIN_GROUPS` | Exakte Gruppenfreigaben und Administratorgruppen |
| `CTV_OIDC_ALLOWED_SUBJECTS`, `CTV_OIDC_ADMIN_SUBJECTS` | Exakte Subject-Freigaben und Administrator-Subjects |
| `CTV_MAX_TRANSCODERS`, `CTV_HLS_TEMP_MB`, `CTV_CACHE_MB` | Ressourcenlimits |
| `CTV_SCAN_INTERVAL_SECONDS` | Bestehender technischer Scanintervall-Ausgangswert; die automatische tägliche Indexierung wird separat unter Betrieb konfiguriert |

Optional bleibt `CTV_OIDC_CLIENT_SECRET_FILE` mit zusätzlich schreibgeschützt
gemounteter Secret-Datei möglich. Nicht gleichzeitig mit `CTV_OIDC_CLIENT_SECRET`
setzen. Die Secret-Datei und ihr Mount werden nicht von der GUI verwaltet.

Für den Wechsel von Umgebungswerten zur GUI:

1. Aktuelle Werte in den passenden GUI-Formularen ausdrücklich speichern.
2. OIDC-Client-Secret erneut im Secret-Feld speichern, wenn es bisher nur extern
   gesetzt war. Die angezeigte Herkunft „Weboberfläche“ bestätigt die GUI-Speicherung;
   „Umgebung“ bedeutet, dass das Secret weiterhin extern bezogen wird.
3. Die angebotene Administrator-Testanmeldung abschließen und eine neue reguläre
   Anmeldung prüfen. Bis zur erfolgreichen Testanmeldung bleibt die bisherige
   Anmeldekonfiguration aktiv.
4. Erst danach die entsprechenden optionalen Variablen aus der eigenen `.env`
   entfernen und den Container neu erstellen. Den Datenordner erhalten.

Der letzte aktive lokale Administrator bleibt geschützt. Die CLI-Wiederherstellung
steht weiterhin zur Verfügung; siehe [README](../README.md#mit-docker-compose-starten).

## Liveansicht und externe Streamleser

**Verwaltung → Kameras → Livequellen und Freigabe** enthält RTSP-Quellen,
Verbindungstest und die optionale Home-Assistant-Freigabe. `CTV_GO2RTC_PATH` wählt
die lokale go2rtc-Binärdatei (Standard: `go2rtc` im PATH),
`CTV_LIVE_RTSP_ADDRESS` den optionalen Listener (Standard `127.0.0.1:8554`).
Die einzelne `compose.yaml` bindet RTSP-Port 8554 standardmäßig nur an den Host.
Für Home Assistant die erste Adresse im Portmapping `127.0.0.1:8554:8554/tcp`
direkt auf die Heimnetz-IP ändern. Der
Listener wird erst durch die globale Freigabe in der Weboberfläche aktiviert.
[Einrichtung](LIVE.md).

## Verschlüsselte Zugangsdaten (ab 0.11.0)

Der Viewer verschlüsselt wieder lesbare Geheimnisse mit AES-256-GCM:
Kamerakonfiguration und Zugangsdaten, Home-Assistant-Freigabekonfiguration,
gespeicherte Server-/OIDC-Einstellungen sowie CSRF-Tokens und kurzlebige
OIDC-Anmeldegeheimnisse. Die vollständigen Konfigurations-JSON-Werte sind dabei
verschlüsselt, nicht nur einzelne Passwortfelder. Lokale Benutzerpasswörter
bleiben mit Argon2id gehasht. Aufnahmeindex, Kameranamen und persönliche
Darstellungspräferenzen sind keine vollständig verschlüsselte Datenbank.

### Automatische Übernahme vorhandener Daten

Beim ersten Start werden vorhandene Klartextwerte in einer Datenbanktransaktion
übernommen. Die bisher gespeicherten Zugangsdaten bleiben gültig; erneute Eingabe
ist nicht erforderlich. Bereits verschlüsselte Werte werden auf Entschlüsselbarkeit
geprüft. Ein Fehler bricht den Start ab, ohne eine teilweise Übernahme zu
bestätigen. Freie Datenbankseiten und das bisherige WAL werden nach der
Umstellung bereinigt. Eine unterbrochene Bereinigung wird beim nächsten Start
automatisch fortgesetzt. Während der Migration nur einen Viewer-Prozess auf
das Datenverzeichnis zugreifen lassen. Unabhängige ältere Sicherungen bleiben unverändert und
können weiterhin Klartext enthalten.

### Schlüssel und Wiederherstellung

Der Viewer erzeugt automatisch eine zufällige 32-Byte-Datei **secrets.key** im
konfigurierten Datenverzeichnis, außerhalb der Datenbank und mit Dateirechten
0600 auf POSIX-Systemen. Unter Windows das Datenverzeichnis mit passenden
Zugriffsrechten schützen. Beim üblichen Containerbetrieb liegt sie damit im persistenten
Datenvolume. Auch das Datenverzeichnis muss geschützt bleiben: Wer Datenbank
und Schlüssel gemeinsam lesen kann, kann die Geheimnisse entschlüsseln.

Für Backups und einen Serverwechsel **viewer.db und secrets.key gemeinsam**
sichern beziehungsweise wiederherstellen. Für eine konsistente Dateisicherung
den Viewer vorher anhalten; bei Sicherung im Betrieb ein SQLite-kompatibles
Backupverfahren nutzen. Die Schlüsseldatei nicht bearbeiten oder neu erzeugen.

Fehlt der Schlüssel einer bereits verschlüsselten Datenbank oder ist er falsch,
startet der Viewer mit einem verständlichen Fehler. Er erzeugt keinen Ersatz
und setzt Zugangsdaten nicht zurück. Die ursprüngliche Schlüsseldatei muss aus
der passenden Sicherung wiederhergestellt werden. Für einen Wechsel zurück auf
eine ältere Programmversion ist eine passende Sicherung von vor der Migration
erforderlich; die verschlüsselten Konfigurationen sind dort nicht lesbar.
