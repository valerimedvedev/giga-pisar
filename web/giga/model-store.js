// Где живёт модель в браузере: в Cache Storage самой страницы.
//
// Модель весит 309 МБ, и тянуть её при каждом открытии нельзя. Поэтому
// её один раз кладут в хранилище браузера — из архива, который человек
// выбрал у себя на диске, или с сервера, где лежит страница, — и дальше
// она читается оттуда без сети. Работает и в странице, и в воркере.
//
// Архив — тот же gigaam-v3-onnx-int8.tar.gz, что качает маковское
// приложение. Распаковываем его сами, потоком: gzip снимает браузер
// (DecompressionStream), tar разбираем здесь.

import { MODEL_FILES } from "./recognizer.js";

export const CACHE_NAME = "giga-pisar-model-v3";
// Ключи в кеше должны быть адресами. Берём выдуманный постоянный адрес,
// чтобы страница и воркер (у них разные location) видели одно и то же.
const KEY_BASE = "https://giga-pisar.invalid/model/";
const NEEDED = Object.values(MODEL_FILES);

/** Примерный размер распакованной модели — для процентов, когда сервер размер не сказал. */
export const MODEL_BYTES = 325_000_000;
/** Архив модели там, где его выкладывает автор приложения. */
export const ARCHIVE_URL =
  "https://github.com/moznoazachem/giga-pisar-cli/releases/download/v1.0/gigaam-v3-onnx-int8.tar.gz";

const key = (name) => KEY_BASE + name;

export function storageAvailable() {
  return typeof caches !== "undefined";
}

/** Все пять файлов на месте? */
export async function hasModel() {
  if (!storageAvailable()) return false;
  const cache = await caches.open(CACHE_NAME);
  for (const name of NEEDED) if (!(await cache.match(key(name)))) return false;
  return true;
}

/** Файлы модели для Recognizer.create: yaml строкой, остальное байтами. */
export async function readModel() {
  const cache = await caches.open(CACHE_NAME);
  const get = async (name) => {
    const r = await cache.match(key(name));
    if (!r) throw new Error(`в хранилище нет файла модели ${name}`);
    return r;
  };
  return {
    yaml: await (await get(MODEL_FILES.yaml)).text(),
    encoder: new Uint8Array(await (await get(MODEL_FILES.encoder)).arrayBuffer()),
    decoder: new Uint8Array(await (await get(MODEL_FILES.decoder)).arrayBuffer()),
    joint: new Uint8Array(await (await get(MODEL_FILES.joint)).arrayBuffer()),
    tokenizer: new Uint8Array(await (await get(MODEL_FILES.tokenizer)).arrayBuffer()),
  };
}

export async function deleteModel() {
  if (storageAvailable()) await caches.delete(CACHE_NAME);
}

/** Сколько места занимает модель, байт (0 — если её нет). */
export async function modelSize() {
  if (!storageAvailable()) return 0;
  const cache = await caches.open(CACHE_NAME);
  let total = 0;
  for (const name of NEEDED) {
    const r = await cache.match(key(name));
    if (r) total += Number(r.headers.get("content-length")) || 0;
  }
  return total;
}

async function put(cache, name, blob) {
  try {
    await cache.put(key(name), new Response(blob, {
      headers: { "content-length": String(blob.size), "content-type": "application/octet-stream" },
    }));
  } catch (e) {
    // Чаще всего это место: в приватном окне хранилище урезано до крох,
    // а браузер называет это «внутренней ошибкой».
    throw new Error(`браузер не дал сохранить модель (нужно около ${Math.round(MODEL_BYTES / 1e6)} МБ). ` +
      `Освободите место на диске или откройте страницу в обычном, не приватном окне. [${e.message}]`);
  }
}

/** Просим браузер не выселять модель при нехватке места. Отказ не страшен. */
async function persist() {
  try { await navigator.storage?.persist?.(); } catch { /* ну и ладно */ }
}

