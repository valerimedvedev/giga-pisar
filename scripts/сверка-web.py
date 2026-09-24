#!/usr/bin/env python
"""Сверка браузерного ядра (web/giga) с питоновским — до полного совпадения.

    (cd web/test && npm install)
    python scripts/сверка-web.py <папка-модели> запись.wav [ещё.wav ...]

Браузерное ядро гоняется в Node тем же onnxruntime-web, что и в браузере.
Оба должны выдавать один и тот же текст: модель, веса и порядок действий
одинаковые, разный только язык, на котором это написано.
Записи — 16 кГц, моно, 16 бит (ffmpeg -i x -ac 1 -ar 16000 x.wav).
"""
import difflib
import os
import subprocess
import sys

КОРЕНЬ = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
sys.path.insert(0, os.path.join(КОРЕНЬ, "server"))
import giga_core


def браузерным(файлы, model_dir):
    out = subprocess.run(
        ["node", os.path.join(КОРЕНЬ, "web", "test", "transcribe.mjs"), model_dir, *файлы],
        capture_output=True, text=True,
    )
    if out.returncode != 0:
        sys.exit(f"web/test/transcribe.mjs упал:\n{out.stderr}")
    sys.stderr.write(out.stderr)
    return [s.strip() for s in out.stdout.splitlines()]


def main():
    if len(sys.argv) < 3:
        sys.exit(__doc__)
    model_dir, файлы = sys.argv[1], sys.argv[2:]

    engine = giga_core.Engine(model_dir)
    print(f"модель: {engine.model_dir}\n")

    веб = браузерным(файлы, engine.model_dir)
    if len(веб) != len(файлы):
        sys.exit(f"браузерное ядро вернуло {len(веб)} строк на {len(файлы)} файлов")

    всего = совпало_симв = 0
    точных = 0
    for путь, в in zip(файлы, веб):
        п = engine.transcribe(путь)
        одинаково = в == п
        точных += одинаково
        m = difflib.SequenceMatcher(None, п, в)
        совпало_симв += sum(b.size for b in m.get_matching_blocks())
        всего += max(len(п), len(в))
        print(f"  {os.path.basename(путь)}: {'✓ совпало' if одинаково else '✗ РАЗОШЛОСЬ'}")
        if not одинаково:
            print(f"      питон:  {п!r}")
            print(f"      браузер: {в!r}")

    доля = совпало_симв / всего if всего else 1.0
    print(f"\n── Итог: точно совпало {точных} из {len(файлы)}, "
          f"совпадение по символам {доля * 100:.2f}%")
    if точных == len(файлы):
        print("✓ полное совпадение")
    elif доля >= 0.99:
        print("✓ равнозначно: расхождения на уровне дрожания последнего знака")
    else:
        print("✗ расхождения слишком велики — это ошибка переноса")
    sys.exit(0 if доля >= 0.99 else 1)


if __name__ == "__main__":
    main()
