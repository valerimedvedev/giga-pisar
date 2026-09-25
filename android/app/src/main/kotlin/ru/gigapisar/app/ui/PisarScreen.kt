@file:OptIn(ExperimentalMaterial3Api::class, ExperimentalLayoutApi::class)

package ru.gigapisar.app.ui

import android.Manifest
import android.content.Intent
import android.content.pm.PackageManager
import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.ExperimentalLayoutApi
import androidx.compose.foundation.layout.FlowRow
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.heightIn
import androidx.compose.foundation.layout.imePadding
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.layout.width
import androidx.compose.foundation.lazy.LazyColumn
import androidx.compose.foundation.lazy.items
import androidx.compose.foundation.text.KeyboardActions
import androidx.compose.foundation.text.KeyboardOptions
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.filled.ContentCopy
import androidx.compose.material.icons.filled.Delete
import androidx.compose.material.icons.filled.Mic
import androidx.compose.material.icons.filled.PushPin
import androidx.compose.material.icons.filled.Settings
import androidx.compose.material.icons.filled.Share
import androidx.compose.material.icons.filled.Stop
import androidx.compose.material.icons.outlined.PushPin
import androidx.compose.material.icons.filled.Psychology
import androidx.compose.material3.AlertDialog
import androidx.compose.material3.AssistChip
import androidx.compose.material3.Button
import androidx.compose.material3.ButtonDefaults
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.HorizontalDivider
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.LinearProgressIndicator
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.ModalBottomSheet
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.material3.rememberModalBottomSheetState
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.graphics.Color
import androidx.compose.ui.platform.LocalClipboardManager
import androidx.compose.ui.platform.LocalContext
import androidx.compose.ui.text.AnnotatedString
import androidx.compose.ui.text.input.ImeAction
import androidx.compose.ui.text.style.TextOverflow
import androidx.compose.ui.unit.dp
import androidx.core.content.ContextCompat
import ru.gigapisar.app.PisarViewModel
import ru.gigapisar.app.PisarViewModel.Kind

