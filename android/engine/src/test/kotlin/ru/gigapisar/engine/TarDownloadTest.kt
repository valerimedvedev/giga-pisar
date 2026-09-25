package ru.gigapisar.engine

import com.sun.net.httpserver.HttpServer
import org.junit.Assert.assertArrayEquals
import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.io.ByteArrayOutputStream
import java.io.File
import java.net.InetSocketAddress
import java.util.zip.GZIPOutputStream

class TarDownloadTest {
    private fun tarGz(files: Map<String, ByteArray>): ByteArray {
        val bo = ByteArrayOutputStream()
        GZIPOutputStream(bo).use { gz ->
            for ((name, data) in files) {
                val h = ByteArray(512)
                name.toByteArray().copyInto(h, 0)
                "0000644\u0000".toByteArray().copyInto(h, 100)
                String.format("%011o\u0000", data.size).toByteArray().copyInto(h, 124)
                h[156] = '0'.code.toByte()
                "ustar\u0000".toByteArray().copyInto(h, 257)
                gz.write(h); gz.write(data)
                val pad = (512 - data.size % 512) % 512
                gz.write(ByteArray(pad))
            }
            gz.write(ByteArray(1024))
        }
        return bo.toByteArray()
    }

    @Test fun extractsWantedFilesOnly() {
        val dir = File.createTempFile("tar", "").apply { delete(); mkdirs() }
        val payload = ByteArray(700) { it.toByte() }
        val archive = tarGz(mapOf("model/v3_e2e_rnnt.yaml" to "sample_rate: 16000\n".toByteArray(),
            "model/other.bin" to ByteArray(10), "model/v3_e2e_rnnt_joint.onnx" to payload))
        val got = ArrayList<String>()
        Tar.extract(archive.inputStream(), dir, setOf("v3_e2e_rnnt.yaml", "v3_e2e_rnnt_joint.onnx")) { got.add(it) }
        assertEquals(listOf("v3_e2e_rnnt.yaml", "v3_e2e_rnnt_joint.onnx"), got)
        assertArrayEquals(payload, File(dir, "v3_e2e_rnnt_joint.onnx").readBytes())
        assertTrue(!File(dir, "other.bin").exists())
    }

    @Test fun resumesAfterBreak() {
        val data = ByteArray(300_000) { (it * 7).toByte() }
        val server = HttpServer.create(InetSocketAddress("127.0.0.1", 0), 0)
        server.createContext("/f.bin") { ex ->
            val range = ex.requestHeaders.getFirst("Range")
            val from = range?.removePrefix("bytes=")?.removeSuffix("-")?.toInt() ?: 0
            ex.sendResponseHeaders(if (from > 0) 206 else 200, (data.size - from).toLong())
            ex.responseBody.use { it.write(data, from, data.size - from) }
        }
        server.start()
        try {
            val dest = File.createTempFile("dltest", ".bin").apply { delete() }
            // половина уже «скачана»
            File(dest.path + ".part").writeBytes(data.copyOfRange(0, 100_000))
            var last = 0L to 0L
            Downloader.download("http://127.0.0.1:${server.address.port}/f.bin", dest) { d, t -> last = d to t }
            assertArrayEquals(data, dest.readBytes())
            assertEquals(300_000L to 300_000L, last)
        } finally { server.stop(0) }
    }
}
