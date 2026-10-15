# UI-Sichtung: sechs Kameras und Indexierung

## Behobene Probleme

- Die frühere Höhenregel ab 1600 Pixeln erzwang Kachelgrößen unabhängig vom Raster. Mit sechs Dual-Kameras führte das zu Überlappungen und horizontalem Überlauf.
- Ein festes 3×2-Raster erzeugte bei 390 Pixel Bildschirmbreite Dual-Bilder von etwa 46×35 Pixeln. Feste Raster werden jetzt vorübergehend an die verfügbare Breite angepasst, ohne die gespeicherte Auswahl zu verändern.
- Dual-Ansichten stehen unter 488 Pixel Gruppenbreite untereinander. Unter 600 Pixel Rasterbreite erscheinen höchstens zwei Kameragruppen je Seite.
- Die Kapazität eines Hotspots berücksichtigt jetzt alle vier belegten Zellen; Suche und Kameraseiten verwenden dieselbe Berechnung.
- Originalvideos werden unverändert ausgeliefert. Neue Ansichten verwenden das natürliche Seitenverhältnis und zeigen das vollständige Bild. Bewusst gespeicherte Profile und Seitenverhältnisse bleiben erhalten.
- Hell, Dunkel und System stehen bereits auf der Anmeldeseite zur Verfügung. Das Farbschema wird vor den Styles geladen, lokal gespeichert und auch für die Canvas-Zeitleisten verwendet.

## Indexierung

Der Dienst prüft bis zu drei Kameras gleichzeitig und arbeitet Aufträge nacheinander ab. Manuelle Aufträge werden bevorzugt; spätestens nach drei manuellen Aufträgen folgt ein wartender automatischer Auftrag. Identische Aufträge werden dedupliziert.

Automatische Scans bleiben über die vorhandene Betriebseinstellung optional. Sie berücksichtigen nun auch rekursive Kameraquellen. Datumspartitionierte Quellen prüfen den lokalen Tag sowie die beiden vorherigen Tage. Bekannte Dateien werden auf Größe und Änderungszeit geprüft; nur neue oder geänderte Videos benötigen FFprobe. Die Ruhezeit vor der Analyse beträgt weiterhin 30 Sekunden.

Verzeichnisse werden in Portionen von 128 Einträgen verarbeitet. Inkrementelle Scans benötigen keine vollständige Kandidatenliste. Manuelle Bestandsabgleiche behalten eine Menge der gefundenen Pfade, um entfernte Aufnahmen abzugleichen. Bei unvollständiger Verarbeitung oder nicht erreichbaren Quellen bleiben bestehende Indexeinträge erhalten. Bereits erfolgreich verarbeitete neue Dateien dürfen auch bei einem teilweise erfolgreichen Lauf verfügbar werden.

`GET /api/scan` behält die bisherigen Felder und ergänzt `jobId`, `state`, `queue`, `cameras`, `inspected`, `added`, `changed`, `skipped` und `completion`. `completion` steigt bei jedem abgeschlossenen Auftrag innerhalb eines Serverlaufs; die UI aktualisiert dadurch den geöffneten Tag auch zwischen aufeinanderfolgenden Aufträgen. Aufträge werden nicht über einen Serverneustart hinweg gespeichert. Der vorhandene Zeitplan bleibt in SQLite.

## Prüfumfang

- Sechs Einzelkameras, sechs Dual-Kameras und eine gemischte Konfiguration.
- Bildschirmbreiten 390, 768, 980, 1440 und 1920 Pixel; Automatik, 3×2, eigenes Raster und Hotspot.
- Echte synthetische H.264-Videos in 16:9, 4:3 und Hochformat. Separate Prüfung für sechs beziehungsweise zwölf gleichzeitig laufende Originalvideos ohne Transkodierungsanfragen.
- Seitenwechsel, vollständige Erreichbarkeit aller Kameras, Synchronisation, gespeicherte Einstellungen und unveränderte Bildproportionen.
- Theme-Wechsel, lokale Speicherung, Systemänderungen, Canvas-Farben und Textkontraste wichtiger Bedienelemente. Screenshots für Viewer, Suche und Kameraeinstellungen.
- Backend-Regressionsfälle für Warteschlange, Fairness, sechs Quellen, geänderte stabile Dateien, unveränderte Dateien ohne erneuten FFprobe-Aufruf, Quellenausfall, teilweise erfolgreiche Scans, Tageswechsel, Zeitumstellung und Herunterfahren.

Synthetische Tests prüfen Funktion und Layout. Sie ersetzen keine Durchsatzmessung mit sechs hochauflösenden Kameras auf der späteren Serverhardware. Nicht unterstützte Original-Codecs erfordern weiterhin ein ausdrücklich ausgewähltes kompatibles Profil.

## Weitere Vorschläge

Diese Erweiterungen sind bewusst als Vorschläge dokumentiert:

1. Alle Kameras gemeinsam auswählen oder die Auswahl aufheben.
2. Kamerareihenfolge direkt im Viewer per Tastatur und Touch verändern.
3. Bei belegten Umwandlungsplätzen eine direkte Aktion „Original verwenden“ anbieten.

4. Vorschaubilder nach vorübergehender Auslastung erneut anfordern: Der vorhandene Thumbnail-Endpunkt kann bei gleichzeitigen Anfragen HTTP 503 liefern; bislang werden solche Vorschauen nicht erneut geladen.

## Ergebnis der lokalen Abnahme – 0.5.0

Die vollständige Chromium-Suite mit 25 Browser-Tests einschließlich Audio besteht. Typprüfung, TypeScript-Unit-Tests, Build, alle 20 Python-Tests, `go vet ./...` sowie die vollständigen Go-Race-Tests ohne ausgeschlossene Testgruppen bestehen ebenfalls. Die Paketversion und das gebaute Programm melden `0.5.0`.

WebKit ist auf diesem Host auch nach Ergänzung temporärer Bibliotheken wegen eines WPE/EGL-Grafikfehlers nicht lauffähig. Docker ist nicht installiert. Diese Prüfungen bleiben offen. Exakte Befehle, Ergebnisse und Diagnosepfade stehen im [Testbericht](TEST_REPORT.md).
