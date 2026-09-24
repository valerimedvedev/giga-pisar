// Диктовка в конкретное поле по кнопке.
//
// Нажали кнопку — пошла запись, нажали ещё раз — звук уходит в модель,
// а текст встаёт в поле туда, где стоял курсор (или вместо выделения).
// Состояние пишется словами в строку статуса — без волн и столбиков.
// Esc во время записи — отмена.

import { Mic } from "./mic.js";

const ru = (navigator.language || "ru").toLowerCase().startsWith("ru");
const L = (r, e) => (ru ? r : e);

const clock = (s) => `${Math.floor(s / 60)}:${String(Math.floor(s % 60)).padStart(2, "0")}`;

/** Вставляет text в поле вместо [start, end) и ставит курсор после него.
 *  Пробелы по краям добавляются, только если без них слова слипнутся. */
export function insertText(field, text, start, end) {
  const v = field.value;
  const before = v[start - 1], after = v[end];
  let t = text;
  if (before && !/\s/.test(before)) t = " " + t;
  if (after && !/[\s.,!?;:…)\]»"']/.test(after)) t = t + " ";
  field.setRangeText(t, start, end, "end");
  // сообщаем фреймворкам страницы, что значение поменялось
  field.dispatchEvent(new InputEvent("input", { bubbles: true, inputType: "insertText", data: t }));
}

/**
 * @param field   <textarea> или <input type=text>
 * @param button  кнопка «Диктовать»
 * @param status  элемент для строки состояния
 * @param engine  Engine из engine.js (модель грузится снаружи)
 */
export function attachDictation({ field, button, status, engine }) {
  const mic = new Mic();
  let state = "idle";          // idle | starting | recording | busy
  let enabled = true;
  let timer = null;
  let sel = { start: field.value.length, end: field.value.length };

  const say = (text, kind = "") => {
    status.textContent = text;
    status.dataset.kind = kind;
  };
  const label = () => {
    button.textContent = state === "recording" ? L("Стоп", "Stop") : L("Диктовать", "Dictate");
    button.setAttribute("aria-pressed", String(state === "recording"));
    button.disabled = !enabled || state === "starting" || state === "busy";
    button.dataset.state = state;
  };

  // Кнопка не должна забирать фокус у поля: иначе курсор и выделение теряются.
  button.addEventListener("mousedown", (e) => e.preventDefault());
  const remember = () => { sel = { start: field.selectionStart, end: field.selectionEnd }; };
  for (const ev of ["select", "keyup", "mouseup", "input", "focus", "blur"]) field.addEventListener(ev, remember);

  async function start() {
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
    const tick = () => say(`● ${L("Запись", "Recording")} ${clock(mic.seconds)} — ${L("нажмите «Стоп», когда закончите", "press Stop when done")}`, "recording");
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
      if (!text) {
        say(L("Ничего не расслышал — попробуйте ещё раз", "Heard nothing — try again"), "warn");
      } else {
        const focused = document.activeElement === field;
        const s = focused ? field.selectionStart : sel.start;
        const e = focused ? field.selectionEnd : sel.end;
        insertText(field, text, s, e);
        remember();
        say(L(`Готово: ${seconds.toFixed(1)} с речи за ${(ms / 1000).toFixed(1)} с`,
              `Done: ${seconds.toFixed(1)} s of speech in ${(ms / 1000).toFixed(1)} s`), "ok");
      }
    } catch (e) {
      say(L(`Не распознал: ${e.message}`, `Recognition failed: ${e.message}`), "error");
    }
    state = "idle";
    label();
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

  label();
  return {
    /** Кнопку можно нажимать (модель готова) или нет. */
    setEnabled(on) {
      enabled = on;
      label();
    },
    say,
    get state() { return state; },
  };
}
