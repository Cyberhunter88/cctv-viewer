# Entwicklung, CI und manuelle Releases

Der Ablauf ist: Feature-Branch → Commit → Push → PR → automatische CI → **manueller Merge** → Main-CI und Commit-Image → **manueller Release-Start** → GitHub Release und Docker-Versionstags.

Normale Pushes und Merges erstellen kein offizielles Release und verändern weder `latest` noch Versions-Tags. Auch ein in der GitHub-Releases-Oberfläche angelegtes Release startet keine Docker-Veröffentlichung mehr. Offizielle Releases werden ausschließlich mit dem unten beschriebenen Actions-Workflow erstellt. Codex darf PRs erstellen, aber weder mergen noch Releases starten.

## 1. Entwickeln und Pull Request erstellen

Von einem aktuellen `main` starten und einen eigenen Branch verwenden:

```bash
git fetch origin
git switch -c feat/meine-aenderung origin/main
# Änderungen vornehmen und lokal prüfen
git add <betroffene-dateien>
git commit -m "feat: beschreibe die Änderung"
git push -u origin feat/meine-aenderung
gh pr create --base main --title "feat: beschreibe die Änderung" --body-file /tmp/pr.md
```

Bei jedem PR gegen `main`, auch aus einem Fork, laufen Go-Vet und Go-Race-Tests, TypeScript-Prüfung, Frontend-Tests/-Build, Python-Tests der Release-Skripte, actionlint mit SHA256-geprüfter Tool-Version, ShellCheck sowie die vorhandenen Chrome- und WebKit-Tests. Die Browser verwenden dieselbe einmal gebaute Testserver-Binärdatei. Neuere PR-Commits brechen veraltete PR-Läufe ab. Ein Feature-Push ohne PR startet keinen zweiten CI-Lauf. PRs erhalten keine Berechtigung zum Schreiben von Paketen und bauen keine Docker-Images.

Der stabile Abschlusscheck heißt **Anwendung prüfen**. Bei einem PR muss er Code und beide Browser erfolgreich sehen; bei einem Main-Push zusätzlich beide Docker-Jobs und die Veröffentlichung des Commit-Images. Ein fehlgeschlagener, abgebrochener oder übersprungener Pflichtjob lässt diesen Check fehlschlagen.

## 2. Manuell mergen und Main-Build kontrollieren

Der Eigentümer prüft den PR, wartet auf grüne Checks und mergt ihn **selbst**. Kein Workflow führt einen Merge durch.

Der Push des tatsächlichen Merge-Commits auf `main` führt alle Tests erneut aus. Anschließend bauen die vorhandenen nativen Runner `ubuntu-24.04` und `ubuntu-24.04-arm` je ein Image für `linux/amd64` und `linux/arm64`. Der architekturspezifische BuildKit-Cache bleibt aktiv. Jeder Build wird per Digest heruntergeladen und auf seinem nativen Runner mit `scripts/smoke.sh` geprüft:

- Start, Anmeldung, persistente Sitzung nach Neustart und Healthcheck;
- Benutzer ohne Root-Rechte und nur lesbare Originalaufnahmen;
- native Architektur sowie OCI-Version und Commit-Label;
- Ausgabe von `cctv version` stimmt mit der Projektversion überein.

Nach beiden Smoke-Tests werden die geprüften Digests zu einem Multiarch-Index zusammengefasst. SBOM- und Provenienz-Manifeste bleiben erhalten. Die CI veröffentlicht ausschließlich:

```text
ghcr.io/cyberhunter88/cctv-viewer:sha-<vollständiger-40-stelliger-commit-sha>
```

Interne Tags `build-<run>-<attempt>-<arch>` und `candidate-<run>-<attempt>` dienen der Zusammenstellung; sie sind keine Releases. Der endgültige Commit-Tag wird nicht überschrieben. Bei Wiederholung verwendet die CI ein bereits vorhandenes vollständiges Commit-Image, prüft beide Architekturen erneut und erstellt einen neuen Nachweis, ohne einen zweiten Docker-Build anzustoßen. Ein beschädigter oder widersprüchlicher Commit-Tag führt zum Abbruch.

