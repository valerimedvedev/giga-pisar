# onnxruntime и наш JNI: имена нативных методов должны остаться
-keep class ai.onnxruntime.** { *; }
-keep class ru.gigapisar.app.LocalLlm { *; }
-keepclasseswithmembernames class * { native <methods>; }
