"""One-time Inter v4.1 ASCII outline extraction, NOT part of generation or CI.

Download https://github.com/rsms/inter/releases/download/v4.1/Inter-4.1.zip
into a new empty /tmp directory. Extract into another new empty directory.
Create a /tmp venv and install fonttools==4.67.0 there. From the repository
(not the downloaded/extracted directory), run:

/tmp/trello-inter-venv/bin/python -I scripts/site/fonts/extract_inter.py \
    /absolute/path/to/extracted-release

Only the three static upright TTFs and LICENSE.txt are read. No downloaded
code is executed. Output paths default to this script's directory. The JSON
contains font-unit SVG paths and hmtx advances; layout deliberately uses
advances only, not Inter's multi-lookup GPOS shaping/kerning. No fonttools
or font files are needed to generate the charts from the checked-in JSON.
"""
from pathlib import Path
import argparse
import hashlib
import json

from fontTools.pens.basePen import BasePen
from fontTools.ttLib import TTFont


class OutlinePen(BasePen):
    """Decompose components and emit only absolute M/L/Q/C/Z commands."""

    def __init__(self, glyphs):
        super().__init__(glyphs)
        self.parts = []

    def command(self, kind, *points):
        numbers = (format(round(value, 2), '.2f').rstrip('0').rstrip('.')
                   for point in points for value in point)
        self.parts.append(kind + ' '.join(numbers))

    def _moveTo(self, point):
        self.command('M', point)

    def _lineTo(self, point):
        self.command('L', point)

    def _qCurveToOne(self, control, point):
        self.command('Q', control, point)

    def _curveToOne(self, control1, control2, point):
        self.command('C', control1, control2, point)

    def _closePath(self):
        self.parts.append('Z')

    def _endPath(self):
        raise ValueError('Expected closed glyph contours')


def extract(directory):
    result = {'family': 'Inter', 'release': 'v4.1', 'license': 'OFL-1.1', 'weights': {}}
    for weight, name in ((400, 'Regular'), (600, 'SemiBold'), (700, 'Bold')):
        path = directory / 'extras/ttf' / f'Inter-{name}.ttf'
        with TTFont(path) as font:
            glyphs = font.getGlyphSet()
            cmap = font.getBestCmap()
            outlines = {}
            for code in range(0x20, 0x7f):
                glyph_name = cmap[code]
                pen = OutlinePen(glyphs)
                glyphs[glyph_name].draw(pen)
                outlines[chr(code)] = {'advance': font['hmtx'][glyph_name][0],
                                       'path': ''.join(pen.parts)}
            result['weights'][str(weight)] = {
                'unitsPerEm': font['head'].unitsPerEm,
                'source': path.name,
                'sha256': hashlib.sha256(path.read_bytes()).hexdigest(),
                'glyphs': outlines,
            }
    return json.dumps(result, ensure_ascii=True, separators=(',', ':')) + '\n'


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('directory', type=Path, help='Absolute path to extracted official Inter v4.1 release')
    parser.add_argument('--output', type=Path, default=Path(__file__).resolve().parent)
    args = parser.parse_args()
    args.output.joinpath('inter.json').write_text(extract(args.directory), encoding='utf-8')
    args.output.joinpath('OFL.txt').write_bytes(args.directory.joinpath('LICENSE.txt').read_bytes())


if __name__ == '__main__':
    main()