Unter **Actions → CCTV Viewer bauen und prüfen** den Lauf für den exakten Merge-SHA kontrollieren. Erst wenn der ganze Lauf erfolgreich ist, steht der Commit für ein Release bereit. Der Lauf enthält das Artefakt `commit-image-proof-<attempt>` mit `commit-image-proof.json`: Version, Commit, Run-ID/-Versuch, Multiarch-Digest und beide geprüften Plattform-Digests. Es wird 90 Tage aufbewahrt, soweit die Repository-Richtlinie das zulässt. Ist es abgelaufen, den vollständigen Main-CI-Lauf erneut ausführen; vorhandene Commit-Images werden wiederverwendet. „Re-run all jobs“ verwenden, damit Jobs und Nachweis demselben Versuch angehören.

## 3. Version vorbereiten

Die Version ist Teil des getesteten Builds. Vor dem Release in einem normalen PR beide Versionsangaben aktualisieren:

```bash
# Auf einem Feature-Branch, nicht im Release-Workflow:
npm version 0.6.0 --no-git-tag-version
# Danach package.json und package-lock.json prüfen, committen und PR erstellen.
```

| Änderung | Beispiel | Zweck |
| --- | --- | --- |
| PATCH | `0.5.0` → `0.5.1` | kompatible Fehlerkorrektur |
| MINOR | `0.5.0` → `0.6.0` | zusätzliche Funktionalität |
| MAJOR | `0.5.0` → `1.0.0` | inkompatible Änderung |
| Vorabversion | `0.7.0-beta.1` | ausdrücklich als Vorabversion veröffentlichen |

Die Actions-Eingabe enthält ein führendes `v`, `package.json`, Lockdatei, Anwendung und Docker-Labels enthalten die Version ohne `v`. Verwendet werden `vX.Y.Z` und SemVer-Vorabversionen; führende Nullen und Build-Metadaten mit `+` werden abgelehnt, da Docker-Versionstags dieses Format nicht übernehmen können. Bei Versionsänderungen auch eventuell fest eingetragene UI-Versionsanzeigen im selben PR synchronisieren.

Der Release-Workflow verändert keine Version und schreibt nicht nach `main`. Weicht die gewünschte Version von `package.json`, den beiden Lockdatei-Einträgen, dem CI-Nachweis oder dem Image ab, bricht er mit einer Fehlermeldung ab. Eine nachträgliche Umbenennung eines anders versionierten Images ist ausgeschlossen.

## 4. Offizielles Release manuell starten

1. Erfolgreichen vollständigen Main-Lauf abwarten.
2. **Actions → Release und Docker veröffentlichen → Run workflow** öffnen.
3. Als Workflow-Branch **main** auswählen; andere Branches werden abgelehnt.
4. Pflichtfeld **version** ausfüllen, beispielsweise `v0.6.0` oder `v0.7.0-beta.1`.
5. Optional **commit** als vollständigen 40-stelligen SHA angeben. Ohne Eingabe wird der bei der Vorbereitung aktuelle HEAD von `main` verwendet. Für eine Wiederholung eines älteren Releases dessen exakten Commit ausdrücklich angeben.
6. Workflow manuell starten und die Zusammenfassung kontrollieren.

Der Workflow verlangt Main-Zugehörigkeit und wartet maximal 30 Minuten auf den neuesten Push-CI-Lauf für exakt diesen SHA. Ein grüner PR-Lauf genügt nicht. Auch ein insgesamt grüner Workflow genügt nicht, wenn einer der erforderlichen Code-, Browser-, Docker-, Commit-Image- oder Abschlussjobs fehlt bzw. übersprungen wurde. Der Nachweis wird aus genau dessen Run-Versuch geladen und mit den Registry-Digests und OCI-Labels beider Architekturen verglichen. Ein vorhandener Git-Tag muss auf denselben Commit zeigen, auch bei annotierten Tags. Ein vorhandener Docker-Versionstag muss auf denselben Multiarch-Digest zeigen.

Nach diesen Prüfungen wird zusätzlich der anonyme Download des Commit-Images und dessen Start auf AMD64 geprüft. Danach wird ein Release-Entwurf mit automatisch generierten Release Notes angelegt. Das geprüfte Multiarch-Image wird per Digest übernommen, ohne Neubau. Assets werden hochgeladen; erst dann wird der Entwurf offiziell veröffentlicht. Bei Wiederholung werden passende Entwürfe, bereits korrekte Tags und identische Assets weiterverwendet. Abweichende Assets werden abgelehnt. Ein bestehender Release-Nachweis bleibt bei einem erneuten CI-Lauf erhalten, sofern Version, Commit und alle Image-Digests unverändert sind.

