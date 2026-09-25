# Иконка GigaBrain без внешних библиотек: PNG-кадры (256/48/32/16) упакованы в .ico
import struct, zlib, math

def png(size):
    S = size; rows = []
    cx = cy = (S - 1) / 2; R = S * 0.47
    def blend(dst, src, a):
        return tuple(int(dst[i] * (1 - a) + src[i] * a) for i in range(3))
    for y in range(S):
        row = bytearray([0])
        for x in range(S):
            d = math.hypot(x - cx, y - cy)
            a = max(0.0, min(1.0, R - d + 0.5))          # круг с антиалиасом
            t = (x + y) / (2 * S)
            col = blend((94, 53, 177), (156, 39, 176), t)  # фиолетовый градиент
            # «мозг»: два овала-полушария и складка посередине
            lx, rx = cx - S * 0.11, cx + S * 0.11
            inl = (((x - lx) / (S * 0.20)) ** 2 + ((y - cy) / (S * 0.24)) ** 2) <= 1
            inr = (((x - rx) / (S * 0.20)) ** 2 + ((y - cy) / (S * 0.24)) ** 2) <= 1
            gap = abs(x - cx) < S * 0.012 and abs(y - cy) < S * 0.22
            if (inl or inr) and not gap:
                col = (255, 255, 255)
                # извилины: тонкие дуги
                for k in (0.10, 0.17):
                    for ox in (lx, rx):
                        rr = math.hypot(x - ox, y - cy)
                        if abs(rr - S * k) < S * 0.012:
                            col = (206, 147, 216)
            row += bytes(col) + bytes([int(255 * a)])
        rows.append(bytes(row))
    raw = b"".join(rows)
    def chunk(t, d):
        return struct.pack(">I", len(d)) + t + d + struct.pack(">I", zlib.crc32(t + d) & 0xffffffff)
    return (b"\x89PNG\r\n\x1a\n" + chunk(b"IHDR", struct.pack(">IIBBBBB", S, S, 8, 6, 0, 0, 0))
            + chunk(b"IDAT", zlib.compress(raw, 9)) + chunk(b"IEND", b""))

sizes = [256, 48, 32, 16]
frames = [png(s) for s in sizes]
out = struct.pack("<HHH", 0, 1, len(sizes))
off = 6 + 16 * len(sizes)
for s, f in zip(sizes, frames):
    out += struct.pack("<BBBBHHII", s % 256, s % 256, 0, 0, 1, 32, len(f), off)
    off += len(f)
out += b"".join(frames)
open("gigabrain.ico", "wb").write(out)
open("gigabrain-256.png", "wb").write(frames[0])
print("ok", len(out))
