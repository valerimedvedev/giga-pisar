# Надстройка Word — задание для сессии STAT

1. На сервере vmindlab.ru обновить страницу из ветки
   `claude/new-repo-fork-package-i5w0kf` (`git pull` в клоне) и повторить
   `sudo bash web/deploy/deploy.sh` — она скопирует `web/word/` в `/var/www/pisar/word/`.
   Сниппет nginx `web/deploy/nginx/pisar-location.conf` изменился: скопировать
   его в `/etc/nginx/snippets/pisar-location.conf`, `nginx -t && systemctl reload nginx`.
2. Проверить: `curl -sI https://vmindlab.ru/pisar/word/taskpane.html` → 200,
   в заголовках нет `Cross-Origin-Embedder-Policy`;
   `curl -sI https://vmindlab.ru/pisar/word/manifest.xml` → 200, `text/xml`.
3. На компьютере с Word: скачать `https://vmindlab.ru/pisar/word/manifest.xml`
   в «Загрузки» и выполнить в PowerShell:
   ```powershell
   $m = "$env:USERPROFILE\Downloads\manifest.xml"
   New-Item -Path "HKCU:\Software\Microsoft\Office\16.0\WEF\Developer" -Force | Out-Null
   Set-ItemProperty -Path "HKCU:\Software\Microsoft\Office\16.0\WEF\Developer" -Name "GigaPisar" -Value $m
   ```
   Перезапустить Word → «Главная» → «Гига Писарь». В панели: «Скачать с сайта»
   (пакет распознавания), затем «Запись». Сообщить, дал ли Word микрофон
   (если нет — версию Word: Файл → Учётная запись → О программе).