Die Assets stammen aus dem gewählten Release-Commit:

- `compose.yaml`;
- `config.env.example`, mit auf die Release-Version gesetztem `CTV_VERSION`;
- `release-proof.json` mit Commit, CI-Lauf/-Versuch, Index-/Plattform-Digests und den freigegebenen Tags.

## 5. Docker-Tags und Nachvollziehbarkeit

Bei einem neuesten stabilen Release `v0.6.0` entstehen `0.6.0`, `0.6`, `0` und `latest`. Alle zeigen auf denselben geprüften Multiarch-Digest. Versions-Tags sind unveränderlich. `0.6` zeigt nur auf die höchste veröffentlichte stabile Version innerhalb von `0.6`, `0` entsprechend innerhalb von Major `0`. `latest` zeigt auf die höchste stabile Version insgesamt. Vorabversionen erhalten ausschließlich ihren vollständigen Versionstag.

Ältere Releases setzen jüngere Versionszeiger nicht zurück. Die Berechnung berücksichtigt veröffentlichte Releases und zusätzlich die tatsächlichen Image-Versionen vorhandener Aliases. Auch ein teilweise veröffentlichter neuerer Release-Entwurf kann dadurch nicht von einem älteren Lauf zurückgesetzt werden. Ein Alias ohne eindeutig passenden stabilen Versions-Tag wird nicht stillschweigend überschrieben; die Veröffentlichung bricht vor Tag-Änderungen ab.

```bash
docker buildx imagetools inspect ghcr.io/cyberhunter88/cctv-viewer:latest
docker pull ghcr.io/cyberhunter88/cctv-viewer:0.6.0
docker inspect --format '{{index .Config.Labels "org.opencontainers.image.version"}}' ghcr.io/cyberhunter88/cctv-viewer:0.6.0
```

Der `latest`-Digest lässt sich dem unveränderlichen Versions-Tag, dessen OCI-Versions-/Commit-Labels und dem Release-Asset `release-proof.json` zuordnen. Zum dauerhaft reproduzierbaren Einsatz kann statt eines Tags `ghcr.io/cyberhunter88/cctv-viewer@sha256:<digest>` verwendet werden. Server werden durch diese Workflows nicht automatisch aktualisiert.

## 6. Fehler beheben und sicher erneut starten

| Fehler | Vorgehen |
| --- | --- |
| Version passt nicht | Version im Feature-Branch synchronisieren, testen, manuell mergen; passenden neuen Commit verwenden. |
| Commit liegt nicht auf Main | PR manuell mergen und den tatsächlich auf Main liegenden SHA verwenden. |
| CI fehlt, ist rot oder unvollständig | Fehlerprotokolle und Browser-Artefakte prüfen; Ursache beheben und vollständige Main-CI abwarten. |
| Nachweis fehlt/ist abgelaufen | Vollständige Main-CI für denselben Commit wiederholen. Für ältere Commits ohne diesen Workflow ist ein neuer Main-Commit mit der neuen Pipeline erforderlich. |
| GHCR nicht öffentlich/kein Zugriff | Paketeinstellungen korrigieren und denselben Release mit derselben Version und demselben SHA erneut starten. |
| Version gehört zu anderem Commit/Image | Abbrechen und eine neue Version über einen normalen PR vorbereiten; vorhandene Tags nicht umhängen. |
| Upload oder Veröffentlichung abgebrochen | Bestehenden Entwurf beibehalten und Workflow mit derselben Version und demselben Commit wiederholen. |
| Unerklärlicher Alias-Konflikt | Version, Digest und Release-Nachweis vergleichen; keine automatische Reparatur durch Überschreiben. |

GitHub Release und GHCR haben keine gemeinsame Transaktion. Ein Netzfehler kann daher einen Entwurf, einen Versionstag oder bereits teilweise aktualisierte Aliases hinterlassen. Ein erneuter Start setzt passende Artefakte fort. Fremde oder widersprüchliche Artefakte führen zum Abbruch. Laufende Veröffentlichungen werden nicht automatisch abgebrochen und sind durch die bestehende Concurrency-Gruppe serialisiert. GitHub kann einen bereits wartenden Lauf durch einen neueren wartenden Lauf ersetzen; nach einem solchen Abbruch gezielt erneut starten. Externe manuelle Tag-Änderungen/Release-Löschungen sind von dieser Sperre nicht erfasst und sollen während einer Veröffentlichung unterbleiben.

