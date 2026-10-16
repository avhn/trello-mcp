"""Validate the static site, docs mirror and release versions offline with the standard library.

Run: python3 -I scripts/site/validate.py
Parser regression checks: python3 -I scripts/site/validate.py --self-test
Release gate: RELEASE_TAG=v0.7.0 python3 -I scripts/site/validate.py
"""
from collections import Counter
from html import unescape
from html.parser import HTMLParser
from importlib.util import module_from_spec, spec_from_file_location
from pathlib import Path
from urllib.parse import unquote, urljoin, urlsplit
import json
import os
import re
import shlex
import sys
import tomllib
import unittest
import xml.etree.ElementTree as ET

ROOT = Path(__file__).resolve().parents[2]
SITE = ROOT / 'site'
BASE = 'https://avhn.github.io/trello-mcp/'
DOCS = 'https://github.com/avhn/trello-mcp/blob/main/docs/'
RAW_SETUP = 'https://raw.githubusercontent.com/avhn/trello-mcp/main/docs/setup.md'
RAW_DOCS = 'https://raw.githubusercontent.com/avhn/trello-mcp/main/docs/'
COPY_SRC = '/trello-mcp/assets/copy.js'
ROUTES = ('', 'setup/', 'tools/', 'security/')
GENERATE_SOCIAL_SPEC = spec_from_file_location(
    'generate_social', Path(__file__).with_name('generate_social.py'))
if GENERATE_SOCIAL_SPEC is None or GENERATE_SOCIAL_SPEC.loader is None:
    raise RuntimeError('Unable to load social card generator')
GENERATE_SOCIAL = module_from_spec(GENERATE_SOCIAL_SPEC)
sys.dont_write_bytecode = True
GENERATE_SOCIAL_SPEC.loader.exec_module(GENERATE_SOCIAL)
read_png_scanlines = GENERATE_SOCIAL.read_png_scanlines
render_scanlines = GENERATE_SOCIAL.render_scanlines
VOID = set('area base br col embed hr img input link meta param source track wbr'.split())
CAPTURE = set('title h1 h2 h3 p pre td th dt dd'.split())
TRADEMARK = ('Trello is a trademark of Atlassian. trello-mcp is an independent project, '
             'not affiliated with or endorsed by Atlassian.')
ALLOWED_ATTRS = set('''aria-current aria-hidden aria-label aria-labelledby charset class content d data-mark
data-mirror data-section defer fill focusable height href id lang name property rel rx scope src stroke
stroke-linejoin stroke-width type viewbox width'''.split())
FORBIDDEN_TEXT = r'[\u2013\u2014\U0001f300-\U0001faff]'
SECTIONS = ['agent', 'install', 'auth', 'clients', 'desktop', 'profile', 'verify', 'lifecycle']
CLIENTS = ['Claude Code', 'Claude Desktop', 'Codex CLI and desktop', 'ChatGPT desktop', 'OpenCode', 'Cursor', 'VS Code']
PLACEHOLDER = '/absolute/path/to/trello-mcp'
PROFILE_TOOLS = ('workspace_find,board_find,board_use,list_find,card_find,card_get,comment_find,'
                 'checklist_get,checklist_items,server_health')
# Markdown-vs-site relative destinations; every other target must match exactly.
LINK_MAP = {DOCS + 'tools.md': '/trello-mcp/tools/', DOCS + 'setup.md': '/trello-mcp/setup/'}
REVERSE_LINK_MAP = {value: key for key, value in LINK_MAP.items()}
SEMVER = re.compile(r'\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?')


def validate_brand(source):
    footer = re.search(r'<footer\b[^>]*>(.*?)</footer>', source, re.S)
    require(footer is not None and TRADEMARK in footer.group(1), 'Missing footer trademark notice')
    require('data-mark="tm"' in source, 'Missing independent TM monogram')
    require(not re.search(r'#(?:0079bf|0052cc)\b', source, re.I), 'Do not use platform brand colors')


def require(condition, message):
    if not condition:
        raise ValueError(message)


def normalized(text):
    return ' '.join(text.split())


def slug(text):
    return re.sub(r'\s', '-', re.sub(r'[^\w\s-]', '', text.lower()))


class Page(HTMLParser):
    def __init__(self):
        super().__init__(convert_charrefs=True)
        self.stack = []
        self.ids = set()
        self.elements = []
        self.links = []
        self.meta = {}
        self.blocks = []
        self.captures = []
        self.schemas = []
        self.script = None
        self.src_script = False
        self.copy_scripts = 0
        self.cells = []
        self.rows = []
        self.doctype = False

    def handle_decl(self, decl):
        require(decl.lower() == 'doctype html', 'Expected an HTML5 doctype')
        self.doctype = True

    def handle_starttag(self, tag, attributes):
        attrs = dict(attributes)
        require(len(attrs) == len(attributes), f'Duplicate attribute on {tag}')
        self.elements.append((tag, attrs))
        if 'id' in attrs:
            require(attrs['id'] not in self.ids, f'Duplicate id: {attrs["id"]}')
            self.ids.add(attrs['id'])
        for key in ('href', 'src'):
            if key in attrs:
                self.links.append(attrs[key])
        for attr in attrs:
            require(not attr.startswith('on'), f'Inline event handler: {attr}')
            require(attr != 'style', 'Use the shared stylesheet, not inline styles')
            require(attr in ALLOWED_ATTRS, f'Unapproved attribute {attr} on {tag}')
        require(tag not in {'style', 'iframe', 'object', 'embed', 'base', 'noscript'}, f'Unexpected element: {tag}')
        if tag == 'script':
            if attrs.get('type') == 'application/ld+json':
                require(set(attrs) == {'type'}, 'JSON-LD scripts take no other attributes')
                self.script = []
            else:
                require(set(attrs) == {'src', 'defer'} and attrs['src'] == COPY_SRC and attrs['defer'] is None,
                        f'Only the deferred {COPY_SRC} script and inline JSON-LD are allowed')
                self.copy_scripts += 1
                require(self.copy_scripts == 1, 'Duplicate copy script')
                self.src_script = True
        if tag == 'img':
            require('alt' in attrs, 'Image is missing alternative text')
        if tag == 'meta':
            key = attrs.get('name') or attrs.get('property')
            if key:
                require(key not in self.meta, f'Duplicate meta tag: {key}')
                self.meta[key] = attrs.get('content', '')
        if tag == 'a':
            require('a' not in self.stack, 'Nested anchor')
        if tag in {'section', 'div', 'pre', 'table', 'ul', 'ol'}:
            require('p' not in self.stack, f'{tag} cannot be inside a paragraph')
        if tag in CAPTURE:
            self.captures.append([tag, attrs, []])
        if tag == 'tr':
            self.cells = []
        if tag not in VOID:
            self.stack.append(tag)

    def handle_startendtag(self, tag, attrs):
        self.handle_starttag(tag, attrs)
        if tag not in VOID:
            self.handle_endtag(tag)

    def handle_endtag(self, tag):
        require(tag not in VOID, f'Void element has a closing tag: {tag}')
        require(self.stack and self.stack[-1] == tag,
                f'Unexpected closing {tag}; open elements: {self.stack}')
        self.stack.pop()
        if tag in CAPTURE:
            require(self.captures and self.captures[-1][0] == tag, f'Invalid capture nesting: {tag}')
            name, attrs, parts = self.captures.pop()
            text = ''.join(parts)
            self.blocks.append((name, attrs, text))
            if tag in {'td', 'th'}:
                self.cells.append(normalized(text))
        if tag == 'tr':
            self.rows.append(self.cells)
        if tag == 'script':
            if self.script is not None:
                self.schemas.append(json.loads(''.join(self.script)))
            self.script = None
            self.src_script = False

    def handle_data(self, data):
        if self.script is not None:
            self.script.append(data)
        elif self.src_script:
            require(not data.strip(), 'The external script element must be empty')
        else:
            for _, _, parts in self.captures:
                parts.append(data)

    def finish(self):
        self.close()
        require(not self.stack, f'Unclosed elements: {self.stack}')
        require(not self.captures, 'Unclosed text capture')

    def text(self, tag):
        return [text for name, _, text in self.blocks if name == tag]


# ---- Restricted Markdown subset: extraction on both sides of the setup mirror ----

def inline_text(text):
    return normalized(text)


def check_subset_line(line):
    require(not line.startswith('>'), f'Quotes are outside the setup subset: {line}')
    require(not re.match(r'\s+([-*]|\d+\.)\s', line), f'Nested lists are outside the setup subset: {line}')
    require('![' not in line, f'Images are outside the setup subset: {line}')
    require(not re.match(r'\s*<', line), f'Raw HTML is outside the setup subset: {line}')
    plain = re.sub(r'`[^`]*`', '', line)
    plain = re.sub(r'\]\([^)]*\)', ']', plain)
    plain = re.sub(r'https?://\S+', '', plain)
    require(not re.search(r'\*|(?<![\w])_|_(?![\w])', plain), f'Emphasis is outside the setup subset: {line}')


