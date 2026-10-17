"""Generate measured comparison SVGs and two marked regions per page, offline.

Run: python3 -I scripts/site/generate_benchmarks.py [--check]
Only aggregate measurements belong in the input, never private Trello payloads.
"""
from html import escape
from pathlib import Path
import argparse
import json
import re

ROOT = Path(__file__).resolve().parents[2]
DATA = ROOT / 'docs/benchmarks/2026-10-17.json'
MARKER = re.compile(r'<!-- measured:start -->\n.*?<!-- measured:end -->', re.S)
CHARTS = ('benchmark-cards.svg', 'benchmark-responses.svg', 'benchmark-definitions.svg')
HEADINGS = dict(zip(CHARTS, (
    ('16 cards in one list', 'Response text, characters. Lower is smaller.'),
    ('Measured response text', 'Chars; independent zero-based row scales.'),
    ('Tool definitions', 'Claude tokens from /context, one session. Node not measured. Lower is smaller.'),
)))
# Text, green, blue-violet, amber, axis. Panels match code blocks on each host.
PALETTES = {
    'site-light': ('#172e2b', '#176249', '#6776a5', '#a56a2c', '#718778'),
    'site-dark': ('#e4eee7', '#a4d6ab', '#aab9ef', '#edba7d', '#6c8778'),
    'light': ('#1f2328', '#176249', '#6776a5', '#a56a2c', '#718778'),
    'dark': ('#e6edf3', '#a4d6ab', '#aab9ef', '#edba7d', '#6c8778'),
}
BACKGROUNDS = {'site-light': '#edf1eb', 'site-dark': '#20322d', 'light': '#f6f8fa', 'dark': '#151b23'}


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


def chart_image(name, alt, html=False, height=None):
    title, subtitle = HEADINGS[name]
    heading = (f'<p><strong>{escape(title)}</strong>. {escape(subtitle)}</p>\n' if html
               else f'**{title}**. {subtitle}\n\n')
    return heading + image(name, alt, html, height)


def hero_description(data):
    values = next(row['values'] for row in data['rows'] if row['id'] == 'cards_summary')
    names = [data['servers'][key]['name'] for key in ('ours', 'official', 'node')]
    return '16 cards in one list, response characters: ' + ', '.join(
        f'{name} {fmt(value)}' for name, value in zip(names, values))


def blocks(data, html=False):
    headers = ['Call', 'trello-mcp', 'Official', 'Node 1.8.1', 'vs Official', 'vs Node']
    heights = {name: re.search(r' height="(\d+)"', content).group(1) for name, content in assets(data).items()}
    hero = chart_image(CHARTS[0], hero_description(data), html, heights[CHARTS[0]])
    text = caption(data)
    if html:
        text = (text.replace('than the official server for this call (summary mode)', 'vs official (summary mode)')
                .replace('on one private work board with', 'on a private board with').replace('Full mode:', 'Full:')
                .replace("For one card's details the official server returned less:", 'For one card, official returned less:'))
        hero += f'\n<p>{text} <a href="#measured-comparison">All results and method</a>.</p>'
    else:
        hero += f'\n\n{text} [All results and method](#measured-comparison).'
    charts = [chart_image(CHARTS[1], 'Five response comparisons; independent row scales', html, heights[CHARTS[1]]),
              chart_image(CHARTS[2], 'Claude definition tokens; Node unmeasured', html, heights[CHARTS[2]])]
    if html:
        counts = data['rows'][0]['denominators']
        compact = ('One private board/day/account. '
                   f'{data["servers"]["ours"]["commit"]} (v{data["servers"]["ours"]["version"]}); verbatim headless Claude Code '
                   f'OAuth results; Node {data["servers"]["node"]["version"]} via npx. '
                   f'Boards: {"/".join(map(str, counts["board_counts"]))} '
                   f'({counts["node_closed_boards"]} closed in Node; ours adds a guest board). '
                   f'<a href="{README}">Full method</a>.')
        chars = data['definitions']['tools_list']
        compact_definitions = ('Rows over 1k rounded to 0.1k; '
                               f'tools/list {fmt(chars["ours"])} vs {fmt(chars["node"])} chars '
                               f'({comparison(chars["ours"], chars["node"])}).')
        compact_rows = [[row[0].replace('characters', 'chars'), *row[1:]] for row in table_rows(data)]
        detail = ('<section aria-labelledby="measured-comparison"><h2 id="measured-comparison">Measured comparison</h2>\n'
                  + '\n'.join(charts) + '\n<p>' + compact_definitions + '</p>\n'
                  + html_table(headers, compact_rows) + '\n<p>' + compact + '</p>\n'
                  + '<p>I built trello-mcp for lighter, easier Trello use with AI assistants.</p>'
                  '<p>Tool definitions and responses consume task context; growing context can reduce AI output quality.</p>'
                  + f'<p>Features, {data["date"]}: trello-mcp / <a href="{OFFICIAL}">Official</a> / '
                  f'<a href="{NODE}">Node 1.8.1</a>.</p>'
                  + ''.join(f'<p><strong>{row[0]}:</strong> ' + ' / '.join(escape(cell) for cell in row[1:]) + '.</p>'
                            for row in (FEATURES[i] for i in (0, 1, 4, 5)))
                  + f'<p>{CHOOSE}</p></section>')
    else:
        detail = ('## Measured comparison\n\n' + '\n\n'.join(charts) + '\n\n' + definition_note(data)
                  + '\n\n' + markdown_table(headers, table_rows(data)) + '\n\n' + method(data)
                  + '\n\n[Aggregate data](docs/benchmarks/2026-10-17.json). '
                  '[Method and harness](docs/benchmarks/method.md). Percent fewer = round(100 * (1 - ours/theirs)); '
                  'per-board values are rounded totals/count.\n\n' + '\n\n'.join(OWNER)
                  + f'\n\nFeatures checked {data["date"]}.\n\n'
                  + markdown_table(['Feature', 'trello-mcp', f'[Official]({OFFICIAL})', f'[Node 1.8.1]({NODE})'], FEATURES)
                  + '\n\n' + CHOOSE)
    return [f'<!-- measured:start -->\n{content}\n<!-- measured:end -->' for content in (hero, detail)]


