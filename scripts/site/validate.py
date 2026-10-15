"""Validate the static site offline with Python's standard library.

Run: python3 -I scripts/site/validate.py
Parser regression checks: python3 -I scripts/site/validate.py --self-test
"""
from collections import Counter
from html import unescape
from html.parser import HTMLParser
from importlib.util import module_from_spec, spec_from_file_location
from pathlib import Path
from urllib.parse import unquote, urljoin, urlsplit
import json
import re
import sys
import unittest
import xml.etree.ElementTree as ET

ROOT = Path(__file__).resolve().parents[2]
SITE = ROOT / 'site'
BASE = 'https://avhn.github.io/trello-mcp/'
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
        for attr, value in attrs.items():
            require(not attr.startswith('on'), f'Inline event handler: {attr}')
            require(attr != 'style', 'Use the shared stylesheet, not inline styles')
        require(tag not in {'style', 'iframe', 'object', 'embed', 'base'}, f'Unexpected element: {tag}')
        if tag == 'script':
            require(attrs.get('type') == 'application/ld+json' and 'src' not in attrs,
                    'Only inline JSON-LD scripts are allowed')
            self.script = []
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
            self.schemas.append(json.loads(''.join(self.script)))
            self.script = None

    def handle_data(self, data):
        if self.script is not None:
            self.script.append(data)
        else:
            for _, _, parts in self.captures:
                parts.append(data)

    def finish(self):
        self.close()
        require(not self.stack, f'Unclosed elements: {self.stack}')
        require(not self.captures, 'Unclosed text capture')

    def text(self, tag):
        return [text for name, _, text in self.blocks if name == tag]


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