function checkComplete(found) {
  const missing = NEEDED.filter((n) => !found.has(n));
  if (missing.length) throw new Error(`в архиве не хватает файлов модели: ${missing.join(", ")}`);
}

// ─────────────────────────── распаковка tar.gz ───────────────────────────

/** Очередь байтов: куски приходят как придут, а читать надо ровно по 512. */
class ByteQueue {
  constructor(reader) {
    this.reader = reader;
    this.chunks = [];
    this.length = 0;
    this.done = false;
  }
  async fill(n) {
    while (this.length < n && !this.done) {
      const { value, done } = await this.reader.read();
      if (done) { this.done = true; break; }
      if (value.length) { this.chunks.push(value); this.length += value.length; }
    }
    return this.length >= n;
  }
  /** Ровно n байт одним куском (для заголовков). */
  take(n) {
    const out = new Uint8Array(n);
    let off = 0;
    while (off < n) {
      const c = this.chunks[0];
      const k = Math.min(c.length, n - off);
      out.set(c.subarray(0, k), off);
      off += k;
      this.consume(k);
    }
    return out;
  }
  /** До n байт — сколько лежит в первом куске, без копирования. */
  takeUpTo(n) {
    const c = this.chunks[0];
    const k = Math.min(c.length, n);
    const out = c.subarray(0, k);
    this.consume(k);
    return out;
  }
  consume(k) {
    const c = this.chunks[0];
    if (k === c.length) this.chunks.shift();
    else this.chunks[0] = c.subarray(k);
    this.length -= k;
  }
}

const ascii = new TextDecoder("utf-8");
const field = (h, from, len) => {
  const s = h.subarray(from, from + len);
  const end = s.indexOf(0);
  return ascii.decode(end >= 0 ? s.subarray(0, end) : s);
};

function tarSize(h) {
  // Большие файлы GNU tar пишет двоичным числом со старшим битом.
  if (h[124] & 0x80) {
    let n = 0;
    for (let i = 125; i < 136; i++) n = n * 256 + h[i];
    return n;
  }
  return parseInt(field(h, 124, 12).trim() || "0", 8);
}

/** Имя из pax-заголовка ("27 path=folder/file.onnx\n"). */
function paxPath(bytes) {
  const text = ascii.decode(bytes);
  for (const rec of text.split("\n")) {
    const m = /^\d+ path=(.*)$/.exec(rec);
    if (m) return m[1];
  }
  return null;
}

/** Разбирает поток tar и складывает нужные файлы в кеш. */
async function untarToCache(stream, cache) {
  const q = new ByteQueue(stream.getReader());
  const found = new Set();
  let longName = null;

  while (await q.fill(512)) {
    const h = q.take(512);
    if (h.every((b) => b === 0)) break;               // конец архива
    const size = tarSize(h);
    const type = String.fromCharCode(h[156] || 48);   // '0' — обычный файл
    const prefix = field(h, 345, 155);
    let name = longName ?? (prefix ? `${prefix}/${field(h, 0, 100)}` : field(h, 0, 100));
    longName = null;

    // тело записи: либо копим, либо пропускаем
    const wantMeta = type === "x" || type === "L";
    const base = name.split("/").pop();
    const wantFile = (type === "0" || type === "\0") && NEEDED.includes(base) && !base.startsWith("._");
    const parts = [];
    let left = size;
    while (left > 0) {
      if (!(await q.fill(1))) throw new Error("архив оборвался — скачайте его заново");
      const piece = q.takeUpTo(left);
      if (wantMeta || wantFile) parts.push(piece);
      left -= piece.length;
    }
    const pad = (512 - (size % 512)) % 512;
    if (pad) {
      if (!(await q.fill(pad))) throw new Error("архив оборвался — скачайте его заново");
      q.take(pad);
    }

    if (type === "x") longName = paxPath(await new Blob(parts).arrayBuffer());
    else if (type === "L") longName = field(new Uint8Array(await new Blob(parts).arrayBuffer()), 0, size);
    else if (wantFile) {
      await put(cache, base, new Blob(parts));
      found.add(base);
    }
  }
  return found;
}

