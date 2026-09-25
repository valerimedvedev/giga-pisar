#!/bin/bash
# Выкладка браузерного Гиги Писаря на сервер (Linux, nginx, systemd).
# Запускать на сервере от root из папки web/ клона репозитория:
#
#   sudo bash deploy/deploy.sh            # страница + модели + мозг GigaChat
#   sudo bash deploy/deploy.sh --no-brain # без GigaChat (мало памяти на сервере)
#
# Что делает (повторный запуск безопасен, скачанное не качает заново):
#   1. страницу → /var/www/pisar (движки onnxruntime-web и wllama — туда же)
#   2. модель распознавания GigaAM (213 МБ) → /var/www/pisar/, для кнопки
#      «Скачать с этого сайта»
#   3. Qwen3-4B (1,9 ГБ) → /var/www/pisar/brain-models/, чтобы мозг в браузере
#      качался с сайта, а не с Hugging Face
#   4. GigaChat (6,5 ГБ) + llama-server → /opt/pisar-brain, служба pisar-brain
#   5. nginx НЕ трогает: кладёт куски конфига в /etc/nginx/snippets/ и
#      пишет, что вставить в server { } сайта
set -euo pipefail

WEB_SRC="$(cd "$(dirname "$0")/.." && pwd)"
DEST=/var/www/pisar
BRAIN=/opt/pisar-brain
WITH_BRAIN=1
[ "${1:-}" = "--no-brain" ] && WITH_BRAIN=0

GIGAAM_URL="https://github.com/moznoazachem/giga-pisar-cli/releases/download/v1.0/gigaam-v3-onnx-int8.tar.gz"
QWEN_FILE="Qwen3-4B-Instruct-2507-Q3_K_M.gguf"
QWEN_URL="https://huggingface.co/unsloth/Qwen3-4B-Instruct-2507-GGUF/resolve/main/$QWEN_FILE"
GIGACHAT_FILE="GigaChat3.1-10B-A1.8B-q4_K_M.gguf"
GIGACHAT_URL="https://huggingface.co/ai-sage/GigaChat3.1-10B-A1.8B-GGUF/resolve/main/$GIGACHAT_FILE"

say() { printf '\n== %s\n' "$*"; }
# скачать, если файла ещё нет; докачивает оборванное (-C -)
get() {
    local url="$1" out="$2"
    if [ -s "$out" ] && [ ! -e "$out.part" ]; then echo "   уже есть: $out"; return 0; fi
    mkdir -p "$(dirname "$out")"
    touch "$out.part"
    curl -fL --retry 3 -C - --progress-bar -o "$out" "$url"
    rm -f "$out.part"
}

[ "$(id -u)" = 0 ] || { echo "Запускать от root (sudo)"; exit 1; }
for c in curl tar; do command -v $c >/dev/null || { echo "Нужен $c"; exit 1; }; done

say "1/5 Страница → $DEST"
[ -f "$WEB_SRC/vendor/ort/ort.wasm.min.mjs" ] && [ -f "$WEB_SRC/vendor/wllama/index.js" ] \
    || bash "$WEB_SRC/fetch-vendor.sh"
mkdir -p "$DEST"
cp -r "$WEB_SRC/index.html" "$WEB_SRC/app.js" "$WEB_SRC/app.css" "$WEB_SRC/giga" "$WEB_SRC/vendor" "$WEB_SRC/word" "$DEST/"
chmod -R a+rX "$DEST"

say "2/5 Модель распознавания GigaAM (213 МБ)"
get "$GIGAAM_URL" "$DEST/gigaam-v3-onnx-int8.tar.gz"

say "3/5 Мозг для браузера: Qwen3-4B (1,9 ГБ)"
if ! get "$QWEN_URL" "$DEST/brain-models/$QWEN_FILE"; then
    rm -f "$DEST/brain-models/$QWEN_FILE" "$DEST/brain-models/$QWEN_FILE.part"
    echo "   ! Не скачался с Hugging Face. Страница будет качать Qwen у людей"
    echo "     напрямую с Hugging Face. Можно положить файл руками в $DEST/brain-models/"
fi
chmod -R a+rX "$DEST"

