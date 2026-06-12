# install.ps1 — one-shot installer for loba (stable channel, Windows)
#
# Downloads the latest tagged release from GitHub, verifies the SHA-256
# checksum, installs loba.exe to %LOCALAPPDATA%\Programs\loba, and adds
# that directory to the user PATH when missing.
#
# Usage (PowerShell):
#   irm https://raw.githubusercontent.com/zwenger/TUI-LOBA/main/install.ps1 | iex
#
# Requires: Windows PowerShell 5.1+ or PowerShell 7+. No admin rights needed.

$ErrorActionPreference = 'Stop'

$Repo         = 'zwenger/TUI-LOBA'
$Binary       = 'loba.exe'
$GitHubApi    = "https://api.github.com/repos/$Repo/releases/latest"
$DownloadBase = "https://github.com/$Repo/releases/download"

# Windows PowerShell 5.1 defaults to TLS 1.0 — force TLS 1.2 for GitHub.
[Net.ServicePointManager]::SecurityProtocol = `
    [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

# ── 1. Fetch latest release tag ────────────────────────────────────────────────
Write-Host 'Buscando la última versión de loba...'
$Tag = (Invoke-RestMethod -Uri $GitHubApi -UseBasicParsing).tag_name
if (-not $Tag) {
    throw 'No se pudo obtener la versión más reciente desde GitHub.'
}
Write-Host "Versión encontrada: $Tag"

# ── 2. Build archive name and download URLs ────────────────────────────────────
# Releases ship a single Windows asset: amd64. Windows-on-ARM runs it via
# the built-in x64 emulation.
$Version      = $Tag.TrimStart('v')
$Archive      = "loba_${Version}_windows_amd64.zip"
$Url          = "$DownloadBase/$Tag/$Archive"
$ChecksumsUrl = "$DownloadBase/$Tag/checksums.txt"

# ── 3. Download to a temp directory ────────────────────────────────────────────
$Tmp = Join-Path $env:TEMP ("loba-install-" + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $Tmp | Out-Null

try {
    $ArchivePath   = Join-Path $Tmp $Archive
    $ChecksumsPath = Join-Path $Tmp 'checksums.txt'

    Write-Host "Descargando $Archive..."
    Invoke-WebRequest -Uri $Url -OutFile $ArchivePath -UseBasicParsing

    Write-Host 'Descargando checksums.txt...'
    Invoke-WebRequest -Uri $ChecksumsUrl -OutFile $ChecksumsPath -UseBasicParsing

    # ── 4. Verify checksum ─────────────────────────────────────────────────────
    Write-Host 'Verificando checksum SHA-256...'
    $ChecksumLine = Select-String -Path $ChecksumsPath -Pattern ([regex]::Escape($Archive)) |
        Select-Object -First 1
    if (-not $ChecksumLine) {
        throw "No se encontró el checksum para $Archive en checksums.txt."
    }
    $Expected = ($ChecksumLine.Line -split '\s+')[0].ToLower()
    $Actual   = (Get-FileHash -Algorithm SHA256 -Path $ArchivePath).Hash.ToLower()
    if ($Actual -ne $Expected) {
        throw "Checksum no coincide.`n  Esperado: $Expected`n  Obtenido: $Actual"
    }
    Write-Host 'Checksum OK.'

    # ── 5. Extract binary ──────────────────────────────────────────────────────
    Expand-Archive -Path $ArchivePath -DestinationPath $Tmp -Force
    $BinaryPath = Join-Path $Tmp $Binary
    if (-not (Test-Path $BinaryPath)) {
        throw "El archivo $Binary no apareció al extraer $Archive."
    }

    # ── 6. Install to %LOCALAPPDATA%\Programs\loba ─────────────────────────────
    $InstallDir = Join-Path $env:LOCALAPPDATA 'Programs\loba'
    New-Item -ItemType Directory -Path $InstallDir -Force | Out-Null
    Copy-Item -Path $BinaryPath -Destination (Join-Path $InstallDir $Binary) -Force

    # ── 7. Add the install dir to the user PATH when missing ──────────────────
    $UserPath = [Environment]::GetEnvironmentVariable('Path', 'User')
    if (($UserPath -split ';') -notcontains $InstallDir) {
        [Environment]::SetEnvironmentVariable('Path', "$UserPath;$InstallDir", 'User')
        Write-Host "Se agregó $InstallDir al PATH del usuario."
        Write-Host 'Abrí una terminal nueva para que el cambio tome efecto.'
    }

    Write-Host ''
    Write-Host "loba $Tag instalado en $InstallDir\$Binary"
    Write-Host ''
    Write-Host '  Para jugar:'
    Write-Host '    loba host --name TuNombre                  # crear sala'
    Write-Host '    loba join <host:puerto> --name TuNombre    # unirse'
    Write-Host '    loba                                       # menú interactivo'
    Write-Host ''
}
finally {
    Remove-Item -Recurse -Force $Tmp -ErrorAction SilentlyContinue
}
