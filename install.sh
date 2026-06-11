#!/bin/sh
# install.sh — one-shot installer for loba (stable channel)
#
# Downloads the latest tagged release from GitHub, verifies the SHA-256
# checksum, installs the binary to /usr/local/bin (or ~/.local/bin as
# a fallback), and prints a quickstart hint.
#
# Usage:
#   curl -sL https://raw.githubusercontent.com/zwenger/TUI-LOBA/main/install.sh | sh
#
# Requires: curl, grep, sed, tar/unzip (all standard on macOS and major
# Linux distros). Does NOT require jq.
set -eu

REPO="zwenger/TUI-LOBA"
BINARY="loba"
GITHUB_API="https://api.github.com/repos/${REPO}/releases/latest"
DOWNLOAD_BASE="https://github.com/${REPO}/releases/download"

# ── 1. Detect OS ───────────────────────────────────────────────────────────────
OS="$(uname -s)"
case "${OS}" in
  Darwin) GOOS="darwin" ;;
  Linux)  GOOS="linux"  ;;
  *)
    echo "Error: sistema operativo '${OS}' no soportado." >&2
    echo "Usuarios de Windows: descargá el .zip desde https://github.com/${REPO}/releases" >&2
    echo "o usá play.ps1 desde PowerShell para correr desde el código fuente." >&2
    exit 1
    ;;
esac

# ── 2. Detect architecture ─────────────────────────────────────────────────────
ARCH="$(uname -m)"
case "${ARCH}" in
  x86_64)          GOARCH="amd64"  ;;
  arm64 | aarch64) GOARCH="arm64"  ;;
  *)
    echo "Error: arquitectura '${ARCH}' no soportada." >&2
    echo "Descargá un binario manualmente desde https://github.com/${REPO}/releases" >&2
    exit 1
    ;;
esac

# ── 3. Fetch latest release tag (no jq required) ───────────────────────────────
echo "Buscando la última versión de loba..."
TAG="$(curl -fsSL "${GITHUB_API}" | grep '"tag_name"' | sed 's/.*"tag_name": *"\([^"]*\)".*/\1/')"

if [ -z "${TAG}" ]; then
  echo "Error: no se pudo obtener la versión más reciente desde GitHub." >&2
  exit 1
fi
echo "Versión encontrada: ${TAG}"

# ── 4. Build archive name and download URL ─────────────────────────────────────
ARCHIVE_NAME="${BINARY}_${TAG#v}_${GOOS}_${GOARCH}"
EXT="tar.gz"
ARCHIVE="${ARCHIVE_NAME}.${EXT}"
URL="${DOWNLOAD_BASE}/${TAG}/${ARCHIVE}"
CHECKSUMS_URL="${DOWNLOAD_BASE}/${TAG}/checksums.txt"

# ── 5. Download to a temp directory ───────────────────────────────────────────
TMP="$(mktemp -d)"
trap 'rm -rf "${TMP}"' EXIT

echo "Descargando ${ARCHIVE}..."
curl -fsSL -o "${TMP}/${ARCHIVE}" "${URL}"

echo "Descargando checksums.txt..."
curl -fsSL -o "${TMP}/checksums.txt" "${CHECKSUMS_URL}"

# ── 6. Verify checksum ─────────────────────────────────────────────────────────
echo "Verificando checksum SHA-256..."
# Extract the expected hash for this specific archive.
EXPECTED="$(grep "${ARCHIVE}" "${TMP}/checksums.txt" | awk '{print $1}')"
if [ -z "${EXPECTED}" ]; then
  echo "Error: no se encontró el checksum para ${ARCHIVE} en checksums.txt." >&2
  exit 1
fi

# shasum is available on macOS; sha256sum on Linux.
if command -v sha256sum >/dev/null 2>&1; then
  ACTUAL="$(sha256sum "${TMP}/${ARCHIVE}" | awk '{print $1}')"
elif command -v shasum >/dev/null 2>&1; then
  ACTUAL="$(shasum -a 256 "${TMP}/${ARCHIVE}" | awk '{print $1}')"
else
  echo "Advertencia: no se encontró sha256sum ni shasum — saltando verificación." >&2
  ACTUAL="${EXPECTED}"
fi

if [ "${ACTUAL}" != "${EXPECTED}" ]; then
  echo "Error: checksum no coincide." >&2
  echo "  Esperado: ${EXPECTED}" >&2
  echo "  Obtenido: ${ACTUAL}" >&2
  exit 1
fi
echo "Checksum OK."

# ── 7. Extract binary ──────────────────────────────────────────────────────────
tar -xzf "${TMP}/${ARCHIVE}" -C "${TMP}"

# ── 8. Choose install location ─────────────────────────────────────────────────
INSTALL_DIR="/usr/local/bin"
if [ ! -w "${INSTALL_DIR}" ]; then
  INSTALL_DIR="${HOME}/.local/bin"
  mkdir -p "${INSTALL_DIR}"
  # Warn if the fallback dir is not in PATH.
  case ":${PATH}:" in
    *":${INSTALL_DIR}:"*) ;;
    *)
      echo "Advertencia: ${INSTALL_DIR} no está en tu PATH." >&2
      echo "  Agregá esta línea a tu ~/.bashrc o ~/.zshrc:" >&2
      echo "  export PATH=\"\${HOME}/.local/bin:\${PATH}\"" >&2
      ;;
  esac
fi

# ── 9. Install ─────────────────────────────────────────────────────────────────
cp "${TMP}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
chmod +x "${INSTALL_DIR}/${BINARY}"

echo ""
echo "loba ${TAG} instalado en ${INSTALL_DIR}/${BINARY}"
echo ""
echo "  Para jugar:"
echo "    loba host --name TuNombre        # crear sala"
echo "    loba join <host:puerto> --name TuNombre   # unirse"
echo "    loba                             # menú interactivo"
echo ""