def parse_markdown(text):
    """Return semantic items for the restricted subset, including paired section markers."""
    lines = text.split('\n')
    items, para, i = [], [], 0
    open_section = None
    seen = []

    def flush():
        if para:
            items.append(('p', inline_text(' '.join(para))))
            para.clear()
    while i < len(lines):
        line = lines[i]
        if line.startswith('<!--'):
            flush()
            block = line
            while '-->' not in block:
                i += 1
                require(i < len(lines), 'Unterminated HTML comment')
                block += '\n' + lines[i]
            marker = re.fullmatch(r'<!-- (/?)section:(\w+) -->', block)
            if marker:
                name = marker.group(2)
                if marker.group(1):
                    require(open_section == name, f'Section {name} closed without being open')
                    open_section = None
                    items.append(('/section', name))
                else:
                    require(open_section is None, f'Section {name} nested inside {open_section}')
                    open_section = name
                    seen.append(name)
                    items.append(('section', name))
            else:
                require(not re.match(r'<!--\s*/?section:', block), f'Malformed section marker: {block}')
        elif line.startswith('```'):
            flush()
            language = line[3:].strip()
            require(language, 'Fenced code blocks need a language tag')
            body = []
            i += 1
            while i < len(lines) and not lines[i].startswith('```'):
                body.append(lines[i])
                i += 1
            require(i < len(lines), 'Unterminated code fence')
            items.append(('code', language, '\n'.join(body)))
        elif re.match(r'#{1,3} ', line):
            flush()
            hashes, heading = line.split(' ', 1)
            check_subset_line(heading)
            items.append(('h', len(hashes), inline_text(heading)))
        elif line.startswith('#'):
            raise ValueError(f'Heading level is outside the setup subset: {line}')
        elif line.startswith('|'):
            flush()
            rows = []
            while i < len(lines) and lines[i].startswith('|'):
                check_subset_line(lines[i])
                rows.append([inline_text(cell) for cell in lines[i].strip().strip('|').split('|')])
                i += 1
            i -= 1
            require(len(rows) >= 3 and all(re.fullmatch(r':?-+:?', c) for c in rows[1]), 'Malformed pipe table')
            del rows[1]
            require(len({len(r) for r in rows}) == 1, 'Ragged pipe table')
            items.append(('table', rows))
        elif re.match(r'(- |\d+\. )', line):
            flush()
            kind = 'ul' if line.startswith('- ') else 'ol'
            entries = []
            while i < len(lines) and re.match(r'(- |\d+\. )', lines[i]):
                check_subset_line(lines[i])
                entries.append(inline_text(re.sub(r'^(- |\d+\. )', '', lines[i])))
                i += 1
            i -= 1
            items.append((kind, entries))
        elif not line.strip():
            flush()
        else:
            check_subset_line(line)
            para.append(line.strip())
        i += 1
    flush()
    require(open_section is None, f'Section {open_section} is never closed')
    require(seen == SECTIONS, f'Section markers differ from the frozen list: {seen}')
    return items


class Mirror(HTMLParser):
    """Extracts the same semantic items from the mirrored region of a site page."""
    BLOCK = {'h1', 'h2', 'h3', 'p', 'li', 'th', 'td'}
    ALLOWED = BLOCK | {'div', 'section', 'ul', 'ol', 'a', 'code', 'pre', 'table', 'thead', 'tbody', 'tr'}

    def __init__(self):
        super().__init__(convert_charrefs=True)
        self.stack, self.items = [], []
        self.depth = None
        self.parts = None
        self.href = []
        self.list = None
        self.table = None
        self.row = None
        self.code = None
        self.language = None
        self.section_depths = []
        self.done = False

    def handle_starttag(self, tag, attributes):
        attrs = dict(attributes)
        if self.done:
            return
        if self.depth is None:
            if tag == 'div' and attrs.get('data-mirror') == 'setup':
                self.depth = len(self.stack)
            if tag not in VOID:
                self.stack.append(tag)
            return
        require(tag in self.ALLOWED, f'Element outside the mirrored subset: {tag}')
        if tag == 'div':
            require(attrs.get('class') == 'table-wrap', 'Unexpected div in the mirrored region')
        if tag == 'section':
            name = attrs.get('data-section')
            require(name, 'Mirrored section without a marker name')
            self.items.append(('section', name))
            self.section_depths.append((len(self.stack), name))
        if tag in self.BLOCK:
            require(self.parts is None, f'Nested block {tag}')
            self.parts = []
            self.block_tag = tag
            self.block_attrs = attrs
        if tag == 'a':
            require(self.parts is not None, 'Link outside a text block')
            self.href.append(attrs.get('href', ''))
            self.parts.append('[')
        if tag == 'code' and 'pre' not in self.stack:
            require(self.parts is not None, 'Inline code outside a text block')
            self.parts.append('`')
        if tag == 'pre':
            self.code = []
        if tag == 'code' and 'pre' in self.stack:
            match = re.fullmatch(r'language-(\w+)', attrs.get('class', ''))
            require(match, 'Code blocks need a language-* class')
            self.language = match.group(1)
        if tag in {'ul', 'ol'}:
            self.list = (tag, [])
        if tag == 'table':
            self.table = []
        if tag == 'tr':
            self.row = []
        self.stack.append(tag) if tag not in VOID else None

    def handle_endtag(self, tag):
        if self.done:
            return
        if self.depth is None:
            if self.stack and self.stack[-1] == tag:
                self.stack.pop()
            return
        require(self.stack and self.stack[-1] == tag, f'Mismatched closing {tag}')
        self.stack.pop()
        if tag == 'a':
            href = self.href.pop()
            self.parts.append(f']({REVERSE_LINK_MAP.get(href, href)})')
        elif tag == 'code' and 'pre' not in self.stack:
            self.parts.append('`')
        elif tag == 'code':
            text = ''.join(self.code)
            self.items.append(('code', self.language, text[:-1] if text.endswith('\n') else text))
        elif tag in self.BLOCK:
            text = inline_text(''.join(self.parts))
            self.parts = None
            if tag in {'h1', 'h2', 'h3'}:
                self.items.append(('h', int(tag[1]), text))
                require(self.block_attrs.get('id') == slug(text), f'Heading id differs from its GitHub anchor: {text}')
            elif tag == 'p':
                self.items.append(('p', text))
            elif tag == 'li':
                self.list[1].append(text)
            else:
                self.row.append(text)
        elif tag in {'ul', 'ol'}:
            self.items.append((tag, self.list[1]))
            self.list = None
        elif tag == 'tr':
            self.table.append(self.row)
            self.row = None
        elif tag == 'table':
            self.items.append(('table', self.table))
            self.table = None
        elif tag == 'section':
            depth, name = self.section_depths.pop()
            self.items.append(('/section', name))
        elif tag == 'pre':
            self.code = None
        elif tag == 'div' and len(self.stack) == self.depth:
            self.done = True

    def handle_data(self, data):
        if self.done or self.depth is None:
            return
        if self.code is not None and 'pre' in self.stack:
            self.code.append(data)
        elif self.parts is not None:
            self.parts.append(data)
        else:
            require(not data.strip(), f'Text outside a mirrored block: {data.strip()[:40]}')


def mirror_items(source):
    parser = Mirror()
    parser.feed(source)
    parser.close()
    require(parser.done, 'Setup page has no complete data-mirror region')
    return parser.items


def flat_text(item):
    if item[0] == 'p':
        return item[1]
    if item[0] in {'ul', 'ol'}:
        return ' '.join(item[1])
    return ''


def describe(item):
    text = repr(item)
    return text if len(text) < 160 else text[:157] + '...'


def check_mirror(markdown, page_source):
    expected = parse_markdown(markdown)
    actual = mirror_items(page_source)
    for index, (want, got) in enumerate(zip(expected, actual)):
        require(want == got, f'Setup mirror differs at item {index}: expected {describe(want)}, found {describe(got)}')
    require(len(expected) == len(actual), f'Setup mirror has {len(actual)} items; setup.md has {len(expected)}')
    return expected


# ---- Versions ----

