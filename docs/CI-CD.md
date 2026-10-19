# Entwicklung, CI und manuelle Releases

Der Ablauf ist: Feature-Branch → Commit → Push → PR → automatische CI → **manueller Merge** → Main-CI und Commit-Image → **manueller Release-Start** → GitHub Release und Docker-Versionstags.

Normale Pushes und Merges erstellen kein offizielles Release und verändern weder `latest` noch Versions-Tags. Auch ein in der GitHub-Releases-Oberfläche angelegtes Release startet keine Docker-Veröffentlichung mehr. Offizielle Releases werden ausschließlich mit dem unten beschriebenen Actions-Workflow erstellt. Codex darf PRs erstellen, aber weder mergen noch Releases starten.

## 1. Entwickeln und Pull Request erstellen

Von einem aktuellen `main` starten und einen eigenen Branch verwenden:

```bash
git fetch origin
git switch -c codex/meine-aenderung origin/main
# Änderungen vornehmen und lokal prüfen
git add <betroffene-dateien>
git commit -m "feat: beschreibe die Änderung"
git push -u origin codex/meine-aenderung
gh pr create --base main --fill
```

Die Workflows sind nach Ereignis getrennt: `pr.yml` läuft ausschließlich für Pull Requests, `ci.yml` ausschließlich für Pushes auf `main`. Dadurch werden ereignisfremde Jobs nicht angelegt und erscheinen nicht als übersprungen. Bei jedem PR gegen `main`, auch aus einem Fork, laufen Go-Vet und Go-Race-Tests, TypeScript-Prüfung, Frontend-Tests/-Build, Python-Tests der Release-Skripte, actionlint mit SHA256-geprüfter Tool-Version, ShellCheck sowie die vorhandenen Chrome- und WebKit-Tests. Der Job „Code prüfen“ erstellt nach dem Frontend-Build auch den eingebetteten Browser-Testserver. Anschließend führen „Browser prüfen (chrome)“ und „Browser prüfen (webkit)“ jeweils die vollständige Browser-Suite mit einem eigenen Testserver und Acht-Minuten-Limit aus. Fehlerartefakte werden je Browser hochgeladen. Damit erscheinen beim PR vier aktive Checks: Code, Chrome, WebKit und „Anwendung prüfen“. Auf Main entfallen Browser-Testserver, Artefakt-Upload und beide Browser-Läufe. Dort laufen Code, zwei Docker-Prüfungen, Commit-Image-Veröffentlichung und Abschlusscheck, insgesamt fünf aktive Checks. Bereits vorhandene FFmpeg-/ShellCheck-Werkzeuge werden direkt verwendet; nur fehlende Pakete werden installiert. Neuere PR-Commits brechen veraltete PR-Läufe ab. Ein Feature-Push ohne PR startet keinen zweiten CI-Lauf. PRs erhalten keine Berechtigung zum Schreiben von Paketen und bauen keine Docker-Images.

Die gemeinsamen Codeprüfungen liegen zentral in `.github/actions/code-checks/action.yml`. Beide Workflows rufen dieselbe lokale Action auf; die vier PR- und fünf Main-Checks behalten ihre bisherigen Namen. Der Browser-Testserver wird weiterhin ausschließlich im PR und erst nach dem Frontend-Build erstellt. Das Actionlint-Archiv wird mit einer versions- und SHA256-gebundenen Cache-ID wiederverwendet und vor jeder Ausführung erneut auf seine Prüfsumme geprüft. Bereits vorhandene Systemwerkzeuge werden weiterverwendet. Code-Zusammenfassungen nennen Prüfergebnisse, Gesamtdauer und Cachetreffer; die Einzelschrittzeiten stehen im jeweiligen Actions-Job. Dadurch lassen sich weitere Laufzeitänderungen messen, ohne ein zusätzliches CI-Image pflegen zu müssen.

Der stabile Abschlusscheck heißt **Anwendung prüfen**. Bei einem PR muss er Code und beide Browser erfolgreich sehen; bei einem Main-Push Code, beide Docker-Jobs und die Veröffentlichung des Commit-Images. Auf Main ist kein Browser-Job definiert. Ein fehlgeschlagener, abgebrochener oder übersprungener Pflichtjob lässt diesen Check fehlschlagen.

## 2. Manuell mergen und Main-Build kontrollieren

Der Eigentümer prüft den PR, wartet auf grüne Checks und mergt ihn **selbst**. Kein Workflow führt einen Merge durch.

