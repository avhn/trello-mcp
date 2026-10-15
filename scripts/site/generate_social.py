"""Generate the checked-in 1200x630 social card with only the standard library.

Run manually with Python 3. Deployment does not generate or build assets.
"""
from pathlib import Path
import argparse
import struct
import zlib

# Original 5x7 bitmap lettering avoids font files and platform-dependent rendering.
FONT = {
    'A': '01110/10001/10001/11111/10001/10001/10001',
    'B': '11110/10001/10001/11110/10001/10001/11110',
    'C': '01111/10000/10000/10000/10000/10000/01111',
    'D': '11110/10001/10001/10001/10001/10001/11110',
    'E': '11111/10000/10000/11110/10000/10000/11111',
    'F': '11111/10000/10000/11110/10000/10000/10000',
    'G': '01111/10000/10000/10111/10001/10001/01111',
    'H': '10001/10001/10001/11111/10001/10001/10001',
    'I': '11111/00100/00100/00100/00100/00100/11111',
    'J': '00111/00010/00010/00010/10010/10010/01100',
    'K': '10001/10010/10100/11000/10100/10010/10001',
    'L': '10000/10000/10000/10000/10000/10000/11111',
    'M': '10001/11011/10101/10101/10001/10001/10001',
    'N': '10001/11001/10101/10011/10001/10001/10001',
    'O': '01110/10001/10001/10001/10001/10001/01110',
    'P': '11110/10001/10001/11110/10000/10000/10000',
    'Q': '01110/10001/10001/10001/10101/10010/01101',
    'R': '11110/10001/10001/11110/10100/10010/10001',
    'S': '01111/10000/10000/01110/00001/00001/11110',
    'T': '11111/00100/00100/00100/00100/00100/00100',
    'U': '10001/10001/10001/10001/10001/10001/01110',
    'V': '10001/10001/10001/10001/10001/01010/00100',
    'W': '10001/10001/10001/10101/10101/10101/01010',
    'X': '10001/10001/01010/00100/01010/10001/10001',
    'Y': '10001/10001/01010/00100/00100/00100/00100',
    'Z': '11111/00001/00010/00100/01000/10000/11111',
    '-': '00000/00000/00000/11111/00000/00000/00000',
    '+': '00000/00100/00100/11111/00100/00100/00000',
    '/': '00001/00001/00010/00100/01000/10000/10000',
    '.': '00000/00000/00000/00000/00000/00110/00110',
    ' ': '00000/00000/00000/00000/00000/00000/00000',
}
WIDTH, HEIGHT = 1200, 630


def render_scanlines():
    background = (17, 29, 27)
    ink = (230, 243, 227)
    accent = (164, 214, 171)
    muted = (177, 197, 186)
    pixels = bytearray(bytes(background) * WIDTH * HEIGHT)

    def rect(x, y, width, height, color):
        if not (0 <= x < x + width <= WIDTH and 0 <= y < y + height <= HEIGHT):
            raise ValueError('Drawing exceeds the image bounds')
        row = bytes(color) * width
        for line in range(y, y + height):
            start = (line * WIDTH + x) * 3
            pixels[start:start + len(row)] = row

    def text(value, x, y, scale, color):
        for char in value.upper():
            for row, bits in enumerate(FONT[char].split('/')):
                for col, bit in enumerate(bits):
                    if bit == '1':
                        rect(x + col * scale, y + row * scale, scale, scale, color)
            x += scale * 6

    rect(0, 0, WIDTH, 12, accent)
    rect(82, 70, 54, 54, accent)
    # Original TM monogram, not a product or platform logo.
    text('TM', 92, 86, 3, background)
    text('GO + MODEL CONTEXT PROTOCOL', 162, 86, 3, accent)
    text('TRELLO-MCP', 82, 177, 12, ink)
    text('TOKEN-EFFICIENT TRELLO MCP SERVER', 82, 300, 4, ink)
    text('LOCAL STDIO / WORKSPACE ALLOWLIST', 82, 368, 4, muted)
    text('READ-ONLY OPT-IN', 82, 414, 4, muted)
    rect(82, 486, 1036, 2, (61, 84, 75))
    text('CLAUDE + CODEX + CHATGPT', 82, 525, 4, accent)
    text('AVHN.GITHUB.IO/TRELLO-MCP', 82, 581, 2, muted)

    stride = WIDTH * 3
    return b''.join(b'\x00' + pixels[y * stride:(y + 1) * stride] for y in range(HEIGHT))


def render():
    def chunk(kind, data):
        return (struct.pack('!I', len(data)) + kind + data
                + struct.pack('!I', zlib.crc32(kind + data) & 0xffffffff))

    header = struct.pack('!IIBBBBB', WIDTH, HEIGHT, 8, 2, 0, 0, 0)
    return (b'\x89PNG\r\n\x1a\n' + chunk(b'IHDR', header)
            + chunk(b'IDAT', zlib.compress(render_scanlines(), 9)) + chunk(b'IEND', b''))


def read_png_scanlines(data):
    if data[:8] != b'\x89PNG\r\n\x1a\n':
        raise ValueError('Social card is not a PNG')
    offset, compressed, header, ended = 8, bytearray(), None, False
    while offset < len(data):
        if ended or len(data) - offset < 12:
            raise ValueError('Incomplete PNG')
        size = struct.unpack('!I', data[offset:offset + 4])[0]
        end = offset + size + 12
        if end > len(data):
            raise ValueError('Incomplete PNG')
        kind = data[offset + 4:offset + 8]
        payload = data[offset + 8:end - 4]
        crc = struct.unpack('!I', data[end - 4:end])[0]
        if zlib.crc32(kind + payload) & 0xffffffff != crc:
            raise ValueError('Invalid PNG checksum')
        if kind == b'IHDR':
            if header is not None or size != 13:
                raise ValueError('Invalid PNG header')
            header = struct.unpack('!IIBBBBB', payload)
        elif kind == b'IDAT':
            compressed.extend(payload)
        elif kind == b'IEND':
            if ended or payload:
                raise ValueError('Invalid PNG end')
            ended = True
        offset = end
    if header != (WIDTH, HEIGHT, 8, 2, 0, 0, 0):
        raise ValueError('Expected a 1200x630 RGB social card')
    if not ended:
        raise ValueError('Incomplete PNG')
    scanlines = zlib.decompress(compressed)
    if len(scanlines) != HEIGHT * (1 + WIDTH * 3):
        raise ValueError('Invalid PNG pixels')
    return scanlines


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--check', action='store_true', help='Compare without changing files')
    args = parser.parse_args()
    path = Path(__file__).resolve().parents[2] / 'site/assets/social.png'
    if args.check:
        try:
            actual = read_png_scanlines(path.read_bytes())
        except (OSError, ValueError, zlib.error) as error:
            raise SystemExit(f'FAIL: social.png is invalid: {error}') from error
        if actual != render_scanlines():
            raise SystemExit('FAIL: social.png pixels differ from the generator')
        print(f'PASS: social.png has generated pixels ({WIDTH}x{HEIGHT}, {len(actual)} scanline bytes)')
    else:
        data = render()
        path.write_bytes(data)
        print(f'Created {path.name}: {WIDTH}x{HEIGHT}, {len(data)} bytes')


if __name__ == '__main__':
    main()