def check_versions(site, health, manifest, tag=None):
    versions = {'internal/tools/health.go': health, 'server.json': manifest}
    for name, value in site.items():
        versions[f'site {name}'] = value
    for name, value in versions.items():
        require(value and SEMVER.fullmatch(value), f'Invalid version in {name}: {value!r}')
    require(len(set(versions.values())) == 1, 'Version mismatch: ' + ', '.join(f'{k}={v}' for k, v in versions.items()))
    if tag:
        stripped = tag[1:] if tag.startswith('v') else tag
        require(stripped == health, f'RELEASE_TAG {tag} does not match source version {health}')
    return health


def check_manifest(manifest):
    require(manifest.get('$schema', '').startswith('https://static.modelcontextprotocol.io/schemas/'), 'server.json schema URL')
    require(re.fullmatch(r'[a-zA-Z0-9.-]+/[a-zA-Z0-9._-]+', manifest.get('name', '')), 'server.json name format')
    require(manifest['name'] == 'io.github.avhn/trello-mcp', 'server.json name')
    require(manifest.get('title') == 'trello-mcp', 'server.json title')
    require(0 < len(manifest.get('description', '')) < 100, 'server.json description must be under 100 characters')
    require(manifest.get('repository') == {'url': 'https://github.com/avhn/trello-mcp', 'source': 'github'}, 'server.json repository')
    require(manifest.get('websiteUrl') == BASE, 'server.json websiteUrl')
    require('packages' not in manifest and 'remotes' not in manifest,
            'server.json is a metadata-only source template; release packages belong in generated metadata')


def local_file(reference, page_url):
    url = urlsplit(urljoin(page_url, reference))
    if url.scheme != 'https' or url.netloc != 'avhn.github.io':
        require(url.scheme in {'https', 'mailto'}, f'Unexpected URL scheme: {reference}')
        return None, ''
    require(url.path.startswith('/trello-mcp/'), f'Link escapes the project mount: {reference}')
    relative = unquote(url.path[len('/trello-mcp/'):])
    target = SITE / relative
    if url.path.endswith('/'):
        target /= 'index.html'
    target = target.resolve()
    require(target.is_relative_to(SITE), f'Link escapes the site: {reference}')
    require(target.is_file(), f'Broken local link: {reference}')
    return target, unquote(url.fragment)


def page_url(file):
    relative = file.relative_to(SITE).as_posix()
    return BASE + (relative[:-10] if relative.endswith('index.html') else relative)


def validate_png():
    read_png_scanlines((SITE / 'assets/social.png').read_bytes())


def fenced_blocks(markdown):
    return re.findall(r'^```(\w+)\n(.*?)^```$', markdown, re.S | re.M)


def validate_snippets(name, markdown):
    count = 0
    for language, body in fenced_blocks(markdown):
        try:
            if language == 'json':
                decoder, position = json.JSONDecoder(), 0
                while position < len(body):  # one document, or several consecutive examples
                    _, position = decoder.raw_decode(body, position)
                    position += len(body[position:]) - len(body[position:].lstrip())
            elif language == 'toml':
                tomllib.loads(body)
            else:
                continue
        except (ValueError, tomllib.TOMLDecodeError) as error:
            raise ValueError(f'{name}: invalid {language} snippet: {error}') from error
        count += 1
    return count


def check_claude_registration(command):
    """Check the documented forms, including the variadic --env option boundary."""
    tokens = shlex.split(command)
    prefix = ['claude', 'mcp', 'add', '--scope', 'user']
    suffix = ['--transport', 'stdio', 'trello', '--', PLACEHOLDER]
    require(tokens in (prefix + suffix, prefix + ['--env', f'TRELLO_TOOLS={PROFILE_TOOLS}'] + suffix),
            'Claude registration must use --scope user and --transport stdio before the server name; '
            '--env is variadic and must end at an option boundary')


def check_source_examples(markdown):
    # Presence only: never execute documentation or resolve the user's Go installation.
    examples = (
        ('sh', 'trello_mcp="$(go env GOBIN)"; if [ -z "$trello_mcp" ]; then '
         'trello_mcp="$(go env GOPATH)"; trello_mcp="${trello_mcp%%:*}/bin"; fi; '
         'trello_mcp="$trello_mcp/trello-mcp"; printf \'%s\\n\' "$trello_mcp"'),
        ('powershell', '$bin = go env GOBIN; if (-not $bin) { $gopath = (go env GOPATH) '
         "-split [IO.Path]::PathSeparator; $bin = Join-Path $gopath[0] 'bin' }; "
         "$trelloMcp = Join-Path $bin 'trello-mcp.exe'; $trelloMcp"),
    )
    blocks = fenced_blocks(markdown)
    for language, example in examples:
        require(any(lang == language and example in body.splitlines() for lang, body in blocks),
                f'Missing {language} source-path example')
    for command in ('"$trello_mcp" auth logout', '& $trelloMcp auth logout'):
        require(any(command in body.splitlines() for _, body in blocks),
                f'Missing resolved source logout command: {command}')


def check_desktop_contract(setup, tools_doc, manifest, health_source):
    require('## Claude Desktop extension\n' in setup, 'Missing stable Claude Desktop extension anchor')
    require('preview' not in setup.lower(), 'Setup must not retain the Desktop preview anchor or wording')
    require('supports macOS and Windows amd64' in setup and 'Linux Desktop is itself a beta' in setup,
            'Setup must distinguish supported Desktop bundles from Linux beta')
    expected = {'ok': True, 'credentialSource': 'env', 'access': 'unverified; call workspace_find'}
    require('"unverified; call workspace_find"' in health_source, 'Health source access guidance drift')
    for name, text in (('setup.md', setup), ('tools.md', tools_doc)):
        examples = []
        for language, body in fenced_blocks(text):
            if language != 'json':
                continue
            decoder, position = json.JSONDecoder(), 0
            while position < len(body):
                example, position = decoder.raw_decode(body, position)
                examples.append(example)
                position += len(body[position:]) - len(body[position:].lstrip())
        require(expected in examples, f'{name}: missing configured basic health response')
        require('health-only profile' in text.lower() and 'upstream read' in text,
                f'{name}: health-only profiles cannot verify upstream access')
    help_text = manifest['long_description']
    require('macOS and Windows are supported.' in help_text and 'preview' not in help_text.lower(),
            'Desktop manifest must describe supported bundles, not a preview')
    for phrase in ('Health does not verify Trello access.', 'workspace_find {}', 'health-only profile',
                   'No CLI auth/status or credential copying is required.',
                   'Windows refuses local path uploads and attachment downloads even when directories are set'):
        require(phrase in help_text, f'Desktop manifest help missing: {phrase}')
    health_help = next(tool['description'] for tool in manifest['tools'] if tool['name'] == 'server_health')
    require('not Trello auth' in health_help and 'workspace_find' in health_help,
            'Desktop static health help must distinguish local config from Trello access')
    for setting in ('download_dir', 'upload_dir'):
        description = manifest['user_config'][setting]['description']
        require('Windows refuses' in description and 'cannot enable' in description,
                f'Desktop {setting} must retain Windows refusal guidance')