Der Push des tatsächlichen Merge-Commits auf `main` führt die Code-Prüfungen erneut aus. Chrome und WebKit werden ausschließlich vor dem Merge im PR geprüft; der erforderliche PR-Abschlusscheck und der aktuelle Branch-Stand sichern diesen Übergang ab. Anschließend bauen die vorhandenen nativen Runner `ubuntu-24.04` und `ubuntu-24.04-arm` je ein Image für `linux/amd64` und `linux/arm64`. Der architekturspezifische BuildKit-Cache bleibt aktiv. Jeder Build wird per Digest heruntergeladen und auf seinem nativen Runner mit `scripts/smoke.sh` geprüft:

- Start, Anmeldung, persistente Sitzung nach Neustart und Healthcheck;
- Benutzer ohne Root-Rechte und nur lesbare Originalaufnahmen;
- native Architektur sowie OCI-Version und Commit-Label;
- Ausgabe von `cctv version` stimmt mit der Projektversion überein.

Nach beiden Smoke-Tests werden die geprüften Digests zu einem Multiarch-Index zusammengefasst. SBOM- und Provenienz-Manifeste bleiben erhalten. Die CI veröffentlicht ausschließlich:

```text
ghcr.io/cyberhunter88/cctv-viewer-builds:sha-<vollständiger-40-stelliger-commit-sha>
```

Interne Tags `build-<run>-<attempt>-<arch>` und `candidate-<run>-<attempt>` liegen ebenfalls ausschließlich im separaten Paket `cctv-viewer-builds`. Sie dienen der Zusammenstellung und sind keine Releases. Das Installationspaket `cctv-viewer` erhält durch Main-CI keine neuen Tags. Der endgültige Commit-Tag wird nicht überschrieben. Bei Wiederholung verwendet die CI ein bereits vorhandenes vollständiges Commit-Image, prüft beide Architekturen erneut und erstellt einen neuen Nachweis, ohne einen zweiten Docker-Build anzustoßen. Ein beschädigter oder widersprüchlicher Commit-Tag führt zum Abbruch.

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

Äußere Leerzeichen in den manuellen Eingaben für Version und Commit werden entfernt; innere Leerzeichen und ungültige Werte werden abgelehnt. Alle Folgeschritte verwenden den geprüften Release-Tag. Die Actions-Eingabe enthält ein führendes `v`, `package.json`, Lockdatei, Anwendung und Docker-Labels enthalten die Version ohne `v`. Verwendet werden `vX.Y.Z` und SemVer-Vorabversionen; führende Nullen und Build-Metadaten mit `+` werden abgelehnt, da Docker-Versionstags dieses Format nicht übernehmen können. Bei Versionsänderungen auch eventuell fest eingetragene UI-Versionsanzeigen im selben PR synchronisieren.

Der Release-Workflow verändert keine Version und schreibt nicht nach `main`. Weicht die gewünschte Version von `package.json`, den beiden Lockdatei-Einträgen, dem CI-Nachweis oder dem Image ab, bricht er mit einer Fehlermeldung ab. Eine nachträgliche Umbenennung eines anders versionierten Images ist ausgeschlossen.

## 4. Offizielles Release manuell starten

1. Erfolgreichen vollständigen Main-Lauf abwarten.
2. **Actions → Release und Docker veröffentlichen → Run workflow** öffnen.
3. Als Workflow-Branch **main** auswählen; andere Branches werden abgelehnt.
4. Pflichtfeld **version** ausfüllen, beispielsweise `v0.6.0` oder `v0.7.0-beta.1`.
5. Optional **commit** als vollständigen 40-stelligen SHA angeben. Ohne Eingabe wird der bei der Vorbereitung aktuelle HEAD von `main` verwendet. Für eine Wiederholung eines älteren Releases dessen exakten Commit ausdrücklich angeben.
6. Workflow manuell starten und die Zusammenfassung kontrollieren.

