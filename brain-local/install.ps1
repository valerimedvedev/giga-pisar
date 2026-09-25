# Мозг Писаря на своём компьютере — Windows.
#
# Ставит llama-server (движок llama.cpp) и нейронку в папку %LOCALAPPDATA%\GigaBrain,
# делает ярлык «Мозг Писаря» на рабочем столе. Запущенный мозг слушает
# http://127.0.0.1:8091 и отвечает только страницам, знающим ключ доступа.
#
#   powershell -ExecutionPolicy Bypass -File install.ps1                 # GigaChat (по умолчанию)
#   powershell -ExecutionPolicy Bypass -File install.ps1 -Model qwen3-14b
#   powershell -ExecutionPolicy Bypass -File install.ps1 -ModelUrl https://…/любая.gguf
#   powershell -ExecutionPolicy Bypass -File install.ps1 -Backend vulkan  # считать на видеокарте
#
# Модели см. в catalog.json (там же — сколько памяти нужно).
param(
    [string]$Model = "gigachat",
    [string]$ModelUrl = "",
    [ValidateSet("cpu", "vulkan", "cuda")][string]$Backend = "cpu",
    [int]$Port = 8091
)
$ErrorActionPreference = "Stop"
[Net.ServicePointManager]::SecurityProtocol = [Net.SecurityProtocolType]::Tls12

$Home_ = Join-Path $env:LOCALAPPDATA "GigaBrain"
$Bin = Join-Path $Home_ "bin"
$Models = Join-Path $Home_ "models"
New-Item -ItemType Directory -Force -Path $Bin, $Models | Out-Null

$catalog = Get-Content (Join-Path $PSScriptRoot "catalog.json") -Raw | ConvertFrom-Json
if ($ModelUrl) {
    $file = [IO.Path]::GetFileName(([Uri]$ModelUrl).AbsolutePath)
    $entry = [pscustomobject]@{ id = "custom"; name = $file; url = $ModelUrl; file = $file; ram_gb = 0 }
} else {
    $entry = $catalog.models | Where-Object { $_.id -eq $Model }
    if (-not $entry) { throw "Нет модели '$Model'. Есть: $(($catalog.models | ForEach-Object id) -join ', ')" }
}

$ramGb = [math]::Round((Get-CimInstance Win32_ComputerSystem).TotalPhysicalMemory / 1GB)
Write-Host "== Мозг Писаря: $($entry.name)"
Write-Host "   память компьютера: $ramGb ГБ, нужно около $($entry.ram_gb) ГБ"
if ($entry.ram_gb -and $ramGb -lt $entry.ram_gb) {
    Write-Warning "Памяти впритык: модель запустится, но Windows будет выгружать другие программы на диск."
}

# 1. llama-server — свежая сборка с GitHub
$serverExe = Join-Path $Bin "llama-server.exe"
if (-not (Test-Path $serverExe)) {
    Write-Host "== Скачиваю llama.cpp ($Backend)"
    $rel = Invoke-RestMethod "https://api.github.com/repos/ggml-org/llama.cpp/releases/latest" -Headers @{ "User-Agent" = "giga-pisar" }
    $pattern = switch ($Backend) {
        "cpu"    { "bin-win-cpu-x64\.zip$" }
        "vulkan" { "bin-win-vulkan-x64\.zip$" }
        "cuda"   { "bin-win-cuda-12.*x64\.zip$" }
    }
    $asset = $rel.assets | Where-Object { $_.name -match $pattern } | Select-Object -First 1
    if (-not $asset) { throw "В выпуске $($rel.tag_name) нет сборки под '$Backend' — посмотрите названия на https://github.com/ggml-org/llama.cpp/releases" }
    $zip = Join-Path $env:TEMP $asset.name
    Invoke-WebRequest $asset.browser_download_url -OutFile $zip
    $tmp = Join-Path $env:TEMP "llama-unzip"
    Remove-Item -Recurse -Force $tmp -ErrorAction SilentlyContinue
    Expand-Archive $zip -DestinationPath $tmp
    $found = Get-ChildItem $tmp -Recurse -Filter "llama-server.exe" | Select-Object -First 1
    Copy-Item (Join-Path $found.DirectoryName "*") $Bin -Recurse -Force
    if ($Backend -eq "cuda") {
        # библиотеки CUDA лежат отдельным архивом cudart-*.zip
        $cudart = $rel.assets | Where-Object { $_.name -match "^cudart.*win.*\.zip$" } | Select-Object -First 1
        if ($cudart) {
            $czip = Join-Path $env:TEMP $cudart.name
            Invoke-WebRequest $cudart.browser_download_url -OutFile $czip
            Expand-Archive $czip -DestinationPath $Bin -Force
        }
    }
    Remove-Item -Recurse -Force $tmp, $zip -ErrorAction SilentlyContinue
    Write-Host "   ✓ llama.cpp $($rel.tag_name)"
} else {
    Write-Host "== llama-server уже есть: $serverExe"
}

# 2. модель (докачивается, если оборвалось)
$modelPath = Join-Path $Models $entry.file
if (-not (Test-Path $modelPath)) {
    Write-Host "== Скачиваю модель $($entry.file) — это надолго"
    $part = "$modelPath.part"
    & curl.exe -fL --retry 5 -C - --progress-bar -o $part $entry.url
    if ($LASTEXITCODE -ne 0) { throw "Модель не скачалась. Проверьте адрес: $($entry.url)" }
    Move-Item $part $modelPath -Force
} else {
    Write-Host "== Модель уже есть: $modelPath"
}

# 3. ключ доступа: чтобы к мозгу не могла обратиться любая случайная страница
$keyFile = Join-Path $Home_ "key.txt"
if (-not (Test-Path $keyFile)) {
    $bytes = New-Object byte[] 18
    [Security.Cryptography.RandomNumberGenerator]::Create().GetBytes($bytes)
    [Convert]::ToBase64String($bytes).TrimEnd("=").Replace("+", "a").Replace("/", "b") | Set-Content $keyFile -NoNewline
}
$key = (Get-Content $keyFile -Raw).Trim()

# 4. скрипт запуска и ярлык
$threads = [math]::Max(1, [Environment]::ProcessorCount - 2)
$run = @"
@echo off
title Мозг Писаря — $($entry.name)
echo Мозг Писаря слушает http://127.0.0.1:$Port
echo Ключ доступа (вставьте на странице в окне "Мозг"): $key
echo Закройте это окно, чтобы выключить мозг.
"$serverExe" -m "$modelPath" --host 127.0.0.1 --port $Port --api-key $key -c 8192 --jinja -ngl 99 -t $threads --no-webui
pause
"@
$runPath = Join-Path $Home_ "start.cmd"
[IO.File]::WriteAllText($runPath, $run, (New-Object Text.UTF8Encoding $false))
$ws = New-Object -ComObject WScript.Shell
$lnk = $ws.CreateShortcut((Join-Path ([Environment]::GetFolderPath("Desktop")) "Мозг Писаря.lnk"))
$lnk.TargetPath = $runPath
$lnk.WorkingDirectory = $Home_
$lnk.Save()

Write-Host ""
Write-Host "✓ Готово. Запуск: ярлык «Мозг Писаря» на рабочем столе (или $runPath)."
Write-Host "  На странице: ⚙ Мозг → «Нейронка на моём компьютере» → адрес http://127.0.0.1:$Port"
Write-Host "  Ключ доступа: $key   (лежит в $keyFile)"
