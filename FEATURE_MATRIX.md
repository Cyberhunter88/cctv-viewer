# Funktionsabgleich

Referenz: [cctv-timeline-viewer-core, Commit 9c3128c756430529dc5c34c52d6bb4e8e5c18133](https://github.com/robertoamd90/cctv-timeline-viewer-core/tree/9c3128c756430529dc5c34c52d6bb4e8e5c18133).
Abgleich von README, Kamera-/Such-/System-APIs, Indexierung, Hotspot und Oberfläche.
Eigenständiger Nachbau in Go/TypeScript; Originalcode und Assets wurden nicht übernommen.

## Übernommene Viewer-Funktionen

| Referenzfunktion | Umsetzung | Nachweis |
|---|---|---|
| Synchronisierte Kameras und gemeinsame Uhr | Erhalten, unabhängige Fehlerbehandlung | Browser: viewer |
| Tag, Datum, Heute, Aufnahmelücken | Erhalten, Berliner 23-/25-Stunden-Tage | Go: Timestamps; Browser: viewer |
| Aufnahmegrenzen, 0,25× bis 16× | Ergänzt | Frontend: view-state; Go: ProfilesSpeedTransport |
| Timeline-Zoom, Verschieben, Vorschaubilder | Erhalten; Touch, Tastatur, Umschalt+Ziehen | Browser: viewer, archive |
| Übersichtstimeline und verstellbare Höhe | Ergänzt | Browser: archive |
| Automatische/feste/eigene Raster und Kameraseiten | Ergänzt, eigene Raster bis 8 × 8 | Frontend: view-state; Browser: viewer, archive |
| Kameraauswahl und Zieh-Sortierung | Ergänzt; je Benutzer gespeichert | Browser: archive; Frontend: view-state |
| Bildverhältnisse 4:3, 16:9, Auto, Ausfüllen | Ergänzt | Browser: archive |
| Manueller und automatischer Hotspot | Neuester aktiver Aufnahmestart; Gleichstand nach Reihenfolge | Frontend: view-state; Browser: archive |
| Kompakte/einklappbare mobile Bedienung | Ergänzt im bisherigen Layout | Browser: archive, Screenshot |
| Kameraanlage, Bearbeiten, Löschen, Ordnerauswahl | Erhalten und erweitert | Go: RootConfinement; Browser: features |
| Quellstatus/Scanstand in Kamera-Verwaltung | Ergänzt | Go: OfflineSourcePreservesIndex; Browser: features |
| Datums- und vollständige rekursive Indexierung | Ergänzt | Go: MetadataFallbackAndFullDirectory |
| Verzeichnismuster YYYY/YY/MM/DD und Zeitzonen | Ergänzt; alte Dateimuster weiter lesbar | Go: ReplacementTimestamp; bestehende Parsertests |
| Zeitversatz ohne erneutes Untersuchen der Videos | Dezimaler Versatz, direkte Indexverschiebung | Go: MetadataFallbackAndFullDirectory |
| Dateiname → Medienzeit → Änderungszeit | Ergänzt | Go: ReplacementTimestamp, MetadataFallbackAndFullDirectory |
| MP4/AVI/MKV/MOV/TS/H264/H265/DAV; keine Bilder | Ergänzt; Dauer/Codec weiterhin erforderlich | Go: ReplacementTimestamp; Browser: features |
| Einzel-/Gesamtscans und Index-Neuaufbau | Geschützt; Neuaufbau während Scan gesperrt | Go: SearchRebuildAndRemovedEndpoints |
| Globale automatische Tagesindexierung | Aus/60 Minuten als Vorgabe, 1–10080 Minuten | Go: SchedulePersistsAndSkipsRecursiveCameras |
| Persistente Scanplanung, inkrementelle stabile Dateien | Ergänzt; interaktive Warteschlange bevorzugt | Go: IncrementalScanAndReconciliation, SchedulePersists |
| Offline-Quellen, Löschabgleich, große Verzeichnisse | Erhalten/erweitert, maximal 2 Mio. Einträge | Go: OfflineSourcePreservesIndex, IncrementalScan, LargeSyntheticPartition |
| Suche mit Kamera, Zeitraum, Mindestdauer | Geschützte API und Dialog; Öffnen eines Treffers | Go: SearchRebuild; Browser: features |
| Original/Ausgewogen/Schnell, konfigurierbare Profile | Ergänzt; Original unverändert | Go: ProfilesSpeedTransport; Browser: viewer |
| Metadaten-/automatisches Vorladen | Ergänzt und gespeichert | Browser: archive |
| Serverseitige beschleunigte komprimierte Wiedergabe | Faktor bis 16×; hohe Geschwindigkeiten mit Keyframes am Clipbeginn | Go: ProfilesSpeedTransport |
| Fragmentiertes MP4 / natives HLS | Ergänzt; Auswahl über Browserfähigkeit | Go: ProfilesSpeedTransport; Browser: viewer |
| HLS-Startschwellen bei 1×/4×/8×/16× | 1/2/4 Segmente oder abgeschlossener Clip | Go: ProfilesSpeedTransport |
| Gemeinsame Encoderlimits/temporärer Speicher | 0 = unbegrenzt; Budget-Watchdog alle 500 ms | Go: ProfilesSpeedTransport, ResourceBudget |
| Fehler pro Kamera und begrenzte Wiederherstellung | Drei Versuche; 30 Sekunden Wiederherstellungsfrist | Browser: recovery |
| Pause-Puffer, Streamfreigabe beim Verlassen | Unvollständige Streams nach 2 s; fertige Puffer bleiben | Browser: regressions |
| Wiedergabestatus, Sitzungsdiagnose, Diagnose-Download | Geschützt, Eigentümerprüfung, begrenztes JSON ohne Secrets/Pfade | Go: ProfilesSpeedTransport; Browser: regressions |
| Geordnetes Beenden/Neustart | Bestehender Shutdown, persistenter Scanplan | Go: OIDC-/Persistenztests; Container-CI |
| Datenbanksicherung | Eigenständige Sicherung vor Migration mit committed WAL-Daten | Go: ReplacementMigrationBackupAndDormantData |
| Docker, AMD64/ARM64, lesende Aufnahme-Mounts | Bestehende Verpackung erhalten | Container-Smoke/Release-CI |

## Bewusste Abweichungen und Ausschlüsse

| Funktion | Entscheidung |
|---|---|
| Home-Assistant-App, Ingress, Supervisor, Sensorzuordnung | Ausgeschlossen gemäß Nutzerentscheidung |
| Personen-/Fahrzeug-/Tier-/Bewegungs-/Klingelereignisse | Ausgeschlossen; kein anderer Ereignissammler ergänzt |
| Ereignisfilter und zeitgesteuerte Erkennungsanzeigen | Ausgeschlossen zusammen mit der HA-Anbindung |
| HA-Hot-Backup-Hooks und HA-Releasekatalog | Ausgeschlossen; lokale Migrationssicherung und eigene Docker-CI bleiben |
| Englische/italienische Originaloberfläche | Deutsche Oberfläche und bisheriges Layout bleiben |
| Ungeschützter Standalone-Modus des Originals | Bestehendes SSO, Rollen, lokale Anmeldung und Wiederherstellung bleiben |
| Beliebige absolute Quellpfade | Bestehende Begrenzung auf das eingebundene Aufnahmeverzeichnis bleibt |
| Mehrdeutige lokale Dateizeiten | Mit Warnung überspringen, vorhandenen Schutz erhalten |
| Persönliche Lesezeichen, Zeitlinks, Archivkalender/-übersicht | Entfernt; alte Daten erhalten, Endpunkte liefern 404 |
| Clip-Export und Timeline-Exportauswahl | Entfernt einschließlich APIs und Hintergrundaufträgen |
| JPG/JPEG-Einzelbilder und zugehörige Originalansicht | Entfernt; Originaldateien unverändert |
| Digitaler Zoom, Vollbild- und Audiobedienung | Entfernt; Video stumm wie im Vorbild |

## Prüfgrenzen

Lokale Tests arbeiten ausschließlich mit synthetischen Aufnahmen. Der große
Verzeichnistest verwendet 10.000 Dateien; daraus wird keine Million-Dateien-
Benchmarkzusage abgeleitet. HTTP-Streamingtests decken HLS und fragmentiertes MP4
bei mehreren Geschwindigkeiten ab. Browser- und Containerergebnisse werden im
Abschlussbericht separat benannt; eine vorhandene CI-Konfiguration ist kein
Nachweis eines bereits erfolgreich ausgeführten Containerlaufs.

## Ergebnis der lokalen Prüfung

- TypeScript-Prüfung und Frontend-Logiktests: bestanden.
- `go vet ./...` und `go test -race ./...`, einschließlich OIDC, Migration,
  UTC-Tagesgrenzen, persistenter Scanplanung und Streaming: bestanden.
- Acht Chromium-Browsertests: bestanden, einschließlich 16×, Kamera-Sortierung,
  Diagnose-Download, defekter Kamera und begrenzter Bandbreite.
- Sechs Release-Skript-Tests und `git diff --check`: bestanden.
- WebKit: lokal nicht erfolgreich ausführbar; der Browser scheitert bereits
  beim Erzeugen der ersten Seite am WPE/EGL-Display. Kein Anwendungstest in
  WebKit wird deshalb als bestanden ausgewiesen. HLS wurde serverseitig geprüft;
  der zusätzliche WebKit-CI-Schritt ist eingerichtet.
- Docker-Smoke-Test: lokal nicht ausführbar, weil Docker/Container-Daemon fehlen.
  Der bestehende verpflichtende Container-CI-Schritt bleibt erhalten.

Die lokale Funktionsprüfung ist damit abgeschlossen; die vollständige Abnahme
für WebKit und den Container erfordert noch erfolgreiche CI-Läufe.
