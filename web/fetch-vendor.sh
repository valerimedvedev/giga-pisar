#!/bin/bash
# Кладёт движки рядом со страницей, в web/vendor:
#   vendor/ort     — onnxruntime-web (распознавание речи), 14 МБ
#   vendor/wllama  — llama.cpp на WebAssembly (мозг Qwen в браузере), 9 МБ
#
# Без этого страница берёт их с CDN (jsdelivr). Своя копия нужна, чтобы:
#   - распознавание шло в несколько потоков (с чужого адреса браузер
#     не даст onnxruntime завести свои потоки);
#   - страница не зависела от CDN и открывалась без интернета.
set -e

ORT_VERSION=1.30.0
WLLAMA_VERSION=3.6.1
VENDOR="$(cd "$(dirname "$0")" && pwd)/vendor"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

fetch() {   # пакет версия → распакован в $TMP/$1
    mkdir -p "$TMP/$1"
    curl -fsSL "https://registry.npmjs.org/$2/-/$(basename "$2")-$3.tgz" | tar xz -C "$TMP/$1"
}

echo "== onnxruntime-web $ORT_VERSION"
fetch ort onnxruntime-web "$ORT_VERSION"
mkdir -p "$VENDOR/ort"
for f in ort.wasm.min.mjs ort-wasm-simd-threaded.mjs ort-wasm-simd-threaded.wasm; do
    cp "$TMP/ort/package/dist/$f" "$VENDOR/ort/"
done

echo "== wllama $WLLAMA_VERSION"
fetch wllama @wllama/wllama "$WLLAMA_VERSION"
mkdir -p "$VENDOR/wllama"
cp "$TMP/wllama/package/esm/index.js" "$VENDOR/wllama/index.js"
cp "$TMP/wllama/package/esm/wasm/wllama.wasm" "$VENDOR/wllama/wllama.wasm"

echo "✓ Готово: $(du -sh "$VENDOR" | cut -f1) в $VENDOR"
