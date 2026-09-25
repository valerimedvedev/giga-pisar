// Ядро Писаря на чистом Kotlin: признаки, токенизатор, RNN-T, нарезка,
// мозг (промпты, команды, клиент OpenAI-совместимых серверов), скачивание.
// Ни одной зависимости от Android — поэтому проверяется на JVM:
//   ./gradlew :engine:test   (с моделью и записями — см. README)
plugins {
    alias(libs.plugins.kotlin.jvm)
    `java-library`
}
java {
    sourceCompatibility = JavaVersion.VERSION_17
    targetCompatibility = JavaVersion.VERSION_17
}
kotlin { compilerOptions { jvmTarget.set(org.jetbrains.kotlin.gradle.dsl.JvmTarget.JVM_17) } }
dependencies {
    compileOnly(libs.onnxruntime.jvm)   // на телефоне тот же пакет ai.onnxruntime из onnxruntime-android
    compileOnly(libs.json)              // org.json есть в Android; на JVM — из Maven
    testImplementation(libs.onnxruntime.jvm)
    testImplementation(libs.json)
    testImplementation(libs.junit)
}
tasks.test {
    // путь к папке модели и записям для сверки с питоновским ядром
    systemProperty("giga.model", System.getenv("GIGA_MODEL") ?: "")
    systemProperty("giga.wav", System.getenv("GIGA_WAV") ?: "")
    testLogging { events("passed", "skipped", "failed"); showStandardStreams = true }
    maxHeapSize = "2g"
}