def validate_clients(items):
    """Check the seven-client matrix content and native snippet shapes in setup.md."""
    start = next(i for i, item in enumerate(items) if item == ('section', 'clients'))
    end = next(i for i, item in enumerate(items) if item == ('/section', 'clients'))
    section = items[start:end]
    headings = [item[2] for item in section if item[0] == 'h' and item[1] == 3]
    require(headings == CLIENTS, f'Client headings differ from the seven-client matrix: {headings}')
    by_client, current = {}, None
    for item in section:
        if item[0] == 'h' and item[1] == 3:
            current = item[2]
            by_client[current] = []
        elif current:
            by_client[current].append(item)
    prose = {name: ' '.join(flat_text(i) for i in blocks) for name, blocks in by_client.items()}
    codes = {name: [i for i in blocks if i[0] == 'code'] for name, blocks in by_client.items()}
    for blocks in codes.values():
        for _, language, body in blocks:
            if language in {'json', 'toml', 'sh'}:
                require(PLACEHOLDER in body, f'Client snippet lacks the {PLACEHOLDER} placeholder')
    claude = codes['Claude Code'][0][2]
    require(claude.startswith('claude mcp add --scope user --transport stdio trello -- '), 'Claude Code must register with --scope user')
    require('--scope local' in prose['Claude Code'] and '--scope project' in prose['Claude Code'], 'Claude Code alternatives must be explicitly scoped')
    require('/mcp reconnect all' in prose['Claude Code'], 'Claude Code reconnect action')
    desktop = prose['Claude Desktop']
    for path in ('~/Library/Application Support/Claude/claude_desktop_config.json', '%APPDATA%\\Claude\\claude_desktop_config.json'):
        require(path in desktop, f'Claude Desktop path missing: {path}')
    require(json.loads(codes['Claude Desktop'][0][2])['mcpServers']['trello']['command'] == PLACEHOLDER, 'Claude Desktop snippet shape')
    require('~/.codex/config.toml' in prose['Codex CLI and desktop'] and '~/.codex/config.toml' in prose['ChatGPT desktop'], 'Codex and ChatGPT desktop share config.toml')
    require('ChatGPT in a browser' in ' '.join(flat_text(i) for i in items), 'ChatGPT web limitation must be stated')
    require(tomllib.loads(codes['Codex CLI and desktop'][0][2])['mcp_servers']['trello']['command'] == PLACEHOLDER, 'Codex TOML snippet shape')
    require('~/.config/opencode/opencode.json' in prose['OpenCode'] and 'opencode.json' in prose['OpenCode'], 'OpenCode locations')
    opencode = json.loads(codes['OpenCode'][0][2])['mcp']['trello']
    require(opencode['type'] == 'local' and opencode['command'] == [PLACEHOLDER], 'OpenCode snippet shape')
    require('~/.cursor/mcp.json' in prose['Cursor'] and '.cursor/mcp.json' in prose['Cursor'], 'Cursor locations')
    require(json.loads(codes['Cursor'][0][2])['mcpServers']['trello']['command'] == PLACEHOLDER, 'Cursor snippet shape')
    require('.vscode/mcp.json' in prose['VS Code'] and 'MCP: Open User Configuration' in prose['VS Code'], 'VS Code locations')
    vscode = json.loads(codes['VS Code'][0][2])['servers']['trello']
    require(vscode['type'] == 'stdio' and vscode['command'] == PLACEHOLDER, 'VS Code snippet shape')
    for item in items:
        if item[0] == 'code' and 'claude mcp add' in item[2] and item[2].startswith('claude'):
            check_claude_registration(item[2])
    require(any(item[0] == 'code' and item[2].startswith('claude mcp add') and
                f'--env TRELLO_TOOLS={PROFILE_TOOLS}' in item[2] for item in items),
            'Missing Claude read-oriented profile registration')
    flat = ' '.join(flat_text(i) for i in items)
    require('private terminal' in flat and 'never run bare' in flat.lower(), 'Human-only auth handoff')
    require('server_health' in flat and 'trello-mcp auth status' in flat, 'Secret-free verification')


# ---- Read-parameter table against the registration and runtime schema ----

READ_HEADING = '## Read parameters'


def registry_tools(source):
    """Return {name: (parameters, required, is_write)} from the tool definitions."""
    creation = re.search(r'const cardCreateParams = "([^"]+)"', source)
    tools = {}
    for line in source.splitlines():
        definition = re.search(r'\{name: "([a-z_]+)"', line)
        if not definition:
            continue
        params = re.search(r'params: "([^"]*)"', line)
        require(params is not None, f'Tool definition without params: {definition.group(1)}')
        names = params.group(1).split()
        if '+ cardCreateParams' in line:
            require(creation is not None, 'Missing cardCreateParams constant')
            names += creation.group(1).split()
        required = re.search(r'required: "([^"]*)"', line)
        tools[definition.group(1)] = (names, set(required.group(1).split()) if required else set(), 'write: true' in line)
    return tools


def schema_facts(arguments_source):
    """Extract defaults, ceilings and enums the runtime schema publishes."""
    facts = {}
    body = re.search(r'func maxLimit\(.*?\n}\n', arguments_source, re.S)
    require(body is not None, 'Missing maxLimit in arguments.go')
    limits = {name: (int(a), int(b)) for name, a, b in re.findall(r'case "(\w+)":\s+return (\d+), (\d+)', body.group(0))}
    fallback = re.search(r'default:\s+return (\d+), (\d+)', body.group(0))
    require(fallback is not None, 'Missing default limits in arguments.go')
    facts['limits'] = (limits, (int(fallback.group(1)), int(fallback.group(2))))
    for key, pattern in (
            ('filter', r'case "filter":\s+p\["default"\] = "([^"]+)"'),
            ('checklist_name', r'case "checklist_get":\s+p\["default"\] = "([^"]+)"'),
            ('includeDesc', r'case "includeDesc":\s+p = map\[string\]any\{"type": "boolean", "default": (true|false)\}'),
            ('view', r'case "view":\s+p\["enum"\] = \[\]string\{([^}]*)\}'),
            ('extraFields', r'case "extraFields":\s+p = map\[string\]any\{[^\n]*"enum": \[\]string\{([^}]*)\}'),
            ('detail', r'values := \[\]string\{([^}]*)\}'),
            ('health', r'values = \[\]string\{([^}]*)\}')):
        match = re.search(pattern, arguments_source, re.S)
        require(match is not None, f'Missing schema fact in arguments.go: {key}')
        facts[key] = match.group(1)
    for key in ('view', 'extraFields', 'detail', 'health'):
        facts[key] = set(re.findall(r'"([^"]+)"', facts[key]))
    return facts


def read_table(markdown):
    start = markdown.find(READ_HEADING)
    require(start >= 0, f'Missing {READ_HEADING} section in tools.md')
    rows = []
    for line in markdown[start + len(READ_HEADING):].split('\n'):
        if line.startswith('|'):
            rows.append([cell.strip() for cell in line.strip().strip('|').split('|')])
        elif rows:
            break
    require(len(rows) > 2 and rows[0] == ['Tool', 'Required', 'Optional', 'Defaults and accepted values'], 'Read parameter table header')
    del rows[1]
    require(all(len(row) == 4 for row in rows), 'Ragged read parameter table')
    return rows[1:]


def ticks(cell):
    return set(re.findall(r'`([^`]+)`', cell))


def check_read_parameters(markdown, registry_source, arguments_source):
    """Compare the tools.md read table with registered non-write tools; return its rows."""
    tools = registry_tools(registry_source)
    facts = schema_facts(arguments_source)
    limits, fallback = facts['limits']
    rows = read_table(markdown)
    reads = {name for name, (_, _, write) in tools.items() if not write}
    listed = [row[0].strip('`') for row in rows]
    require(len(listed) == len(set(listed)), 'Duplicate tool in the read parameter table')
    require(set(listed) == reads, f'Read parameter table tools differ from registry: missing {sorted(reads - set(listed))}, extra {sorted(set(listed) - reads)}')
    for tool, required_cell, optional_cell, rules in rows:
        name = tool.strip('`')
        params, required, _ = tools[name]
        have_required, have_optional = ticks(required_cell), ticks(optional_cell)
        require(have_required == required, f'Requiredness drift for {name}: registry requires {sorted(required)}, table has {sorted(have_required)}')
        require(have_optional == set(params) - required, f'Parameter drift for {name}: optional {sorted(set(params) - required)}, table has {sorted(have_optional)}')
        mentioned = set(re.findall(r'`(\w+)`:', rules))
        expected = set()
        if 'limit' in params:
            expected.add('limit')
            default, ceiling = limits.get(name, fallback)
            require(re.search(rf'`limit`: default {default}, max {ceiling}(?!\d)', rules), f'Limit drift for {name}: default {default}, max {ceiling}')
        if 'filter' in params:
            expected.add('filter')
            require(f'`filter`: default `{facts["filter"]}`' in rules, f'Filter default drift for {name}')
        if 'includeDesc' in params:
            expected.add('includeDesc')
            require(f'`includeDesc`: default `{facts["includeDesc"]}`' in rules, f'includeDesc default drift for {name}')
        if name == 'checklist_get':
            expected.add('name')
            require(f'`name`: default `{facts["checklist_name"]}`' in rules, 'checklist_get name default drift')
        for key, values in (('detail', facts['health'] if name == 'server_health' else facts['detail']),
                            ('view', facts['view']), ('extraFields', facts['extraFields'])):
            if key in params:
                expected.add(key)
                segment = re.search(rf'`{key}`: ([^;]*)', rules)
                require(segment is not None, f'Missing {key} values for {name}')
                require(ticks(segment.group(1)) == values, f'Enum drift for {name}.{key}: {sorted(values)}')
        require(mentioned == expected, f'Undocumented or extra rules for {name}: expected {sorted(expected)}, found {sorted(mentioned)}')
    return rows


def check_read_table_on_page(rows, page):
    """The site tools page must carry the same table rows, with code formatting removed."""
    published = {tuple(row) for row in page.rows}
    for row in rows:
        want = tuple(normalized(cell.replace('`', '')) for cell in row)
        require(want in published, f'Tools page lacks the read parameter row for {row[0].strip("`")}')