/** Поток, который считает прошедшие через него байты. */
function counting(stream, onBytes) {
  let seen = 0;
  return stream.pipeThrough(new TransformStream({
    transform(chunk, ctl) {
      seen += chunk.length;
      onBytes(seen);
      ctl.enqueue(chunk);
    },
  }));
}

async function importArchiveStream(stream, total, onProgress) {
  if (typeof DecompressionStream === "undefined") {
    throw new Error("этот браузер не умеет распаковывать gzip — обновите его");
  }
  await deleteModel();
  const cache = await caches.open(CACHE_NAME);
  const body = counting(stream, (n) => onProgress?.(n, total))
    .pipeThrough(new DecompressionStream("gzip"));
  try {
    checkComplete(await untarToCache(body, cache));
  } catch (e) {
    await deleteModel();                              // полмодели хуже, чем ничего
    throw e;
  }
  await persist();
}

// ─────────────────────────── откуда берётся модель ───────────────────────────

/** Модель из файлов на диске: архив .tar.gz или уже распакованные файлы. */
export async function importFiles(fileList, onProgress) {
  const files = [...fileList];
  const archive = files.find((f) => /\.(tar\.gz|tgz)$/i.test(f.name));
  if (archive) return importArchiveStream(archive.stream(), archive.size, onProgress);

  const byName = new Map(files.map((f) => [f.name, f]));
  const missing = NEEDED.filter((n) => !byName.has(n));
  if (missing.length) throw new Error(`не хватает файлов модели: ${missing.join(", ")}`);

  await deleteModel();
  const cache = await caches.open(CACHE_NAME);
  const total = NEEDED.reduce((s, n) => s + byName.get(n).size, 0);
  let done = 0;
  try {
    for (const name of NEEDED) {
      const f = byName.get(name);
      await put(cache, name, f);
      done += f.size;
      onProgress?.(done, total);
    }
  } catch (e) {
    await deleteModel();
    throw e;
  }
  await persist();
}

/** Модель с сервера: адрес архива .tar.gz или папки с распакованными файлами. */
export async function downloadModel(url, onProgress) {
  if (/\.(tar\.gz|tgz)$/i.test(new URL(url, location.href).pathname)) {
    const r = await fetch(url);
    if (!r.ok) throw new Error(`сервер не отдал архив модели (${r.status})`);
    return importArchiveStream(r.body, Number(r.headers.get("content-length")) || 0, onProgress);
  }

  const base = url.endsWith("/") ? url : url + "/";
  await deleteModel();
  const cache = await caches.open(CACHE_NAME);
  let done = 0;
  try {
    for (const name of NEEDED) {
      const r = await fetch(base + name);
      if (!r.ok) throw new Error(`сервер не отдал ${name} (${r.status})`);
      const start = done;
      const body = counting(r.body, (n) => { done = start + n; onProgress?.(done, MODEL_BYTES); });
      await put(cache, name, await new Response(body).blob());
    }
  } catch (e) {
    await deleteModel();
    throw e;
  }
  onProgress?.(MODEL_BYTES, MODEL_BYTES);
  await persist();
}

/** Лежит ли модель рядом со страницей (папка или архив)? Вернёт адрес или null. */
export async function findServerModel(candidates) {
  for (const url of candidates) {
    const probe = /\.(tar\.gz|tgz)$/i.test(url) ? url : (url.endsWith("/") ? url : url + "/") + MODEL_FILES.yaml;
    try {
      const r = await fetch(probe, { method: "HEAD", cache: "no-store" });
      if (r.ok) return url;
    } catch { /* нет — так нет */ }
  }
  return null;
}
