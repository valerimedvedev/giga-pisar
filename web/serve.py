#!/usr/bin/env python3
"""Локальный сервер для браузерного Гиги Писаря.

    python3 web/serve.py            # http://localhost:8000
    python3 web/serve.py 8080

Отличие от «python3 -m http.server» — заголовки изоляции страницы
(COOP/COEP). С ними браузер разрешает WebAssembly-потоки, и распознавание
идёт в несколько ядер — в 2–3 раза быстрее. Без них всё тоже работает,
просто в один поток. На своём хостинге выставьте те же два заголовка.
"""
import http.server
import os
import sys


class Handler(http.server.SimpleHTTPRequestHandler):
    extensions_map = {
        **http.server.SimpleHTTPRequestHandler.extensions_map,
        ".js": "text/javascript",
        ".mjs": "text/javascript",
        ".wasm": "application/wasm",
    }

    def end_headers(self):
        self.send_header("Cross-Origin-Opener-Policy", "same-origin")
        self.send_header("Cross-Origin-Embedder-Policy", "require-corp")
        self.send_header("Cache-Control", "no-cache")
        super().end_headers()


def main():
    port = int(sys.argv[1]) if len(sys.argv) > 1 else 8000
    os.chdir(os.path.dirname(os.path.abspath(__file__)))
    server = http.server.ThreadingHTTPServer(("127.0.0.1", port), Handler)
    print(f"Гига Писарь: http://localhost:{port}  (Ctrl+C — выход)")
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass


if __name__ == "__main__":
    main()
