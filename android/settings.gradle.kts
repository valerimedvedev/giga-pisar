pluginManagement {
    repositories {
        google()
        mavenCentral()
        gradlePluginPortal()
    }
}
dependencyResolutionManagement {
    repositoriesMode.set(RepositoriesMode.FAIL_ON_PROJECT_REPOS)
    repositories {
        google()
        mavenCentral()
    }
}
rootProject.name = "GigaPisar"
include(":engine")
// Само приложение — только при наличии Android SDK: ядро (engine) собирается
// и проверяется и без него (см. README: «Проверка ядра без Android»).
if (System.getenv("ANDROID_HOME") != null || System.getenv("ANDROID_SDK_ROOT") != null ||
    File(rootDir, "local.properties").let { it.exists() && it.readText().contains("sdk.dir") }) {
    include(":app")
}