Der Workflow verlangt Main-Zugehörigkeit und wartet maximal 30 Minuten auf den neuesten Push-CI-Lauf für exakt diesen SHA. Ein grüner PR-Lauf genügt nicht. Auch ein insgesamt grüner Workflow genügt nicht, wenn einer der erforderlichen Code-, Docker-, Commit-Image- oder Abschlussjobs fehlt bzw. übersprungen wurde. Neue Main-Läufe enthalten keine Browser-Jobs. In älteren Läufen sind absichtlich übersprungene Browser-Jobs weiterhin zulässig; fehlgeschlagene oder abgebrochene Browser-Jobs bleiben ein Fehler. Historische Main-Läufe mit ausgeführten Browser-Prüfungen müssen weiterhin beide Browser beziehungsweise alle vier Browser-Shards vollständig erfolgreich enthalten. Der Nachweis wird aus genau dessen Run-Versuch geladen und mit den Registry-Digests und OCI-Labels beider Architekturen verglichen. Ein vorhandener Git-Tag muss auf denselben Commit zeigen, auch bei annotierten Tags. Ein vorhandener Docker-Versionstag muss auf denselben Multiarch-Digest zeigen.

Nach diesen Prüfungen wird das Commit-Image mit Workflow-Zugangsdaten heruntergeladen und auf AMD64 gestartet. Danach wird ein Release-Entwurf mit automatisch generierten Release Notes angelegt. Das geprüfte Multiarch-Image wird aus `cctv-viewer-builds` mit `skopeo copy --all --preserve-digests` in `cctv-viewer` kopiert, ohne Neubau und ohne Formatumwandlung. Beide Plattformen und die SBOM-/Provenienz-Manifeste werden übernommen; Index-Digest, Plattform-Digests und OCI-Labels werden am Ziel erneut geprüft. Dabei wird zunächst ausschließlich der unveränderliche vollständige Versionstag bereitgestellt. Dann wird der anonyme Download des Release-Images geprüft. Assets werden hochgeladen; erst nach erfolgreicher Download-Prüfung und vollständigem Asset-Upload werden die weiterhin zulässigen Versions-Aliases einschließlich `latest` gesetzt. Danach wird der Entwurf offiziell veröffentlicht. Bei Wiederholung werden passende Entwürfe, bereits korrekte Tags und identische Assets weiterverwendet. Abweichende Assets werden abgelehnt. Ein bestehender Release-Nachweis bleibt bei einem erneuten CI-Lauf erhalten, sofern Version, Commit und alle Image-Digests unverändert sind. Historische Commit-Nachweise mit `image=ghcr.io/cyberhunter88/cctv-viewer` bleiben gültig. Als Build-Quelle sind ausschließlich das Release-Paket selbst und dessen festes Schwesterpaket mit Suffix `-builds` zugelassen; beliebige Paketnamen aus Artefakten werden abgelehnt.

Die Assets stammen aus dem gewählten Release-Commit:

- `compose.yaml`;
- `config.env.example`, mit auf die Release-Version gesetztem `CTV_VERSION`;
- `release-proof.json` mit Release-Paket `image`, Build-Quelle `source_image`, Commit, CI-Lauf/-Versuch, Index-/Plattform-Digests und den freigegebenen Tags. Alte Nachweise ohne `source_image` bleiben kompatibel.

## 5. Docker-Tags und Nachvollziehbarkeit

| GHCR-Paket | Tags | Verwendung |
| --- | --- | --- |
| `cctv-viewer` | vollständige Version, stabile Versions-Aliases, `latest` | Installation und Releases |
| `cctv-viewer-builds` | `sha-*`, `build-*`, `candidate-*` | geprüfte Main-Builds und Release-Vorbereitung |

Die Compose-Adresse bleibt `ghcr.io/cyberhunter88/cctv-viewer:${CTV_VERSION:-latest}`. Das Build-Paket darf privat bleiben. Die Trennung gilt für neue Builds nach dem Merge dieser Pipeline. Historische Tags werden dadurch nicht verschoben oder entfernt.

Bei einem neuesten stabilen Release `v0.6.0` entstehen `0.6.0`, `0.6`, `0` und `latest`. Alle zeigen auf denselben geprüften Multiarch-Digest. Versions-Tags sind unveränderlich. `0.6` zeigt nur auf die höchste veröffentlichte stabile Version innerhalb von `0.6`, `0` entsprechend innerhalb von Major `0`. `latest` zeigt auf die höchste stabile Version insgesamt. Vorabversionen erhalten ausschließlich ihren vollständigen Versionstag.

