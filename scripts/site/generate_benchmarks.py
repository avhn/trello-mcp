"""Generate measured comparison SVGs and two marked regions per page, offline.

Run: python3 -I scripts/site/generate_benchmarks.py [--check]
Only aggregate measurements belong in the input, never private Trello payloads.
"""
from functools import lru_cache
from html import escape
from pathlib import Path
import argparse
import json
import re

ROOT = Path(__file__).resolve().parents[2]
DATA = ROOT / 'docs/benchmarks/2026-10-17.json'
MARKER = re.compile(r'<!-- measured:start -->\n.*?<!-- measured:end -->', re.S)
CHARTS = ('benchmark-cards.svg', 'benchmark-responses.svg', 'benchmark-definitions.svg')
FONT = json.loads(Path(__file__).with_name('fonts').joinpath('inter.json').read_text())
# Text, green, blue-violet, amber, axis. Backgrounds are validation targets only;
# every SVG is transparent. Fixed palettes follow GitHub, adaptive ones the site.
PALETTES = {
    'site-light': ('#172e2b', '#176249', '#6776a5', '#a56a2c', '#718778'),
    'site-dark': ('#e4eee7', '#a4d6ab', '#aab9ef', '#edba7d', '#6c8778'),
    'light': ('#1f2328', '#176249', '#6776a5', '#a56a2c', '#718778'),
    'dark': ('#e6edf3', '#a4d6ab', '#aab9ef', '#edba7d', '#6c8778'),
}
BACKGROUNDS = {'site-light': '#f7f8f3', 'site-dark': '#111d1b', 'light': '#ffffff', 'dark': '#0d1117'}


def variant(name, theme):
    return name.replace('.svg', f'-{theme}.svg')


GENERATED_CHARTS = (*CHARTS, *(variant(name, theme) for name in CHARTS for theme in ('light', 'dark')))
ROW_IDS = ('boards', 'cards_summary', 'cards_full', 'actions', 'details', 'comments')
OFFICIAL = 'https://github.com/atlassian/trello-mcp-server'
NODE = 'https://github.com/delorenj/mcp-server-trello/blob/v1.8.1/src/index.ts'
README = 'https://github.com/avhn/trello-mcp#measured-comparison'
OWNER = (
    'I use Trello with AI assistants a lot, so I built trello-mcp to make it lighter and easier to use.',
    'A thought: with AI agents, managing context may be one of the main optimizations we need to make now. '
    'Every tool definition and response a model reads takes space from the actual task, and as context grows, '
    'output quality tends to drop.',
)
FEATURES = [
    ['Runs', 'Local binary', 'Hosted, OAuth', 'Local, Node or Bun'],
    ['Guest boards', 'Yes', "Not reached in the owner's check, 2026-10-16", 'Yes'],
    ['Delete comments', 'Yes', 'No: list, add, edit', 'Yes'],
    ['Create, update, delete labels', 'Yes', 'No: list, attach, detach', 'Yes'],
    ['Attachments', 'Upload and download to disk, opt-in, Unix', 'Listing only',
     'Any local path upload, inline base64 download'],
    ['Per-object workspace allowlist', 'Yes', 'Per-workspace OAuth consent',
     'Listing and some workspace operations'],
    ['Server-side read-only mode', 'Yes', 'n/a (hosted)', 'Not documented'],
]
CHOOSE = 'Choose official for zero install, web and mobile, Inbox, Planner or keyword search.'


def load_data():
    data = json.loads(DATA.read_text())
    if data['date'] != '2026-10-17' or tuple(r['id'] for r in data['rows']) != ROW_IDS:
        raise ValueError('Expected dated six-row measurement dataset')
    for row in data['rows']:
        if (len(row['values']) != 3 or any(type(v) is not int or v <= 0 for v in row['values'])
                or row['unit'] not in {'characters', 'characters/board'}):
            raise ValueError(f'Invalid measured values or units: {row["id"]}')
    definitions = data['definitions']
    if (definitions['unit'] != 'Claude tokens' or len(definitions['values']) != 3
            or definitions['values'][2] is not None
            or any(type(v) is not int or v <= 0 for v in definitions['values'][:2])):
        raise ValueError('Definitions must distinguish measured Claude tokens from unmeasured Node')
    return data


