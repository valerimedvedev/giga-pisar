# Плагин на сервер M — задание для сессии STAT

Нужно выложить плагин WordPress «Гига Писарь» на сервер **M** (Москва) в
открытый доступ: чтобы любой мог скачать архив по прямой ссылке.

Код лежит в ветке `claude/new-repo-fork-package-i5w0kf` репозитория
`valerimedvedev/giga-pisar`, папка `wordpress/`.

## Сборка (на компьютере или на M)

```bash
git clone -b claude/new-repo-fork-package-i5w0kf https://github.com/valerimedvedev/giga-pisar
cd giga-pisar && bash wordpress/build.sh
# → wordpress/dist/giga-pisar-1.3.1.zip (~6 МБ)
```

`build.sh` скачивает движки с registry.npmjs.org. Если на M туда нет выхода,
собрать на компьютере и скопировать готовый zip.

## Выкладка на M

1. Выбрать сайт на M, где будет раздача (домен из nginx на M), и папку, например
   `/var/www/<сайт>/downloads/`.
2. Положить архив и ссылку на последнюю версию:
   ```bash
   mkdir -p /var/www/<сайт>/downloads
   cp giga-pisar-1.3.1.zip /var/www/<сайт>/downloads/
   ln -sfn giga-pisar-1.3.1.zip /var/www/<сайт>/downloads/giga-pisar-latest.zip
   sha256sum /var/www/<сайт>/downloads/giga-pisar-1.3.1.zip > /var/www/<сайт>/downloads/giga-pisar-1.3.1.zip.sha256
   ```
3. Если `/downloads/` ещё не раздаётся, добавить в server { } сайта:
   ```nginx
   location /downloads/ {
       alias /var/www/<сайт>/downloads/;
       autoindex on;
       types { application/zip zip; text/plain sha256; }
   }
   ```
   Затем `nginx -t && systemctl reload nginx`.
4. Проверить: `curl -sI https://<сайт>/downloads/giga-pisar-latest.zip` должен отдать `200`
   и `application/zip`. Сообщить ссылку.

## GigaBrain — рядом с плагином

Собрать: `bash brain-local/build.sh` (нужен Go 1.24+ и доступ к proxy.golang.org) → `brain-local/dist/`.
Выложить в ту же папку `downloads/`: `GigaBrain.exe`, `GigaBrain-macos-arm64`,
`GigaBrain-macos-intel`, `GigaBrain-linux-x64`, `SHA256SUMS.txt`. В nginx
добавить тип: `application/octet-stream exe;` (в `types { }` того же location).

## Если на M есть сайты на WordPress

Можно сразу поставить плагин (только с согласия владельца сайта):

```bash
wp plugin install /path/giga-pisar-1.3.1.zip --activate --path=/var/www/<wp-сайт>
```

Потом в админке: Гига Писарь → Модели на сервере → «Скачать на сервер» у
пакета распознавания. Мозг по умолчанию выключен; включает администратор.

## Откат

```bash
rm -f /var/www/<сайт>/downloads/giga-pisar-*
# и убрать location /downloads/, если его добавляли только ради плагина
```
