# Плагин на сервер M — задание для сессии STAT

Нужно выложить плагин WordPress «Гига Писарь» на сервер **M** (Москва) в
открытый доступ: чтобы любой мог скачать архив по прямой ссылке.

Код лежит в ветке `claude/new-repo-fork-package-i5w0kf` репозитория
`valerimedvedev/giga-pisar`, папка `wordpress/`.

## Сборка (на компьютере или на M)

```bash
git clone -b claude/new-repo-fork-package-i5w0kf https://github.com/valerimedvedev/giga-pisar
cd giga-pisar && bash wordpress/build.sh
# → wordpress/dist/giga-pisar-1.1.0.zip (~6 МБ)
```

`build.sh` скачивает движки с registry.npmjs.org. Если на M туда нет выхода,
собрать на компьютере и скопировать готовый zip.

## Выкладка на M

1. Выбрать сайт на M, где будет раздача (домен из nginx на M), и папку, например
   `/var/www/<сайт>/downloads/`.
2. Положить архив и ссылку на последнюю версию:
   ```bash
   mkdir -p /var/www/<сайт>/downloads
   cp giga-pisar-1.1.0.zip /var/www/<сайт>/downloads/
   ln -sfn giga-pisar-1.1.0.zip /var/www/<сайт>/downloads/giga-pisar-latest.zip
   sha256sum /var/www/<сайт>/downloads/giga-pisar-1.1.0.zip > /var/www/<сайт>/downloads/giga-pisar-1.1.0.zip.sha256
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

## Если на M есть сайты на WordPress

Можно сразу поставить плагин (только с согласия владельца сайта):

```bash
wp plugin install /path/giga-pisar-1.1.0.zip --activate --path=/var/www/<wp-сайт>
```

Потом в админке: Гига Писарь → Модели на сервере → «Скачать на сервер» у
пакета распознавания. Мозг по умолчанию выключен; включает администратор.

## Откат

```bash
rm -f /var/www/<сайт>/downloads/giga-pisar-*
# и убрать location /downloads/, если его добавляли только ради плагина
```