Ältere Releases setzen jüngere Versionszeiger nicht zurück. Die Berechnung berücksichtigt veröffentlichte Releases und zusätzlich die tatsächlichen Image-Versionen vorhandener Aliases. Auch ein teilweise veröffentlichter neuerer Release-Entwurf kann dadurch nicht von einem älteren Lauf zurückgesetzt werden. Ein Alias ohne eindeutig passenden stabilen Versions-Tag wird nicht stillschweigend überschrieben; die Veröffentlichung bricht vor Tag-Änderungen ab.

```bash
docker buildx imagetools inspect ghcr.io/cyberhunter88/cctv-viewer:latest
docker pull ghcr.io/cyberhunter88/cctv-viewer:0.6.0
docker inspect --format '{{index .Config.Labels "org.opencontainers.image.version"}}' ghcr.io/cyberhunter88/cctv-viewer:0.6.0
```

Der `latest`-Digest lässt sich dem unveränderlichen Versions-Tag, dessen OCI-Versions-/Commit-Labels und dem Release-Asset `release-proof.json` zuordnen. Zum dauerhaft reproduzierbaren Einsatz kann statt eines Tags `ghcr.io/cyberhunter88/cctv-viewer@sha256:<digest>` verwendet werden. Server werden durch diese Workflows nicht automatisch aktualisiert.

### Alte interne GHCR-Einträge gezielt bereinigen

Der separate Workflow **Alte interne Docker-Images bereinigen** wird ausschließlich manuell auf `main` gestartet. Die Auswahl **package** trennt `release` (historische interne Einträge im Installationspaket) und `builds` (Aufbewahrung im Schwesterpaket). Standard ist `release`. Lösch-IDs gelten ausschließlich für das ausgewählte Paket.

1. Pakettyp auswählen und zuerst ohne aktiviertes **apply** und ohne **version_ids** starten. Das Artefakt `ghcr-cleanup-plan` enthält die Vorschau.
2. Die dortigen Kandidaten prüfen. Für `release` sind ausschließlich Versionen zulässig, deren sämtliche Tags `build-<run>-<attempt>-<arch>` oder `candidate-<run>-<attempt>` entsprechen und die seit mindestens 14 Tagen unverändert sind. Für `builds` gelten die unten beschriebenen erweiterten Schutzregeln und mindestens 30 Tage.
3. Für eine Löschung erneut starten, **apply** aktivieren und ausschließlich die geprüften numerischen GHCR-Versions-IDs durch Leerzeichen getrennt angeben.

Im Pakettyp `release` bleiben Versionen mit `latest`, Versions-Tags, `sha-*`, unbekannten Tags oder ohne Tags erhalten. Von beibehaltenen Manifesten direkt oder indirekt referenzierte Versionen sind ebenfalls geschützt; dazu gehören Plattform-, SBOM-, Provenienz- und Subject-Referenzen. Bei fehlenden/unbekannten Manifesten, fehlenden Berechtigungen, verändertem Inventar oder laufender/wartender CI beziehungsweise Release-Pipeline wird abgebrochen. Eine Teilauswahl darf keine Referenz eines beibehaltenen Kandidaten beschädigen. Es gibt keinen Zeitplan für Löschungen und keine pauschale Löschung ungetaggter Versionen.

#### Aufbewahrung des Build-Pakets

Für **package = builds** gelten zusätzlich folgende Regeln:

- Mindestens **30 Tage** seit Erstellung und letzter Änderung aufbewahren; ein kürzerer Sicherheitsabstand wird abgelehnt.
- Den aktuellen Main-Commit und die **zehn neuesten unterschiedlichen Commits erfolgreicher Main-Push-CI-Läufe** behalten. Wiederholte Läufe desselben Commits zählen nur einmal.
- Alle Digests schützen, die auch im Installationspaket vorhanden sind, einschließlich vorbereiteter Release-Images. Damit bleiben veröffentlichte Images und ihre transitiven Manifest-Abhängigkeiten auch in der Build-Quelle erhalten.
- Nur Versionen mit ausschließlich bekannten `build-*`, `candidate-*` und vollständigen `sha-*`-Tags können Kandidaten werden. Ungetaggte oder unbekannt getaggte Versionen bleiben geschützt. Daher garantiert die Bereinigung keine vollständige Freigabe aller Layer alter Builds; sie entfernt ausschließlich nachweislich zulässige Paketversionen.
- Bei Teilauswahl alle nicht ausgewählten Versionen als Wurzeln des Manifestgraphen behandeln. Abhängigkeiten eines beibehaltenen alten Builds dürfen nicht gelöscht werden.

