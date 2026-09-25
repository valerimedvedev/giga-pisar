package ru.gigapisar.engine

import java.io.File
import java.io.InputStream
import java.util.zip.GZIPInputStream

/** Распаковка .tar.gz с моделью: нужные файлы кладутся в папку по именам (без путей). */
object Tar {
    fun extract(input: InputStream, dest: File, wanted: Set<String>, onFile: (String) -> Unit = {}) {
        dest.mkdirs()
        val gz = GZIPInputStream(input, 1 shl 16)
        val header = ByteArray(512)
        val buf = ByteArray(1 shl 16)
        while (true) {
            if (!readFully(gz, header, 512)) return
            if (header.all { it == 0.toByte() }) return
            val name = cstr(header, 0, 100).let { n -> val prefix = cstr(header, 345, 155); if (prefix.isEmpty()) n else "$prefix/$n" }
            val size = cstr(header, 124, 12).trim().toLongOrNull(8) ?: 0L
            val type = header[156].toInt().toChar()
            val base = name.substringAfterLast('/')
            val padded = (size + 511) / 512 * 512
            if ((type == '0' || type == '\u0000') && base in wanted) {
                val tmp = File(dest, "$base.part")
                tmp.outputStream().use { out ->
                    var left = size
                    while (left > 0) {
                        val n = gz.read(buf, 0, minOf(buf.size.toLong(), left).toInt())
                        if (n < 0) error("архив оборван на $base")
                        out.write(buf, 0, n); left -= n
                    }
                }
                tmp.renameTo(File(dest, base))
                onFile(base)
                skip(gz, padded - size)
            } else {
                skip(gz, padded)
            }
        }
    }

    private fun readFully(s: InputStream, b: ByteArray, n: Int): Boolean {
        var got = 0
        while (got < n) { val r = s.read(b, got, n - got); if (r < 0) return false; got += r }
        return true
    }

    private fun skip(s: InputStream, n: Long) { var left = n; while (left > 0) { val r = s.skip(left); if (r <= 0) { if (s.read() < 0) return; left-- } else left -= r } }

    private fun cstr(b: ByteArray, off: Int, len: Int): String {
        var end = off
        while (end < off + len && b[end] != 0.toByte()) end++
        return String(b, off, end - off, Charsets.UTF_8)
    }
}
