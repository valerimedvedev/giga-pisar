// Гига Писарь Диктовка — Android. Модули: engine (ядро на чистом Kotlin,
// проверяется на JVM без Android SDK) и app (само приложение: Compose +
// onnxruntime + llama.cpp). Плагины объявляют сами модули (версии — в
// gradle/libs.versions.toml); здесь ничего не подключается, иначе Gradle
// видит плагин Kotlin «уже на classpath» и отказывает app в его версии.