Der Workflow **Build-Bereinigung vorschauen** läuft montags um 05:23 UTC und kann zusätzlich manuell auf `main` gestartet werden. Er besitzt ausschließlich Leserechte und eine eigene Ausführungssperre, damit eine geplante Vorschau keine wartende Veröffentlichung verdrängt. Er erzeugt das Artefakt `ghcr-cleanup-plan` für 14 Tage; er löscht nichts. Der Plan enthält Kandidaten sowie alle beibehaltenen Versions-IDs mit Schutzgründen. Die Actions-Zusammenfassung nennt Paket, Mindestalter, Anzahl der Kandidaten und geschützten Versionen. Die manuelle Löschung erzeugt ihren Plan erneut, kontrolliert Aufbewahrung, Registry-Inventar und laufende CI-/Release-Jobs und protokolliert jede erfolgreich gelöschte ID separat.

Alte, nicht veröffentlichte Commit-Images können dadurch bewusst ausgemustert werden. Nach einer solchen manuellen Löschung sind deren frühere Digests nicht mehr als Build-Quelle verfügbar. Für einen späteren Release muss die vollständige Main-CI erneut laufen und gegebenenfalls neu bauen. Bereits veröffentlichte Release-Images werden von dieser Aufbewahrung nicht entfernt. Die automatische Vorschau verschiebt ebenso wie die manuelle Bereinigung keine Tags.

Lokal eine Build-Vorschau erstellen:

```bash
python3 scripts/cleanup_registry.py --repository Cyberhunter88/cctv-viewer --build-package --output .local-reports/ghcr-build-cleanup-plan.json
```

GitHub löscht über diese API ganze Paketversionen mit sämtlichen zugehörigen Tags. Deshalb bleibt eine historische Zeile mit beispielsweise `0.7.0`, `latest`, `sha-*` und `candidate-*` zusammen geschützt. Nach dem Wechsel entstehen solche internen Tags im Release-Paket nicht mehr. Die Prüfung erfolgt unmittelbar vor der Löschung erneut; während des kurzen Löschlaufs keine Main-Pushes oder manuellen Registry-Änderungen starten. Cleanup und Release teilen sich die Veröffentlichungssperre.

Fehlende externe Manifest-Verweisziele (etwa historische Subject-Verweise), die nicht im Paketinventar stehen und von der Registry ausdrücklich mit `manifest unknown` beantwortet werden, erscheinen unter `missing_external_manifests` im Plan. Sie blockieren die Bereinigung nicht, weil sie selbst keine löschbaren Paketversionen sind. Fehlende Manifeste gelisteter Paketversionen sowie Zugriffs-, Netzwerk- und Formatfehler führen weiterhin zum Abbruch.

Die Vorschau lässt sich auch lokal erzeugen; JSON-Ausgaben gehören in `.local-reports/`:

```bash
python3 scripts/cleanup_registry.py --repository Cyberhunter88/cctv-viewer --output .local-reports/ghcr-cleanup-plan.json
```

Erforderlich sind `gh` mit Paket-Lesezugriff und angemeldetes Skopeo. Zum Löschen benötigt der Zugang Paket-Adminrechte; der manuelle Workflow verwendet dafür den Repository-`GITHUB_TOKEN`. Keine Tokens oder Zugangsdaten in Befehlsargumente, Reports oder das Repository schreiben.

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

Der abschließende Release-Bericht zeigt für Buildprüfung, Entwurf, Versionstag, anonymen Download, Assets, Aliases und Veröffentlichung jeweils das Ergebnis dieses Versuchs. Ein fehlgeschlagener oder abgebrochener Schreibschritt wird als möglicherweise teilweise ausgeführt gekennzeichnet. Der Bericht nennt die geprüfte Version und den vollständigen Commit für die Wiederaufnahme; vorhandene Artefakte früherer Versuche bleiben berücksichtigt. Ein harter Runner-Ausfall kann auch den abschließenden Bericht verhindern.

