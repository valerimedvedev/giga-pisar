package ru.gigapisar.engine

import java.io.ByteArrayOutputStream
import java.util.zip.ZipEntry
import java.util.zip.ZipOutputStream

/** Экспорт текста: txt, md и docx (минимальный OOXML, без библиотек — Word и LibreOffice открывают). */
object Export {
    fun txt(text: String): ByteArray = text.toByteArray(Charsets.UTF_8)

    fun md(text: String, title: String? = null): ByteArray =
        ((if (title.isNullOrBlank()) "" else "# $title\n\n") + text.trimEnd() + "\n").toByteArray(Charsets.UTF_8)

    /** Абзацы — по переводам строк; пустые строки остаются пустыми абзацами. */
    fun docx(text: String, title: String? = null): ByteArray {
        val body = StringBuilder()
        if (!title.isNullOrBlank()) body.append("<w:p><w:pPr><w:pStyle w:val=\"Heading1\"/></w:pPr><w:r><w:t xml:space=\"preserve\">${esc(title)}</w:t></w:r></w:p>")
        for (line in text.replace("\r\n", "\n").split("\n")) {
            body.append("<w:p><w:r><w:t xml:space=\"preserve\">${esc(line)}</w:t></w:r></w:p>")
        }
        val document = """<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>$body<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1134" w:right="850" w:bottom="1134" w:left="1701" w:header="708" w:footer="708" w:gutter="0"/></w:sectPr></w:body></w:document>"""
        val styles = """<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:ascii="Calibri" w:hAnsi="Calibri" w:cs="Calibri"/><w:sz w:val="24"/><w:lang w:val="ru-RU"/></w:rPr></w:rPrDefault><w:pPrDefault><w:pPr><w:spacing w:after="120" w:line="276" w:lineRule="auto"/></w:pPr></w:pPrDefault></w:docDefaults><w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/></w:style><w:style w:type="paragraph" w:styleId="Heading1"><w:name w:val="heading 1"/><w:basedOn w:val="Normal"/><w:pPr><w:spacing w:before="240" w:after="120"/></w:pPr><w:rPr><w:b/><w:sz w:val="32"/></w:rPr></w:style></w:styles>"""
        val contentTypes = """<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/><Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/></Types>"""
        val rels = """<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>"""
        val docRels = """<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/></Relationships>"""
        val bo = ByteArrayOutputStream()
        ZipOutputStream(bo).use { z ->
            for ((name, data) in listOf("[Content_Types].xml" to contentTypes, "_rels/.rels" to rels, "word/document.xml" to document,
                "word/_rels/document.xml.rels" to docRels, "word/styles.xml" to styles)) {
                z.putNextEntry(ZipEntry(name)); z.write(data.toByteArray(Charsets.UTF_8)); z.closeEntry()
            }
        }
        return bo.toByteArray()
    }

    private fun esc(s: String) = s.replace("&", "&amp;").replace("<", "&lt;").replace(">", "&gt;")
        .filter { it >= ' ' || it == '\t' }
}
