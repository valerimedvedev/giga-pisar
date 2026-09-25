package ru.gigapisar.app.ui

import androidx.compose.foundation.isSystemInDarkTheme
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.darkColorScheme
import androidx.compose.material3.lightColorScheme
import androidx.compose.runtime.Composable
import androidx.compose.ui.graphics.Color

private val Purple = Color(0xFF5E35B1)
private val PurpleLight = Color(0xFFB39DDB)
private val Red = Color(0xFFD32F2F)

private val Light = lightColorScheme(primary = Purple, secondary = Color(0xFF7E57C2), tertiary = Red)
private val Dark = darkColorScheme(primary = PurpleLight, secondary = Color(0xFFB39DDB), tertiary = Color(0xFFEF9A9A))

@Composable
fun PisarTheme(content: @Composable () -> Unit) {
    MaterialTheme(colorScheme = if (isSystemInDarkTheme()) Dark else Light, content = content)
}