def validate():
    pages = {}
    for file in sorted(SITE.rglob('*.html')):
        source = file.read_text()
        validate_brand(source)
        require(not re.search(FORBIDDEN_TEXT, source), f'Forbidden punctuation or emoji: {file}')
        page = Page()
        try:
            page.feed(source)
            page.finish()
        except (ValueError, json.JSONDecodeError) as error:
            raise ValueError(f'{file.relative_to(SITE)}: {error}') from error
        pages[file] = page
    expected_files = {SITE / 'index.html', SITE / 'setup/index.html', SITE / 'tools/index.html',
                      SITE / 'security/index.html', SITE / '404.html'}
    require(set(pages) == expected_files, f'Expected four content routes plus 404, found {sorted(p.relative_to(SITE).as_posix() for p in pages)}')
    titles, descriptions, link_count, schema_count = [], [], 0, 0
    site_versions = {}
    for file, page in pages.items():
        url = page_url(file)
        prefix = file.relative_to(SITE).as_posix()
        require(page.doctype, f'{prefix}: missing doctype')
        require([attrs.get('lang') for tag, attrs in page.elements if tag == 'html'] == ['en'], f'{prefix}: missing language')
        for tag in ('main', 'header', 'footer'):
            require(sum(name == tag for name, _ in page.elements) == 1, f'{prefix}: expected one {tag}')
        require(page.copy_scripts == 1, f'{prefix}: expected exactly one deferred copy script')
        require(len(page.text('h1')) == 1, f'{prefix}: expected one h1')
        headings = [int(tag[1]) for tag, _, _ in page.blocks if re.fullmatch('h[1-3]', tag)]
        require(headings and headings[0] == 1, f'{prefix}: headings must start with h1')
        require(all(b <= a + 1 for a, b in zip(headings, headings[1:])), f'{prefix}: skipped heading level')
        title = page.text('title')
        require(len(title) == 1 and 0 < len(title[0]) < 60, f'{prefix}: title length')
        titles.append(title[0])
        description = page.meta.get('description', '')
        require(120 <= len(description) <= 160, f'{prefix}: description length')
        descriptions.append(description)
        require(page.meta.get('viewport') == 'width=device-width, initial-scale=1', f'{prefix}: viewport')
        require(page.meta.get('theme-color'), f'{prefix}: theme color')
        require(('robots' in page.meta) == (file == SITE / '404.html'), f'{prefix}: only the 404 page is noindex')
        if 'robots' in page.meta:
            require(page.meta['robots'] == 'noindex', f'{prefix}: robots must be noindex')
        canonicals = [attrs.get('href') for tag, attrs in page.elements if tag == 'link' and attrs.get('rel') == 'canonical']
        require(canonicals == [url], f'{prefix}: canonical mismatch')
        for key, value in {
            'og:type': 'website', 'og:title': title[0], 'og:description': description,
            'og:url': url, 'og:image': BASE + 'assets/social.png', 'og:image:width': '1200',
            'og:image:height': '630', 'twitter:card': 'summary_large_image',
            'twitter:title': title[0], 'twitter:description': description,
            'twitter:image': BASE + 'assets/social.png',
        }.items():
            require(page.meta.get(key) == value, f'{prefix}: invalid {key}')
        require(page.meta.get('og:image:alt') and page.meta.get('twitter:image:alt'), f'{prefix}: social image alt text')
        stylesheets = [attrs.get('href') for tag, attrs in page.elements if tag == 'link' and attrs.get('rel') == 'stylesheet']
        require(stylesheets == ['/trello-mcp/assets/style.css'], f'{prefix}: expected one local stylesheet')
        for reference in page.links + [page.meta['og:image'], page.meta['twitter:image']]:
            target, fragment = local_file(reference, url)
            if target and fragment:
                require(target in pages and fragment in pages[target].ids, f'{prefix}: missing anchor {reference}')
            link_count += 1
        types = Counter(schema.get('@type') for schema in page.schemas)
        require(types['SoftwareApplication'] == 1, f'{prefix}: missing SoftwareApplication')
        require(types['BreadcrumbList'] == (file != SITE / 'index.html'), f'{prefix}: breadcrumbs')
        app = next(schema for schema in page.schemas if schema['@type'] == 'SoftwareApplication')
        for key in ('name', 'operatingSystem', 'softwareVersion', 'downloadUrl', 'codeRepository', 'license'):
            require(app.get(key), f'{prefix}: missing application {key}')
        site_versions[prefix] = app['softwareVersion']
        require(app['applicationCategory'] == 'DeveloperApplication', f'{prefix}: application category')
        require(app['offers']['price'] == '0', f'{prefix}: expected a free offer')
        require(app['license'].endswith('/blob/main/LICENSE') and (ROOT / 'LICENSE').read_text().split()[:7] == ['GNU', 'GENERAL', 'PUBLIC', 'LICENSE', 'Version', '3,', '29'], 'License mismatch')
        for schema in page.schemas:
            require(schema['@context'] == 'https://schema.org', f'{prefix}: schema context')
            if schema['@type'] == 'BreadcrumbList':
                require([item['position'] for item in schema['itemListElement']] == [1, 2], f'{prefix}: breadcrumb positions')
                require(schema['itemListElement'][-1]['item'] == url, f'{prefix}: breadcrumb URL')
            if schema['@type'] == 'FAQPage':
                require(file == SITE / 'security/index.html', 'FAQ schema belongs on the security page')
                visible = {text: i for i, text in enumerate(page.text('h3'))}
                paragraphs = [normalized(p) for p in page.text('p')]
                for question in schema['mainEntity']:
                    require(question['name'] in visible, f'FAQ question is not visible: {question["name"]}')
                    require(normalized(question['acceptedAnswer']['text']) in paragraphs, f'FAQ answer is not visible: {question["name"]}')
                faq_headings = [attrs for tag, attrs, _ in page.blocks if tag == 'h3' and attrs.get('id', '').startswith('question-')]
                require(len(faq_headings) == len(schema['mainEntity']), 'Visible FAQ and FAQPage schema list different questions')
        require(types['FAQPage'] == (file == SITE / 'security/index.html'), f'{prefix}: FAQ schema')
        for snippet in page.text('pre'):
            if snippet.lstrip().startswith('{'):
                json.loads(snippet)
        schema_count += len(page.schemas)
    require(len(set(titles)) == len(titles), 'Duplicate page titles')
    require(len(set(descriptions)) == len(descriptions), 'Duplicate descriptions')

    sitemap = ET.parse(SITE / 'sitemap.xml').getroot()
    urls = [loc.text for loc in sitemap.findall('{*}url/{*}loc')]
    require(urls == [BASE + route for route in ROUTES], f'Sitemap must list exactly the four canonical routes: {urls}')
    require('Sitemap: ' + BASE + 'sitemap.xml' in (SITE / 'robots.txt').read_text(), 'Missing sitemap in robots.txt')
    css = (SITE / 'assets/style.css').read_text()
    require('@import' not in css and 'url(' not in css, 'Stylesheet must not load network resources')
    require(len(css.encode()) < 10000, 'Keep the shared stylesheet under 10,000 bytes')
    require('prefers-color-scheme: dark' in css, 'Missing dark mode')
    copy_js = (SITE / 'assets/copy.js').read_bytes()
    require(len(copy_js) <= 4096, 'copy.js must stay within 4 KiB')
    copy_source = copy_js.decode()
    require('execCommand' not in copy_source and 'http' not in copy_source and 'import(' not in copy_source,
            'copy.js must use the Clipboard API only, with no remote code')
    require((SITE / '.nojekyll').is_file(), 'Missing .nojekyll')
    html_sizes = {f: f.stat().st_size for f in pages}
    require(html_sizes[SITE / 'index.html'] <= 9000, f'Home HTML exceeds 9,000 bytes: {html_sizes[SITE / "index.html"]}')
    require(sum(html_sizes.values()) <= 104 * 1024, 'Total HTML exceeds 104 KiB')
    favicon = ET.parse(SITE / 'assets/favicon.svg').getroot()
    require(favicon.get('data-mark') == 'tm', 'Favicon must use the independent TM mark')
    validate_png()

    # Docs, README and the setup mirror.
    readme = (ROOT / 'README.md').read_text()
    setup = (ROOT / 'docs/setup.md').read_text()
    configuration = (ROOT / 'docs/configuration.md').read_text()
    tools_doc = (ROOT / 'docs/tools.md').read_text()
    llms = (SITE / 'llms.txt').read_text()
    for name, text in (('README.md', readme), ('setup.md', setup), ('configuration.md', configuration),
                       ('tools.md', tools_doc), ('llms.txt', llms)):
        require(not re.search(FORBIDDEN_TEXT, text), f'Forbidden punctuation or emoji: {name}')
        require('claude-desktop-extension-preview' not in text, f'Obsolete Desktop preview fragment: {name}')
    require(normalized(TRADEMARK) in normalized(readme), 'Missing README trademark notice')
    lines = readme.splitlines()
    require(len(lines) <= 250, f'README exceeds 250 lines: {len(lines)}')
    require(any('install.sh | sh' in line for line in lines[:25]), 'README install command must be within the first 25 lines')
    paste = next(line for line in setup.splitlines() if line.startswith('Set up trello-mcp by following '))
    require(RAW_SETUP in paste and any(line == paste for line in lines[:35]), 'README paste-to-agent line must be within the first 35 lines')
    require(readme.count('img.shields.io') == 4, 'README must carry exactly four Shields badges')
    items = check_mirror(setup, (SITE / 'setup/index.html').read_text())
    validate_clients(items)
    check_source_examples(setup)
    check_desktop_contract(setup, tools_doc,
                           json.loads((ROOT / 'packaging/mcpb/manifest.template.json').read_text()),
                           (ROOT / 'internal/tools/health.go').read_text())
    for file in SITE.rglob('*'):
        if file.suffix in {'.html', '.txt'}:
            require('claude-desktop-extension-preview' not in file.read_text(),
                    f'Obsolete Desktop preview fragment: {file}')
    home = pages[SITE / 'index.html']
    require(paste in [t.strip() for t in home.text('pre')], 'Home page must carry the exact paste-to-agent line')
    snippet_count = sum(validate_snippets(name, text) for name, text in (('setup.md', setup), ('configuration.md', configuration), ('tools.md', tools_doc)))
    profile_line = f'TRELLO_TOOLS={PROFILE_TOOLS}'
    for name, text in (('setup.md', setup), ('README.md', readme), ('tools.md', tools_doc)):
        require(profile_line in text, f'{name}: missing the exact named read-oriented profile')
    for name in ('llms.txt',):
        require(RAW_SETUP in llms and RAW_DOCS + 'tools.md' in llms and RAW_DOCS + 'configuration.md' in llms, f'{name}: must link raw setup, tools and configuration')
    # Anchors of README links into setup.md and README itself.
    setup_slugs = {slug(i[2]) for i in items if i[0] == 'h'}
    for fragment in re.findall(r'docs/setup\.md#([\w-]+)', readme):
        require(fragment in setup_slugs, f'README links to a missing setup anchor: {fragment}')
    readme_slugs = {slug(m.group(2)) for m in re.finditer(r'^(#{1,6}) (.+)$', readme, re.M)}
    for fragment in re.findall(r'\]\(#([\w-]+)\)', readme):
        require(fragment in readme_slugs, f'README links to a missing anchor: {fragment}')
    security_source = (SITE / 'security/index.html').read_text()
    require(DOCS + 'configuration.md' in security_source, 'Security page must link the configuration reference')
    for path in ('~/.secrets/.trello-mcp.secret', 'TRELLO_MCP_CREDENTIALS', '0700', '0600'):
        require(path in unescape(security_source), f'security: missing credential summary term {path}')
    require('Credential storage' in configuration, 'configuration.md must remain the credential reference')
    for name in ('credentials.go', 'private_unix.go', 'private_windows.go'):
        source = (ROOT / 'internal/credentials' / name).read_text()
        errors = re.findall(r'fmt.Errorf\("(credentials (?:directory is|file is|must be owned|parent must|file must|require a)[^"]+)"\)', source)
        for error in errors:
            require(error in configuration, f'Undocumented credential refusal in configuration.md: {error}')
    bench = readme.split('<!-- bench:start -->')[1].split('<!-- bench:end -->')[0]
    tables, current = [], []
    for line in bench.splitlines():
        if line.startswith('|'):
            current.append([cell.strip() for cell in line.strip().strip('|').split('|')])
        elif current:
            tables.append(current)
            current = []
    if current:
        tables.append(current)
    require(len(tables) == 3, 'README benchmark needs the response, startup and independent-response tables')
    responses = tables[0][2:]
    require(len(responses) == 6, 'README benchmark must keep exactly six response rows')
    for scenario, raw, ours, reduction in responses:
        a, b = float(raw.strip('~k')), float(ours.strip('~k'))
        require(abs((1 - b / a) * 100 - float(reduction.strip('%'))) <= 1.5, f'Benchmark reduction does not match its columns: {scenario}')
    require(any('48 tools' in cell for cell in tables[1][0]) and any('10 tools' in cell for cell in tables[1][0]), 'Startup table must name the 48-tool default and 10-tool profile')
    require('Raw Trello JSON' in tables[0][0], 'Benchmark must label its raw reference')

    # Tools page against the registry.
    source = ''.join((ROOT / 'internal/tools' / name).read_text() for name in ('registry.go', 'delivery.go'))
    names = set(re.findall(r'\{name: "([a-z_]+)"', source))
    tools_page = pages[SITE / 'tools/index.html']
    actual = {attrs['id'] for tag, attrs, _ in tools_page.blocks if tag == 'dt'}
    require(actual == names, 'Tools page differs from registry and appended delivery definitions')
    require(set(PROFILE_TOOLS.split(',')) <= names and len(PROFILE_TOOLS.split(',')) == 10, 'Named profile must be ten registered tools')
    creation_params = re.search(r'const cardCreateParams = "([^"]+)"', source).group(1)
    details = {}
    current = None
    for tag, attrs, text in tools_page.blocks:
        if tag == 'dt':
            current = attrs['id']
        elif tag == 'dd':
            details[current] = normalized(text)
    for line in source.splitlines():
        definition = re.search(r'\{name: "([a-z_]+)"', line)
        if not definition:
            continue
        params = re.search(r'params: "([^"]*)"', line).group(1)
        if '+ cardCreateParams' in line:
            params += creation_params
        expected_params = 'Parameters: ' + ', '.join(params.split()) + '.'
        require(expected_params in details[definition.group(1)],
                f'Parameter drift for {definition.group(1)}: {expected_params}')
    registry_source = (ROOT / 'internal/tools/registry.go').read_text() + (ROOT / 'internal/tools/delivery.go').read_text()
    read_rows = check_read_parameters(tools_doc, registry_source, (ROOT / 'internal/tools/arguments.go').read_text())
    check_read_table_on_page(read_rows, tools_page)
    for text_name, text in (('tools.md', tools_doc), ('configuration.md', configuration)):
        require('Trello documents no limit' in normalized(text) and 'Up to 16384 chars' in normalized(text), f'{text_name}: text-limit evidence labels')
    tools_text = ' '.join(tools_page.text('p'))
    require('filter' in details['board_activity'] and 'filter' in details['card_history'], 'Activity tools must document filter')
    require('summary' in tools_text and 'full' in tools_text and 'url' in tools_text, 'Tools page must explain summary versus full detail')

    # One version gate: source, manifest, every SoftwareApplication, optional release tag.
    health = re.search(r'var Version = "([^"]+)"', (ROOT / 'internal/tools/health.go').read_text())
    require(health is not None, 'Missing default Version in internal/tools/health.go')
    manifest = json.loads((ROOT / 'server.json').read_text())
    check_manifest(manifest)
    tag = os.environ.get('RELEASE_TAG', '')
    version = check_versions(site_versions, health.group(1), manifest['version'], tag)

    print(f'PASS: {len(pages)} HTML pages (four routes plus 404), {link_count} links/assets, {schema_count} JSON-LD blocks, titles and descriptions')
    print(f'PASS: sitemap lists {len(urls)} canonical routes; robots, noindex 404, favicon, 1200x630 PNG, llms.txt and {len(names)} tools')
    print(f'PASS: setup page mirrors docs/setup.md ({len(items)} items, exact code text); {snippet_count} JSON/TOML snippets parse; seven-client matrix')
    print(f'PASS: README {len(lines)} lines, six-row benchmark, paste line, four badges; credential refusals documented in configuration.md')
    print(f'PASS: version {version} matches health.go, server.json and all site pages' + (f'; RELEASE_TAG {tag} accepted' if tag else ' (no RELEASE_TAG)'))
    print(f'PASS: read parameter table matches {len(read_rows)} registered non-write tools and the site tools page')
    print('PASS: server.json source template checked for metadata-only shape; release package validation belongs to the pinned publisher and release_manifest.py')
    total = sum(file.stat().st_size for file in SITE.rglob('*') if file.is_file())
    print(f'PASS: one local CSS file ({len(css.encode())} bytes), copy.js ({len(copy_js)} bytes), home {html_sizes[SITE / "index.html"]} bytes, HTML {sum(html_sizes.values())} bytes; site {total} bytes')