if [ "$WITH_BRAIN" = 1 ]; then
    say "4/5 Мозг на сервере: GigaChat + llama-server"
    MEM_GB=$(awk '/MemTotal/ {printf "%d", $2/1024/1024}' /proc/meminfo)
    CORES=$(nproc)
    echo "   память: ${MEM_GB} ГБ, ядер: $CORES"
    if [ "$MEM_GB" -lt 12 ]; then
        echo "   ! Меньше 12 ГБ памяти: GigaChat (нужно ~8 ГБ) задавит остальные службы."
        echo "     Пропускаю. Мозг на странице будет только Qwen (в браузере)."
        WITH_BRAIN=0
    fi
fi

if [ "$WITH_BRAIN" = 1 ]; then
    id pisar >/dev/null 2>&1 || useradd --system --home "$BRAIN" --shell /usr/sbin/nologin pisar
    mkdir -p "$BRAIN/bin" "$BRAIN/models"

    if [ ! -x "$BRAIN/bin/llama-server" ]; then
        # свежая сборка llama.cpp под Linux x64 с GitHub
        ASSET=$(curl -fsSL https://api.github.com/repos/ggml-org/llama.cpp/releases/latest \
            | grep -o '"browser_download_url": *"[^"]*bin-ubuntu-x64\.\(zip\|tar\.gz\)"' \
            | head -1 | sed 's/.*"\(http[^"]*\)"/\1/')
        [ -n "$ASSET" ] || { echo "   ! Не нашёл сборку llama.cpp для Linux x64"; exit 1; }
        TMP=$(mktemp -d)
        curl -fL --progress-bar -o "$TMP/llama.pkg" "$ASSET"
        case "$ASSET" in
            *.zip) command -v unzip >/dev/null || apt-get install -y -q unzip; unzip -q "$TMP/llama.pkg" -d "$TMP/x" ;;
            *) mkdir -p "$TMP/x"; tar xzf "$TMP/llama.pkg" -C "$TMP/x" ;;
        esac
        SRV=$(find "$TMP/x" -name llama-server -type f | head -1)
        cp "$(dirname "$SRV")"/* "$BRAIN/bin/"
        chmod +x "$BRAIN/bin/llama-server"
        rm -rf "$TMP"
    fi
    LD_LIBRARY_PATH="$BRAIN/bin" "$BRAIN/bin/llama-server" --version 2>&1 | head -2 || true

    get "$GIGACHAT_URL" "$BRAIN/models/$GIGACHAT_FILE"
    chown -R pisar:pisar "$BRAIN"

    # половина ядер: сайт и остальные службы должны жить дальше
    echo "PISAR_THREADS=$(( CORES > 1 ? CORES / 2 : 1 ))" > /etc/default/pisar-brain
    cp "$WEB_SRC/deploy/pisar-brain.service" /etc/systemd/system/
    systemctl daemon-reload
    systemctl enable --now pisar-brain
    systemctl restart pisar-brain
    echo "   жду, пока GigaChat загрузится в память…"
    for i in $(seq 1 90); do
        curl -fs http://127.0.0.1:8091/health >/dev/null 2>&1 && { echo "   ✓ мозг отвечает"; break; }
        sleep 2
    done
    curl -fs http://127.0.0.1:8091/health >/dev/null 2>&1 || echo "   ! мозг не поднялся: journalctl -u pisar-brain -n 50"
else
    say "4/5 Мозг на сервере пропущен"
fi

say "5/5 nginx"
mkdir -p /etc/nginx/snippets
cp "$WEB_SRC/deploy/nginx/pisar-location.conf" /etc/nginx/snippets/pisar-location.conf
cp "$WEB_SRC/deploy/nginx/pisar-http.conf" /etc/nginx/conf.d/pisar-http.conf
cat <<MSG
   Куски конфига разложены:
     /etc/nginx/conf.d/pisar-http.conf        (http: лимит запросов к мозгу)
     /etc/nginx/snippets/pisar-location.conf  (страница и мозг)
   Осталось вставить в server { } сайта (блок с listen 443):
     include /etc/nginx/snippets/pisar-location.conf;
   и проверить:  nginx -t && systemctl reload nginx

✓ Готово. Страница: https://<сайт>/pisar/
MSG
