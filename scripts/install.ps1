# Installs (or updates) the latest Noxio FTP release on Windows:
#
#   irm https://raw.githubusercontent.com/Lauras-Universe/noxioftp/main/scripts/install.ps1 | iex
#
# Files downloaded with PowerShell are not marked as "from the internet"
# (no Mark-of-the-Web), so SmartScreen does not show its warning for the
# unsigned program. A download through the browser is marked and still needs
# "More info" > "Run anyway".
#
# The program recognises this kind of installation (marker file `install-method`
# in its settings folder) and, when an update is available, asks you to run
# this command again instead of replacing itself.
#
# Overrides (mainly for testing): $env:NOXIOFTP_REPO, $env:NOXIOFTP_URL,
# $env:NOXIOFTP_DIR, $env:NOXIOFTP_CONFIG_DIR.
$ErrorActionPreference = 'Stop'

$repo = if ($env:NOXIOFTP_REPO) { $env:NOXIOFTP_REPO } else { 'Lauras-Universe/noxioftp' }
$url = if ($env:NOXIOFTP_URL) { $env:NOXIOFTP_URL } else { "https://github.com/$repo/releases/latest/download/noxioftp-windows.zip" }
$dest = if ($env:NOXIOFTP_DIR) { $env:NOXIOFTP_DIR } else { Join-Path $env:LOCALAPPDATA 'Programs\NoxioFTP' }

$cfg = if ($env:NOXIOFTP_CONFIG_DIR) { $env:NOXIOFTP_CONFIG_DIR } else { Join-Path $env:APPDATA 'NoxioFTP' }

$tmp = Join-Path ([IO.Path]::GetTempPath()) ("noxioftp-" + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $tmp | Out-Null
try {
    Write-Host "Lade $url"
    $zip = Join-Path $tmp 'noxioftp-windows.zip'
    Invoke-WebRequest -Uri $url -OutFile $zip -UseBasicParsing

    New-Item -ItemType Directory -Force -Path $dest | Out-Null
    Expand-Archive -Path $zip -DestinationPath $dest -Force
    $exe = Join-Path $dest 'NoxioFTP.exe'
    if (-not (Test-Path $exe)) { throw 'NoxioFTP.exe nicht im Archiv gefunden' }
    Unblock-File -Path $exe   # removes the Mark-of-the-Web if there is one

    # Start menu entry
    $lnk = Join-Path ([Environment]::GetFolderPath('Programs')) 'Noxio FTP.lnk'
    $sc = (New-Object -ComObject WScript.Shell).CreateShortcut($lnk)
    $sc.TargetPath = $exe
    $sc.WorkingDirectory = $dest
    $sc.Save()
    # marker: the program then asks to run this command again for updates
    New-Item -ItemType Directory -Force -Path $cfg | Out-Null
    Set-Content -Path (Join-Path $cfg 'install-method') -Value @('script', $exe) -Encoding ASCII
    Write-Host "Installiert: $exe (Startmenü: Noxio FTP)"
}
finally {
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
}