class ParserTests(unittest.TestCase):
    def parse(self, source):
        page = Page()
        page.feed(source)
        page.finish()
        return page

    def test_independent_mark_and_notice(self):
        validate_brand('<header data-mark="tm"></header><footer>' + TRADEMARK + '</footer>')

    def test_missing_trademark_notice(self):
        with self.assertRaisesRegex(ValueError, 'trademark'):
            validate_brand('<header data-mark="tm"></header><footer>Independent</footer>')

    def test_platform_brand_colors(self):
        with self.assertRaisesRegex(ValueError, 'brand colors'):
            validate_brand('<header data-mark="tm" fill="#0079BF"></header><footer>' + TRADEMARK + '</footer>')

    def test_missing_independent_mark(self):
        with self.assertRaisesRegex(ValueError, 'monogram'):
            validate_brand('<header></header><footer>' + TRADEMARK + '</footer>')

    def test_balanced_html_and_svg(self):
        self.parse('<main><h1>Title</h1><svg><rect width="1"/></svg><br></main>')

    def test_unclosed_tag(self):
        with self.assertRaisesRegex(ValueError, 'Unclosed'):
            self.parse('<main><p>Missing end')

    def test_mismatched_tag(self):
        with self.assertRaisesRegex(ValueError, 'Unexpected closing'):
            self.parse('<main><p>Mismatch</main></p>')

    def test_duplicate_id(self):
        with self.assertRaisesRegex(ValueError, 'Duplicate id'):
            self.parse('<p id="x"></p><p id="x"></p>')

    def test_approved_copy_script(self):
        page = self.parse(f'<script src="{COPY_SRC}" defer></script>')
        self.assertEqual(page.copy_scripts, 1)

    def test_rejected_scripts(self):
        for source in (
            '<script src="https://example.com/app.js"></script>',
            f'<script src="https://example.com{COPY_SRC}" defer></script>',
            '<script src="/trello-mcp/assets/other.js" defer></script>',
            f'<script src="{COPY_SRC}"></script>',
            f'<script src="{COPY_SRC}" defer async></script>',
            f'<script src="{COPY_SRC}" defer type="module"></script>',
            f'<script src="{COPY_SRC}" defer>alert(1)</script>',
            '<script>alert(1)</script>',
            '<script type="module">alert(1)</script>',
            f'<script src="{COPY_SRC}" defer></script><script src="{COPY_SRC}" defer></script>',
        ):
            with self.subTest(source=source), self.assertRaises(ValueError):
                self.parse(source)

    def test_inline_handlers_and_unapproved_attributes(self):
        for source in ('<a href="/" onclick="x()">x</a>', '<a href="/" style="color:red">x</a>', '<a href="/" target="_blank">x</a>',
                       '<div data-copy="x"></div>'):
            with self.subTest(source=source), self.assertRaises(ValueError):
                self.parse(source)

    def test_invalid_json_ld(self):
        with self.assertRaises(json.JSONDecodeError):
            self.parse('<script type="application/ld+json">{broken}</script>')

    def test_json_ld_takes_no_extra_attributes(self):
        with self.assertRaisesRegex(ValueError, 'JSON-LD'):
            self.parse('<script type="application/ld+json" defer>{}</script>')

    def test_missing_local_link(self):
        with self.assertRaisesRegex(ValueError, 'Broken local link'):
            local_file('/trello-mcp/does-not-exist/', BASE)

    def test_mount_escape(self):
        with self.assertRaisesRegex(ValueError, 'escapes the project mount'):
            local_file('/other-project/', BASE)

    def test_removed_routes_are_gone(self):
        for route in ('install/', 'clients/', 'faq/'):
            with self.subTest(route=route), self.assertRaisesRegex(ValueError, 'Broken local link'):
                local_file('/trello-mcp/' + route, BASE)

    def test_social_card_pixels_match_generator(self):
        actual = read_png_scanlines((SITE / 'assets/social.png').read_bytes())
        self.assertEqual(actual, render_scanlines())

    def test_malformed_sitemap_and_routes(self):
        self.assertEqual(ROUTES, ('', 'setup/', 'tools/', 'security/'))
        self.assertNotIn('404.html', ROUTES)


