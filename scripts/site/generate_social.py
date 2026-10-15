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


def render():
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
    rect(93, 81, 12, 32, background)
    rect(113, 81, 12, 21, background)
    text('GO + MODEL CONTEXT PROTOCOL', 162, 86, 3, accent)
    text('TRELLO-MCP', 82, 177, 12, ink)
    text('TOKEN-EFFICIENT TRELLO MCP SERVER', 82, 300, 4, ink)
    text('LOCAL STDIO / WORKSPACE ALLOWLIST', 82, 368, 4, muted)
    text('READ-ONLY OPT-IN', 82, 414, 4, muted)
    rect(82, 486, 1036, 2, (61, 84, 75))
    text('CLAUDE + CODEX + CHATGPT', 82, 525, 4, accent)
    text('AVHN.GITHUB.IO/TRELLO-MCP', 82, 581, 2, muted)

    def chunk(kind, data):
        return (struct.pack('!I', len(data)) + kind + data
                + struct.pack('!I', zlib.crc32(kind + data) & 0xffffffff))

    stride = WIDTH * 3
    scanlines = b''.join(b'\x00' + pixels[y * stride:(y + 1) * stride] for y in range(HEIGHT))
    header = struct.pack('!IIBBBBB', WIDTH, HEIGHT, 8, 2, 0, 0, 0)
    return (b'\x89PNG\r\n\x1a\n' + chunk(b'IHDR', header)
            + chunk(b'IDAT', zlib.compress(scanlines, 9)) + chunk(b'IEND', b''))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--check', action='store_true', help='Compare without changing files')
    args = parser.parse_args()
    path = Path(__file__).resolve().parents[2] / 'site/assets/social.png'
    data = render()
    if args.check:
        if not path.is_file() or path.read_bytes() != data:
            raise SystemExit('FAIL: social.png differs from the generator')
        print(f'PASS: social.png is reproducible ({WIDTH}x{HEIGHT}, {len(data)} bytes)')
    else:
        path.write_bytes(data)
        print(f'Created {path.name}: {WIDTH}x{HEIGHT}, {len(data)} bytes')


if __name__ == '__main__':
    main()