def fewer(ours, theirs):
    return round(100 * (1 - ours / theirs))


def comparison(ours, theirs):
    value = fewer(ours, theirs)
    return f'{abs(value)}% {"fewer" if value >= 0 else "more"}'


def fmt(value):
    return f'{value:,}'


def caption(data):
    rows = {r['id']: r['values'] for r in data['rows']}
    summary, full, details = (rows[k] for k in ('cards_summary', 'cards_full', 'details'))
    return (f'{fewer(*summary[:2])}% less response text than the official server for this call (summary mode). '
            f'Measured {data["date"]} on one private work board with {data["board_cards"]} cards; characters, not tokens. '
            f'Full mode: {fmt(full[0])} ({comparison(*full[:2])}). '
            f"For one card's details the official server returned less: {fmt(details[1])} vs {fmt(details[0])}.")


def definition_note(data):
    defs = data['definitions']
    chars = defs['tools_list']
    return (f'Claude Code /context in one session; rows over 1k display-rounded to 0.1k. '
            f'Node could not be loaded into /context. Separately, tools/list: '
            f'{fmt(chars["ours"])} vs {fmt(chars["node"])} characters '
            f'({comparison(chars["ours"], chars["node"])} vs Node), not tokens.')


def method(data):
    ours, official, node = (data['servers'][k] for k in ('ours', 'official', 'node'))
    counts = data['rows'][0]['denominators']
    defs = data['definitions']
    return (f'One board, one day, one account. trello-mcp at commit {ours["commit"]} (v{ours["version"]}); '
            f'Atlassian hosted {official["endpoint"]} over OAuth, results captured verbatim from a headless Claude Code session; '
            f'{node["package"]} {node["version"]} via npx. Board list figures are characters per returned board over different sets '
            f'({" / ".join(str(n) for n in counts["board_counts"])}; Node includes {counts["node_closed_boards"]} closed boards; '
            'the extra trello-mcp board is a guest board in another organization). '
            'Card details and comments use one card with two comments. The board is private, so others cannot rerun on the same data. '
            'Official results reflect its hosted version on that date. Tool definitions are Claude tokens from /context; '
            f'chars/4 would give about {fmt(round(defs["tools_list"]["ours"] / 4))} for trello-mcp instead of {fmt(defs["values"][0])}.')


def table_rows(data):
    rows = []
    for row in data['rows']:
        ours, official, node = row['values']
        rows.append([f'{row["label"]} ({row["unit"]})', *map(fmt, row['values']),
                     comparison(ours, official), comparison(ours, node)])
    defs = data['definitions']
    ours, official = defs['values'][:2]
    rows.append(['Tool definitions (Claude tokens)',
                 f'{fmt(ours)} ({defs["tool_counts"][0]} tools)',
                 f'{fmt(official)} ({defs["tool_counts"][1]} tools)',
                 'Not measured', comparison(ours, official), 'n/a'])
    return rows


def markdown_table(header, rows):
    return '\n'.join('| ' + ' | '.join(row) + ' |' for row in [header, ['---'] * len(header), *rows])


def html_table(header, rows):
    return ('<div class="table-wrap"><table><thead><tr>'
            + ''.join(f'<th scope="col">{cell}</th>' for cell in header) + '</tr></thead><tbody>'
            + ''.join('<tr>' + ''.join(f'<td>{escape(cell)}</td>' for cell in row) + '</tr>' for row in rows)
            + '</tbody></table></div>')


def image(name, alt, html=False, height=None):
    if html:
        return f'<img src="/trello-mcp/assets/{name}" alt="{escape(alt, quote=True)}" width="400" height="{height}">'
    return (f'<picture><source media="(prefers-color-scheme: dark)" srcset="site/assets/{variant(name, "dark")}">'
            f'<img src="site/assets/{variant(name, "light")}" alt="{escape(alt, quote=True)}" width="400"></picture>')


def hero_description(data):
    values = next(row['values'] for row in data['rows'] if row['id'] == 'cards_summary')
    names = [data['servers'][key]['name'] for key in ('ours', 'official', 'node')]
    return '16 cards in one list, response characters: ' + ', '.join(
        f'{name} {fmt(value)}' for name, value in zip(names, values))


