# Einstellungen über die Weboberfläche

Nach der einmaligen Erstellung des ersten Administrators per CLI werden Konten
und Servereinstellungen links unter **Verwaltung** gepflegt. Es ist keine
Bearbeitung der `.env` für weitere Benutzer erforderlich.

| Bereich | Einstellungen |
| --- | --- |
| Einstellungen → Darstellung | Farbschema und Oberflächendichte des eigenen Kontos |
| Einstellungen → Wiedergabe | Kamerabilder, Raster, Vorladen, Zeitleiste, Tastenkürzel und Diagnose |
| Einstellungen → Mein Konto / Meine Sitzungen | Eigenes lokales Passwort ändern; eigene aktive Sitzungen einzeln oder gemeinsam beenden |
| Verwaltung → Kameras | Aufnahmeordner, Ansichten, Dateizuordnung, Zeitzonen und Indexierung |
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
| `CTV_RECORDINGS_PATH`, `CTV_DATA_PATH` | Host-Mounts für schreibgeschützte Aufnahmen und persistente Daten |
| `CTV_BIND_ADDRESS`, `CTV_HTTP_PORT` | Vom Host veröffentlichte Adresse und Port |
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
