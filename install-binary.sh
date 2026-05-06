#!/usr/bin/env sh
# Helm plugin install hook. Two flows:
#
#   - If a freshly built `bin/helm-diffyml` exists in $HELM_PLUGIN_DIR (developer
#     ran `make build` before `helm plugin install file://$PWD`), use it.
#   - Otherwise download the matching release tarball from GitHub, verify its
#     SHA-256, and place the binary in $HELM_PLUGIN_DIR/bin/helm-diffyml.
#
# Linux + macOS, amd64 + arm64. Windows is out of scope.
set -eu

if [ -z "${HELM_PLUGIN_DIR:-}" ]; then
  echo "install-binary.sh: HELM_PLUGIN_DIR is not set; run via 'helm plugin install'." >&2
  exit 1
fi

PLUGIN_DIR="$HELM_PLUGIN_DIR"

# Discover plugin version (used for the release URL).
VERSION="$(awk -F'"' '/^version:/ {print $2; exit}' "$PLUGIN_DIR/plugin.yaml" 2>/dev/null || true)"
[ -n "$VERSION" ] || { echo "install-binary.sh: could not read plugin.yaml version" >&2; exit 1; }
TAG="v$VERSION"

# Dev path: a local build was committed/copied alongside the source.
if [ -x "$PLUGIN_DIR/bin/helm-diffyml" ] && file "$PLUGIN_DIR/bin/helm-diffyml" 2>/dev/null | grep -q -E 'executable|Mach-O|ELF'; then
  echo "helm-diffyml: using locally-built binary at $PLUGIN_DIR/bin/helm-diffyml"
  "$PLUGIN_DIR/bin/helm-diffyml" version | head -n 1 || true
  exit 0
fi

OS_RAW="$(uname -s)"
case "$OS_RAW" in
  Linux)  OS="linux" ;;
  Darwin) OS="darwin" ;;
  *) echo "install-binary.sh: unsupported OS '$OS_RAW' (linux, darwin only)." >&2; exit 1 ;;
esac

ARCH_RAW="$(uname -m)"
case "$ARCH_RAW" in
  x86_64|amd64)  ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) echo "install-binary.sh: unsupported arch '$ARCH_RAW' (amd64, arm64 only)." >&2; exit 1 ;;
esac

RELEASE_BASE_URL="${HELM_DIFFYML_RELEASE_BASE_URL:-https://github.com/szhekpisov/helm-diffyml/releases/download}"
ARCHIVE="helm-diffyml_${VERSION}_${OS}_${ARCH}.tar.gz"
URL="${RELEASE_BASE_URL}/${TAG}/${ARCHIVE}"
CHECKSUMS_URL="${RELEASE_BASE_URL}/${TAG}/checksums.txt"

if command -v curl >/dev/null 2>&1; then
  fetch() { curl -fsSL --retry 3 --retry-delay 1 -o "$2" "$1"; }
elif command -v wget >/dev/null 2>&1; then
  fetch() { wget -q -O "$2" "$1"; }
else
  echo "install-binary.sh: need either curl or wget on PATH." >&2
  exit 1
fi

TMP="$(mktemp -d -t helm-diffyml-install.XXXXXX)"
# shellcheck disable=SC2064 # $TMP is fully resolved at this point.
trap "rm -rf '$TMP'" EXIT INT TERM HUP

echo "helm-diffyml: downloading $TAG for ${OS}/${ARCH}..."
fetch "$URL"           "$TMP/$ARCHIVE"
fetch "$CHECKSUMS_URL" "$TMP/checksums.txt"

# Verify SHA-256 against the official checksums.txt.
EXPECTED="$(awk -v n="$ARCHIVE" '$2 == n || $2 == "*" n { print $1; exit }' "$TMP/checksums.txt")"
[ -n "$EXPECTED" ] || { echo "install-binary.sh: no checksum entry for $ARCHIVE" >&2; exit 1; }
if command -v sha256sum >/dev/null 2>&1; then
  ACTUAL="$(sha256sum "$TMP/$ARCHIVE" | awk '{print $1}')"
elif command -v shasum >/dev/null 2>&1; then
  ACTUAL="$(shasum -a 256 "$TMP/$ARCHIVE" | awk '{print $1}')"
else
  echo "install-binary.sh: need sha256sum or shasum on PATH." >&2
  exit 1
fi
[ "$EXPECTED" = "$ACTUAL" ] || {
  echo "install-binary.sh: checksum mismatch for $ARCHIVE" >&2
  echo "  expected: $EXPECTED" >&2
  echo "  actual:   $ACTUAL"   >&2
  exit 1
}

mkdir -p "$PLUGIN_DIR/bin"
( cd "$TMP" && tar -xzf "$ARCHIVE" )
[ -f "$TMP/helm-diffyml" ] || { echo "install-binary.sh: archive did not contain a helm-diffyml binary at the root." >&2; exit 1; }
mv "$TMP/helm-diffyml" "$PLUGIN_DIR/bin/helm-diffyml"
chmod 0755 "$PLUGIN_DIR/bin/helm-diffyml"

echo "helm-diffyml: installed $("$PLUGIN_DIR/bin/helm-diffyml" version | head -n 1)"
