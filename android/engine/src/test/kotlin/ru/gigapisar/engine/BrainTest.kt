package ru.gigapisar.engine

import org.junit.Assert.assertEquals
import org.junit.Assert.assertNull
import org.junit.Test

class BrainTest {
    @Test fun commandAtTheEnd() {
        val c = Brain.parseCommand("сегодня хорошая погода, Писарь, переведи на английский")!!
        assertEquals("сегодня хорошая погода", c.body)
        assertEquals("переведи на английский", c.command)
    }

    @Test fun lastAddressWins() {
        val c = Brain.parseCommand("я писарь и это описарь моего дела, гига писарь сократи")!!
        assertEquals("я писарь и это описарь моего дела", c.body)
        assertEquals("сократи", c.command)
    }

    @Test fun noCommand() {
        assertNull(Brain.parseCommand("просто текст без обращения"))
        assertNull(Brain.parseCommand("Писарь, исправь"))            // нет тела
        assertNull(Brain.parseCommand("текст, Писарь"))               // нет команды
    }

    @Test fun stripAddressAndLabels() {
        assertEquals("сократи", Brain.stripAddress("Писарь, сократи"))
        assertEquals("Перевожу…", Brain.actionLabel("переведи на английский"))
        assertEquals("Сокращаю…", Brain.actionLabel("сократи"))
        assertEquals("ответ", Brain.stripThinking("<think>мысли</think>ответ"))
    }

    @Test fun messages() {
        val m = Brain.messagesFor("тело", "сократи", selection = false)
        assertEquals("system", m[0].role)
        assert(m[0].content.endsWith("Команда пользователя к тексту: сократи."))
        assertEquals("тело", m[1].content)
        assertEquals(7, Brain.DEFAULT_CHIPS.size)
    }
}
