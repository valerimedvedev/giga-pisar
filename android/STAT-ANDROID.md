# Собрать приложение для Android — задание для сессии STAT

Нужно собрать APK «Гига Писарь Диктовка» и поставить на Galaxy S23 Ultra.
Код: ветка `claude/new-repo-fork-package-i5w0kf` репозитория
`valerimedvedev/giga-pisar`, папка `android/`. Собирается только там, где есть
Android SDK; в облачной сессии его нет.

## На компьютере с Windows (проще всего)

1. Поставить Android Studio (https://developer.android.com/studio) и Git
   (https://git-scm.com) — Git нужен: при первой сборке CMake скачивает llama.cpp.
2. Android Studio → *More Actions → SDK Manager*:
   * SDK Platforms: **Android 15 (API 35)**;
   * SDK Tools: **NDK (Side by side)**, **CMake 3.22.1**, Android SDK Build-Tools 35.
3. Клонировать и открыть:
   ```
   git clone -b claude/new-repo-fork-package-i5w0kf https://github.com/valerimedvedev/giga-pisar
   ```
   *File → Open* → `giga-pisar\android`. Дождаться Gradle Sync.
4. *Build → Build App Bundle(s) / APK(s) → Build APK(s)*. Готовый файл:
   `android\app\build\outputs\apk\release\app-release.apk` (или debug —
   для установки себе разницы нет). Либо в терминале Android Studio:
   `gradlew :app:assembleRelease`.
5. На телефоне: включить *Параметры разработчика → Отладка по USB*, подключить
   кабелем, в Android Studio нажать *Run* (зелёный треугольник) — приложение
   встанет и запустится. Или скопировать APK на телефон и открыть его.

Первая сборка — 10–20 минут (компилируется llama.cpp). Если llama.cpp не
скачался (нет git или сети), скачать вручную
https://github.com/ggml-org/llama.cpp/archive/refs/tags/v0.5.0.zip, распаковать и
в `app/build.gradle.kts` → `externalNativeBuild.cmake.arguments` добавить
`"-DLLAMA_DIR=C:/путь/llama.cpp-0.5.0"`.

## На телефоне после установки

1. Открыть «Гига Писарь» → «Запись» → разрешить микрофон → согласиться скачать
   пакет распознавания (213 МБ, один раз).
2. Настройки → Мозг → выбрать, где считает:
   * *На телефоне*: скачать Qwen3-4B (2,5 ГБ) или Qwen3-1.7B (1,1 ГБ, быстрее);
   * *На компьютере*: в окне GigaBrain на ПК включить галочку «Доступ с телефона
     по домашней сети», на телефоне нажать «Найти в сети», вставить ключ из окна
     GigaBrain, «Проверить и сохранить».
3. Проверка скорости: надиктовать 3–4 фразы с паузами — они должны появляться
   в поле по ходу речи; после «Стоп» текст дописывается за долю секунды.
   Сказать «…, Писарь, сократи» в конце — текст пройдёт через мозг.

Сообщить: версия Android, время до появления первой фразы, время правки
мозгом на телефоне (строка статуса показывает миллисекунды).