def blocks(data, html=False):
    headers = ['Call', 'trello-mcp', 'Official', 'Node 1.8.1', 'vs Official', 'vs Node']
    heights = {name: re.search(r' height="(\d+)"', content).group(1) for name, content in assets(data).items()}
    hero = image(CHARTS[0], hero_description(data), html, heights[CHARTS[0]])
    text = caption(data)
    if html:
        hero += f'\n<p>{text} <a href="#measured-comparison">All results and method</a>.</p>'
    else:
        hero += f'\n\n{text} [All results and method](#measured-comparison).'
    charts = [image(CHARTS[1], 'Five response comparisons; independent row scales', html, heights[CHARTS[1]]),
              image(CHARTS[2], 'Claude definition tokens; Node unmeasured', html, heights[CHARTS[2]])]
    if html:
        counts = data['rows'][0]['denominators']
        compact = ('One private board, day and account. '
                   f'{data["servers"]["ours"]["commit"]} (v{data["servers"]["ours"]["version"]}); hosted OAuth, '
                   f'verbatim headless Claude Code results; Node {data["servers"]["node"]["version"]} via npx. '
                   f'Boards: {"/".join(map(str, counts["board_counts"]))} '
                   f'({counts["node_closed_boards"]} closed in Node; ours adds a guest board). '
                   f'<a href="{README}">Full method</a>.')
        chars = data['definitions']['tools_list']
        compact_definitions = ('Claude /context tokens, rounded above 1k. Node not in /context; '
                               f'tools/list {fmt(chars["ours"])} vs {fmt(chars["node"])} chars '
                               f'({comparison(chars["ours"], chars["node"])}).')
        compact_rows = [[row[0].replace('characters', 'chars'), *row[1:]] for row in table_rows(data)]
        detail = ('<section aria-labelledby="measured-comparison"><h2 id="measured-comparison">Measured comparison</h2>\n'
                  + '\n'.join(charts) + '\n<p>' + compact_definitions + '</p>\n'
                  + html_table(headers, compact_rows) + '\n<p>' + compact + '</p>\n'
                  + ''.join(f'<p>{escape(p)}</p>' for p in OWNER)
                  + f'<p>Features, {data["date"]}: trello-mcp / <a href="{OFFICIAL}">Official</a> / '
                  f'<a href="{NODE}">Node 1.8.1</a>.</p>'
                  + ''.join(f'<p><strong>{row[0]}:</strong> ' + ' / '.join(escape(cell) for cell in row[1:]) + '.</p>'
                            for row in (FEATURES[i] for i in (0, 1, 4, 5)))
                  + f'<p>{CHOOSE}</p></section>')
    else:
        detail = ('## Measured comparison\n\n' + '\n\n'.join(charts) + '\n\n' + definition_note(data)
                  + '\n\n' + markdown_table(headers, table_rows(data)) + '\n\n' + method(data)
                  + '\n\n[Aggregate data](docs/benchmarks/2026-10-17.json). Percent fewer = round(100 * (1 - ours/theirs)); '
                  'per-board values are rounded totals/count.\n\n' + '\n\n'.join(OWNER)
                  + f'\n\nFeatures checked {data["date"]}.\n\n'
                  + markdown_table(['Feature', 'trello-mcp', f'[Official]({OFFICIAL})', f'[Node 1.8.1]({NODE})'], FEATURES)
                  + '\n\n' + CHOOSE)
    return [f'<!-- measured:start -->\n{content}\n<!-- measured:end -->' for content in (hero, detail)]


def number(value):
    return format(round(value, 2) or 0, '.2f').rstrip('0').rstrip('.')


def text_width(text, size=16, weight=400):
    font = FONT['weights'][str(weight)]
    return sum(font['glyphs'][char]['advance'] for char in text) * size / font['unitsPerEm']


def wrap_text(text, width=352, size=14):
    """Wrap at words using the same Inter advances as the outlines, not character counts."""
    lines = []
    for word in text.split():
        if not lines or text_width(lines[-1] + ' ' + word, size) > width:
            lines.append(word)
        else:
            lines[-1] += ' ' + word
    return lines


@lru_cache(maxsize=None)
def glyph_commands(char, weight):
    path = FONT['weights'][str(weight)]['glyphs'][char]['path']
    return [(part[0], tuple(map(float, part[1:].split())))
            for part in re.findall(r'[MLQCZ][^MLQCZ]*', path)]