class MirrorTests(unittest.TestCase):
    markdown = (ROOT / 'docs/setup.md').read_text()
    source = (SITE / 'setup/index.html').read_text()

    def test_unmodified_mirror_matches(self):
        check_mirror(self.markdown, self.source)

    def test_one_edit_in_site_fails(self):
        for label, old, new in (
            ('command', 'codex mcp add', 'codex mcp adds'),
            ('path', '~/.codex/config.toml', '~/.codex/config.json'),
            ('scope', '--scope user', '--scope project'),
            ('reconnect', '/mcp reconnect all', '/mcp reconnect'),
            ('table cell', 'Quit and reopen', 'Quit'),
            ('link', 'blob/main/docs/configuration.md', 'blob/main/docs/config.md'),
            ('install command', 'install.sh | sh', 'install.sh | bash'),
            ('prose', 'Resolve every path', 'Resolve each path'),
        ):
            with self.subTest(label=label):
                self.assertIn(old, self.source)
                with self.assertRaisesRegex(ValueError, 'Setup mirror differs|has .* items'):
                    check_mirror(self.markdown, self.source.replace(old, new, 1))

    def test_one_edit_in_markdown_fails(self):
        with self.assertRaises(ValueError):
            check_mirror(self.markdown.replace('--scope user', '--scope local', 1), self.source)

    def test_edit_in_late_section_fails(self):
        start = self.source.index('<h2 id="verify">')
        with self.assertRaises(ValueError):
            check_mirror(self.markdown, self.source[:start] + self.source[start:].replace('<li>', '<li>x', 1))

    def test_site_chrome_is_excluded(self):
        check_mirror(self.markdown, self.source.replace('<p>Canonical source:', '<p>Different chrome:', 1))

    def test_subset_violations_in_markdown(self):
        base = '<!-- section:agent -->\n## A\n%s\n<!-- /section:agent -->\n'
        for bad in ('> quote', '  - nested', '![img](x.png)', '<b>html</b>', 'some *emphasis* here', '#### deep', '```\nno language\n```'):
            with self.subTest(bad=bad), self.assertRaises(ValueError):
                parse_markdown(base % bad)

    def test_section_markers_must_pair_and_not_nest(self):
        with self.assertRaisesRegex(ValueError, 'nested'):
            parse_markdown('<!-- section:agent -->\n<!-- section:install -->\n')
        with self.assertRaisesRegex(ValueError, 'never closed|closed without'):
            parse_markdown('<!-- section:agent -->\ntext\n')
        with self.assertRaisesRegex(ValueError, 'frozen list'):
            parse_markdown('<!-- section:agent -->\n<!-- /section:agent -->\n')

    def test_site_rejects_elements_outside_the_subset(self):
        wrapped = '<div data-mirror="setup">%s</div>'
        for bad in ('<p>a <em>b</em></p>', '<blockquote>q</blockquote>', '<img src="x" alt="">', 'loose text'):
            with self.subTest(bad=bad), self.assertRaises(ValueError):
                mirror_items(wrapped % bad)

    def test_client_matrix_content(self):
        validate_clients(parse_markdown(self.markdown))
        with self.assertRaises(ValueError):
            validate_clients(parse_markdown(self.markdown.replace('### Cursor', '### Cursor IDE')))
        with self.assertRaises(ValueError):
            validate_clients(parse_markdown(self.markdown.replace('claude mcp add --scope user', 'claude mcp add', 1)))


