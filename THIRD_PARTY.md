# Drittanbieter

Diese Anwendung ist eine eigenständige Implementierung. Der CCTV Timeline Viewer
von robertoamd90 diente als funktionale Anregung; Quellcode und Assets wurden nicht
übernommen.

Die MIT-Lizenz dieses Repositorys betrifft unseren eigenen Code. Abhängigkeiten
behalten ihre jeweiligen Lizenzen. Insbesondere:

- hls.js: Apache-2.0; Lizenzhinweise bleiben im gebauten JavaScript enthalten.
- Go: BSD-3-Clause; Go-Abhängigkeiten sind in go.mod/go.sum festgehalten.
- modernc.org/sqlite: BSD-3-Clause; SQLite-Kern: Public Domain.
- FFmpeg einschließlich libx264 sowie Debian-Laufzeitpakete: jeweilige LGPL/GPL-
  und Paketlizenzen. Paket-Copyright-Dateien befinden sich im Image unter
  `/usr/share/doc`. Versionen lassen sich mit `dpkg-query -W` ermitteln.
  Debian-Quellpakete der exakt installierten Paketversionen sind über
  https://sources.debian.org/ und die Debian-Paketarchive erhältlich;
  `apt-get source ffmpeg` in einer passenden Debian-Umgebung mit aktivierten
  deb-src-Quellen lädt den Quellcode und Debian-Bauanweisungen.
- tini: MIT.

Der Container führt FFmpeg als separates Programm aus. FFmpeg wird nicht in die
Go-Anwendung eingebunden.
