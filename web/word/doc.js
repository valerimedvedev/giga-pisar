// Документ Word глазами Писаря: что обрабатывать, куда вставлять, как откатить.
// Всё через Office.js (Word.run). Область работы — выделенное, а если ничего
// не выделено — абзац с курсором: замена целого документа убила бы оформление.

/* global Word */

const SPACE_AFTER = /[\s.,!?;:…)\]»"']/;

/** Что сейчас под рукой: { kind: "selection"|"paragraph", text }. */
export async function readScope() {
  return Word.run(async (ctx) => {
    const sel = ctx.document.getSelection();
    sel.load("text,isEmpty");
    const para = sel.paragraphs.getFirst();
    para.load("text");
    await ctx.sync();
    if (!sel.isEmpty && sel.text.trim()) return { kind: "selection", text: sel.text };
    return { kind: "paragraph", text: para.text };
  });
}

/** Заменяет область текстом; возвращает ручку для отката. */
export async function replaceScope(kind, text) {
  return Word.run(async (ctx) => {
    const sel = ctx.document.getSelection();
    const target = kind === "selection" ? sel : sel.paragraphs.getFirst();
    target.load("text");
    await ctx.sync();
    const old = target.text;
    const range = target.insertText(text, "Replace");
    range.track();
    range.select("Select");
    await ctx.sync();
    return { old, range };
  });
}

/** Вставляет распознанное туда, где курсор (или вместо выделения), с пробелами по краям при нужде. */
export async function insertAtCursor(text) {
  return Word.run(async (ctx) => {
    const sel = ctx.document.getSelection();
    const para = sel.paragraphs.getFirst();
    const before = para.getRange("Start").expandTo(sel.getRange("Start"));
    const after = sel.getRange("End").expandTo(para.getRange("End"));
    before.load("text");
    after.load("text");
    await ctx.sync();
    let t = text;
    const b = before.text.slice(-1), a = after.text.charAt(0);
    if (b && !/\s/.test(b)) t = " " + t;
    if (a && !SPACE_AFTER.test(a)) t = t + " ";
    const range = sel.insertText(t, "Replace");
    range.select("End");
    await ctx.sync();
  });
}

/** Откат правки нейронкой: старый текст на место нового. */
export async function undo(handle) {
  if (!handle) return;
  await Word.run(handle.range, async (ctx) => {
    const r = handle.range.insertText(handle.old, "Replace");
    r.select("Select");
    handle.range.untrack();
    await ctx.sync();
  });
}
