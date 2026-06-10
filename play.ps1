# play.ps1 — Self-updating launcher for Loba (PowerShell).
# Clone the repo once and always run the latest version:
#   .\play.ps1 host --name Alvaro
#   .\play.ps1 join <host:port> --name Pablo

# Always run from the repo root regardless of where the script was called from.
$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Definition
Set-Location $ScriptDir

# ── Update ────────────────────────────────────────────────────────────────────
Write-Host "Actualizando el juego..."
$pullResult = git pull --ff-only 2>&1
if ($LASTEXITCODE -ne 0) {
    Write-Host "Advertencia: no se pudo actualizar (sin conexion o cambios locales). Continuando con la version actual."
}

# ── Build ─────────────────────────────────────────────────────────────────────
Write-Host "Compilando..."
go build -o loba.exe .
if ($LASTEXITCODE -ne 0) {
    Write-Error "Error: fallo la compilacion. Revisa que Go este instalado correctamente."
    exit 1
}

# ── Run ───────────────────────────────────────────────────────────────────────
& .\loba.exe @args
