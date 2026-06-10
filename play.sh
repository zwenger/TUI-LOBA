#!/usr/bin/env bash
# play.sh — Self-updating launcher for Loba.
# Clone the repo once and always run the latest version:
#   ./play.sh host --name Alvaro
#   ./play.sh join <host:port> --name Pablo

set -euo pipefail

# Always run from the repo root regardless of where the script was invoked from.
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$SCRIPT_DIR"

# ── Update ────────────────────────────────────────────────────────────────────
echo "Actualizando el juego..."
if ! git pull --ff-only 2>&1; then
    echo "Advertencia: no se pudo actualizar (sin conexión o cambios locales). Continuando con la versión actual."
fi

# ── Build ─────────────────────────────────────────────────────────────────────
echo "Compilando..."
if ! go build -o loba .; then
    echo "Error: falló la compilación. Revisá que Go esté instalado correctamente."
    exit 1
fi

# ── Run ───────────────────────────────────────────────────────────────────────
exec ./loba "$@"
