package ru.gigapisar.engine

/**
 * Номера, которые выдаёт модель → текст.
 *
 * Модель говорит кусочками слов (SentencePiece); список кусочков лежит в
 * v3_e2e_rnnt_tokenizer.model (protobuf). Нужно одно поле, разбираем сами:
 *   ModelProto    { repeated SentencePiece pieces = 1 }
 *   SentencePiece { optional string piece = 1 }
 */
class Tokenizer(bytes: ByteArray) {
    val pieces: List<String>

    init {
        val out = ArrayList<String>()
        var i = 0
        while (i < bytes.size) {
            val t = varint(bytes, i) ?: break
            i = t.second
            val field = t.first ushr 3; val wire = (t.first and 7).toInt()
            if (field == 1L && wire == 2) {
                val l = varint(bytes, i) ?: break
                val end = l.second + l.first.toInt()
                if (end > bytes.size) break
                out.add(piece(bytes, l.second, end))
                i = end
            } else {
                i = skip(bytes, i, wire) ?: break
            }
        }
        require(out.isNotEmpty()) { "не разобрал токенизатор" }
        pieces = out
    }

    /** Номер «пустышки» — модель выдаёт его, когда сказать нечего. */
    val blankId get() = pieces.size

    /** Кусочки склеиваются, знак ▁ означает пробел перед словом. */
    fun decode(ids: IntArray): String {
        val sb = StringBuilder()
        for (id in ids) if (id in pieces.indices) sb.append(pieces[id])
        return sb.toString().replace('▁', ' ').trim()
    }

    private companion object {
        fun varint(d: ByteArray, from: Int): Pair<Long, Int>? {
            var result = 0L; var shift = 0; var i = from
            while (i < d.size) {
                val b = d[i++].toInt() and 0xff
                result = result or ((b and 0x7f).toLong() shl shift)
                if (b and 0x80 == 0) return result to i
                shift += 7
                if (shift > 63) return null
            }
            return null
        }

        fun skip(d: ByteArray, from: Int, wire: Int): Int? = when (wire) {
            0 -> varint(d, from)?.second
            1 -> if (from + 8 <= d.size) from + 8 else null
            2 -> varint(d, from)?.let { val end = it.second + it.first.toInt(); if (end <= d.size) end else null }
            5 -> if (from + 4 <= d.size) from + 4 else null
            else -> null
        }

        fun piece(d: ByteArray, from: Int, to: Int): String {
            var i = from
            while (i < to) {
                val t = varint(d, i) ?: break
                i = t.second
                val field = t.first ushr 3; val wire = (t.first and 7).toInt()
                if (field == 1L && wire == 2) {
                    val l = varint(d, i) ?: break
                    val end = minOf(l.second + l.first.toInt(), to)
                    return String(d, l.second, end - l.second, Charsets.UTF_8)
                }
                i = skip(d, i, wire) ?: break
            }
            return ""
        }
    }
}