class SetupContractTests(unittest.TestCase):
    setup = (ROOT / 'docs/setup.md').read_text()
    tools = (ROOT / 'docs/tools.md').read_text()
    manifest = json.loads((ROOT / 'packaging/mcpb/manifest.template.json').read_text())
    health = (ROOT / 'internal/tools/health.go').read_text()

    def test_profile_option_boundary(self):
        command = (f'claude mcp add --scope user --env TRELLO_TOOLS={PROFILE_TOOLS} '
                   f'--transport stdio trello -- "{PLACEHOLDER}"')
        check_claude_registration(command)
        check_claude_registration(f'claude mcp add --scope user --transport stdio trello -- "{PLACEHOLDER}"')
        for bad in (command.replace('--transport stdio ', ''),
                    command.replace('--transport stdio ', '').replace('--scope user', '--scope user --transport stdio'),
                    command.replace('--scope user', '--scope local'),
                    command.replace(' trello -- ', ' trello ')):
            with self.subTest(command=bad), self.assertRaisesRegex(ValueError, 'option boundary'):
                check_claude_registration(bad)

    def test_source_example_presence(self):
        check_source_examples(self.setup)
        for old, new in (('go env GOBIN', 'go env GOPATH'),
                         ('[IO.Path]::PathSeparator', "':'"),
                         ('"$trello_mcp" auth logout', 'trello-mcp auth logout'),
                         ('& $trelloMcp auth logout', 'trello-mcp auth logout')):
            with self.subTest(old=old), self.assertRaisesRegex(ValueError, 'source'):
                check_source_examples(self.setup.replace(old, new))

    def test_desktop_and_health_contract(self):
        check_desktop_contract(self.setup, self.tools, self.manifest, self.health)
        for old, new in (('## Claude Desktop extension', '## Claude Desktop extension preview'),
                         ('Linux Desktop is itself a beta', 'Linux Desktop is supported'),
                         ('"access":"unverified; call workspace_find"', '"access":"verified"')):
            with self.subTest(old=old), self.assertRaises(ValueError):
                check_desktop_contract(self.setup.replace(old, new), self.tools, self.manifest, self.health)
        for old in ('Health does not verify Trello access.', 'No CLI auth/status or credential copying is required.',
                    'macOS and Windows are supported.', 'Windows refuses'):
            manifest = json.loads(json.dumps(self.manifest).replace(old, 'removed'))
            with self.subTest(old=old), self.assertRaises(ValueError):
                check_desktop_contract(self.setup, self.tools, manifest, self.health)
        for setting in ('download_dir', 'upload_dir'):
            manifest = json.loads(json.dumps(self.manifest))
            manifest['user_config'][setting]['description'] = 'Set a directory to enable transfers.'
            with self.subTest(setting=setting), self.assertRaisesRegex(ValueError, setting):
                check_desktop_contract(self.setup, self.tools, manifest, self.health)
        manifest = json.loads(json.dumps(self.manifest))
        manifest['tools'][0]['description'] = 'Validates Trello authentication.'
        with self.assertRaisesRegex(ValueError, 'static health help'):
            check_desktop_contract(self.setup, self.tools, manifest, self.health)
        with self.assertRaisesRegex(ValueError, 'source access guidance'):
            check_desktop_contract(self.setup, self.tools, self.manifest,
                                   self.health.replace('unverified; call workspace_find', 'verified'))
        with self.assertRaisesRegex(ValueError, 'health response'):
            check_desktop_contract(self.setup, self.tools.replace('"credentialSource":"env"',
                                                                '"credentialSource":"file"'),
                                   self.manifest, self.health)


class ReadParameterTests(unittest.TestCase):
    markdown = (ROOT / 'docs/tools.md').read_text()
    registry = (ROOT / 'internal/tools/registry.go').read_text() + (ROOT / 'internal/tools/delivery.go').read_text()
    arguments = (ROOT / 'internal/tools/arguments.go').read_text()

    def check(self, markdown=None, registry=None, arguments=None):
        return check_read_parameters(markdown or self.markdown, registry or self.registry, arguments or self.arguments)

    def edit(self, old, new):
        start = self.markdown.index(READ_HEADING)
        head, tail = self.markdown[:start], self.markdown[start:]
        self.assertIn(old, tail)
        return head + tail.replace(old, new, 1)

    def test_live_table_matches(self):
        self.assertEqual(len(self.check()), 19)

    def test_missing_tool_row_fails(self):
        row = '| `member_find` | none | `boardId`, `nameFilter` | none |\n'
        with self.assertRaisesRegex(ValueError, 'tools differ'):
            self.check(self.edit(row, ''))

    def test_extra_tool_row_fails(self):
        with self.assertRaisesRegex(ValueError, 'tools differ'):
            self.check(self.edit('| `member_find` |', '| `board_create` | `name` | none | none |\n| `member_find` |'))

    def test_missing_parameter_fails(self):
        with self.assertRaisesRegex(ValueError, 'Parameter drift for card_find'):
            self.check(self.edit('`before`, `includeDesc` |', '`before` |'))

    def test_extra_parameter_fails(self):
        with self.assertRaisesRegex(ValueError, 'Parameter drift for card_get'):
            self.check(self.edit('| `boardId`, `detail`, `view`, `extraFields` |', '| `boardId`, `detail`, `view`, `extraFields`, `fields` |'))

    def test_requiredness_drift_fails(self):
        with self.assertRaisesRegex(ValueError, 'Requiredness drift for card_get'):
            self.check(self.edit('| `card_get` | `cardId` |', '| `card_get` | none |'))

    def test_limit_drift_fails(self):
        for old, new in (('`limit`: default 100, max 1000', '`limit`: default 50, max 1000'),
                         ('`limit`: default 20, max 100 ', '`limit`: default 20, max 1000 ')):
            with self.subTest(old=old), self.assertRaisesRegex(ValueError, 'Limit drift'):
                self.check(self.edit(old, new))

    def test_default_drift_fails(self):
        with self.assertRaisesRegex(ValueError, 'includeDesc default drift'):
            self.check(self.edit('`includeDesc`: default `true`', '`includeDesc`: default `false`'))
        with self.assertRaisesRegex(ValueError, 'Filter default drift'):
            self.check(self.edit('`filter`: default `all`', '`filter`: default `none`'))

    def test_enum_drift_fails(self):
        with self.assertRaisesRegex(ValueError, 'Enum drift for card_get.view'):
            self.check(self.edit('`view`: `json` or `markdown`', '`view`: `json`, `markdown` or `html`'))
        with self.assertRaisesRegex(ValueError, 'Enum drift for server_health.detail'):
            self.check(self.edit('`performance` or `policy`', '`performance`'))

    def test_missing_include_desc_rule_fails(self):
        with self.assertRaisesRegex(ValueError, 'includeDesc default drift'):
            self.check(self.edit('; `includeDesc`: default `true`', ''))

    def test_registry_drift_is_detected(self):
        registry = self.registry.replace('params: "boardId limit since before filter"', 'params: "boardId limit since before filter extra"', 1)
        with self.assertRaisesRegex(ValueError, 'Parameter drift for board_activity'):
            self.check(registry=registry)
        arguments = self.arguments.replace('return 100, 1000', 'return 100, 500', 1)
        with self.assertRaisesRegex(ValueError, 'Limit drift for card_find'):
            self.check(arguments=arguments)


class VersionTests(unittest.TestCase):
    site = {'index.html': '0.5.0', 'setup/index.html': '0.5.0'}

    def test_matching_versions_and_tag(self):
        self.assertEqual(check_versions(self.site, '0.5.0', '0.5.0'), '0.5.0')
        self.assertEqual(check_versions(self.site, '0.5.0', '0.5.0', 'v0.5.0'), '0.5.0')
        self.assertEqual(check_versions(self.site, '0.5.0', '0.5.0', '0.5.0'), '0.5.0')

    def test_mismatches_fail(self):
        with self.assertRaisesRegex(ValueError, 'Version mismatch'):
            check_versions({**self.site, 'setup/index.html': '0.4.0'}, '0.5.0', '0.5.0')
        with self.assertRaisesRegex(ValueError, 'Version mismatch'):
            check_versions(self.site, '0.5.0', '0.4.9')
        with self.assertRaisesRegex(ValueError, 'Version mismatch'):
            check_versions(self.site, '0.5.1', '0.5.0')

    def test_tag_mismatch_fails(self):
        for tag in ('v0.5.1', 'v0.4.0', 'v0.5', 'vv0.5.0', 'release-0.5.0'):
            with self.subTest(tag=tag), self.assertRaisesRegex(ValueError, 'RELEASE_TAG'):
                check_versions(self.site, '0.5.0', '0.5.0', tag)

    def test_invalid_version_fails(self):
        with self.assertRaisesRegex(ValueError, 'Invalid version'):
            check_versions({'index.html': 'v0.5.0'}, '0.5.0', '0.5.0')

    def test_manifest_stays_metadata_only(self):
        manifest = json.loads((ROOT / 'server.json').read_text())
        check_manifest(manifest)
        for extra in ({'packages': []}, {'remotes': []}):
            with self.assertRaisesRegex(ValueError, 'metadata-only'):
                check_manifest({**manifest, **extra})
        with self.assertRaisesRegex(ValueError, 'description'):
            check_manifest({**manifest, 'description': 'x' * 100})

    def test_live_sources_agree(self):
        health = re.search(r'var Version = "([^"]+)"', (ROOT / 'internal/tools/health.go').read_text()).group(1)
        self.assertEqual(json.loads((ROOT / 'server.json').read_text())['version'], health)


if __name__ == '__main__':
    if sys.argv[1:] == ['--self-test']:
        unittest.main(argv=[sys.argv[0]])
    elif sys.argv[1:]:
        raise SystemExit('Usage: validate.py [--self-test]')
    else:
        try:
            validate()
        except (ValueError, KeyError, OSError, ET.ParseError, tomllib.TOMLDecodeError) as error:
            raise SystemExit(f'FAIL: {error}') from error