def glyph_id(char, weight):
    return {400: 'r', 600: 's', 700: 'b'}[weight] + format(ord(char), 'x')


@lru_cache(maxsize=None)
def glyph_path(char, weight):
    """Compact an outline in font units, shared across every occurrence and size."""
    parts = []
    current = start = (0, 0)
    control = None
    previous = None
    for command, points in glyph_commands(char, weight):
        if command == 'Z':
            parts.append('Z')
            current = start
            control = previous = None
            continue
        choices = [command + ' '.join(map(number, points)).replace(' -', '-')]
        relative = [value - current[i % 2] for i, value in enumerate(points)]
        choices.append(command.lower() + ' '.join(map(number, relative)).replace(' -', '-'))
        if command == 'M':
            start = points
        elif command == 'L':
            if points[0] == current[0]:
                choices.extend(('V' + number(points[1]), 'v' + number(relative[1])))
            if points[1] == current[1]:
                choices.extend(('H' + number(points[0]), 'h' + number(relative[0])))
        elif command == 'Q' and control is not None:
            # TrueType implied on-curve midpoints permit SVG smooth quadratics,
            # preserving every control point rather than approximating the font.
            if points[:2] == tuple(2 * current[i] - control[i] for i in range(2)):
                choices.extend(('T' + ' '.join(map(number, points[2:])),
                                't' + ' '.join(map(number, relative[2:])).replace(' -', '-')))
        spellings = []
        for choice in choices:
            kind = choice[0]
            if kind == previous or (previous, kind) in {('M', 'L'), ('m', 'l')}:
                body = choice[1:]
                separator = '' if body.startswith('-') else ' '
                spellings.append((separator + body, kind))
            else:
                spellings.append((choice, kind))
        spelling, previous = min(spellings, key=lambda item: len(item[0]))
        parts.append(spelling)
        control = points[:2] if command == 'Q' else None
        current = points[-2:]
    return ''.join(parts)


@lru_cache(maxsize=512)
def glyph_uses(text, weight):
    parts = []
    advance = 0
    for char in text:
        if char != ' ':
            position = f' x="{advance}"' if advance else ''
            parts.append(f'<use href="#{glyph_id(char, weight)}"{position}/>')
        advance += FONT['weights'][str(weight)]['glyphs'][char]['advance']
    return ''.join(parts)


def glyph_definitions(used, shared):
    return ('<defs>' + ''.join(f'<path id="{glyph_id(char, weight)}" d="{glyph_path(char, weight)}"/>'
                              for weight, char in sorted(used))
            + ''.join(f'<g id="{name}">{glyph_uses(text, 400)}</g>' for text, name in shared.items()) + '</defs>')


@lru_cache(maxsize=512)
def text_run(text, x, y, size=16, weight=400, css='text', right=False, reference=None):
    """Place glyph references using font-unit advances; only run origins are rounded."""
    font = FONT['weights'][str(weight)]
    if right:
        x -= text_width(text, size, weight)
    # Retain the exact scale for all three sizes with the 2048-unit Inter em.
    scale = format(size / font['unitsPerEm'], '.11f').rstrip('0').lstrip('0')
    content = f'<use href="#{reference}"/>' if reference else glyph_uses(text, weight)
    return (f'<g class="{css}" transform="translate({number(x)} {number(y)})scale({scale} -{scale})">'
            + content + '</g>')


def palette_style(theme):
    ink, green, blue, amber, axis = PALETTES[theme]
    return (f'svg{{color:{ink}}}.s0{{fill:{green}}}.s1{{fill:{blue}}}'
            f'.s2{{fill:{amber}}}.axis{{stroke:{axis}}}')


