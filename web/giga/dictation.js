// Диктовка в конкретное поле по кнопке.
//
// Нажали кнопку — пошла запись, нажали ещё раз — звук уходит в модель,
// а текст встаёт в поле туда, где стоял курсор (или вместо выделения).
// Состояние пишется словами в строку статуса — без волн и столбиков.
// Esc во время записи — отмена.
//
// С мозгом (brain.js) — как в приложении:
//   «…текст, Писарь, исправь» — текст идёт в нейронку с этой командой;
//   выделили текст и продиктовали команду — она выполняется над выделенным;
//   runCommand() — то же для кнопок «Причесать», «Сократить» и т. п.
// После правки нейронкой можно «Вернуть как было».

import { Mic } from "./mic.js";
import { parseCommand, stripAddress } from "./brain.js";

const ru = (navigator.language || "ru").toLowerCase().startsWith("ru");
const L = (r, e) => (ru ? r : e);

const clock = (s) => `${Math.floor(s / 60)}:${String(Math.floor(s % 60)).padStart(2, "0")}`;

/** Вставляет text в поле вместо [start, end) и ставит курсор после него.
 *  spacing — пробелы по краям, только если без них слова слипнутся. */
export function insertText(field, text, start, end, spacing = true) {
  const v = field.value;
  const before = v[start - 1], after = v[end];
  let t = text;
  if (spacing && before && !/\s/.test(before)) t = " " + t;
  if (spacing && after && !/[\s.,!?;:…)\]»"']/.test(after)) t = t + " ";
  field.setRangeText(t, start, end, "end");
  // сообщаем фреймворкам страницы, что значение поменялось
  field.dispatchEvent(new InputEvent("input", { bubbles: true, inputType: "insertText", data: t }));
}

/**
 * @param field   <textarea> или <input type=text>
 * @param button  кнопка «Диктовать»
 * @param status  элемент для строки состояния
 * @param engine  Engine из engine.js (модель грузится снаружи)
 * @param brain   Brain из brain.js (необязательно)
 * @param revert  кнопка «Вернуть как было» (необязательно)
 * @param onBusy  (busy: boolean) => void — чтобы страница гасила свои кнопки
 */
