#!/bin/bash
# Собирает GigaBrain — мозг Писаря одним файлом — для Windows, macOS и Linux.
#   bash brain-local/build.sh          → brain-local/dist/GigaBrain.exe и остальные
# Нужен Go 1.22+. Внешних библиотек нет.
set -e
HERE="$(cd "$(dirname "$0")" && pwd)"
cd "$HERE/gigabrain"
cp ../catalog.json catalog.json              # каталог вшивается в программу
[ -f go.mod ] || go mod init gigabrain >/dev/null 2>&1
mkdir -p ../dist
build() {   # GOOS GOARCH имя
    echo "== $1/$2 → $3"
    CGO_ENABLED=0 GOOS=$1 GOARCH=$2 go build -trimpath -ldflags "-s -w" -o "../dist/$3" .
}
build windows amd64 GigaBrain.exe
build windows arm64 GigaBrain-arm64.exe
build darwin  arm64 GigaBrain-macos-arm64
build darwin  amd64 GigaBrain-macos-intel
build linux   amd64 GigaBrain-linux-x64
(cd ../dist && sha256sum GigaBrain* > SHA256SUMS.txt)
echo "✓ $(ls -1 ../dist | tr '\n' ' ')"
