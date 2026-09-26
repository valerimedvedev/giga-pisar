package ru.gigapisar.engine

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class CloudTest {
    @Test fun parsesKeysFile() {
        val (keys, def) = Cloud.parseKeys("""{"format":"giga-pisar-keys/1","default":"groq","services":{"groq":{"key":" gsk_1 "},"cloudflare":{"key":"cf","account":"acc1"},"unknown":{"key":"x"}}}""")
        assertEquals(setOf("groq", "cloudflare"), keys.keys)
        assertEquals("gsk_1", keys["groq"]!!.key)
        assertEquals("groq", def)
        assertEquals("https://api.cloudflare.com/client/v4/accounts/acc1/ai/v1", Cloud.baseFor(Cloud.byId("cloudflare")!!, keys["cloudflare"]))
    }

    @Test fun parsesShortForm() {
        val (keys, def) = Cloud.parseKeys("""{"gemini":"AIza","mistral":""}""")
        assertEquals(setOf("gemini"), keys.keys)
        assertNull(def)
    }

    @Test(expected = IllegalArgumentException::class)
    fun rejectsEmpty() { Cloud.parseKeys("""{"services":{}}""") }
}