GitHub Release und GHCR haben keine gemeinsame Transaktion. Ein Netzfehler kann daher einen Entwurf oder einen Versionstag hinterlassen. Fehler bei Download-Prüfung oder Asset-Upload verschieben keine Aliases. Erst in der abschließenden Freigabe können teilweise aktualisierte Aliases zurückbleiben; ein Fehler beim Veröffentlichen des Entwurfs kann bereits aktualisierte Aliases hinterlassen. Ein erneuter Start setzt passende Artefakte fort. Fremde oder widersprüchliche Artefakte führen zum Abbruch. Laufende Veröffentlichungen werden nicht automatisch abgebrochen und sind durch die bestehende Concurrency-Gruppe serialisiert. GitHub kann einen bereits wartenden Lauf durch einen neueren wartenden Lauf ersetzen; nach einem solchen Abbruch gezielt erneut starten. Externe manuelle Tag-Änderungen/Release-Löschungen sind von dieser Sperre nicht erfasst und sollen während einer Veröffentlichung unterbleiben.

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
- **GHCR-Pakete:** `cctv-viewer` und `cctv-viewer-builds` müssen mit diesem Repository verbunden sein und ihm unter „Manage Actions access“ Schreibzugriff geben. Das neue Build-Paket wird beim ersten Main-Build angelegt und darf privat bleiben. Das Installationspaket `cctv-viewer` unter „Package settings → Change visibility“ einmalig **Public** stellen. Die Sichtbarkeit des Repositorys allein reicht dafür nicht aus. Die Pipeline ändert die Sichtbarkeit nicht. Die manuelle Bereinigung benötigt zusätzlich Paket-Adminrechte; automatisch mit dem Workflow-Repository verbundene Pakete gewähren diese üblicherweise dem `GITHUB_TOKEN`. Bei abweichender Paketzuordnung Zugriff vor dem ersten Cleanup prüfen.
- **Runner/Artefakte:** Native Ubuntu-AMD64- und Ubuntu-ARM64-Runner müssen verfügbar sein. Die Artefaktaufbewahrung muss die benötigte Zeit bis zum Release abdecken; dieser Workflow verlangt 90 Tage. Bei kürzerer Richtlinie rechtzeitig veröffentlichen oder die CI erneut ausführen. GHCR-Commit-Images und Release-Tags nicht durch externe automatische Aufräumregeln löschen. Die integrierte Build-Aufbewahrung erstellt automatisch nur Vorschauen; eine Löschung benötigt Paketwahl und explizite Versions-IDs.
- **Release-Berechtigungen:** Nur berechtigte Maintainer sollen den Workflow starten können. Falls gewünscht kann zusätzlich eine GitHub-Environment-Freigabe eingerichtet werden; der vorliegende Ablauf benötigt keine neue Environment-Konfiguration.

## 9. Prüfungen und Grenzen

Die Python-Suite prüft zusätzlich die Paketgrenzen, unveränderte Cross-Package-Kopien einschließlich Attestierungen, Fehler während der Kopie, historische Nachweise und die konservative Bereinigung anhand von Manifestgraphen. Sie prüft echte lokale Git-Historien für Main-Zugehörigkeit und Tag-Konflikte, simulierte CI-Antworten für fehlgeschlagene/fehlende Jobs sowie den echten Promotion-Einstiegspunkt gegen eine Registry-Simulation. Sie prüft beide Plattformen, Versions-/Commit-Labels, Digest-Gleichheit, Wiederholungen, ältere Releases, Vorabversionen, Alias-Konflikte und abgebrochene Veröffentlichungen. Die Workflow-Verträge sichern die Trigger, Berechtigungen, SHA-Pins und die Veröffentlichungsschranken ab.

```bash
python3 -m unittest discover -s scripts -p 'test_*.py'
python3 -m compileall -q scripts
bash -n scripts/promote_image.sh scripts/smoke.sh
# Mit lokal installierten Prüfern:
actionlint
shellcheck scripts/promote_image.sh scripts/smoke.sh
```

Registry-Simulationen ersetzen weder einen echten Main-Build noch eine Veröffentlichung. PR-CI kann Code-/Browser-Prüfungen bestätigen; die nativen Docker-Smoke-Tests laufen erst nach dem manuellen Merge. Der erste produktive manuelle Release muss vom Eigentümer gestartet werden. Die Quellen beschreiben [Workflow-Concurrency](https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/control-workflow-concurrency), [unverändertes Kopieren von Image-Indexes](https://docs.docker.com/reference/cli/docker/buildx/imagetools/create/) und [GHCR-Zugriff/Sichtbarkeit](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry).

Zur Registry-Kopie siehe [Skopeo copy](https://github.com/containers/skopeo/blob/main/docs/skopeo-copy.1.md); zu Paketversionen und Berechtigungen siehe [GitHub Packages REST API](https://docs.github.com/en/rest/packages/packages).