/** Главный экран: поле, строка статуса словами и две кнопки — Запись и Мозг. */
@Composable
fun PisarScreen(vm: PisarViewModel, onSettings: () -> Unit) {
    val ui by vm.ui.collectAsState()
    val ctx = LocalContext.current
    val clipboard = LocalClipboardManager.current
    var brainOpen by remember { mutableStateOf(false) }

    val askMic = rememberLauncherForActivityResult(ActivityResultContracts.RequestPermission()) { ok ->
        if (ok) vm.startRecording()
    }
    fun record() {
        if (ui.recording) { vm.stopRecording(); return }
        if (ContextCompat.checkSelfPermission(ctx, Manifest.permission.RECORD_AUDIO) == PackageManager.PERMISSION_GRANTED) vm.startRecording()
        else askMic.launch(Manifest.permission.RECORD_AUDIO)
    }

    Scaffold(
        topBar = {
            TopAppBar(
                title = { Text("Гига Писарь") },
                actions = {
                    IconButton(onClick = { clipboard.setText(AnnotatedString(vm.text.text)) }, enabled = vm.text.text.isNotEmpty()) { Icon(Icons.Default.ContentCopy, "Копировать") }
                    IconButton(onClick = {
                        ctx.startActivity(Intent.createChooser(Intent(Intent.ACTION_SEND).apply { type = "text/plain"; putExtra(Intent.EXTRA_TEXT, vm.text.text) }, "Отправить текст"))
                    }, enabled = vm.text.text.isNotEmpty()) { Icon(Icons.Default.Share, "Отправить") }
                    IconButton(onClick = { vm.clearText() }, enabled = vm.text.text.isNotEmpty() && !ui.recording) { Icon(Icons.Default.Delete, "Очистить") }
                    IconButton(onClick = onSettings) { Icon(Icons.Default.Settings, "Настройки") }
                },
            )
        },
    ) { pad ->
        Column(Modifier.padding(pad).fillMaxSize().imePadding().padding(horizontal = 12.dp)) {
            OutlinedTextField(
                value = vm.text,
                onValueChange = vm::onTextChange,
                modifier = Modifier.fillMaxWidth().weight(1f),
                placeholder = { Text("Текст появится здесь. Выделите часть — команды мозга сработают только над ней.") },
                textStyle = MaterialTheme.typography.bodyLarge,
                readOnly = ui.busy,
            )
            Spacer(Modifier.height(6.dp))
            ui.progress?.let { p ->
                Row(verticalAlignment = Alignment.CenterVertically) {
                    Column(Modifier.weight(1f)) {
                        Text(p.label + if (p.total > 0) "  ${p.done / 1_000_000} / ${p.total / 1_000_000} МБ" else "", style = MaterialTheme.typography.bodySmall)
                        if (p.total > 0) LinearProgressIndicator(progress = { p.done.toFloat() / p.total }, Modifier.fillMaxWidth())
                        else LinearProgressIndicator(Modifier.fillMaxWidth())
                    }
                    TextButton(onClick = { vm.cancelDownload() }) { Text("Отмена") }
                }
            }
            Text(
                ui.status + if (ui.words > 0 && !ui.recording) "   ·   ${ui.words} сл." else "",
                style = MaterialTheme.typography.bodyMedium,
                color = when (ui.kind) {
                    Kind.OK -> Color(0xFF2E7D32); Kind.WARN -> Color(0xFFEF6C00); Kind.ERROR -> MaterialTheme.colorScheme.error
                    else -> MaterialTheme.colorScheme.onSurfaceVariant
                },
                maxLines = 2, overflow = TextOverflow.Ellipsis,
                modifier = Modifier.fillMaxWidth().heightIn(min = 40.dp),
            )
            Row(Modifier.fillMaxWidth().padding(bottom = 12.dp), horizontalArrangement = Arrangement.spacedBy(12.dp)) {
                Button(
                    onClick = { record() },
                    enabled = !ui.busy,
                    modifier = Modifier.weight(1f).height(60.dp),
                    colors = if (ui.recording) ButtonDefaults.buttonColors(containerColor = MaterialTheme.colorScheme.tertiary) else ButtonDefaults.buttonColors(),
                ) {
                    Icon(if (ui.recording) Icons.Default.Stop else Icons.Default.Mic, null)
                    Spacer(Modifier.width(8.dp))
                    Text(if (ui.recording) "Стоп" else "Запись", style = MaterialTheme.typography.titleMedium)
                }
                OutlinedButton(
                    onClick = { if (vm.brainEnabled) brainOpen = true else onSettings() },
                    enabled = !ui.recording,
                    modifier = Modifier.weight(1f).height(60.dp),
                ) {
                    Icon(Icons.Default.Psychology, null)
                    Spacer(Modifier.width(8.dp))
                    Text(if (ui.busy && ui.progress == null) "Думает…" else "Мозг", style = MaterialTheme.typography.titleMedium)
                }
            }
        }
    }

    if (ui.askDownload) AlertDialog(
        onDismissRequest = { vm.dismissDownload() },
        title = { Text("Пакет распознавания речи") },
        text = { Text("Для диктовки нужно один раз скачать пакет GigaAM v3 (213 МБ). Дальше речь распознаётся прямо на телефоне, звук никуда не уходит. Скачать?") },
        confirmButton = { TextButton(onClick = { vm.downloadGigaAm() }) { Text("Скачать") } },
        dismissButton = { TextButton(onClick = { vm.dismissDownload() }) { Text("Позже") } },
    )

    if (brainOpen) BrainSheet(vm, onClose = { brainOpen = false }, onSettings = { brainOpen = false; onSettings() })
}

