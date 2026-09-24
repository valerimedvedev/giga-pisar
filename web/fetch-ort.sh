#!/bin/bash
# Кладёт onnxruntime-web рядом со страницей, в web/vendor/ort.
#
# Без этого страница берёт его с CDN (jsdelivr). Своя копия нужна, чтобы:
#   - распознавание шло в несколько потоков (с чужого адреса браузер
#     не даст onnxruntime завести свои потоки);
#   - страница не зависела от CDN и открывалась без интернета.
set -e

VERSION=1.30.0
DEST="$(cd "$(dirname "$0")" && pwd)/vendor/ort"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

echo "== onnxruntime-web $VERSION → $DEST"
curl -fL --progress-bar "https://registry.npmjs.org/onnxruntime-web/-/onnxruntime-web-$VERSION.tgz" \
    | tar xz -C "$TMP"
mkdir -p "$DEST"
for f in ort.wasm.min.mjs ort-wasm-simd-threaded.mjs ort-wasm-simd-threaded.wasm; do
    cp "$TMP/package/dist/$f" "$DEST/"
done
echo "✓ Готово: $(du -sh "$DEST" | cut -f1)"
