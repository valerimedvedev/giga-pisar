package ru.gigapisar.app

import android.content.Context
import android.net.Uri
import ru.gigapisar.engine.Catalog
import ru.gigapisar.engine.Downloader
import ru.gigapisar.engine.LlmModel
import ru.gigapisar.engine.Rnnt
import ru.gigapisar.engine.Tar
import java.io.File
import java.io.IOException
import java.util.concurrent.atomic.AtomicBoolean

/** Файлы моделей на телефоне: пакет распознавания и нейронки .gguf. */
class Models(private val context: Context) {
    val gigaamDir = File(context.filesDir, "gigaam")
    val llmDir = File(context.filesDir, "llm").apply { mkdirs() }

    fun hasGigaAm() = Rnnt.hasModel(gigaamDir)

    /** Скачивает архив во временную папку и распаковывает нужные пять файлов. */
    fun downloadGigaAm(cancel: AtomicBoolean, onProgress: (Long, Long) -> Unit) {
        val tmp = File(context.cacheDir, "gigaam.tar.gz")
        Downloader.download(Catalog.GIGAAM_URL, tmp, cancel) { d, t -> onProgress(d, if (t > 0) t else Catalog.GIGAAM_BYTES) }
        onProgress(0, 0)
        tmp.inputStream().use { Tar.extract(it, gigaamDir, Rnnt.FILES.toSet()) }
        tmp.delete()
        if (!hasGigaAm()) throw IOException("в архиве не оказалось файлов модели")
    }

    fun deleteGigaAm() { gigaamDir.deleteRecursively() }

    fun llmFile(m: LlmModel) = File(llmDir, m.file)
    fun hasLlm(m: LlmModel) = llmFile(m).length() > 0
    fun downloadLlm(m: LlmModel, cancel: AtomicBoolean, onProgress: (Long, Long) -> Unit) =
        Downloader.download(m.url, llmFile(m), cancel) { d, t -> onProgress(d, if (t > 0) t else (m.sizeGb * 1e9).toLong()) }

    /** Все .gguf в папке — из каталога и свои. */
    fun installedLlm(): List<File> = llmDir.listFiles { f -> f.name.endsWith(".gguf") }?.sortedBy { it.name } ?: emptyList()

    fun llmByChoice(choice: String): File? {
        Catalog.byId(choice)?.let { m -> return llmFile(m).takeIf { it.length() > 0 } }
        return File(llmDir, choice).takeIf { it.name.endsWith(".gguf") && it.length() > 0 }
    }

    /** Свой .gguf с телефона: копируется в папку приложения. */
    fun importLlm(uri: Uri, cancel: AtomicBoolean, onProgress: (Long, Long) -> Unit): File {
        val name = context.contentResolver.query(uri, null, null, null, null)?.use { c ->
            val i = c.getColumnIndex(android.provider.OpenableColumns.DISPLAY_NAME); if (c.moveToFirst() && i >= 0) c.getString(i) else null
        } ?: "model.gguf"
        if (!name.endsWith(".gguf")) throw IOException("нужен файл .gguf")
        val dest = File(llmDir, name); val part = File(llmDir, "$name.part")
        val total = context.contentResolver.openFileDescriptor(uri, "r")?.use { it.statSize } ?: 0L
        context.contentResolver.openInputStream(uri)!!.use { inp ->
            part.outputStream().use { out ->
                val buf = ByteArray(1 shl 20); var done = 0L
                while (true) {
                    if (cancel.get()) throw IOException("отменено")
                    val n = inp.read(buf); if (n < 0) break
                    out.write(buf, 0, n); done += n; onProgress(done, total)
                }
            }
        }
        part.renameTo(dest)
        return dest
    }

    fun deleteLlm(file: File) { file.delete() }

    fun freeBytes(): Long = context.filesDir.usableSpace
}