def svg(title, subtitle, groups, names, description=None, theme=None):
    """Zero-based linear bars; groups have independent maxima, direct labels and values."""
    used = set()
    # Reuse the three server labels too when the chart has multiple row groups.
    shared = {name: f'n{i}' for i, name in enumerate(names)} if len(groups) > 1 else {}

    def text(value, x, y, size=16, weight=400, css='text', right=False):
        used.update((weight, char) for char in value if char != ' ')
        reference = shared.get(value) if weight == 400 else None
        return text_run(value, x, y, size, weight, css, right, reference)

    subtitle_lines = wrap_text(subtitle)
    subtitle_extra = (len(subtitle_lines) - 1) * 20
    height = 82 + subtitle_extra + sum((42 if label else 18) + len(values) * 53 for label, values in groups)
    if description is None:
        description = subtitle + ' ' + '; '.join(
            (label + ': ' if label else '') + ', '.join(f'{name} {fmt(v)}' for name, v in zip(names, values))
            for label, values in groups)
    parts = [f'<svg xmlns="http://www.w3.org/2000/svg" width="400" height="{height}" viewBox="0 0 400 {height}" role="img" aria-labelledby="title desc">',
             f'<title id="title">{escape(title)}</title><desc id="desc">{escape(description)}</desc>',
             '<style>.text,.title,.sub,.group{fill:currentColor}.axis{fill:none}'
             + (palette_style(theme) if theme else palette_style('site-light')
                + '@media(prefers-color-scheme: dark){' + palette_style('site-dark') + '}') + '</style>',
             text(title, 24, 32, 21, 700, 'title')]
    parts.extend(text(line, 24, 56 + i * 20, 14, css='sub')
                 for i, line in enumerate(subtitle_lines))
    y = 88 + subtitle_extra
    for label, values in groups:
        if label:
            parts.append(text(label, 24, y, weight=600, css='group'))
            y += 24
        top = y + 5
        for i, (name, value) in enumerate(zip(names, values)):
            width = 352 * value / max(values)
            parts.extend([text(name, 24, y),
                          text(fmt(value), 376, y, right=True),
                          f'<rect class="s{i}" x="24" y="{y + 10}" width="{width:.3f}" height="12" rx="2"/>'])
            y += 53
        parts.append(f'<path class="axis" d="M24 {top}V{y - 26}"/>')
        parts.append(text('0', 24, y - 7, 14, css='sub'))
        y += 18
    parts.insert(3, glyph_definitions(used, shared))
    parts.append('</svg>\n')
    return '\n'.join(parts)


def assets(data):
    rows = {r['id']: r for r in data['rows']}
    names = [data['servers'][k]['name'] for k in ('ours', 'official', 'node')]
    response_groups = [('Boards list (chars/returned board)' if k == 'boards' else rows[k]['label'] + ' (chars)',
                        rows[k]['values']) for k in ('boards', 'cards_summary', 'actions', 'details', 'comments')]
    defs = data['definitions']
    specifications = [
        ('16 cards in one list', 'Response text, characters. Lower is smaller.',
         [('Summary mode', rows['cards_summary']['values'])], names, hero_description(data)),
        ('Measured response text', 'Chars; independent zero-based row scales.', response_groups, names, None),
        ('Tool definitions', 'Claude tokens from /context, one session. Node not measured. Lower is smaller.',
         [('', defs['values'][:2])],
         [f'{names[i]} ({defs["tool_counts"][i]} tools)' for i in range(2)], None),
    ]
    return {name if theme is None else variant(name, theme): svg(*spec, theme=theme)
            for name, spec in zip(CHARTS, specifications) for theme in (None, 'light', 'dark')}


def replace_blocks(source, generated):
    if len(MARKER.findall(source)) != len(generated):
        raise ValueError('Expected exactly two measured blocks (hero and detailed comparison)')
    replacements = iter(generated)
    return MARKER.sub(lambda _: next(replacements), source)


def expected_files(data):
    output = {ROOT / 'site/assets' / name: content for name, content in assets(data).items()}
    for path, html in ((ROOT / 'README.md', False), (ROOT / 'site/index.html', True)):
        output[path] = replace_blocks(path.read_text(), blocks(data, html))
    return output


def check_generated(actual, expected, name):
    if actual != expected:
        raise ValueError(f'Stale measured output: {name}; run scripts/site/generate_benchmarks.py')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--check', action='store_true', help='Compare without changing files')
    args = parser.parse_args()
    try:
        output = expected_files(load_data())
        for path, content in output.items():
            if args.check:
                check_generated(path.read_text(), content, str(path.relative_to(ROOT)))
            else:
                path.write_text(content)
    except (OSError, ValueError, KeyError) as error:
        raise SystemExit(f'FAIL: {error}') from error
    print(f'PASS: {len(output)} measured outputs {"match generator" if args.check else "generated"}')


if __name__ == '__main__':
    main()
