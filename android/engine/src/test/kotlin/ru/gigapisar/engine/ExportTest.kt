package ru.gigapisar.engine

import org.junit.Assert.assertEquals
import org.junit.Assert.assertTrue
import org.junit.Test
import java.util.zip.ZipInputStream

class ExportTest {
    @Test fun docxHasParagraphs() {
        val bytes = Export.docx("Первый абзац & <тег>\n\nТретий", "Заголовок")
        val entries = HashMap<String, String>()
        ZipInputStream(bytes.inputStream()).use { z ->
            while (true) { val e = z.nextEntry ?: break; entries[e.name] = z.readBytes().toString(Charsets.UTF_8) }
        }
        assertEquals(setOf("[Content_Types].xml", "_rels/.rels", "word/document.xml", "word/_rels/document.xml.rels", "word/styles.xml"), entries.keys)
        val doc = entries["word/document.xml"]!!
        assertTrue(doc.contains("Первый абзац &amp; &lt;тег&gt;"))
        assertEquals(4, Regex("<w:p>").findAll(doc).count())   // заголовок + 3 абзаца (один пустой)
        assertTrue(doc.contains("Heading1"))
    }

    @Test fun mdAndTxt() {
        assertEquals("# Т\n\nтекст\n", String(Export.md("текст", "Т")))
        assertEquals("текст", String(Export.txt("текст")))
    }
}
