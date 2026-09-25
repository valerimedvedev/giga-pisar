# Обновить плагин «Гига Писарь» на сайте — задание для сессии STAT

Архив: `wordpress/dist/giga-pisar-1.3.1.zip` (собрать: `bash wordpress/build.sh`
в ветке `claude/new-repo-fork-package-i5w0kf` репозитория `valerimedvedev/giga-pisar`).

Через wp-cli на сервере сайта (настройки и скачанные модели сохраняются):
```bash
wp plugin install /path/giga-pisar-1.3.1.zip --force --activate --path=/var/www/<wp-сайт>
wp plugin get giga-pisar --field=version --path=/var/www/<wp-сайт>   # → 1.3.1
```
Если на сайте кеш страниц (LiteSpeed, WP Rocket) — очистить его. Если раздача
идёт с сервера M: заменить `downloads/giga-pisar-latest.zip` (см. STAT-M.md).
