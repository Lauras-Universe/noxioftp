# SFTP-Client

Ein eigener SFTP-Client im FileZilla-Stil mit eingebautem Editor.
Go + [Wails v2](https://wails.io) im Backend, Vue 3 + Pinia + Monaco im Frontend.
Läuft auf macOS, Windows und Linux.

## Funktionen

- **Zwei-Fenster-Browser**: lokal links, Server rechts; Sortierung, Pfadleiste (Klick auf freie Fläche = Pfad tippen), Tastatur (↑/↓, Enter, F2, F5, Entf, ⌘/Strg+A), Rechtsklick-Menü
- **Protokolle**: SFTP, FTPS (explizit und implizit) und FTP. FTPS-Zertifikate ohne öffentliche CA werden beim ersten Mal per Fingerabdruck bestätigt (trust on first use), eine spätere Änderung bricht die Verbindung ab. Unverschlüsseltes FTP verlangt eine ausdrückliche Bestätigung
- **Jump-Host** (wie `ProxyJump`): ein SFTP-Profil kann über ein anderes SFTP-Profil tunneln, auch verkettet. Passwort bzw. Passphrase des Jump-Hosts müssen im Profil gespeichert sein
- **Drag & Drop**: zwischen den Fenstern überträgt, innerhalb eines Fensters verschiebt (auf einen Ordner oder ein Pfad-Segment ziehen)
- **Transfer-Warteschlange**: 3 parallele Übertragungen, Ordner rekursiv (Symlinks werden aufgelöst, Schleifen und defekte Links übersprungen und gemeldet), Fortschritt, Abbrechen, Wiederholen, Konfliktregel (überschreiben / überspringen / umbenennen)
- **Synchronisierung**: „Synchron browsen“ (das andere Fenster folgt), „Vergleichen“ (Unterschiede farbig markiert) und „Abgleichen …“ (Vorschau, dann einseitig angleichen; optional überzählige Dateien löschen, neuere Dateien im Ziel schonen)
- **Verbindungsmanager**: Passwort oder SSH-Schlüssel (mit Passphrase); Geheimnisse liegen im Schlüsselbund des Betriebssystems, nie in einer Datei
- **Editor** (Monaco): Doppelklick auf eine Server-Datei, ⌘/Strg+S speichert direkt zurück, Tabs, Syntax-Highlighting. Warnt, wenn die Datei auf dem Server inzwischen geändert wurde; lehnt Binär-/Nicht-UTF-8-Dateien und Dateien > 5 MB ab
- **Beenden mit Rückfrage**, wenn der Editor ungespeicherte Änderungen hat oder Übertragungen laufen
- **Host-Key-Prüfung** (SFTP, trust on first use): unbekannte Server müssen bestätigt werden, ein geänderter Schlüssel bricht die Verbindung immer ab

## Entwickeln

Voraussetzungen: Go, Node.js, Wails CLI (`go install github.com/wailsapp/wails/v2/cmd/wails@latest`).

```bash
wails dev            # Live-Entwicklung; im Browser auch unter http://localhost:34115
wails build          # Release-Build für die aktuelle Plattform -> build/bin
go test ./...        # Backend-Tests
```

Mit lokalen Test-Servern (nur Loopback, Benutzer `test` / Passwort `test`):

```bash
go run ./tools/testsftpd -root /pfad/zum/testordner   # SFTP auf 127.0.0.1:2222
go run ./tools/testftpd  -root /pfad/zum/testordner   # FTP  auf 127.0.0.1:2121
```

Die Ausgabe der Zeile `[127.0.0.1]:2222 ssh-ed25519 …` kann in die `known_hosts`-Datei
der App kopiert werden, um den Host-Key-Dialog zu überspringen.

Konfiguration liegt unter `os.UserConfigDir()/sftp-client/` (`connections.json`, `known_hosts`, `known_certs`);
mit der Umgebungsvariable `SFTP_CLIENT_CONFIG_DIR` lässt sich ein anderer Ordner wählen.

## Aufbau

```
app.go, main.go          Wails-Bindings (alles, was das Frontend aufruft)
internal/fsinfo          gemeinsame Listing-Typen
internal/localfs         lokales Dateisystem
internal/remote          Session-Interface (gemeinsam für SFTP/FTP/FTPS)
internal/sftpclient      SSH/SFTP-Verbindung inkl. Jump-Host
internal/ftpclient       FTP/FTPS mit Verbindungspool
internal/connections     Profile, Schlüsselbund, known_hosts, Zertifikat-Pins
internal/transfer        Warteschlange, Abgleich (Sync)
frontend/src             Vue-App (stores/, components/)
tools/testsftpd, testftpd lokale Test-Server
```

## Bekannte Grenzen

- Zugriffsrechte (chmod, Rechte-Spalte) gibt es nur bei SFTP
- Bei FTP lassen sich Symlinks nicht auf Schleifen prüfen; die Ordnertiefe ist deshalb auf 64 begrenzt
- Der Abgleich ist einseitig (Quelle → Ziel), kein Zwei-Wege-Merge
- Windows-/Linux-Builds am besten über CI (Linux braucht `libgtk-3-dev` und `libwebkit2gtk-4.1-dev`)
