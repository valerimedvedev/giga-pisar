@file:OptIn(ExperimentalMaterial3Api::class)

package ru.gigapisar.app.ui

import androidx.activity.compose.rememberLauncherForActivityResult
import androidx.activity.result.contract.ActivityResultContracts
import androidx.compose.foundation.layout.Arrangement
import androidx.compose.foundation.layout.Column
import androidx.compose.foundation.layout.Row
import androidx.compose.foundation.layout.Spacer
import androidx.compose.foundation.layout.fillMaxSize
import androidx.compose.foundation.layout.fillMaxWidth
import androidx.compose.foundation.layout.height
import androidx.compose.foundation.layout.padding
import androidx.compose.foundation.rememberScrollState
import androidx.compose.foundation.verticalScroll
import androidx.compose.material.icons.Icons
import androidx.compose.material.icons.automirrored.filled.ArrowBack
import androidx.compose.material3.Button
import androidx.compose.material3.Card
import androidx.compose.material3.ExperimentalMaterial3Api
import androidx.compose.material3.Icon
import androidx.compose.material3.IconButton
import androidx.compose.material3.MaterialTheme
import androidx.compose.material3.OutlinedButton
import androidx.compose.material3.OutlinedTextField
import androidx.compose.material3.RadioButton
import androidx.compose.material3.Scaffold
import androidx.compose.material3.Slider
import androidx.compose.material3.Switch
import androidx.compose.material3.Text
import androidx.compose.material3.TextButton
import androidx.compose.material3.TopAppBar
import androidx.compose.runtime.Composable
import androidx.compose.runtime.collectAsState
import androidx.compose.runtime.getValue
import androidx.compose.runtime.mutableStateOf
import androidx.compose.runtime.remember
import androidx.compose.runtime.setValue
import androidx.compose.ui.Alignment
import androidx.compose.ui.Modifier
import androidx.compose.ui.text.input.PasswordVisualTransformation
import androidx.compose.ui.unit.dp
import ru.gigapisar.app.PisarViewModel
import ru.gigapisar.engine.Catalog
import ru.gigapisar.engine.Chip

