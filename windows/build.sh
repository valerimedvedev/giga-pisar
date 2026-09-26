#!/bin/bash
# Собирает Гигу Писаря для Windows двумя вариантами из одного кода:
#   dist/GigaPisar.exe       — Windows 10/11 (Go 1.24, onnxruntime свежий)
#   dist/GigaPisar-win7.exe  — Windows 7/8/8.1 (Go 1.20 — последний с поддержкой Windows 7;
#                              onnxruntime 1.12.1 скачивается программой сама по версии Windows)
# Нужны Go 1.24+ и доступ к proxy.golang.org (Go 1.20.14 подтянется сам).
set -e
HERE="$(cd "$(dirname "$0")" && pwd)"
cd "$HERE/gigapisar"
if [ "$1" = "--rsrc" ]; then
    go run github.com/akavel/rsrc@v0.10.2 -manifest res/gigapisar.manifest -ico res/gigapisar.ico -arch amd64 -o rsrc_windows_amd64.syso
fi
CGO_ENABLED=0 go test ./brain/ ./core/ ./export/ >/dev/null
mkdir -p ../dist
echo "== Windows 10/11"
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w -H=windowsgui" -o ../dist/GigaPisar.exe .
echo "== Windows 7/8 (Go 1.20)"
GOTOOLCHAIN=go1.20.14 CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags "-s -w -H=windowsgui" -o ../dist/GigaPisar-win7.exe .
(cd ../dist && sha256sum GigaPisar*.exe > SHA256SUMS.txt)
echo "✓ $(ls -1 ../dist | tr '\n' ' ')"
