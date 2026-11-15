#!/bin/sh
# TaskSmith installer for macOS and Linux.
#
# Usage:
#   curl -fsSL https://github.com/masterkeysrd/tasksmith/releases/latest/download/install.sh | sh
#
# Environment variables / flags:
#   VERSION=v0.0.3        Install a specific release tag (default: latest)
#   INSTALL_DIR=~/.local/bin  Target directory for the binary (default: $HOME/.local/bin)

set -eu

REPO="masterkeysrd/tasksmith"
BINARY="tasksmith"
VERSION="${VERSION:-latest}"
INSTALL_DIR="${INSTALL_DIR:-}"

usage() {
  cat <<'EOF'
Usage: install.sh [-v VERSION] [-b INSTALL_DIR] [-h]

Options:
  -v, --version VERSION      Release version to install (default: latest)
  -b, --bin-dir INSTALL_DIR  Installation directory (default: ~/.local/bin)
  -h, --help                 Show this help message
EOF
}

while [ "$#" -gt 0 ]; do
  case "$1" in
    -v|--version)
      [ "$#" -ge 2 ] || { echo "error: $1 requires a value" >&2; exit 1; }
      VERSION="$2"
      shift 2
      ;;
    -b|--bin-dir)
      [ "$#" -ge 2 ] || { echo "error: $1 requires a value" >&2; exit 1; }
      INSTALL_DIR="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "error: unknown argument: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

if [ -z "$INSTALL_DIR" ]; then
  if [ -z "${HOME:-}" ]; then
    echo "error: HOME is not set; specify INSTALL_DIR or -b <dir>" >&2
    exit 1
  fi
  INSTALL_DIR="$HOME/.local/bin"
fi

detect_os() {
  uname_s="$(uname -s)"
  case "$uname_s" in
    Darwin) echo "Darwin" ;;
    Linux)  echo "Linux" ;;
    *)
      echo "error: unsupported operating system '$uname_s' (TaskSmith supports macOS and Linux)" >&2
      exit 1
      ;;
  esac
}

detect_arch() {
  uname_m="$(uname -m)"
  case "$uname_m" in
    x86_64|amd64)  echo "x86_64" ;;
    arm64|aarch64) echo "arm64" ;;
    *)
      echo "error: unsupported architecture '$uname_m' (supported: x86_64, arm64)" >&2
      exit 1
      ;;
  esac
}

download() {
  url="$1"
  dest="$2"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url" -o "$dest"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "$dest" "$url"
  else
    echo "error: curl or wget is required to download TaskSmith" >&2
    exit 1
  fi
}

sha256_file() {
  file="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$file" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$file" | awk '{print $1}'
  elif command -v openssl >/dev/null 2>&1; then
    openssl dgst -sha256 "$file" | awk '{print $NF}'
  else
    echo "error: sha256sum, shasum, or openssl is required to verify checksums" >&2
    exit 1
  fi
}

OS="$(detect_os)"
ARCH="$(detect_arch)"
ARCHIVE="${BINARY}_${OS}_${ARCH}.tar.gz"

if [ "$VERSION" = "latest" ]; then
  BASE_URL="https://github.com/${REPO}/releases/latest/download"
else
  case "$VERSION" in
    v*) TAG="$VERSION" ;;
    *)  TAG="v$VERSION" ;;
  esac
  BASE_URL="https://github.com/${REPO}/releases/download/${TAG}"
fi

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT INT TERM

echo "Downloading ${ARCHIVE} (${VERSION})..."
download "${BASE_URL}/${ARCHIVE}" "${TMP_DIR}/${ARCHIVE}"
download "${BASE_URL}/checksums.txt" "${TMP_DIR}/checksums.txt"

EXPECTED_SUM="$(awk -v target="$ARCHIVE" '$2 == target || $2 == "*" target { print $1; exit }' "${TMP_DIR}/checksums.txt")"
if [ -z "$EXPECTED_SUM" ]; then
  echo "error: checksum for ${ARCHIVE} not found in checksums.txt" >&2
  exit 1
fi

ACTUAL_SUM="$(sha256_file "${TMP_DIR}/${ARCHIVE}")"
if [ "$ACTUAL_SUM" != "$EXPECTED_SUM" ]; then
  echo "error: SHA-256 checksum mismatch for ${ARCHIVE}" >&2
  echo "  expected: ${EXPECTED_SUM}" >&2
  echo "  actual:   ${ACTUAL_SUM}" >&2
  exit 1
fi

tar -xzf "${TMP_DIR}/${ARCHIVE}" -C "$TMP_DIR" "${BINARY}"
chmod 0755 "${TMP_DIR}/${BINARY}"

if [ "$OS" = "Darwin" ] && command -v xattr >/dev/null 2>&1; then
  xattr -dr com.apple.quarantine "${TMP_DIR}/${BINARY}" 2>/dev/null || true
fi

mkdir -p "$INSTALL_DIR"
mv "${TMP_DIR}/${BINARY}" "${INSTALL_DIR}/${BINARY}"

echo "Installed ${BINARY} to ${INSTALL_DIR}/${BINARY}"

case ":${PATH:-}:" in
  *":${INSTALL_DIR}:"*) ;;
  *)
    echo ""
    echo "Note: ${INSTALL_DIR} is not in your PATH. Add it to your shell profile:"
    echo "  export PATH=\"${INSTALL_DIR}:\$PATH\""
    ;;
esac