/** Настройки: распознавание, мозг (где считает и какие модели), команды, промпты. */
@Composable
fun SettingsScreen(vm: PisarViewModel, onBack: () -> Unit) {
    val ui by vm.ui.collectAsState()
    val s = vm.settings

    Scaffold(topBar = {
        TopAppBar(title = { Text("Настройки") }, navigationIcon = { IconButton(onClick = onBack) { Icon(Icons.AutoMirrored.Filled.ArrowBack, "Назад") } })
    }) { pad ->
        Column(Modifier.padding(pad).fillMaxSize().verticalScroll(rememberScrollState()).padding(12.dp), verticalArrangement = Arrangement.spacedBy(12.dp)) {

            ui.progress?.let { p ->
                Card(Modifier.fillMaxWidth()) {
                    Column(Modifier.padding(12.dp), verticalArrangement = Arrangement.spacedBy(6.dp)) {
                        Text(p.label, style = MaterialTheme.typography.titleSmall)
                        if (p.total > 0) {
                            Text("${p.done / 1_000_000} из ${p.total / 1_000_000} МБ · ${100 * p.done / p.total}%", style = MaterialTheme.typography.bodySmall)
                            androidx.compose.material3.LinearProgressIndicator(progress = { p.done.toFloat() / p.total }, Modifier.fillMaxWidth())
                        } else androidx.compose.material3.LinearProgressIndicator(Modifier.fillMaxWidth())
                        Text("Связь оборвётся — докачается сама с того же места. Остановить можно, недокачанное сохранится.", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        TextButton(onClick = { vm.cancelDownload() }) { Text("Остановить") }
                    }
                }
            }
            if (ui.kind == PisarViewModel.Kind.ERROR || ui.kind == PisarViewModel.Kind.WARN) Text(ui.status, style = MaterialTheme.typography.bodyMedium,
                color = if (ui.kind == PisarViewModel.Kind.ERROR) MaterialTheme.colorScheme.error else MaterialTheme.colorScheme.tertiary)

            Section("Распознавание речи") {
                Text(if (ui.modelReady) "Пакет GigaAM v3 на телефоне (213 МБ). Звук никуда не уходит." else "Пакет распознавания ещё не скачан.", style = MaterialTheme.typography.bodyMedium)
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    if (!ui.modelReady) Button(onClick = { vm.downloadGigaAm() }, enabled = !ui.busy) { Text("Скачать (213 МБ)") }
                    else OutlinedButton(onClick = { vm.deleteGigaAm() }, enabled = !ui.busy && !ui.recording) { Text("Удалить пакет") }
                }
                SliderRow("Потоков на распознавание: ${s.asrThreads}", s.asrThreads, 1..8) { vm.save(s.copy(asrThreads = it)) }
                SwitchRow("Вставлять фразы по ходу речи", "Каждая фраза распознаётся во время паузы и сразу встаёт в поле — после «Стоп» ждать почти не нужно", s.liveInsert) { vm.save(s.copy(liveInsert = it)) }
                SwitchRow("Причёсывать каждую диктовку", "После «Стоп» надиктованное всегда идёт через мозг (первая команда из списка), даже без слов «Писарь, …»", s.autoTidy) { vm.save(s.copy(autoTidy = it)) }
            }

            Section("Мозг — где считает нейронка") {
                RadioRow("Выключен", s.brainMode == "off") { vm.save(s.copy(brainMode = "off")) }
                RadioRow("На телефоне — нейронка в этом приложении (llama.cpp)", s.brainMode == "phone") { vm.save(s.copy(brainMode = "phone")) }
                RadioRow("На компьютере — GigaBrain / Ollama / LM Studio по домашней сети", s.brainMode == "pc") { vm.save(s.copy(brainMode = "pc")) }
                RadioRow("На сервере — GigaChat или другой OpenAI-совместимый адрес", s.brainMode == "server") { vm.save(s.copy(brainMode = "server")) }
                RadioRow("Облачный сервис с бесплатным тарифом — Gemini, Groq, OpenRouter, Mistral…", s.brainMode == "cloud") { vm.save(s.copy(brainMode = "cloud")) }
                SliderRow("Потоков на нейронку телефона: ${s.llmThreads}", s.llmThreads, 2..8) { vm.save(s.copy(llmThreads = it)) }
            }

            if (s.brainMode == "phone") Section("Нейронки на телефоне") {
                Text("Свободно ${vm.models.freeBytes() / 1_000_000_000} ГБ. Модель скачивается один раз (с докачкой), выбранная отмечена.", style = MaterialTheme.typography.bodySmall)
                for (m in Catalog.PHONE) {
                    val have = vm.models.hasLlm(m)
                    Row(verticalAlignment = Alignment.CenterVertically) {
                        RadioButton(selected = s.phoneModel == m.id, onClick = { vm.save(s.copy(phoneModel = m.id)) }, enabled = have)
                        Column(Modifier.weight(1f)) {
                            Text(m.name, style = MaterialTheme.typography.bodyMedium)
                            Text("${m.sizeGb} ГБ · память ${m.ramGb} ГБ · ${m.about}", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                        }
                        val part = java.io.File(vm.models.llmFile(m).path + ".part")
                        val downloadingThis = ui.progress?.label == m.name
                        if (have) TextButton(onClick = { vm.deleteLlm(vm.models.llmFile(m)) }, enabled = !ui.busy) { Text("Удалить") }
                        else if (downloadingThis) Text("${ui.progress?.let { if (it.total > 0) "${100 * it.done / it.total}%" else "…" }}", style = MaterialTheme.typography.bodyMedium, modifier = Modifier.padding(horizontal = 12.dp))
                        else TextButton(onClick = { vm.downloadLlm(m) }, enabled = !ui.busy) { Text(if (part.exists()) "Докачать (${part.length() / 1_000_000} МБ есть)" else "Скачать") }
                    }
                }
                val own = vm.models.installedLlm().filter { f -> Catalog.PHONE.none { it.file == f.name } }
                for (f in own) Row(verticalAlignment = Alignment.CenterVertically) {
                    RadioButton(selected = s.phoneModel == f.name, onClick = { vm.save(s.copy(phoneModel = f.name)) })
                    Text("${f.name} (${f.length() / 1_000_000_000.0} ГБ)", Modifier.weight(1f), style = MaterialTheme.typography.bodyMedium)
                    TextButton(onClick = { vm.deleteLlm(f) }, enabled = !ui.busy) { Text("Удалить") }
                }
                val pick = rememberLauncherForActivityResult(ActivityResultContracts.OpenDocument()) { uri -> uri?.let { vm.importLlm(it) } }
                OutlinedButton(onClick = { pick.launch(arrayOf("*/*")) }, enabled = !ui.busy) { Text("Свой файл .gguf с телефона…") }
            }

            if (s.brainMode == "pc") Section("Мозг на компьютере") {
                RemoteForm(vm, s.pcBase, s.pcKey, s.pcModel, hint = "http://192.168.1.10:8091",
                    note = "В окне GigaBrain включите «Доступ с телефона по домашней сети» и скопируйте ключ. Ollama: адрес :11434, LM Studio: :1234.",
                    lan = true) { b, k, m -> vm.save(s.copy(pcBase = b, pcKey = k, pcModel = m)) }
            }

            if (s.brainMode == "server") Section("Мозг на сервере") {
                RemoteForm(vm, s.serverBase, s.serverKey, s.serverModel, hint = "https://vmindlab.ru/pisar/brain",
                    note = "Любой OpenAI-совместимый адрес (…/v1/chat/completions). Туда уходит только текст.",
                    lan = false) { b, k, m -> vm.save(s.copy(serverBase = b, serverKey = k, serverModel = m)) }
            }

            if (s.brainMode == "cloud") Section("Облачный сервис") {
                Text("Нужен свой ключ API (бесплатный). Текст уходит в сервис — читайте его условия. Ответы обычно за 1–3 с.", style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                for (c in ru.gigapisar.engine.Cloud.SERVICES) Row(verticalAlignment = Alignment.CenterVertically) {
                    RadioButton(selected = s.cloudService == c.id, onClick = { vm.save(s.copy(cloudService = c.id, cloudBase = c.base, cloudModel = c.model)) })
                    Column(Modifier.weight(1f)) {
                        Text(c.name, style = MaterialTheme.typography.bodyMedium)
                        Text(c.note, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
                    }
                }
                val svc = ru.gigapisar.engine.Cloud.byId(s.cloudService)
                if (svc != null) {
                    val ctx = androidx.compose.ui.platform.LocalContext.current
                    TextButton(onClick = { ctx.startActivity(android.content.Intent(android.content.Intent.ACTION_VIEW, android.net.Uri.parse(svc.keyUrl))) }) { Text("Получить ключ: ${svc.keyUrl.removePrefix("https://")}") }
                }
                RemoteForm(vm, s.cloudBase.ifBlank { svc?.base ?: "" }, s.cloudKey, s.cloudModel.ifBlank { svc?.model ?: "" }, hint = svc?.base ?: "",
                    note = if (svc?.listsModels == false) "Этот сервис не отдаёт список моделей — «Проверить» лишь сохранит; имя модели впишите вручную." else "«Проверить и сохранить» спросит у сервиса список моделей и запомнит ключ.",
                    lan = false) { b, k, m -> vm.save(s.copy(cloudBase = b, cloudKey = k, cloudModel = m)) }
            }

            Section("Команды на кнопках (до 10)") {
                ChipsEditor(s.chips, onChange = { vm.save(s.copy(chips = it)) }, onReset = { vm.resetChips() })
            }

            Section("Промпты нейронке") {
                var d by remember(s.promptDictation) { mutableStateOf(s.promptDictation) }
                var sel by remember(s.promptSelection) { mutableStateOf(s.promptSelection) }
                var ch by remember(s.promptChat) { mutableStateOf(s.promptChat) }
                OutlinedTextField(d, { d = it }, Modifier.fillMaxWidth(), label = { Text("Для надиктованного") }, minLines = 3)
                OutlinedTextField(sel, { sel = it }, Modifier.fillMaxWidth(), label = { Text("Для выделенного текста") }, minLines = 3)
                OutlinedTextField(ch, { ch = it }, Modifier.fillMaxWidth(), label = { Text("Для режима «Общение»") }, minLines = 2)
                Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
                    Button(onClick = { vm.save(s.copy(promptDictation = d, promptSelection = sel, promptChat = ch)) }, enabled = d != s.promptDictation || sel != s.promptSelection || ch != s.promptChat) { Text("Сохранить") }
                    OutlinedButton(onClick = { vm.resetPrompts() }) { Text("Вернуть образец") }
                }
            }

            Section("О программе") {
                Text("Гига Писарь Диктовка 1.0.0. Распознавание — GigaAM v3 (Сбер) через onnxruntime; нейронка на телефоне — llama.cpp. Голосовые команды: скажите в конце «…, Писарь, сократи» / «переведи на английский» / «исправь». Исходники: github.com/valerimedvedev/giga-pisar, папка android/.", style = MaterialTheme.typography.bodySmall)
            }
        }
    }
}

@Composable
private fun Section(title: String, content: @Composable () -> Unit) {
    Card(Modifier.fillMaxWidth()) {
        Column(Modifier.padding(12.dp), verticalArrangement = Arrangement.spacedBy(8.dp)) {
            Text(title, style = MaterialTheme.typography.titleMedium)
            content()
        }
    }
}

@Composable
private fun RadioRow(label: String, selected: Boolean, onClick: () -> Unit) {
    Row(verticalAlignment = Alignment.CenterVertically) {
        RadioButton(selected = selected, onClick = onClick)
        Text(label, style = MaterialTheme.typography.bodyMedium)
    }
}

@Composable
private fun SwitchRow(label: String, note: String, checked: Boolean, onChange: (Boolean) -> Unit) {
    Row(verticalAlignment = Alignment.CenterVertically) {
        Column(Modifier.weight(1f)) {
            Text(label, style = MaterialTheme.typography.bodyMedium)
            Text(note, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
        }
        Switch(checked = checked, onCheckedChange = onChange)
    }
}

@Composable
private fun SliderRow(label: String, value: Int, range: IntRange, onChange: (Int) -> Unit) {
    var v by remember(value) { mutableStateOf(value.toFloat()) }
    Column {
        Text(label, style = MaterialTheme.typography.bodyMedium)
        Slider(value = v, onValueChange = { v = it }, onValueChangeFinished = { onChange(v.toInt()) },
            valueRange = range.first.toFloat()..range.last.toFloat(), steps = range.last - range.first - 1)
    }
}

/** Адрес, ключ, модель и кнопки «Найти в сети» / «Проверить». */
@Composable
private fun RemoteForm(vm: PisarViewModel, base0: String, key0: String, model0: String, hint: String, note: String, lan: Boolean,
                       onSave: (String, String, String) -> Unit) {
    var base by remember(base0) { mutableStateOf(base0) }
    var key by remember(key0) { mutableStateOf(key0) }
    var model by remember(model0) { mutableStateOf(model0) }
    var result by remember { mutableStateOf("") }
    var searching by remember { mutableStateOf(false) }
    Text(note, style = MaterialTheme.typography.bodySmall, color = MaterialTheme.colorScheme.onSurfaceVariant)
    OutlinedTextField(base, { base = it }, Modifier.fillMaxWidth(), label = { Text("Адрес") }, placeholder = { Text(hint) }, singleLine = true)
    OutlinedTextField(key, { key = it }, Modifier.fillMaxWidth(), label = { Text("Ключ доступа (если есть)") }, singleLine = true, visualTransformation = PasswordVisualTransformation())
    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        if (lan) OutlinedButton(onClick = {
            searching = true; result = "Ищу в домашней сети…"
            vm.findInLan(key) { found -> searching = false; if (found.isEmpty()) result = "В сети не нашёл: проверьте, что GigaBrain запущен и доступ по сети включён" else { base = found.first(); result = "Найден: ${found.joinToString()}" } }
        }, enabled = !searching) { Text("Найти в сети") }
        Button(onClick = {
            result = "Проверяю…"
            vm.checkRemote(base, key) { r ->
                r.onSuccess { list -> result = if (list.isEmpty()) "Отвечает, но моделей нет" else "Отвечает. Модели: ${list.joinToString()}"; if (model.isBlank() || model !in list) model = list.firstOrNull() ?: model; onSave(base, key, model) }
                    .onFailure { result = "Не отвечает: ${it.message}" }
            }
        }, enabled = base.isNotBlank()) { Text("Проверить и сохранить") }
    }
    if (vm.pcModels.isNotEmpty()) {
        Text("Модель:", style = MaterialTheme.typography.bodyMedium)
        for (m in vm.pcModels) Row(verticalAlignment = Alignment.CenterVertically) {
            RadioButton(selected = model == m, onClick = { model = m; onSave(base, key, m) })
            Text(m, style = MaterialTheme.typography.bodyMedium)
        }
    }
    OutlinedTextField(model, { model = it }, Modifier.fillMaxWidth(), label = { Text("Модель (имя, можно оставить пустым)") }, singleLine = true)
    if (result.isNotEmpty()) Text(result, style = MaterialTheme.typography.bodySmall)
    if (base != base0 || key != key0 || model != model0) TextButton(onClick = { onSave(base, key, model) }) { Text("Сохранить без проверки") }
}

/** Список команд: название + команда, добавить, удалить, вернуть по умолчанию. */
@Composable
private fun ChipsEditor(chips: List<Chip>, onChange: (List<Chip>) -> Unit, onReset: () -> Unit) {
    var list by remember(chips) { mutableStateOf(chips) }
    for ((i, c) in list.withIndex()) {
        Column {
            OutlinedTextField(c.title, { t -> list = list.toMutableList().also { it[i] = c.copy(title = t) } }, Modifier.fillMaxWidth(), label = { Text("Название кнопки ${i + 1}") }, singleLine = true)
            OutlinedTextField(c.command, { t -> list = list.toMutableList().also { it[i] = c.copy(command = t) } }, Modifier.fillMaxWidth(), label = { Text("Команда нейронке") }, minLines = 2)
            TextButton(onClick = { list = list.toMutableList().also { it.removeAt(i) } }) { Text("Убрать") }
        }
    }
    Row(horizontalArrangement = Arrangement.spacedBy(8.dp)) {
        OutlinedButton(onClick = { list = list + Chip("", "") }, enabled = list.size < 10) { Text("Добавить") }
        Button(onClick = { onChange(list.filter { it.title.isNotBlank() && it.command.isNotBlank() }) }, enabled = list != chips) { Text("Сохранить") }
        TextButton(onClick = onReset) { Text("По умолчанию") }
    }
    Spacer(Modifier.height(4.dp))
}
