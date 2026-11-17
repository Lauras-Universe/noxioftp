#!/bin/sh
# Installs (or updates) the latest Noxio FTP release on macOS or Linux:
#
#   curl -fsSL https://raw.githubusercontent.com/Lauras-Universe/noxioftp/main/scripts/install.sh | sh
#
# Files downloaded with curl are not tagged as "downloaded from the internet"
# (no com.apple.quarantine attribute), so macOS Gatekeeper does not block the
# unsigned app. A download through the browser is tagged and still needs
# "Open Anyway" in System Settings > Privacy & Security.
#
# The program recognises this kind of installation (marker file `install-method`
# in its settings folder) and, when an update is available, asks you to run
# this command again instead of replacing itself.
#
# Overrides (mainly for testing): NOXIOFTP_REPO, NOXIOFTP_URL, NOXIOFTP_DIR,
# NOXIOFTP_CONFIG_DIR.
set -eu

repo="${NOXIOFTP_REPO:-Lauras-Universe/noxioftp}"
case "$(uname -s)" in
  Darwin) asset="noxioftp-macos.zip" ;;
  Linux) asset="noxioftp-linux.tar.gz" ;;
  *) echo "Nicht unterstütztes System: $(uname -s)" >&2; exit 1 ;;
esac
url="${NOXIOFTP_URL:-https://github.com/$repo/releases/latest/download/$asset}"

# settings folder, same place the program uses
case "$(uname -s)" in
  Darwin) cfg="${NOXIOFTP_CONFIG_DIR:-$HOME/Library/Application Support/NoxioFTP}" ;;
  *) cfg="${NOXIOFTP_CONFIG_DIR:-${XDG_CONFIG_HOME:-$HOME/.config}/NoxioFTP}" ;;
esac

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "Lade $url"
curl -fsSL "$url" -o "$tmp/$asset"

case "$(uname -s)" in
  Darwin)
    dest="${NOXIOFTP_DIR:-$HOME/Applications}"
    mkdir -p "$dest" "$tmp/x"
    ditto -x -k "$tmp/$asset" "$tmp/x"
    [ -d "$tmp/x/NoxioFTP.app" ] || { echo "NoxioFTP.app nicht im Archiv gefunden" >&2; exit 1; }
    rm -rf "$dest/NoxioFTP.app"
    mv "$tmp/x/NoxioFTP.app" "$dest/"
    # normally absent already; make sure in case the file came from somewhere else
    xattr -dr com.apple.quarantine "$dest/NoxioFTP.app" 2>/dev/null || true
    mkdir -p "$cfg" && printf 'script\n%s\n' "$dest/NoxioFTP.app" > "$cfg/install-method"
    echo "Installiert: $dest/NoxioFTP.app"
    ;;
  Linux)
    dest="${NOXIOFTP_DIR:-$HOME/.local/bin}"
    mkdir -p "$dest" "$tmp/x"
    tar -xzf "$tmp/$asset" -C "$tmp/x"
    [ -f "$tmp/x/NoxioFTP" ] || { echo "NoxioFTP nicht im Archiv gefunden" >&2; exit 1; }
    install -m 755 "$tmp/x/NoxioFTP" "$dest/noxioftp"
    mkdir -p "$cfg" && printf 'script\n%s\n' "$dest/noxioftp" > "$cfg/install-method"
    # launcher entry with icon, so the program shows up in the application menu
    if [ -f "$tmp/x/noxioftp.png" ]; then
      data="${XDG_DATA_HOME:-$HOME/.local/share}"
      mkdir -p "$data/icons" "$data/applications"
      cp "$tmp/x/noxioftp.png" "$data/icons/noxioftp.png"
      cat > "$data/applications/noxioftp.desktop" <<DESKTOP
[Desktop Entry]
Type=Application
Name=Noxio FTP
Comment=Sicherer Datei-Client für SFTP, FTPS und FTP
Exec=$dest/noxioftp
Icon=$data/icons/noxioftp.png
Categories=Network;FileTransfer;
Terminal=false
DESKTOP
    fi
    echo "Installiert: $dest/noxioftp (Voraussetzung: libgtk-3 und libwebkit2gtk-4.1)"
    ;;
esac
