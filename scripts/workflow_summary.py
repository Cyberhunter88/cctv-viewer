#!/usr/bin/env python3
"""Explain publication outcomes without assuming failed remote writes were atomic."""
import json
import os
import sys


STAGES = (
    ('verify', 'Buildnachweis und AMD64-Smoke-Test'),
    ('draft', 'Release-Entwurf und Konfiguration'),
    ('version', 'Unveränderlicher Docker-Versionstag'),
    ('download', 'Anonymer Image-Download'),
    ('assets', 'Release-Assets'),
    ('promote', 'Versions-Aliases einschließlich latest'),
    ('publish', 'Offizielle Veröffentlichung'),
)


def release_summary(steps, version, commit):
    lines = ['## Stand der Veröffentlichung', '', f'Version: `{version}` · Commit: `{commit}`', '',
             '| Schritt | Ergebnis dieses Versuchs |', '| --- | --- |']
    labels = {'success': 'erfolgreich bestätigt',
              'failure': 'fehlgeschlagen; mögliche Teiländerungen prüfen',
              'cancelled': 'abgebrochen; mögliche Teiländerungen prüfen',
              'skipped': 'nicht ausgeführt'}
    for identifier, title in STAGES:
        outcome = steps.get(identifier, {}).get('outcome', 'skipped')
        lines.append(f'| {title} | {labels.get(outcome, "nicht bestätigt")} |')
    lines += ['', 'Die Tabelle beschreibt diesen Versuch; vorhandene Artefakte früherer Versuche bleiben erhalten.']
    if steps.get('publish', {}).get('outcome') == 'success':
        lines += ['', 'Release abgeschlossen. `release-proof.json` enthält Version, Commit und Plattform-Digests.']
    else:
        lines += ['', 'Nach Beheben des ersten fehlgeschlagenen Schritts den Release-Workflow auf `main` '
                  f'mit Version `v{version}` und Commit `{commit}` erneut starten. '
                  'Passende Artefakte werden wiederverwendet. Keine Tags manuell verschieben.', '',
                  'Bei Fehlern im Entwurf, Versionstag, Asset-Upload, Alias-Schritt oder der Veröffentlichung '
                  'können Teiländerungen vorhanden sein. Ein fehlgeschlagener Alias-Schritt kann bereits '
                  'einzelne Aliases aktualisiert haben; ein Fehler danach lässt diese bestehen.']
    return '\n'.join(lines) + '\n'


if __name__ == '__main__':
    if sys.argv[1:] != ['release']:
        raise SystemExit('Unbekannte Zusammenfassung')
    with open(os.environ['GITHUB_STEP_SUMMARY'], 'a') as output:
        output.write(release_summary(json.loads(os.environ['RELEASE_STEPS']),
                                     os.environ['VERSION'], os.environ['COMMIT']))