## 7. Ältere Version gezielt wiederherstellen

Auf dem Server in `.env` zum Beispiel `CTV_VERSION=0.5.0` setzen und dann bewusst ausführen:

```bash
docker compose pull
docker compose up -d
```

Das ändert keine Registry-Tags und verschiebt `latest` nicht. Falls nötig das Image mit Digest pinnen. Vor einem Anwendungs-Downgrade Datenverzeichnis sichern und die Datenbank-Kompatibilität bzw. die Hinweise unter „Upgrade und Rückkehr“ in der README beachten. Einen Rollback niemals durch Überschreiben eines bestehenden Release-Tags durchführen.

## 8. Einmalige GitHub-Einstellungen

- **Branch Protection / Ruleset für main:** Pull Request verlangen, Status **Anwendung prüfen** als erforderlichen GitHub-Actions-Check eintragen, Branch vor Merge aktuell verlangen, Regeln auch für Administratoren erzwingen, Force-Push und Löschen verbieten. Keine automatischen Merges aktivieren; der Eigentümer mergt manuell. Diese Einstellungen sind bei GitHub zu kontrollieren und werden nicht durch den Workflow geändert.
- **Actions-Berechtigungen:** Die Workflows deklarieren minimale Job-Rechte. Tests benötigen `contents: read`; nur Main-Build/Commit-Publikation benötigen `packages: write`; nur die manuelle Release-Publikation erhält zusätzlich `contents: write`. Für die Prüfung und den Download von CI-Artefakten ist `actions: read` nötig. Kein persönlicher Access-Token und kein zusätzliches Secret sind erforderlich.
- **GHCR-Paket:** Das Paket mit diesem Repository verbinden, dem Repository unter „Manage Actions access“ Schreibzugriff geben und bei öffentlichem Download unter „Package settings → Change visibility“ einmalig **Public** wählen. Die Sichtbarkeit des Repositorys allein reicht dafür nicht aus. Die Pipeline ändert diese Einstellung nicht.
- **Runner/Artefakte:** Native Ubuntu-AMD64- und Ubuntu-ARM64-Runner müssen verfügbar sein. Die Artefaktaufbewahrung muss die benötigte Zeit bis zum Release abdecken; dieser Workflow verlangt 90 Tage. Bei kürzerer Richtlinie rechtzeitig veröffentlichen oder die CI erneut ausführen. GHCR-Commit-Images und Release-Tags nicht durch automatische Aufräumregeln löschen.
- **Release-Berechtigungen:** Nur berechtigte Maintainer sollen den Workflow starten können. Falls gewünscht kann zusätzlich eine GitHub-Environment-Freigabe eingerichtet werden; der vorliegende Ablauf benötigt keine neue Environment-Konfiguration.

## 9. Prüfungen und Grenzen

Die Python-Suite prüft echte lokale Git-Historien für Main-Zugehörigkeit und Tag-Konflikte, simulierte CI-Antworten für fehlgeschlagene/fehlende Jobs sowie den echten Promotion-Einstiegspunkt gegen eine Registry-Simulation. Sie prüft beide Plattformen, Versions-/Commit-Labels, Digest-Gleichheit, Wiederholungen, ältere Releases, Vorabversionen, Alias-Konflikte und abgebrochene Veröffentlichungen. Die Workflow-Verträge sichern die Trigger, Berechtigungen, SHA-Pins und die Veröffentlichungsschranken ab.

```bash
python3 -m unittest discover -s scripts -p 'test_*.py'
python3 -m compileall -q scripts
bash -n scripts/promote_image.sh scripts/smoke.sh
# Mit lokal installierten Prüfern:
actionlint
shellcheck scripts/promote_image.sh scripts/smoke.sh
```

Registry-Simulationen ersetzen weder einen echten Main-Build noch eine Veröffentlichung. PR-CI kann Code-/Browser-Prüfungen bestätigen; die nativen Docker-Smoke-Tests laufen erst nach dem manuellen Merge. Der erste produktive manuelle Release muss vom Eigentümer gestartet werden. Die Quellen beschreiben [Workflow-Concurrency](https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/control-workflow-concurrency), [unverändertes Kopieren von Image-Indexes](https://docs.docker.com/reference/cli/docker/buildx/imagetools/create/) und [GHCR-Zugriff/Sichtbarkeit](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry).
