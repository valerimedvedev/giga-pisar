#!/bin/bash
# Собирает GigaBrain — мозг Писаря одним файлом — для Windows, macOS и Linux.
#   bash brain-local/build.sh          → brain-local/dist/GigaBrain.exe и остальные
# Нужен Go 1.24+. Библиотека окна (github.com/lxn/walk) скачивается из
# proxy.golang.org при первой сборке. Иконка и манифест Windows уже лежат
# в gigabrain/rsrc_windows_*.syso (пересобрать: bash build.sh --rsrc).
set -e
HERE="$(cd "$(dirname "$0")" && pwd)"
cd "$HERE/gigabrain"
cp ../catalog.json catalog.json              # каталог вшивается в программу
if [ "$1" = "--rsrc" ]; then
    python3 res/mkicon.py
    go run github.com/akavel/rsrc@v0.10.2 -manifest res/gigabrain.manifest -ico res/gigabrain.ico -arch amd64 -o rsrc_windows_amd64.syso
    go run github.com/akavel/rsrc@v0.10.2 -manifest res/gigabrain.manifest -ico res/gigabrain.ico -arch arm64 -o rsrc_windows_arm64.syso
fi
go test ./... >/dev/null
mkdir -p ../dist
build() {   # GOOS GOARCH имя [ldflags]
    echo "== $1/$2 → $3"
    CGO_ENABLED=0 GOOS=$1 GOARCH=$2 go build -trimpath -ldflags "-s -w $4" -o "../dist/$3" .
}
build windows amd64 GigaBrain.exe        "-H=windowsgui"   # окно без консоли
build windows arm64 GigaBrain-arm64.exe  "-H=windowsgui"
build darwin  arm64 GigaBrain-macos-arm64
build darwin  amd64 GigaBrain-macos-intel
build linux   amd64 GigaBrain-linux-x64
(cd ../dist && sha256sum GigaBrain* > SHA256SUMS.txt)
echo "✓ $(ls -1 ../dist | tr '\n' ' ')"
