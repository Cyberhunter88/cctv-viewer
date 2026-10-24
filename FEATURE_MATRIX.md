# Funktionsabgleich

Referenz: [cctv-timeline-viewer-core, Commit 9c3128c756430529dc5c34c52d6bb4e8e5c18133](https://github.com/robertoamd90/cctv-timeline-viewer-core/tree/9c3128c756430529dc5c34c52d6bb4e8e5c18133).
Abgleich von README, Kamera-/Such-/System-APIs, Indexierung, Hotspot und Oberfläche.
Eigenständiger Nachbau in Go/TypeScript; Originalcode und Assets wurden nicht übernommen.

## Ergänzungen ab 0.11.2

| Funktion | Umsetzung | Nachweis |
|---|---|---|
| Effizienteres Live-Vorladen | Vorladeverwaltung reagiert auf Änderungen und Nachlaufende ohne dauernde Datenbankabfragen; aktive HA-Routen ersetzen den zusätzlichen Vorladeleser | Go: LiveWarmPolicy, LiveWarmSchedule, LiveWarmReaderHandsOffToGateway |
| Paralleler HA-Streamstart | Verschiedene RTSP-Routen werden parallel aufgebaut; Anfragen für dieselbe Route teilen den Aufbau | Go: LiveGatewayRouteConcurrency |

## Ergänzungen ab 0.11.1

| Funktion | Umsetzung | Nachweis |
|---|---|---|
| Eine Ansicht ohne Zoom-Livequellen | Zoom-Bedienelemente und Freigabeadressen erscheinen nur bei „Weitwinkel + Zoom“; gespeicherte Zoomquellen ruhen bei „Eine Ansicht“ | Browser: settings-011; Go: LiveSingleViewZoom |

## Ergänzungen ab 0.11.0

| Funktion | Umsetzung | Nachweis |
|---|---|---|
| Einheitliche Einstellungen | Gemeinsame Formulare, mobile Listenansicht und Kameraeditor mit Allgemein/Aufnahmen/Live; separate Streamfreigabe | Browser: settings-ux, navigation, live |
| Vorladen je Livequelle | Bei Bedarf, 60 Sekunden Nachlauf oder dauerhaft; Browser und Home Assistant teilen Quellen | Go: LiveWarmPolicy, LiveUsageReferenceCounting, LiveHTTPSFLVPreloadIntegration |
| HTTPS-FLV | Reolink-URL-Import, geschützte Zugangsdaten, Zertifikatsprüfung mit Ausnahme je Quelle | Frontend: live-source; Go: LiveHTTPSRelayTLSAndCredentials, LiveHTTPSFLVPreloadIntegration |
| Verschlüsselte Zugangsdaten | AES-256-GCM für Konfigurationen und Anmeldegeheimnisse, automatische Klartextmigration und separater persistenter Schlüssel | Go: SecretsLegacyMigrationAndReopen, SecretsMissingWrongKeyAndTampering, SecretsMigrationIsAtomic |

## Übernommene Viewer-Funktionen

| Referenzfunktion | Umsetzung | Nachweis |
|---|---|---|
| Ton pro Ansicht | Eine hörbare Quelle; AAC bei komprimierter Wiedergabe, Tonhöhe bleibt erhalten | Go: AudioTransports; Browser: audio |
| Weitwinkel + Zoom pro Kamera | Getrennte MP4s über Kennungen und Aufnahmezeiten; Zoom-Zeitversatz; Zoom-Schalter neben Ton; ab 720 px Kamerabreite nebeneinander, darunter untereinander; Vollbild bleibt bei Größenänderungen erhalten | Go: DualViewClassificationAndPersistence; Browser: dual |
| Sammelanlage und parallele Kamerascans | Bis zu 64 Quellen je Anlage; maximal drei Kameras parallel; Fortschritt | Go: BatchAtomicAndParallelCameraScan; Browser: dual |
| Synchronisierte Kameras und gemeinsame Uhr | Erhalten, unabhängige Fehlerbehandlung | Browser: viewer |
| Tag, Datum, Heute, Aufnahmelücken | Erhalten, Berliner 23-/25-Stunden-Tage | Go: Timestamps; Browser: viewer |
| Aufnahmegrenzen, 0,25× bis 16× | Ergänzt | Frontend: view-state; Go: ProfilesSpeedTransport |
| Timeline-Zoom, Verschieben, Vorschaubilder | Erhalten; Touch, Tastatur (Space, Pfeile, J/K/L, F), Umschalt+Ziehen, Hover-Uhrzeit, paralleles Laden | Browser: viewer, archive |
| Übersichtstimeline und verstellbare Höhe | Ergänzt | Browser: archive |
| Automatische/feste/eigene Raster und Kameraseiten | Ergänzt, eigene Raster bis 8 × 8 | Frontend: view-state; Browser: viewer, archive |
| Wiedergabediagnose | Eigener Dialog über persönliche Einstellungen; Aufzeichnung läuft beim Schließen weiter | Browser: preferences |
| Persönliche Einstellungen auf allen Geräten | Kontospeicherung einschließlich Farbschema; Laden beim Anmelden/Neuladen; einmalige Browserübernahme | Go: Preferences; Browser: preferences |
| Kameraauswahl und Zieh-Sortierung | Ergänzt; dauerhaft je Benutzerkonto gespeichert | Browser: archive; Frontend: view-state |
| Bildverhältnisse 4:3, 16:9, Auto, Ausfüllen | Ergänzt | Browser: archive |
| Manueller und automatischer Hotspot | Neuester aktiver Aufnahmestart; Gleichstand nach Reihenfolge | Frontend: view-state; Browser: archive |
| Responsive Oberfläche für Desktop und Handy | Beschriftete mobile Navigation ab 360 px, Touch-Schaltflächen ab 44 px, Wiedergabe und Zeitleiste gemeinsam unter den Kamerabildern, Hell/Dunkel | Browser: redesign, archive, preferences, multicamera |
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
| Offline-Quellen, bestätigte Indexbereinigung, große Verzeichnisse | Gewöhnliche Scans löschen keine fehlenden Einträge; gesonderte Bereinigung mit Vorschau; maximal 2 Mio. Einträge | Go: MissingRecordingRequiresConfirmedCleanup, OfflineSourcePreservesIndex |
| Persistente Scanaufträge und Verlauf | Wartende Aufträge werden nach Neustart fortgesetzt; begrenzter Verlauf und Wiederholung fehlgeschlagener automatischer Scans | Go: ScanJobPersistedUntilCompletion; Browser: Kameraverwaltung |
| Suche mit Kamera, Zeitraum, Mindestdauer | Geschützte API und Suchseite; Cursor-Seiten, Sortierung, Tagesgruppen, Vorschaubilder | Go: SearchRebuild; Browser: features |
| Original/Ausgewogen/Schnell, konfigurierbare Profile | Original unverändert; einmalige Umstellung auf Original, reduzierte Profile ausdrücklich wählbar | Go: ProfilesSpeedTransport; Browser: viewer |
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
| Docker, AMD64/ARM64, lesende Aufnahme-Mounts | Getrennte GHCR-Build-/Release-Pakete; Multiarch-Promotion ohne Neubau | Container-Smoke/Release-CI und Registry-Simulation |

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
| Digitaler Zoom, separate Vollbildbedienung | Entfernt |

## GHCR-Veröffentlichung

Die Release-Promotion übernimmt den geprüften Multiarch-Digest einschließlich Attestierungen ohne Neubau. Die manuelle Bereinigung trennt Installations- und Build-Paket. Im Build-Paket bleiben mindestens 30 Tage, die letzten zehn erfolgreichen Main-Commits, der aktuelle Main-Stand sowie ins Release-Paket übernommene Digests und deren Manifest-Abhängigkeiten erhalten. Eine wöchentliche Vorschau mit Leserechten zeigt Kandidaten und Schutzgründe; Löschungen benötigen weiterhin explizite Versions-IDs. PR und Main verwenden dieselbe Codeprüfungs-Action. Actions-Zusammenfassungen zeigen Prüfergebnisse und den Stand einzelner Release-Schritte einschließlich Wiederaufnahmehinweisen.

## Prüfgrenzen

Lokale Tests arbeiten ausschließlich mit synthetischen Aufnahmen. Der große
Verzeichnistest verwendet 10.000 Dateien; daraus wird keine Million-Dateien-
Benchmarkzusage abgeleitet. HTTP-Streamingtests decken HLS und fragmentiertes MP4
bei mehreren Geschwindigkeiten ab. Browser- und Containerergebnisse werden im
Abschlussbericht separat benannt; eine vorhandene CI-Konfiguration ist kein
Nachweis eines bereits erfolgreich ausgeführten Containerlaufs.

Die Aufnahmeansicht unterstützt echtes Browser-Vollbild per Schaltfläche,
Doppelklick und Taste F. Die einklappbare Seitenleiste enthält das Kontomenü;
Datum, Kameraauswahl und Seitensteuerung sind für Laptopbreiten kompakt angeordnet.
Bei geschlossenem Zoom verwendet die Zeitleiste ausschließlich die Weitwinkelansicht.

## Verwaltung und persönliche Einstellungen

- Kameraverwaltung mit separater Liste und Editor, mobil als eigener Bearbeitungsschritt; Status, Scan und Sortierung pro Kamera.
- Persönliche Einstellungen einschließlich Farbschema werden erst mit „Speichern“ übernommen. Ungespeicherte Änderungen können nach Rückfrage verworfen werden.
- Abmelden ist direkt in der Seitenleiste erreichbar. Die schmale Symbolleiste hält alle Menüziele mit Beschriftungen für Tastatur und Screenreader erreichbar; ihr Zustand bleibt pro Konto gespeichert.
- Einstellungen sind feste Seiten mit passenden Überschriften und Rückweg zu den Aufnahmen; der Diagnosedialog behält einen eigenen Scrollbereich.

## Seitennavigation und Sitzungen

| Funktion | Verhalten | Prüfung |
| --- | --- | --- |
| Untermenüs links | Feste Seiten mit Hash-Direktlinks, Browserhistorie und mobilem Menü | Browser: navigation, redesign |
| Wiedergabe bei Seitenwechsel | Bild und Ton pausieren; Auswahl und Position bleiben erhalten | Browser: navigation |
| Lokale Konten | Suchbare Benutzerverwaltung, Rollen, Sperren, Passwortsetzen und Löschen | Go: UserManagement; Browser: navigation |
| Sitzungen | Eigene Sitzungen einzeln oder gesammelt beenden; Admins können fremde Sitzungen beenden | Go: SessionManagement; Browser: navigation |
| Minimale Umgebung | Externe Grundeinstellungen in .env.example; optionale Ausgangswerte separat dokumentiert | docs/CONFIGURATION.md |

## Laufzeit und Wartbarkeit

- Scanstatus wird ohne überlappende Abfragen aktualisiert. Verborgene Tabs pausieren die Abfragen; beim Zurückkehren wird sofort aktualisiert. Verbindungsfehler verlängern den Abstand bis maximal 30 Sekunden.
- Neue Suchen und das Verlassen der Suchseite brechen überholte Suchanfragen ab. Bereits geladene Treffer bleiben beim Seitenwechsel erhalten; abgebrochenes Nachladen kann erneut gestartet werden.
- Wiedergabeuhr und Kacheln sind eigenständige Frontendmodule. Während der Wiedergabe werden Kacheln höchstens alle 50 ms synchronisiert und Zeitleiste/Positionssteuerung alle 100 ms aktualisiert; direkte Bedienung bleibt unmittelbar. Bei Pause läuft keine dauerhafte Animationsschleife.
- Die Suche verwendet bei Kameras ohne wirksamen Zoom-Zeitversatz die vorhandenen Zeitindizes für die Sortierung. Trefferzählung, Zeitkorrektur und Cursor-/Offset-Seiten bleiben erhalten; das Datenbankschema bleibt unverändert.
- Frontend-Unit-Tests werden einschließlich Unterordnern automatisch gefunden. Synthetische Suchbenchmarks umfassen 10.000, 100.000 und eine Million Aufnahmen; sie ersetzen keine Messung auf einem realen NAS.

## Liveansicht (0.10.0)

| Funktion | Verhalten |
| --- | --- |
| Livekameras | Eigener Menüpunkt über Aufnahmen, getrennte gespeicherte Kameraauswahl, Alle/Keine, Raster und Vollbild |
| Übertragung | WebRTC mit HTTPS-WebSocket/MSE-Rückfall, H.264-Nebenstream als schnelle Standardqualität |
| Doppelobjektiv und Ton | Separat zuschaltbarer Zoomstream; Ton zunächst stumm, ein Bild gleichzeitig hörbar |
| Verwaltung | RTSP-Quellen, geschützte Zugangsdaten und Verbindungstest je vorhandener Kamera |
| Home Assistant | Optionale authentifizierte RTSP-Freigabe im Heimnetz, feste Adressen, TCP, eigene Zugangsdaten |
| Lebenszyklus | Gemeinsame Quellen für Betrachter, Freigabe unabhängig von persönlicher Auswahl, automatische Wiederverbindung |

Installation und Kompatibilitätsgrenzen: [Liveansicht und Home Assistant](docs/LIVE.md).
