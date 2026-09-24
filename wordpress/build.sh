#!/bin/bash
# Собирает плагин WordPress в архив dist/giga-pisar-<версия>.zip.
#
# Ядро распознавания и мозга — общее со страницей web/ (web/giga/*.js),
# движки onnxruntime-web и wllama — из web/vendor (fetch-vendor.sh).
# В git они в плагин не копируются, только в сборку.
set -e
HERE="$(cd "$(dirname "$0")" && pwd)"
WEB="$HERE/../web"
PLUGIN="$HERE/giga-pisar"
VERSION=$(sed -n 's/^ \* Version: *//p' "$PLUGIN/giga-pisar.php")

[ -f "$WEB/vendor/ort/ort.wasm.min.mjs" ] && [ -f "$WEB/vendor/wllama/index.js" ] || bash "$WEB/fetch-vendor.sh"

rm -rf "$PLUGIN/assets/giga" "$PLUGIN/assets/vendor"
mkdir -p "$PLUGIN/assets/giga"
cp "$WEB"/giga/*.js "$PLUGIN/assets/giga/"
cp -r "$WEB/vendor" "$PLUGIN/assets/vendor"

mkdir -p "$HERE/dist"
ZIP="$HERE/dist/giga-pisar-$VERSION.zip"
rm -f "$ZIP"
(cd "$HERE" && zip -qr "$ZIP" giga-pisar -x '*.DS_Store')
echo "✓ $ZIP ($(du -h "$ZIP" | cut -f1))"
