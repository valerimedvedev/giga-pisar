# Выкладка на vmindlab.ru — задание для сессии STAT

Нужно выложить пробную страницу Гиги Писаря по адресу **https://vmindlab.ru/pisar/**.
На странице поле с голосовым вводом и мозг: GigaChat считает на сервере,
Qwen скачивается в браузер. Код лежит в ветке
`claude/new-repo-fork-package-i5w0kf` репозитория `valerimedvedev/giga-pisar`,
папка `web/`. Всё, что делается на сервере, собрано в `web/deploy/deploy.sh`.

## Шаги

1. **Найти сервер**, где nginx обслуживает vmindlab.ru (`grep -rl vmindlab /etc/nginx/`).
2. **Проверить ресурсы:** `free -g`, `nproc`, `df -h /opt /var/www`.
   - Место: около 9 ГБ в `/opt` (GigaChat 6,5 ГБ + llama.cpp) и 2,2 ГБ в `/var/www`.
   - Память: GigaChat нужно около 8 ГБ. Если всего меньше 12 ГБ, `deploy.sh`
     сам пропустит GigaChat. Тогда запускать с `--no-brain`, на странице
     останется Qwen в браузере.
3. **Доставить код на сервер.** Например, с компьютера:
   ```bash
   git clone -b claude/new-repo-fork-package-i5w0kf https://github.com/valerimedvedev/giga-pisar
   scp -r giga-pisar/web root@<сервер>:/root/pisar-web
   ```
4. **Запустить выкладку** на сервере:
   ```bash
   cd /root/pisar-web && sudo bash deploy/deploy.sh      # или --no-brain
   ```
   Скрипт качает с registry.npmjs.org, github.com и huggingface.co. Если
   сервер куда-то из них не достаёт, скачать на компьютере и положить руками:
   - `gigaam-v3-onnx-int8.tar.gz` → `/var/www/pisar/`
   - `Qwen3-4B-Instruct-2507-Q3_K_M.gguf` → `/var/www/pisar/brain-models/`
   - `GigaChat3.1-10B-A1.8B-q4_K_M.gguf` → `/opt/pisar-brain/models/`
   - `web/vendor/` можно собрать на компьютере (`bash web/fetch-vendor.sh`)
     и скопировать вместе с `web/`
   После этого запустить `deploy.sh` ещё раз: скачанное он не качает заново.
5. **nginx.** Скрипт кладёт `/etc/nginx/conf.d/pisar-http.conf` и
   `/etc/nginx/snippets/pisar-location.conf`. В server { } vmindlab.ru
   (блок с `listen 443`) добавить строку:
   ```nginx
   include /etc/nginx/snippets/pisar-location.conf;
   ```
   Потом `nginx -t && systemctl reload nginx`. Остальные location сайта не
   трогать: заголовки COOP/COEP стоят только на `/pisar/`.

## Проверка

```bash
curl -sI https://vmindlab.ru/pisar/ | grep -i -E "^HTTP|cross-origin"   # 200 + два заголовка
curl -sI https://vmindlab.ru/pisar/vendor/ort/ort-wasm-simd-threaded.wasm | grep -i content-type  # application/wasm
curl -sI https://vmindlab.ru/pisar/gigaam-v3-onnx-int8.tar.gz | head -1  # 200
curl -s  https://vmindlab.ru/pisar/brain/health                         # {"status":"ok"} (если ставили GigaChat)
curl -s  https://vmindlab.ru/pisar/brain/v1/chat/completions -H 'Content-Type: application/json' \
  -d '{"messages":[{"role":"system","content":"Исправь ошибки. Верни только текст."},{"role":"user","content":"превет как дила"}],"max_tokens":50}'
systemctl status pisar-brain --no-pager | head -5
```

В браузере открыть https://vmindlab.ru/pisar/, нажать «Скачать с этого
сайта», дождаться «Готово», продиктовать фразу. В «Мозге Писаря»
GigaChat должен быть «доступен на сервере».

## Откат

```bash
# убрать строку include из server { } vmindlab.ru, затем:
rm -f /etc/nginx/conf.d/pisar-http.conf /etc/nginx/snippets/pisar-location.conf
nginx -t && systemctl reload nginx
systemctl disable --now pisar-brain; rm -f /etc/systemd/system/pisar-brain.service /etc/default/pisar-brain
systemctl daemon-reload
rm -rf /var/www/pisar /opt/pisar-brain; userdel pisar
```
