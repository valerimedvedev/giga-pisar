#!/bin/bash
# Мозг Писаря на своём компьютере — macOS и Linux.
#
# Ставит llama-server (движок llama.cpp) и нейронку в ~/.giga/brain,
# запускается командой ~/.giga/brain/start.sh. Мозг слушает
# http://127.0.0.1:8091 и отвечает только страницам, знающим ключ доступа.
#
#   bash install.sh                       # GigaChat (по умолчанию)
#   bash install.sh qwen3-14b             # другая модель из catalog.json
#   bash install.sh --url https://…/любая.gguf
#
# Нужны: curl, unzip (или tar), python3 (для чтения catalog.json).
set -euo pipefail
HERE="$(cd "$(dirname "$0")" && pwd)"
HOME_DIR="$HOME/.giga/brain"
BIN="$HOME_DIR/bin"; MODELS="$HOME_DIR/models"
PORT="${PORT:-8091}"
MODEL="${1:-gigachat}"; MODEL_URL=""
[ "${1:-}" = "--url" ] && { MODEL="custom"; MODEL_URL="$2"; }
mkdir -p "$BIN" "$MODELS"

if [ -n "$MODEL_URL" ]; then
    FILE="$(basename "$MODEL_URL")"; NAME="$FILE"; RAM=0
else
    read -r NAME FILE MODEL_URL RAM < <(python3 - "$HERE/catalog.json" "$MODEL" <<'EOF'
import json, sys
cat = json.load(open(sys.argv[1]))
m = next((m for m in cat["models"] if m["id"] == sys.argv[2]), None)
if not m:
    sys.exit("Нет модели '%s'. Есть: %s" % (sys.argv[2], ", ".join(x["id"] for x in cat["models"])))
print(m["name"].replace(" ", "_"), m["file"], m["url"], m["ram_gb"])
EOF
)
    NAME="${NAME//_/ }"
fi

case "$(uname -s)-$(uname -m)" in
    Darwin-arm64)  ASSET='bin-macos-arm64\.(zip|tar\.gz)$' ;;
    Darwin-x86_64) ASSET='bin-macos-x64\.(zip|tar\.gz)$' ;;
    Linux-x86_64)  ASSET='bin-ubuntu-x64\.(zip|tar\.gz)$' ;;
    Linux-aarch64) ASSET='bin-ubuntu-arm64\.(zip|tar\.gz)$' ;;
    *) echo "Неизвестная платформа: $(uname -s) $(uname -m)"; exit 1 ;;
esac

echo "== Мозг Писаря: $NAME (нужно около $RAM ГБ памяти)"

# 1. llama-server
if [ ! -x "$BIN/llama-server" ]; then
    echo "== Скачиваю llama.cpp"
    URL=$(curl -fsSL -H 'User-Agent: giga-pisar' https://api.github.com/repos/ggml-org/llama.cpp/releases/latest \
        | grep -o '"browser_download_url": *"[^"]*"' | sed 's/.*"\(http[^"]*\)"/\1/' | grep -E "$ASSET" | head -1)
    [ -n "$URL" ] || { echo "Не нашёл сборку llama.cpp для этой платформы: https://github.com/ggml-org/llama.cpp/releases"; exit 1; }
    TMP=$(mktemp -d); trap 'rm -rf "$TMP"' EXIT
    curl -fL --progress-bar -o "$TMP/pkg" "$URL"
    mkdir -p "$TMP/x"
    case "$URL" in *.zip) unzip -q "$TMP/pkg" -d "$TMP/x" ;; *) tar xzf "$TMP/pkg" -C "$TMP/x" ;; esac
    SRV=$(find "$TMP/x" -name llama-server -type f | head -1)
    cp -R "$(dirname "$SRV")"/* "$BIN/"
    chmod +x "$BIN/llama-server"
    echo "   ✓ llama.cpp"
else
    echo "== llama-server уже есть: $BIN/llama-server"
fi

# 2. модель (докачивается, если оборвалось)
if [ ! -s "$MODELS/$FILE" ]; then
    echo "== Скачиваю модель $FILE — это надолго"
    curl -fL --retry 5 -C - --progress-bar -o "$MODELS/$FILE.part" "$MODEL_URL"
    mv "$MODELS/$FILE.part" "$MODELS/$FILE"
else
    echo "== Модель уже есть: $MODELS/$FILE"
fi

# 3. ключ доступа
[ -s "$HOME_DIR/key.txt" ] || head -c 18 /dev/urandom | base64 | tr '+/' 'ab' | tr -d '=\n' > "$HOME_DIR/key.txt"
KEY=$(cat "$HOME_DIR/key.txt")

# 4. запуск
THREADS=$(( $(nproc 2>/dev/null || sysctl -n hw.ncpu) - 2 )); [ "$THREADS" -lt 1 ] && THREADS=1
cat > "$HOME_DIR/start.sh" <<EOF
#!/bin/bash
echo "Мозг Писаря слушает http://127.0.0.1:$PORT"
echo "Ключ доступа (вставьте на странице в окне «Мозг»): $KEY"
echo "Ctrl+C — выключить мозг."
export DYLD_LIBRARY_PATH="$BIN" LD_LIBRARY_PATH="$BIN"
exec "$BIN/llama-server" -m "$MODELS/$FILE" --host 127.0.0.1 --port $PORT --api-key "$KEY" -c 8192 --jinja -ngl 99 -t $THREADS --no-webui
EOF
chmod +x "$HOME_DIR/start.sh"

echo
echo "✓ Готово. Запуск: $HOME_DIR/start.sh"
echo "  На странице: ⚙ Мозг → «Нейронка на моём компьютере» → адрес http://127.0.0.1:$PORT"
echo "  Ключ доступа: $KEY   (лежит в $HOME_DIR/key.txt)"
