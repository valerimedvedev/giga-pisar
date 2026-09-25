package ru.gigapisar.app

import android.os.Bundle
import androidx.activity.ComponentActivity
import androidx.activity.compose.setContent
import androidx.activity.enableEdgeToEdge
import androidx.activity.viewModels
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import ru.gigapisar.app.ui.PisarScreen
import ru.gigapisar.app.ui.PisarTheme
import ru.gigapisar.app.ui.SettingsScreen

class MainActivity : ComponentActivity() {
    private val vm: PisarViewModel by viewModels()

    override fun onCreate(savedInstanceState: Bundle?) {
        super.onCreate(savedInstanceState)
        enableEdgeToEdge()
        setContent {
            PisarTheme {
                var settings by remember { mutableStateOf(false) }
                if (settings) SettingsScreen(vm, onBack = { settings = false })
                else PisarScreen(vm, onSettings = { settings = true })
            }
        }
    }

    override fun onStop() {
        super.onStop()
        if (vm.ui.value.recording) vm.stopRecording()   // ушли из приложения — запись останавливаем
    }
}