def text(value, x, y, css='name'):
    anchor = ' text-anchor="end"' if css == 'value' else ''
    return f'<text x="{x}" y="{y}" class="{css}"{anchor}>{escape(value)}</text>'


def palette_style(theme):
    ink, green, blue, amber, axis = PALETTES[theme]
    return (f'svg{{color:{ink}}}.panel{{fill:{BACKGROUNDS[theme]}}}.s0{{fill:{green}}}'
            f'.s1{{fill:{blue}}}.s2{{fill:{amber}}}.axis{{stroke:{axis}}}')


def chart_style(theme):
    return ('svg{font-family:-apple-system,BlinkMacSystemFont,"Segoe UI","Noto Sans",Helvetica,Arial,sans-serif;'
            'font-size:15px;font-weight:500}text{fill:currentColor}.value,.group{font-weight:600}'
            '.value{font-variant-numeric:tabular-nums}.zero{font-size:13px}.axis{fill:none;stroke-width:1}'
            + (palette_style(theme) if theme else palette_style('site-light')
               + '@media(prefers-color-scheme: dark){' + palette_style('site-dark') + '}'))


def svg(title, subtitle, groups, names, description=None, theme=None):
    """Code-block panels, zero-based bars, native text and independent group scales."""
    height = 22 + sum((24 if label else 0) + len(values) * 53 + 18 for label, values in groups)
    if description is None:
        description = subtitle + ' ' + '; '.join(
            (label + ': ' if label else '') + ', '.join(f'{name} {fmt(v)}' for name, v in zip(names, values))
            for label, values in groups)
    radius = 6 if theme else 8
    parts = [f'<svg xmlns="http://www.w3.org/2000/svg" width="400" height="{height}" viewBox="0 0 400 {height}" role="img" aria-labelledby="title desc">',
             f'<title id="title">{escape(title)}</title><desc id="desc">{escape(description)}</desc>',
             '<style>' + chart_style(theme) + '</style>',
             f'<rect class="panel" x="0" y="0" width="400" height="{height}" rx="{radius}"/>']
    y = 28
    for label, values in groups:
        if label:
            parts.append(text(label, 16, y, 'group'))
            y += 24
        axis = []
        for i, (name, value) in enumerate(zip(names, values)):
            width = round(368 * value / max(values))
            parts.extend([text(name, 16, y), text(fmt(value), 384, y, 'value'),
                          f'<rect class="s{i}" x="16" y="{y + 10}" width="{width}" height="14" rx="3"/>'])
            axis.append(f'M16.5 {y + 10}v14')
            y += 53
        parts.append(f'<path class="axis" d="{"".join(axis)}"/>')
        parts.append(text('0', 16, y - 7, 'zero'))
        y += 18
    parts.append('</svg>\n')
    return '\n'.join(parts)


def assets(data):
    rows = {r['id']: r for r in data['rows']}
    names = [data['servers'][k]['name'] for k in ('ours', 'official', 'node')]
    response_groups = [('Boards list (chars/returned board)' if k == 'boards' else rows[k]['label'] + ' (chars)',
                        rows[k]['values']) for k in ('boards', 'cards_summary', 'actions', 'details', 'comments')]
    defs = data['definitions']
    specifications = [
        (*HEADINGS[CHARTS[0]],
         [('', rows['cards_summary']['values'])], names, hero_description(data)),
        (*HEADINGS[CHARTS[1]], response_groups, names, None),
        (*HEADINGS[CHARTS[2]],
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
