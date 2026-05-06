#!/usr/bin/env sh
# Regenerate the README demo screenshot using termframe + rsvg-convert.
#
# Renders the fixture chart twice (initial + changed values), runs the
# bundled diffyml binary with the plugin's default flags, and captures the
# output as an SVG (then converts to PNG). The displayed prompt line is
# faked so the screenshot reads like a real `helm diffyml upgrade ...`
# session even though we don't need a Kubernetes cluster to render it.
#
# Requirements:
#   - helm
#   - diffyml (matches the version pinned in install-binary.sh)
#   - termframe (https://github.com/jonsterling/termframe)
#   - rsvg-convert (librsvg)
set -eu

HERE="$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd -P)"
ROOT="$(CDPATH='' cd -- "$HERE/.." && pwd -P)"
FIXTURE="$ROOT/test/fixture-chart"
DOCS="$ROOT/docs"

for tool in helm diffyml termframe rsvg-convert; do
  command -v "$tool" >/dev/null 2>&1 || { echo "missing required tool: $tool" >&2; exit 1; }
done

mkdir -p "$DOCS"

TMP="$(mktemp -d -t helm-diffyml-screenshot.XXXXXX)"
# shellcheck disable=SC2064 # $TMP is fully resolved at this point.
trap "rm -rf '$TMP'" EXIT INT TERM HUP

helm template my-release "$FIXTURE" -f "$FIXTURE/values.yaml"         >"$TMP/from.yaml"
helm template my-release "$FIXTURE" -f "$FIXTURE/values-changed.yaml" >"$TMP/to.yaml"

# Inner runner: fake prompt + real diffyml run.
cat >"$TMP/runner.sh" <<EOF
#!/bin/sh
printf '\033[1;36m\$\033[0m helm diffyml upgrade my-release ./chart -f values-changed.yaml\n'
diffyml --neat --mask-secrets --omit-header -o detailed --color always \\
  "$TMP/from.yaml" "$TMP/to.yaml"
EOF
chmod 0755 "$TMP/runner.sh"

OUT_SVG="$DOCS/demo.svg"
OUT_PNG="$DOCS/demo.png"

termframe \
  --width 100 \
  --height 24 \
  --show-command false \
  --window-shadow true \
  --title "helm diffyml upgrade" \
  --output "$OUT_SVG" \
  sh "$TMP/runner.sh"

rsvg-convert "$OUT_SVG" --zoom 2 -o "$OUT_PNG"

echo "wrote: $OUT_SVG"
echo "wrote: $OUT_PNG"
