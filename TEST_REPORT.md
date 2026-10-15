# Lokale Gesamtprüfung – Version 0.5.0

Prüfdatum: 15. Oktober 2026.

## Bestanden

| Prüfung | Ergebnis |
| --- | --- |
| Installierte npm-Abhängigkeiten (`npm ls --depth=0`) | Stimmen mit den erwarteten direkten Abhängigkeiten überein |
| `npm run typecheck` | Bestanden |
| `npm test` | Alle drei TypeScript-Testdateien bestanden |
| `npm run build` | Bestanden, Webassets frisch erstellt |
| `go vet ./...` | Bestanden |
| `go test -count=1 -race ./...` | Vollständiger Lauf einschließlich Audio bestanden; keine Testgruppe ausgeschlossen |
| `go build -ldflags='-X main.version=0.5.0' -o bin/cctv ./cmd/cctv` | Bestanden |
| `bin/cctv version` | `0.5.0` |
| Vollständige Playwright-Suite mit Chromium/Brave | **25 Tests bestanden**, einschließlich Audio, sechs Einzelkameras, sechs Dual-Kameras, Theme-Wechsel und Wiedergabeprofilen |
| `python3 -m unittest discover -s scripts -p 'test_*.py' -v` | **20 Tests bestanden** |
| Python-Syntaxprüfung der Laufzeit-, Release- und CI-Hilfsskripte | Bestanden |
| `actionlint` für CI- und Release-Workflow | Bestanden |
| `bash -n scripts/smoke.sh scripts/promote_image.sh` | Bestanden |
| `git diff --check` | Bestanden |
| Versionsabgleich zwischen Paketdatei, Lockfile und gebautem Programm | Konsistent auf `0.5.0` |

Node 24.19.0 und Go 1.27.0 wurden für die Prüfung verwendet. Die Browserprüfung nutzte den vorhandenen Brave-Browser als Chromium-Engine; sie ist keine separate Google-Chrome-Abnahme. Die vollständige Chromium-Suite lief vor der reinen Versionsanhebung erfolgreich durch. Nach der Versionsanhebung wurden Typprüfung, TypeScript-Tests, Webbuild und Go-Build erneut erfolgreich ausgeführt. Ein SHA-256-Abgleich bestätigte, dass sich während der Tests ausschließlich die beiden absichtlich angepassten Paketversionsdateien geändert hatten.

Browser-Log: `/tmp/cctv-validation-browser.log`.
Browser-Artefakte: `/tmp/cctv-validation-browser-results/`.
Go-Testlog: `/tmp/cctv-validation-go.log`.
Python-Testlog: `/tmp/cctv-validation-python.log`.

## Lokal nicht abschließend prüfbar

- **WebKit:** Die Suite wurde gestartet. Fehlende Bibliotheken wurden ausschließlich unter `/tmp` ergänzt. Auch eine unabhängige minimale HTML-Testseite scheitert jedoch beim Erzeugen des Grafikdisplays mit `Could not create WPE EGL display: EGL_SUCCESS. Aborting...`. Software-Rendering half nicht. Der Suite-Lauf wurde deshalb beendet; es liegt kein erfolgreicher WebKit-Testlauf vor. Diagnose: `/tmp/cctv-webkit-smoke.log`, `/tmp/cctv-webkit-software.log`, `/tmp/cctv-validation-webkit.log`.
- **Docker-Build und Container-Smoke-Test:** Docker und Podman sind lokal nicht vorhanden. Diese Prüfungen wurden nicht ausgeführt.

Die vorhandene GitHub-CI führt Google-Chrome-/WebKit-Tests, native Docker-Builds für AMD64 und ARM64 sowie Container-Smoke-Tests aus. Deren Ergebnis für diesen uncommitteten Stand ist noch offen. Es wurden kein Commit, Tag oder GitHub-Release erstellt und nichts gepusht.