export function attachDictation({ field, button, status, engine, brain = null, revert = null, onBusy = () => {} }) {
  const mic = new Mic();
  let state = "idle";          // idle | starting | recording | busy
  let enabled = true;
  let timer = null;
  let sel = { start: field.value.length, end: field.value.length };
  let selAtStart = null;       // выделение в момент нажатия «Диктовать»
  let undo = null;             // { value, start, end } — до правки нейронкой
  let ours = false;            // input-событие от нас самих, а не от человека

  const say = (text, kind = "") => {
    status.textContent = text;
    status.dataset.kind = kind;
  };
  const label = () => {
    button.textContent = state === "recording" ? L("Стоп", "Stop") : L("Диктовать", "Dictate");
    button.setAttribute("aria-pressed", String(state === "recording"));
    button.disabled = !enabled || state === "starting" || state === "busy";
    button.dataset.state = state;
    onBusy(state !== "idle");
  };
  const setUndo = (u) => {
    undo = u;
    if (revert) revert.hidden = !u;
  };

  // Кнопки не должны забирать фокус у поля: иначе курсор и выделение теряются.
  button.addEventListener("mousedown", (e) => e.preventDefault());
  revert?.addEventListener("mousedown", (e) => e.preventDefault());
  const remember = () => { sel = { start: field.selectionStart, end: field.selectionEnd }; };
  for (const ev of ["select", "keyup", "mouseup", "input", "focus", "blur"]) field.addEventListener(ev, remember);
  // Человек сам поправил текст — откатывать правку нейронки уже нельзя.
  field.addEventListener("input", () => { if (!ours) setUndo(null); });

  const currentSel = () => document.activeElement === field
    ? { start: field.selectionStart, end: field.selectionEnd }
    : { ...sel };

  function put(text, start, end, spacing = true) {
    ours = true;
    try { insertText(field, text, start, end, spacing); } finally { ours = false; }
    remember();
  }

  /** Правка нейронкой куска [start, end): текст заменяется, старый — в «Вернуть». */
  async function brainReplace(source, command, mode, start, end, spacing) {
    const before = { value: field.value, start, end };
    const out = await brain.transform(source, command, mode, (stage) => say(stage, "busy"));
    put(out, start, end, spacing);
    setUndo(before);
    return out;
  }

  async function start() {
    selAtStart = null;
    const s = currentSel();
    if (s.end > s.start) selAtStart = s;
    state = "starting";
    label();
    say(L("Включаю микрофон…", "Starting the microphone…"));
    try {
      await mic.start();
    } catch (e) {
      state = "idle";
      label();
      const denied = e?.name === "NotAllowedError" || e?.name === "SecurityError";
      say(denied
        ? L("Нет доступа к микрофону — разрешите его в настройках сайта", "No microphone access — allow it in the site settings")
        : L(`Микрофон не включился: ${e.message}`, `Microphone failed: ${e.message}`), "error");
      return;
    }
    state = "recording";
    label();
    const hint = selAtStart && brain?.ready
      ? L("скажите команду над выделенным", "say a command for the selection")
      : L("нажмите «Стоп», когда закончите", "press Stop when done");
    const tick = () => say(`● ${L("Запись", "Recording")} ${clock(mic.seconds)} — ${hint}`, "recording");
    tick();
    timer = setInterval(tick, 500);
  }

  async function stop() {
    clearInterval(timer);
    state = "busy";
    label();
    const samples = await mic.stop();
    const seconds = samples.length / 16000;
    if (seconds < 0.3) {
      state = "idle";
      label();
      say(L("Слишком коротко — нажмите и говорите", "Too short — press and speak"), "warn");
      return;
    }
    say(L(`Распознаю ${seconds.toFixed(1)} с записи…`, `Recognizing ${seconds.toFixed(1)} s of audio…`), "busy");
    try {
      const { text, ms } = await engine.transcribe(samples);
      const done = L(`Готово: ${seconds.toFixed(1)} с речи за ${(ms / 1000).toFixed(1)} с`,
                     `Done: ${seconds.toFixed(1)} s of speech in ${(ms / 1000).toFixed(1)} s`);
      if (!text) {
        say(L("Ничего не расслышал — попробуйте ещё раз", "Heard nothing — try again"), "warn");
      } else {
        await handleSpeech(text, done);
      }
    } catch (e) {
      say(L(`Не распознал: ${e.message}`, `Recognition failed: ${e.message}`), "error");
    }
    state = "idle";
    label();
  }

  /** Что делать с распознанным: вставить, отдать мозгу с командой
   *  или выполнить как команду над выделенным. */
  async function handleSpeech(text, done) {
    const at = currentSel();

    // Было выделение при нажатии «Диктовать» и мозг готов — это команда над ним.
    if (selAtStart && brain?.ready && field.value.length >= selAtStart.end) {
      const { start, end } = selAtStart;
      const cmd = stripAddress(text);
      if (cmd) {
        try {
          await brainReplace(field.value.slice(start, end), cmd, "selection", start, end, false);
          say(L(`Готово: «${cmd}» над выделенным`, `Done: “${cmd}” on the selection`), "ok");
        } catch (e) {
          say(L(`Писарь не справился (${e.message}). Выделенное не тронул`,
                `Pisar could not do it (${e.message}). The selection is untouched`), "error");
        }
        return;
      }
    }

    // «…текст, Писарь, команда» — сперва текст идёт в мозг.
    const parsed = parseCommand(text);
    if (parsed && brain?.ready) {
      try {
        await brainReplace(parsed.body, parsed.command, "dictation", at.start, at.end, true);
        say(L(`Готово: «${parsed.command}»`, `Done: “${parsed.command}”`), "ok");
      } catch (e) {
        put(text, at.start, at.end);
        say(L(`Писарь не справился (${e.message}) — вставил как есть`,
              `Pisar could not do it (${e.message}) — inserted as is`), "error");
      }
      return;
    }

    put(text, at.start, at.end);
    if (parsed && brain && !brain.ready) {
      say(L("Похоже на команду Писарю — выберите мозг ниже", "Sounded like a Pisar command — choose a brain below"), "warn");
    } else {
      say(done, "ok");
    }
  }

  async function cancel() {
    clearInterval(timer);
    await mic.cancel();
    state = "idle";
    label();
    say(L("Запись отменена", "Recording cancelled"));
  }

  button.addEventListener("click", () => {
    if (state === "idle") start();
    else if (state === "recording") stop();
  });
  document.addEventListener("keydown", (e) => {
    if (e.key === "Escape" && state === "recording") cancel();
  });
  revert?.addEventListener("click", () => {
    if (!undo || state !== "idle") return;
    ours = true;
    try {
      field.value = undo.value;
      field.setSelectionRange(undo.start, undo.end);
      field.dispatchEvent(new InputEvent("input", { bubbles: true, inputType: "historyUndo" }));
    } finally { ours = false; }
    remember();
    setUndo(null);
    say(L("Вернул как было", "Restored"), "ok");
  });

  setUndo(null);
  label();
  return {
    /** Кнопку можно нажимать (модель готова) или нет. */
    setEnabled(on) {
      enabled = on;
      label();
    },
    say,
    get state() { return state; },

    /** Команда мозгу над выделенным, а если ничего не выделено — над всем текстом. */
    async runCommand(command) {
      if (state !== "idle" || !brain?.ready) return;
      let { start, end } = currentSel();
      if (end <= start) { start = 0; end = field.value.length; }
      const source = field.value.slice(start, end);
      if (!source.trim()) {
        say(L("Сначала надиктуйте или впишите текст", "Dictate or type some text first"), "warn");
        return;
      }
      state = "busy";
      label();
      try {
        await brainReplace(source, command, "selection", start, end, false);
        say(L("Готово. Не понравилось — «Вернуть как было»", "Done. Not happy — “Put it back”"), "ok");
      } catch (e) {
        say(L(`Писарь не справился (${e.message}). Текст не тронул`,
              `Pisar could not do it (${e.message}). The text is untouched`), "error");
      }
      state = "idle";
      label();
    },
  };
}
