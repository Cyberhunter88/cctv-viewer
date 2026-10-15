# Hinweise für Coding-Agenten

## Projekt und Quellen

- CCTV Viewer ist ein eigenständiger deutscher Viewer für Aufnahmen mit Go-Backend und TypeScript-Oberfläche. Bestehende Architektur und Funktionen weiterverwenden; kein zusätzliches UI-Framework einführen.
- Backend: `cmd/cctv/` und `internal/viewer/`; Frontend: `web/src/`. HTML, CSS und Theme-Skript unter `web/dist/` sind ebenfalls Quellen. Das gebündelte `web/dist/app.js` wird mit `npm run build` erzeugt.
- Browser-Tests liegen unter `tests/`, Release- und Workflow-Tests unter `scripts/test_*.py`.
- Originalaufnahmen bleiben nur lesbar. Keine Änderungen an öffentlichen APIs, Berechtigungen oder Datenbankschemas ohne entsprechenden Auftrag.
- Bestehende lokale Änderungen erhalten. Keine Secrets aus `.env`, Zugangsdaten oder Tokens in Ausgaben, Commits oder Testartefakte aufnehmen.

## Git und Lieferung

- Zuerst Branch, Arbeitsbaum und geltende Anweisungen prüfen. Auf einem Feature- oder Fix-Branch arbeiten; für neue Agenten-Branches standardmäßig `codex/` verwenden.
- Nur zum Auftrag gehörende Dateien committen und Conventional Commits verwenden. Angeforderte Lieferung einschließlich Push und PR gegen `main` abschließen.
- Der Repository-Eigentümer mergt PRs ausschließlich manuell. Agenten dürfen weder selbst nach `main` mergen noch Auto-Merge aktivieren.
- Ein ausdrücklich angeforderter Merge von `main` **in den Feature-Branch** ist zulässig. Konflikte sorgfältig auflösen und betroffene Prüfungen wiederholen; keine fremden Änderungen verwerfen.
- Agenten starten keine offiziellen Releases und verändern weder veröffentlichte Versionstags noch `latest`. Deployment gehört nicht zum Entwicklungsworkflow.

## CI/CD ändern

Die ausführliche Beschreibung steht in [docs/CI-CD.md](docs/CI-CD.md). Vor Änderungen die betroffenen bestehenden Dateien lesen, insbesondere:

- `.github/workflows/ci.yml` und `.github/workflows/release.yml`;
- `scripts/manual_release.py`, `scripts/wait_for_commit_ci.py`, `scripts/image_registry.py`, `scripts/promote_image.sh`, `scripts/release.py` und `scripts/smoke.sh`;
- `Dockerfile`, `compose.yaml`, `package.json` und `package-lock.json`.

Folgende Eigenschaften erhalten:

- PRs gegen `main` und Pushes auf `main` lösen CI aus. Der stabile erforderliche Abschlusscheck heißt **Anwendung prüfen**. Fehlgeschlagene, abgebrochene oder übersprungene Pflichtjobs dürfen ihn nicht grün werden lassen.
- PRs erhalten keine Schreibrechte auf GHCR. Nach einem Main-Push werden Code und Browser erneut geprüft und native AMD64-/ARM64-Images gebaut und mit Smoke-Tests geprüft.
- Automatische Main-Läufe veröffentlichen ausschließlich Commit-Images unter `sha-<vollständiger SHA>` sowie interne Build-Tags. Sie erstellen kein offizielles Release und aktualisieren keine Versions-Aliases oder `latest`.
- Die Release-Pipeline heißt **Release und Docker veröffentlichen** und hat ausschließlich `workflow_dispatch` als Trigger. Kein `release: published`-Trigger und keine Veröffentlichung durch gewöhnliche Pushes.
- Releases verwenden den nachgewiesenen Multiarch-Digest ohne erneuten Docker-Build. Der gewählte vollständige SHA muss auf `main` liegen; der vollständige erfolgreiche Main-Push-CI-Lauf, dessen Run-Versuch und der Image-Nachweis müssen exakt zusammenpassen.
- Version, Commit und beide Plattform-Digests gegen die Registry prüfen. Bestehende vollständige Versions-Tags bleiben unveränderlich; passende Artefakte bei Wiederholung verwenden, widersprüchliche Artefakte klar ablehnen.
- Stabile Aliases dürfen nur vorwärts zeigen. Ältere Releases und Vorabversionen dürfen `latest` nicht zurücksetzen; Vorabversionen erhalten ausschließlich ihren vollständigen Versionstag.
- Veröffentlichungen serialisieren, minimale Job-Berechtigungen und auf Commit-SHAs gepinnte Actions erhalten. Keine Secrets protokollieren und keine bestehenden Schutzmechanismen oder Tests zur Umgehung eines Fehlers entfernen.

## Versionierung

- `package.json` ist die Versionsquelle. Bei beauftragten Versionsänderungen auch die beiden Projektversionen in `package-lock.json` sowie eventuell fest eingetragene UI-Versionsanzeigen synchronisieren.
- Versionsänderungen erfolgen im normalen Feature-Branch/PR, beispielsweise mit `npm version 0.6.1 --no-git-tag-version`. Keine stillschweigende Versionsänderung auf `main` während eines Releases.
- Die manuelle Release-Eingabe verwendet `vX.Y.Z` beziehungsweise eine unterstützte SemVer-Vorabversion. Anwendung, Lockdatei, Image-Labels und CI-Nachweis müssen der Version ohne führendes `v` entsprechen; Abweichungen führen zum Abbruch.

## Prüfung und Fehlerdiagnose

Passende Prüfungen für die Änderung ausführen. Für Änderungen an CI/CD gelten zusätzlich die Python-Tests und statischen Workflow-/Shell-Prüfungen:

```bash
npm ci
npm run typecheck
npm test
npm run build
go vet ./...
go test -race ./...
python3 -m unittest discover -s scripts -p 'test_*.py'
python3 -m compileall -q scripts
bash -n scripts/promote_image.sh scripts/smoke.sh
actionlint
shellcheck scripts/promote_image.sh scripts/smoke.sh
git diff --check
```

- Browser-Prüfungen: nach dem Frontend-Build `go build -o bin/cctv ./cmd/cctv`, dann `npm run test:browser`. Die bestehende Playwright-Konfiguration startet den Testserver selbst; FFmpeg und der passende Browser müssen verfügbar sein. CI prüft Chrome und WebKit.
- Bei UI-Änderungen responsive Darstellung, Hell/Dunkel, Fokuszustände und Bedienbarkeit prüfen. Benutzerpräferenzen und Migration gespeicherter Werte erhalten.
- Bei roter GitHub-CI zuerst den betroffenen Lauf, fehlgeschlagene Job-Logs und gegebenenfalls Artefakte ansehen, etwa mit `gh run view <run-id> --log-failed`. Die belegte Ursache beheben; keine Prüfungen abschwächen, um Grün zu erzwingen.
- Lokale Tests, Simulationen und echte GitHub-/Docker-Prüfungen getrennt berichten. Fehlendes Docker, Browser-Abhängigkeiten oder Sandbox-Netzwerkbeschränkungen ausdrücklich als Verifikationslücke benennen.
- Ein PR bestätigt noch keinen Main-Docker-Build. Native Smoke-Tests laufen nach dem manuellen Merge; ein produktiver Release ist kein Agenten-Testlauf.
- Bei Lieferung geänderte Dateien, ausgeführte Tests, verbleibende Grenzen sowie Commit, PR und den bekannten CI-Status nennen. Erforderliche einmalige GitHub-/GHCR-Einstellungen stehen in `docs/CI-CD.md`.