/** Панель мозга: где считает, над чем, команды, свой промпт с историей, откат. */
@Composable
fun BrainSheet(vm: PisarViewModel, onClose: () -> Unit, onSettings: () -> Unit) {
    val ui by vm.ui.collectAsState()
    val sheet = rememberModalBottomSheetState(skipPartiallyExpanded = true)
    var prompt by remember { mutableStateOf("") }
    val sel = vm.text.selection
    val scope = if (!sel.collapsed) "над выделенным (${sel.max - sel.min} зн.)" else if (vm.text.text.isBlank()) "поле пустое" else "над всем текстом (${PisarViewModel.wordCount(vm.text.text)} сл.)"
    val where = when (vm.settings.brainMode) {
        "phone" -> "на телефоне · " + (vm.settings.phoneModel.ifBlank { "модель не выбрана" })
        "pc" -> "GigaBrain на компьютере · " + vm.settings.pcModel.ifBlank { "модель по умолчанию" }
        "server" -> "на сервере · " + vm.settings.serverModel.ifBlank { "модель по умолчанию" }
        else -> "выключен"
    }
    fun run(c: String, own: Boolean = false) { vm.runCommand(c, own); onClose() }

    ModalBottomSheet(onDismissRequest = onClose, sheetState = sheet) {
        Column(Modifier.padding(horizontal = 16.dp).padding(bottom = 24.dp).imePadding()) {
            Row(verticalAlignment = Alignment.CenterVertically) {
                Column(Modifier.weight(1f)) {
                    Text("Мозг: $where", style = MaterialTheme.typography.titleSmall, maxLines = 1, overflow = TextOverflow.Ellipsis)
                    Text("Работает $scope", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                }
                IconButton(onClick = onSettings) { Icon(Icons.Default.Settings, "Настройки мозга") }
            }
            FlowRow(horizontalArrangement = Arrangement.spacedBy(6.dp)) {
                for (chip in vm.settings.chips) AssistChip(onClick = { run(chip.command) }, label = { Text(chip.title) }, enabled = !ui.busy)
            }
            HorizontalDivider(Modifier.padding(vertical = 8.dp))
            OutlinedTextField(
                value = prompt, onValueChange = { prompt = it },
                modifier = Modifier.fillMaxWidth(),
                label = { Text("Свой промпт: что сделать с текстом") },
                keyboardOptions = KeyboardOptions(imeAction = ImeAction.Go),
                keyboardActions = KeyboardActions(onGo = { if (prompt.isNotBlank()) run(prompt, own = true) }),
                trailingIcon = { TextButton(onClick = { if (prompt.isNotBlank()) run(prompt, own = true) }, enabled = prompt.isNotBlank() && !ui.busy) { Text("Выполнить") } },
            )
            if (vm.history.isNotEmpty()) {
                Text("История (${vm.history.size} из ${ru.gigapisar.app.Store.HISTORY_MAX}; 📌 не вытесняется)", style = MaterialTheme.typography.labelMedium, modifier = Modifier.padding(top = 8.dp, bottom = 4.dp))
                LazyColumn(Modifier.heightIn(max = 220.dp)) {
                    items(vm.history, key = { it.text }) { h ->
                        Row(verticalAlignment = Alignment.CenterVertically) {
                            Text(h.text, Modifier.weight(1f).padding(vertical = 4.dp), maxLines = 2, overflow = TextOverflow.Ellipsis, style = MaterialTheme.typography.bodyMedium)
                            TextButton(onClick = { prompt = h.text }) { Text("Взять") }
                            IconButton(onClick = { vm.historyPin(h.text) }) { Icon(if (h.pinned) Icons.Filled.PushPin else Icons.Outlined.PushPin, if (h.pinned) "Открепить" else "Закрепить") }
                            IconButton(onClick = { vm.historyRemove(h.text) }) { Icon(Icons.Default.Delete, "Удалить из истории") }
                        }
                    }
                }
            }
            HorizontalDivider(Modifier.padding(vertical = 8.dp))
            Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                OutlinedButton(onClick = { vm.undo(); onClose() }, enabled = ui.canUndo) { Text("Вернуть как было") }
                if (ui.busy && ui.progress == null) OutlinedButton(onClick = { vm.cancelBrain() }) { Text("Прервать") }
            }
        }
    }
}
