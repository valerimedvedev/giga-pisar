// Гига Писарь Диктовка — Android. Модули: engine (ядро на чистом Kotlin,
// проверяется на JVM без Android SDK) и app (само приложение: Compose +
// onnxruntime + llama.cpp). Плагины Android объявлены только в app/ —
// поэтому :engine:test работает и там, где Android SDK нет.
plugins {
    alias(libs.plugins.kotlin.jvm) apply false
}
