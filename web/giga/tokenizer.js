// Превращение номеров, которые выдаёт модель, обратно в текст.
//
// Модель говорит кусочками слов (SentencePiece). Чтобы собрать из них текст,
// нужен только список этих кусочков — он лежит в v3_e2e_rnnt_tokenizer.model.
// Файл записан в формате protobuf, но нам нужно одно поле, поэтому разбираем
// его сами, как и swift/Tokenizer.swift:
//
//   ModelProto    { repeated SentencePiece pieces = 1 }
//   SentencePiece { optional string piece = 1 }

const utf8 = new TextDecoder("utf-8");

function varint(d, from) {
  let result = 0n, shift = 0n, i = from;
  while (i < d.length) {
    const b = d[i++];
    result |= BigInt(b & 0x7f) << shift;
    if ((b & 0x80) === 0) return [Number(result), i];
    shift += 7n;
    if (shift > 63n) return null;
  }
  return null;
}

function skip(d, from, wire) {
  switch (wire) {
    case 0: { const v = varint(d, from); return v ? v[1] : null; }
    case 1: return from + 8 <= d.length ? from + 8 : null;
    case 2: {
      const v = varint(d, from);
      if (!v) return null;
      const end = v[1] + v[0];
      return end <= d.length ? end : null;
    }
    case 5: return from + 4 <= d.length ? from + 4 : null;
    default: return null;
  }
}

/** Внутри SentencePiece берём первое поле — саму строку кусочка. */
function piece(d, from, to) {
  let i = from;
  while (i < to) {
    const t = varint(d, i);
    if (!t) break;
    i = t[1];
    const field = Math.floor(t[0] / 8), wire = t[0] & 7;
    if (field === 1 && wire === 2) {
      const l = varint(d, i);
      if (!l) break;
      const end = Math.min(l[1] + l[0], to);
      return utf8.decode(d.subarray(l[1], end));
    }
    const next = skip(d, i, wire);
    if (next === null) break;
    i = next;
  }
  return "";
}

export class Tokenizer {
  /** bytes — содержимое файла *_tokenizer.model (Uint8Array или ArrayBuffer). */
  constructor(bytes) {
    const d = bytes instanceof Uint8Array ? bytes : new Uint8Array(bytes);
    const out = [];
    let i = 0;
    while (i < d.length) {
      const t = varint(d, i);
      if (!t) break;
      i = t[1];
      const field = Math.floor(t[0] / 8), wire = t[0] & 7;
      if (field === 1 && wire === 2) {
        const l = varint(d, i);
        if (!l) break;
        const end = l[1] + l[0];
        if (end > d.length) break;
        out.push(piece(d, l[1], end));
        i = end;
      } else {
        const next = skip(d, i, wire);
        if (next === null) break;
        i = next;
      }
    }
    if (out.length === 0) throw new Error("не разобрал токенизатор");
    this.pieces = out;
  }

  /** Номер «пустышки» — модель выдаёт его, когда сказать нечего. */
  get blankId() {
    return this.pieces.length;
  }

  /** Собирает текст: кусочки склеиваются, знак ▁ означает пробел перед словом. */
  decode(ids) {
    let s = "";
    for (const id of ids) if (id >= 0 && id < this.pieces.length) s += this.pieces[id];
    return s.replaceAll("▁", " ").trim();
  }
}
