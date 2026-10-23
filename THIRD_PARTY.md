# Drittanbieter

Diese Anwendung ist eine eigenständige Implementierung. Der CCTV Timeline Viewer
von robertoamd90 (Referenzstand 9c3128c756430529dc5c34c52d6bb4e8e5c18133)
dient als funktionale Referenz; Quellcode und Assets wurden nicht
übernommen.

Die MIT-Lizenz dieses Repositorys betrifft unseren eigenen Code. Abhängigkeiten
behalten ihre jeweiligen Lizenzen. Insbesondere:

- hls.js: Apache-2.0; Lizenzhinweise bleiben im gebauten JavaScript enthalten.
- Go: BSD-3-Clause; Go-Abhängigkeiten sind in go.mod/go.sum festgehalten.
- modernc.org/sqlite: BSD-3-Clause; SQLite-Kern: Public Domain.
- FFmpeg einschließlich libx264 sowie Alpine-Laufzeitpakete: jeweilige LGPL/GPL-
  und Paketlizenzen. Die Paketversionen im Image liefert `apk info -vv`.
  Alpine-Paketdefinitionen und Quellcode-URLs für den Runtime-Zweig 3.22:
  https://gitlab.alpinelinux.org/alpine/aports/-/tree/3.22-stable/community/ffmpeg
  und https://ffmpeg.org/download.html . Die APKBUILD-Dateien enthalten exakte
  Quellversionen, Prüfsummen, Patches und Bauanweisungen. Die vollständige
  Paketliste steht in der SBOM des jeweiligen Image-Digests.
- tini: MIT.
- go2rtc 1.9.14: MIT; wird als separates Programm mitgeliefert und verwaltet.
  Quellen: https://github.com/AlexxIT/go2rtc/tree/v1.9.14 . Der reproduzierbare
  Build steht in `scripts/build_go2rtc.sh`; die Lizenz liegt im Image unter
  `/usr/share/licenses/go2rtc/LICENSE`. Zwei eigenständige C-Generatoren werden
  vor dem Go-Build weggelassen; die generierten Go-Definitionen bleiben enthalten.
- github.com/bluenviron/gortsplib/v4: MIT; lesender RTSP-Gateway.
- github.com/coder/websocket: ISC; authentifizierte WebSocket-Übertragung.

Der Container führt FFmpeg als separates Programm aus. FFmpeg wird nicht in die
Go-Anwendung eingebunden.
