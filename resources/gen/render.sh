#!/bin/sh
# Render the app icon and the menu-bar icons from their SVG sources.
#
# Sources:   resources/icon.svg          (1024x1024 app icon)
#            resources/tray.svg          (18x18 menu-bar glyph, template)
# Outputs:   resources/icon.png          (1024, what mygo.json packages)
#            resources/tray.png          (18x18 @1x)
#            resources/tray@2x.png       (36x36 @2x)
#            resources/favicon.png       (32x32, web)
#            resources/apple-touch-icon.png (180x180, web)
#
# Requires rsvg-convert (brew install librsvg). sips is used only for the PNG
# variants that differ by size alone.
set -eu
cd "$(dirname "$0")/.."

if ! command -v rsvg-convert >/dev/null 2>&1; then
  echo "render.sh: rsvg-convert not found (brew install librsvg)" >&2
  exit 1
fi

rsvg-convert -w 1024 -h 1024 icon.svg -o icon.png
rsvg-convert -w 32 -h 32 icon.svg -o favicon.png
rsvg-convert -w 180 -h 180 icon.svg -o apple-touch-icon.png

# The menu-bar glyph is drawn on an 18x18 grid; @2x is the same drawing at 36.
rsvg-convert -w 18 -h 18 tray.svg -o tray.png
rsvg-convert -w 36 -h 36 tray.svg -o tray@2x.png

# macOS menu-bar template images must be pure black with alpha; rsvg keeps the
# colours, so force the opaque pixels to black while keeping alpha.
python3 - <<'PY'
from pathlib import Path
import struct, zlib

def blacken(path):
    data = Path(path).read_bytes()
    # Minimal PNG round-trip: decode IHDR, read raw scanlines, set RGB to 0.
    pos = 8
    chunks, idat = [], b''
    w = h = bitdepth = colortype = None
    while pos < len(data):
        length = struct.unpack('>I', data[pos:pos+4])[0]
        ctype = data[pos+4:pos+8]
        body = data[pos+8:pos+8+length]
        if ctype == b'IHDR':
            w, h, bitdepth, colortype = struct.unpack('>IIBB', body[:10])
        if ctype == b'IDAT':
            idat += body
        if ctype not in (b'IDAT',):
            chunks.append((ctype, body))
        pos += 12 + length
    if colortype != 6 or bitdepth != 8:
        raise SystemExit(f'{path}: expected RGBA8, got colortype={colortype} depth={bitdepth}')
    raw = zlib.decompress(idat)
    stride = w * 4
    out = bytearray()
    prev = bytearray(stride)
    i = 0
    for _ in range(h):
        f = raw[i]; i += 1
        line = bytearray(raw[i:i+stride]); i += stride
        if f == 1:
            for x in range(4, stride):
                line[x] = (line[x] + line[x-4]) & 0xFF
        elif f == 2:
            for x in range(stride):
                line[x] = (line[x] + prev[x]) & 0xFF
        elif f == 3:
            for x in range(stride):
                left = line[x-4] if x >= 4 else 0
                line[x] = (line[x] + ((left + prev[x]) >> 1)) & 0xFF
        elif f == 4:
            for x in range(stride):
                a = line[x-4] if x >= 4 else 0
                b = prev[x]
                c = prev[x-4] if x >= 4 else 0
                p = a + b - c
                pa, pb, pc = abs(p-a), abs(p-b), abs(p-c)
                pr = a if (pa <= pb and pa <= pc) else (b if pb <= pc else c)
                line[x] = (line[x] + pr) & 0xFF
        for x in range(0, stride, 4):
            line[x] = line[x+1] = line[x+2] = 0
        out += b'\x00' + line
        prev = line
    def chunk(t, b):
        return struct.pack('>I', len(b)) + t + b + struct.pack('>I', zlib.crc32(t + b) & 0xFFFFFFFF)
    png = b'\x89PNG\r\n\x1a\n' + chunk(b'IHDR', struct.pack('>IIBBBBB', w, h, 8, 6, 0, 0, 0))
    png += chunk(b'IDAT', zlib.compress(bytes(out), 9)) + chunk(b'IEND', b'')
    Path(path).write_bytes(png)

for name in ('tray.png', 'tray@2x.png'):
    blacken(name)
PY

# The web pages and the tray load the icons from their own package directories
# (go:embed cannot reach outside a package), so keep those copies in step.
cp favicon.png apple-touch-icon.png ../web/static/
rsvg-convert -w 144 -h 144 icon.svg -o ../web/static/icon-144.png
cp tray.png tray@2x.png ../internal/host/assets/

echo "icons: icon.png favicon.png apple-touch-icon.png tray.png tray@2x.png"
echo "copies: web/static/{favicon,apple-touch-icon,icon-144}.png internal/host/assets/tray*.png"