def validate():
    pages = {}
    for file in sorted(SITE.rglob('*.html')):
        source = file.read_text()
        validate_brand(source)
        require(not re.search('[–—\U0001f300-\U0001faff]', source), f'Forbidden punctuation or emoji: {file}')
        page = Page()
        try:
            page.feed(source)
            page.finish()
        except (ValueError, json.JSONDecodeError) as error:
            raise ValueError(f'{file.relative_to(SITE)}: {error}') from error
        pages[file] = page
    require(len(pages) == 7, 'Expected seven HTML pages')
    titles, descriptions, link_count, schema_count = [], [], 0, 0
    for file, page in pages.items():
        url = page_url(file)
        prefix = file.relative_to(SITE).as_posix()
        require(page.doctype, f'{prefix}: missing doctype')
        require([attrs.get('lang') for tag, attrs in page.elements if tag == 'html'] == ['en'], f'{prefix}: missing language')
        for tag in ('main', 'header', 'footer'):
            require(sum(name == tag for name, _ in page.elements) == 1, f'{prefix}: expected one {tag}')
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
        require(app['applicationCategory'] == 'DeveloperApplication', f'{prefix}: application category')
        require(app['offers']['price'] == '0', f'{prefix}: expected a free offer')
        require(app['license'].endswith('/blob/main/LICENSE') and (ROOT / 'LICENSE').read_text().startswith('MIT License'), 'License mismatch')
        for schema in page.schemas:
            require(schema['@context'] == 'https://schema.org', f'{prefix}: schema context')
            if schema['@type'] == 'BreadcrumbList':
                require([item['position'] for item in schema['itemListElement']] == [1, 2], f'{prefix}: breadcrumb positions')
                require(schema['itemListElement'][-1]['item'] == url, f'{prefix}: breadcrumb URL')
            if schema['@type'] == 'FAQPage':
                require(file == SITE / 'faq/index.html', 'FAQ schema belongs on FAQ page')
                for question in schema['mainEntity']:
                    require(question['name'] in page.text('h2'), 'FAQ question is not visible')
                    require(normalized(question['acceptedAnswer']['text']) in [normalized(p) for p in page.text('p')], 'FAQ answer is not visible')
        require(types['FAQPage'] == (file == SITE / 'faq/index.html'), f'{prefix}: FAQ schema')
        for snippet in page.text('pre'):
            if snippet.lstrip().startswith('{'):
                json.loads(snippet)
        schema_count += len(page.schemas)
    require(len(set(titles)) == len(titles), 'Duplicate page titles')
    require(len(set(descriptions)) == len(descriptions), 'Duplicate descriptions')
    sitemap = ET.parse(SITE / 'sitemap.xml').getroot()
    urls = [loc.text for loc in sitemap.findall('{*}url/{*}loc')]
    require(len(urls) == len(set(urls)) and set(urls) == {page_url(p) for p in pages}, 'Sitemap must list every page exactly once')
    require('Sitemap: ' + BASE + 'sitemap.xml' in (SITE / 'robots.txt').read_text(), 'Missing sitemap in robots.txt')
    css = (SITE / 'assets/style.css').read_text()
    require('@import' not in css and 'url(' not in css, 'Stylesheet must not load network resources')
    require(len(css.encode()) < 10000, 'Keep the shared stylesheet small')
    require('prefers-color-scheme: dark' in css, 'Missing dark mode')
    require((SITE / '.nojekyll').is_file(), 'Missing .nojekyll')
    favicon = ET.parse(SITE / 'assets/favicon.svg').getroot()
    require(favicon.get('data-mark') == 'tm', 'Favicon must use the independent TM mark')
    validate_png()
    readme = (ROOT / 'README.md').read_text()
    require(TRADEMARK in readme, 'Missing README trademark notice')
    for name in ('security', 'install', 'clients', 'faq'):
        source = (SITE / name / 'index.html').read_text()
        for term in ('~/.secrets/.trello-mcp.secret', '~/.secrets.env', 'TRELLO_MCP_CREDENTIALS',
                     'TRELLO_TOKEN_SCOPE', 'https://github.com/avhn/secret', 'secret show trello-mcp',
                     'secret exec -f trello-mcp', 'secret stash', '0700', '0600'):
            require(term in source, f'{name}: missing credential convention {term}')
    security = unescape((SITE / 'security/index.html').read_text())
    for name in ('credentials.go', 'private_unix.go', 'private_windows.go'):
        source = (ROOT / 'internal/credentials' / name).read_text()
        errors = re.findall(r'fmt.Errorf\("(credentials (?:directory is|file is|must be owned|parent must|file must|require a)[^"]+)"\)', source)
        for error in errors:
            require(error in security and error in readme, f'Undocumented credential refusal: {error}')
    bench = readme.split('<!-- bench:start -->')[1].split('<!-- bench:end -->')[0]
    expected = [[cell.strip() for cell in line.strip().strip('|').split('|')] for line in bench.splitlines() if line.startswith('|')][2:]
    require(pages[SITE / 'index.html'].rows[1:] == expected, 'Benchmark table differs from current README')
    source = ''.join((ROOT / 'internal/tools' / name).read_text() for name in ('registry.go', 'delivery.go'))
    names = set(re.findall(r'\{name: "([a-z_]+)"', source))
    actual = {attrs['id'] for tag, attrs, _ in pages[SITE / 'tools/index.html'].blocks if tag == 'dt'}
    require(actual == names, 'Tools page differs from registry and appended delivery definitions')
    creation_params = re.search(r'const cardCreateParams = "([^"]+)"', source).group(1)
    details = {}
    current = None
    for tag, attrs, text in pages[SITE / 'tools/index.html'].blocks:
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
    print(f'PASS: {len(pages)} HTML pages, {link_count} links/assets, {schema_count} JSON-LD blocks, titles and descriptions')
    print(f'PASS: sitemap covers {len(urls)} pages; robots, favicon, 1200x630 PNG, client JSON, README benchmark and {len(names)} tools')
    total = sum(file.stat().st_size for file in SITE.rglob('*') if file.is_file())
    print(f'PASS: one local CSS file ({len(css.encode())} bytes), no executable JS, no remote fonts/assets; site {total} bytes')


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

    def test_external_script(self):
        with self.assertRaisesRegex(ValueError, 'Only inline JSON-LD'):
            self.parse('<script src="https://example.com/app.js"></script>')

    def test_invalid_json_ld(self):
        with self.assertRaises(json.JSONDecodeError):
            self.parse('<script type="application/ld+json">{broken}</script>')

    def test_missing_local_link(self):
        with self.assertRaisesRegex(ValueError, 'Broken local link'):
            local_file('/trello-mcp/does-not-exist/', BASE)

    def test_mount_escape(self):
        with self.assertRaisesRegex(ValueError, 'escapes the project mount'):
            local_file('/other-project/', BASE)

    def test_social_card_pixels_match_generator(self):
        actual = read_png_scanlines((SITE / 'assets/social.png').read_bytes())
        self.assertEqual(actual, render_scanlines())


if __name__ == '__main__':
    if sys.argv[1:] == ['--self-test']:
        unittest.main(argv=[sys.argv[0]])
    elif sys.argv[1:]:
        raise SystemExit('Usage: validate.py [--self-test]')
    else:
        try:
            validate()
        except (ValueError, KeyError, OSError, ET.ParseError) as error:
            raise SystemExit(f'FAIL: {error}') from error
