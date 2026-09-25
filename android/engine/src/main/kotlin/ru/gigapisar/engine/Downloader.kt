package ru.gigapisar.engine

import java.io.File
import java.io.IOException
import java.net.HttpURLConnection
import java.net.URL
import java.util.concurrent.atomic.AtomicBoolean

/** Скачивание большого файла с докачкой после обрыва и отчётом о ходе. */
object Downloader {
    class NotFound(url: String) : IOException("файла нет по адресу $url")

    /**
     * [onProgress](скачано, всего) — всего = 0, если сервер размер не сказал.
     * [cancel] — выставить, чтобы прервать; недокачанное остаётся в .part.
     */
    fun download(url: String, dest: File, cancel: AtomicBoolean = AtomicBoolean(false),
                 onProgress: (Long, Long) -> Unit = { _, _ -> }) {
        val part = File(dest.path + ".part")
        var have = if (part.exists()) part.length() else 0L
        val c = URL(url).openConnection() as HttpURLConnection
        c.instanceFollowRedirects = true
        c.connectTimeout = 15_000; c.readTimeout = 60_000
        c.setRequestProperty("User-Agent", "giga-pisar-android")
        if (have > 0) c.setRequestProperty("Range", "bytes=$have-")
        try {
            when (c.responseCode) {
                200 -> have = 0
                206 -> {}
                404 -> throw NotFound(url)
                else -> throw IOException("сервер ответил ${c.responseCode}")
            }
            val total = if (c.contentLengthLong >= 0) have + c.contentLengthLong else 0L
            part.parentFile?.mkdirs()
            java.io.FileOutputStream(part, have > 0).use { out ->
                c.inputStream.use { inp ->
                    val buf = ByteArray(1 shl 16)
                    var done = have
                    var last = 0L
                    while (true) {
                        if (cancel.get()) throw IOException("отменено")
                        val n = inp.read(buf); if (n < 0) break
                        out.write(buf, 0, n); done += n
                        val now = System.currentTimeMillis()
                        if (now - last > 250) { last = now; onProgress(done, total) }
                    }
                    onProgress(done, total)
                }
            }
        } finally { c.disconnect() }
        if (!part.renameTo(dest)) throw IOException("не переименовался ${part.name}")
    }
}
