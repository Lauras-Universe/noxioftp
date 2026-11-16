# SFTP-Client

Ein eigener SFTP-Client im FileZilla-Stil mit eingebautem Editor.
Go + [Wails v2](https://wails.io) im Backend, Vue 3 + Pinia + Monaco im Frontend.
Läuft auf macOS, Windows und Linux.

## Funktionen

- **Zwei-Fenster-Browser**: lokal links, Server rechts; Sortierung, Pfadleiste (Klick auf freie Fläche = Pfad tippen), Tastatur (↑/↓, Enter, F2, F5, Entf, ⌘/Strg+A), Rechtsklick-Menü
- **Drag & Drop** zwischen den Fenstern, auch direkt auf einen Ordner
- **Transfer-Warteschlange**: 3 parallele Übertragungen, Ordner rekursiv, Fortschritt, Abbrechen, Wiederholen, Konfliktregel (überschreiben / überspringen / umbenennen)
- **Verbindungsmanager**: Passwort oder SSH-Schlüssel (mit Passphrase); Geheimnisse liegen im Schlüsselbund des Betriebssystems, nie in einer Datei
- **Editor** (Monaco): Doppelklick auf eine Server-Datei, ⌘/Strg+S speichert direkt zurück, Tabs, Syntax-Highlighting. Warnt, wenn die Datei auf dem Server inzwischen geändert wurde; lehnt Binär-/Nicht-UTF-8-Dateien und Dateien > 5 MB ab
- **Host-Key-Prüfung** (trust on first use): unbekannte Server müssen bestätigt werden, ein geänderter Schlüssel bricht die Verbindung immer ab

## Entwickeln

Voraussetzungen: Go, Node.js, Wails CLI (`go install github.com/wailsapp/wails/v2/cmd/wails@latest`).

```bash
wails dev            # Live-Entwicklung; im Browser auch unter http://localhost:34115
wails build          # Release-Build für die aktuelle Plattform -> build/bin
go test ./...        # Backend-Tests
```

Mit einem lokalen Test-Server (nur Loopback, Benutzer `test` / Passwort `test`):

```bash
go run ./tools/testsftpd -root /pfad/zum/testordner
```

Die Ausgabe der Zeile `[127.0.0.1]:2222 ssh-ed25519 …` kann in die `known_hosts`-Datei
der App kopiert werden, um den Host-Key-Dialog zu überspringen.

Konfiguration liegt unter `os.UserConfigDir()/sftp-client/` (`connections.json`, `known_hosts`);
mit der Umgebungsvariable `SFTP_CLIENT_CONFIG_DIR` lässt sich ein anderer Ordner wählen.

## Aufbau

```
app.go, main.go          Wails-Bindings (alles, was das Frontend aufruft)
internal/fsinfo          gemeinsame Listing-Typen
internal/localfs         lokales Dateisystem
internal/sftpclient      SSH/SFTP-Verbindung, Editor-Lesen/Schreiben
internal/connections     Profile, Schlüsselbund, known_hosts
internal/transfer        Warteschlange
frontend/src             Vue-App (stores/, components/)
tools/testsftpd          lokaler Test-Server
```

## Bekannte Grenzen (Version 1)

- Symbolische Links werden bei Ordner-Übertragungen übersprungen
- Kein Verschieben innerhalb eines Fensters per Drag & Drop
- Ungespeicherte Editor-Tabs werden beim Schließen des Programmfensters nicht abgefragt
- Kein FTP/FTPS, keine Synchronisierung, kein Jump-Host
- Windows-/Linux-Builds am besten über CI (Linux braucht `libgtk-3-dev` und `libwebkit2gtk-4.1-dev`)
# own-sftp-tool
