// Optional local browser checks. No packages; the only site JS is the local deferred copy.js.
// Usage: node scripts/site/check_browser.mjs /path/to/chromium
import { spawn } from 'node:child_process';
import { createServer } from 'node:http';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { dirname, extname, join, resolve, sep } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '..', '..');
const site = join(root, 'site');
const executable = process.argv[2];
if (!executable) throw new Error('Pass a Chromium executable path');
const profile = await mkdtemp(join(root, '.browser-'));
const types = { '.html': 'text/html', '.css': 'text/css', '.js': 'text/javascript', '.svg': 'image/svg+xml', '.png': 'image/png' };
// Exact same-origin request allowlist: the five pages, the missing-page 404 and four shared assets.
const allowedPaths = new Set(['/trello-mcp/', '/trello-mcp/setup/', '/trello-mcp/tools/', '/trello-mcp/security/',
  '/trello-mcp/404.html', '/trello-mcp/missing-page/', '/trello-mcp/assets/style.css', '/trello-mcp/assets/copy.js',
  '/trello-mcp/assets/favicon.svg']);
const server = createServer(async (request, response) => {
  let path = decodeURIComponent(new URL(request.url, 'http://localhost').pathname);
  let status = 200;
  if (!path.startsWith('/trello-mcp/')) {
    path = '/trello-mcp/404.html';
    status = 404;
  }
  let file = resolve(site, path.slice('/trello-mcp/'.length));
  if (path.endsWith('/')) file = join(file, 'index.html');
  if (!file.startsWith(site + sep)) {
    response.writeHead(400).end();
    return;
  }
  try {
    const data = await readFile(file);
    response.writeHead(status, { 'Content-Type': types[extname(file)] || 'text/plain' }).end(data);
  } catch {
    response.writeHead(404, { 'Content-Type': 'text/html' }).end(await readFile(join(site, '404.html')));
  }
});
let browser;
let socket;
let session;
let nextId = 0;
const pending = new Map();
const listeners = new Map();
const requests = new Set();
const errors = [];
const timeout = setTimeout(() => {
  console.error('FAIL: browser checks timed out');
  browser?.kill('SIGTERM');
  process.exitCode = 1;
}, 60000);

function command(method, params = {}, useSession = true) {
  return new Promise((resolve, reject) => {
    const id = ++nextId;
    pending.set(id, { resolve, reject });
    socket.send(JSON.stringify({ id, method, params, ...(useSession && session ? { sessionId: session } : {}) }));
  });
}
async function evaluate(expression) {
  const result = await command('Runtime.evaluate', { expression, returnByValue: true, awaitPromise: true });
  if (result.exceptionDetails) throw new Error(JSON.stringify(result.exceptionDetails));
  return result.result.value;
}
async function navigate(url) {
  const loaded = new Promise(resolve => listeners.set('Page.loadEventFired', resolve));
  await command('Page.navigate', { url });
  await loaded;
}
async function press(key, code, keyCode, text) {
  const base = { key, code, windowsVirtualKeyCode: keyCode, nativeVirtualKeyCode: keyCode };
  await command('Input.dispatchKeyEvent', { type: 'keyDown', ...base, ...(text ? { text } : {}) });
  await command('Input.dispatchKeyEvent', { type: 'keyUp', ...base });
}
function assert(condition, message) {
  if (!condition) throw new Error(message);
}
// Each content route at one viewport. The clipboard is a synthetic stub, never the machine clipboard.
async function copyChecks(origin, path) {
  const width = 1024;
  await command('Emulation.setDeviceMetricsOverride', { width, height: 900, deviceScaleFactor: 1, mobile: false });
  const stub = await command('Page.addScriptToEvaluateOnNewDocument', { source: `
    window.__copied = [];
    window.__mode = 'ok';
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: {
      writeText: text => window.__mode === 'reject' ? Promise.reject(new Error('denied')) : (window.__copied.push(text), Promise.resolve()),
    } });` });
  const url = `${origin}/trello-mcp/${path}`;
  await navigate(url);
  const shape = await evaluate(`(() => {
    const codes = [...document.querySelectorAll('pre > code')];
    const buttons = [...document.querySelectorAll('button.copy')];
    return {
      codes: codes.length, buttons: buttons.length,
      typed: buttons.every(b => b.type === 'button' && !b.closest('pre') && b.getAttribute('aria-label')),
      paired: codes.every(c => c.parentElement.previousElementSibling?.matches('button.copy')),
      labels: new Set(buttons.map(b => b.getAttribute('aria-label'))).size,
      live: !!document.querySelector('[role=status][aria-live=polite]'),
      leaked: codes.some(c => /\\bCopy\\b|Copied/.test(c.textContent)),
    };
  })()`);
  assert(shape.codes > (path === 'setup/' ? 10 : 0) && shape.codes === shape.buttons,
    `${path || '/'}: copy buttons do not cover every code block: ${JSON.stringify(shape)}`);
  assert(shape.typed && shape.paired && shape.live && !shape.leaked && shape.labels === shape.buttons, `Copy markup is wrong: ${JSON.stringify(shape)}`);
  const exact = await evaluate(`(async () => {
    const codes = [...document.querySelectorAll('pre > code')];
    const buttons = [...document.querySelectorAll('button.copy')];
    const wait = () => new Promise(r => setTimeout(r, 20));
    const failures = [];
    for (let i = 0; i < codes.length; i++) {
      buttons[i].click();
      await wait();
      if (window.__copied.at(-1) !== codes[i].textContent) failures.push('text ' + i);
      if (buttons[i].textContent !== 'Copied') failures.push('label ' + i);
    }
    const before = window.__copied.length;
    buttons[0].click(); buttons[0].click();
    await wait();
    return { failures, repeat: window.__copied.length - before, status: document.querySelector('[role=status]').textContent };
  })()`);
  assert(!exact.failures.length, `Copied text differs from code text: ${exact.failures.join(', ')}`);
  assert(exact.repeat === 2 && exact.status.startsWith('Copied'), `Repeat clicks failed: ${JSON.stringify(exact)}`);
  // Real key events on the focused button.
  await evaluate(`(() => { window.__copied.length = 0; document.querySelector('button.copy').focus(); })()`);
  await press('Enter', 'Enter', 13, '\r');
  await new Promise(r => setTimeout(r, 50));
  await press(' ', 'Space', 32, ' ');
  await new Promise(r => setTimeout(r, 50));
  const keyboard = await evaluate(`({ count: window.__copied.length, focused: document.activeElement.matches('button.copy') })`);
  assert(keyboard.count === 2 && keyboard.focused, `Keyboard activation failed: ${JSON.stringify(keyboard)}`);
  // Clipboard rejection and an absent API give honest manual guidance, never a false success.
  for (const mode of ['reject', 'absent']) {
    const outcome = await evaluate(`(async () => {
      ${mode === 'reject' ? "window.__mode = 'reject';" : "Object.defineProperty(navigator, 'clipboard', { configurable: true, value: undefined });"}
      const button = document.querySelector('button.copy');
      button.click();
      await new Promise(r => setTimeout(r, 20));
      return { label: button.textContent, status: document.querySelector('[role=status]').textContent };
    })()`);
    assert(outcome.label === 'Copy' && /manually/.test(outcome.status) && !/^Copied/.test(outcome.status), `${mode}: ${JSON.stringify(outcome)}`);
  }
  await command('Page.removeScriptToEvaluateOnNewDocument', { identifier: stub.identifier });
  // Without JavaScript the code stays readable and selectable.
  await command('Emulation.setScriptExecutionDisabled', { value: true });
  await navigate(url);
  const plain = await evaluate(`({ pre: document.querySelectorAll('pre > code').length, buttons: document.querySelectorAll('button').length,
    select: [...document.querySelectorAll('pre')].every(p => getComputedStyle(p).userSelect !== 'none'),
    width: document.documentElement.scrollWidth })`);
  await command('Emulation.setScriptExecutionDisabled', { value: false });
  assert(plain.pre === shape.codes && plain.buttons === 0 && plain.select && plain.width <= width, `No-JS rendering failed: ${JSON.stringify(plain)}`);
}

try {
  await new Promise(resolve => server.listen(0, '127.0.0.1', resolve));
  const origin = `http://127.0.0.1:${server.address().port}`;
  browser = spawn(executable, [
    '--headless', '--disable-gpu', '--remote-debugging-port=0',
    `--user-data-dir=${profile}`, '--no-first-run', '--no-default-browser-check',
    '--disable-background-networking', '--disable-sync', '--disable-component-update',
    '--metrics-recording-only', '--disable-default-apps', 'about:blank',
  ], { stdio: ['ignore', 'ignore', 'pipe'] });
  const endpoint = await new Promise((resolve, reject) => {
    let stderr = '';
    browser.stderr.on('data', chunk => {
      stderr += chunk;
      const match = stderr.match(/DevTools listening on (ws:\/\/\S+)/);
      if (match) resolve(match[1]);
    });
    browser.on('error', reject);
    browser.on('exit', code => reject(new Error(`Chromium exited ${code}: ${stderr}`)));
  });
  socket = new WebSocket(endpoint);
  await new Promise((resolve, reject) => {
    socket.addEventListener('open', resolve, { once: true });
    socket.addEventListener('error', reject, { once: true });
  });
  socket.addEventListener('message', event => {
    const data = JSON.parse(event.data);
    if (data.id) {
      const callback = pending.get(data.id);
      pending.delete(data.id);
      if (data.error) callback.reject(new Error(JSON.stringify(data.error)));
      else callback.resolve(data.result);
    }
    if (data.method === 'Network.requestWillBeSent') requests.add(data.params.request.url);
    if (data.method === 'Runtime.exceptionThrown') errors.push(data.params.exceptionDetails.text);
    if (data.method && listeners.has(data.method)) {
      listeners.get(data.method)(data.params);
      listeners.delete(data.method);
    }
  });
  const target = await command('Target.createTarget', { url: 'about:blank' }, false);
  session = (await command('Target.attachToTarget', { targetId: target.targetId, flatten: true }, false)).sessionId;
  await command('Page.enable');
  await command('Runtime.enable');
  await command('Network.enable');
  const paths = ['', 'setup/', 'tools/', 'security/', '404.html', 'missing-page/'];
  const widths = [320, 390, 420, 421, 760, 761, 768, 1440];
  let checked = 0;
  for (const color of ['light', 'dark']) {
    await command('Emulation.setEmulatedMedia', { features: [{ name: 'prefers-color-scheme', value: color }] });
    for (const width of widths) {
      await command('Emulation.setDeviceMetricsOverride', { width, height: 900, deviceScaleFactor: 1, mobile: false });
      for (const path of paths) {
        const loaded = new Promise(resolve => listeners.set('Page.loadEventFired', resolve));
        await command('Page.navigate', { url: `${origin}/trello-mcp/${path}` });
        await loaded;
        const result = await command('Runtime.evaluate', {
          expression: `(() => {
            const body = getComputedStyle(document.body);
            const luminance = color => {
              const channels = color.match(/[0-9.]+/g).slice(0, 3).map(Number).map(v => {
                v /= 255; return v <= .04045 ? v / 12.92 : ((v + .055) / 1.055) ** 2.4;
              });
              return channels[0] * .2126 + channels[1] * .7152 + channels[2] * .0722;
            };
            const contrast = (fg, bg) => {
              const a = luminance(fg), b = luminance(bg);
              return (Math.max(a, b) + .05) / (Math.min(a, b) + .05);
            };
            const samples = [
              [body.color, body.backgroundColor],
              ...['.lead', '.site-nav a'].flatMap(selector => {
                const node = document.querySelector(selector);
                return node ? [[getComputedStyle(node).color, body.backgroundColor]] : [];
              }),
              ...['.button:not(.secondary)', '.badge', '.note', '.terminal'].flatMap(selector => {
                const node = document.querySelector(selector);
                if (!node) return [];
                const style = getComputedStyle(node);
                return [[style.color, style.backgroundColor]];
              }),
            ];
            const headerTargets = [...document.querySelectorAll('.site-header a')].map(node => {
              const rect = node.getBoundingClientRect();
              return { label: node.textContent.trim(), ...rect.toJSON() };
            });
            const headerTargetFailures = headerTargets.filter(rect => rect.width < 44 || rect.height < 44);
            const headerSpacingFailures = headerTargets.flatMap((a, index) => headerTargets.slice(index + 1).flatMap(b => {
              const gap = Math.hypot(Math.max(0, a.left - b.right, b.left - a.right), Math.max(0, a.top - b.bottom, b.top - a.bottom));
              return gap < 4 ? [[a.label, b.label, gap]] : [];
            }));
            return {
              width: innerWidth,
              headerTargetCount: headerTargets.length,
              headerTargetFailures,
              headerSpacingFailures,
              documentWidth: document.documentElement.scrollWidth,
              bodyWidth: document.body.scrollWidth,
              h1: document.querySelectorAll('h1').length,
              contrast: Math.min(...samples.map(([fg, bg]) => contrast(fg, bg))),
              title: document.title,
            };
          })()`, returnByValue: true,
        });
        if (result.exceptionDetails) throw new Error(JSON.stringify(result.exceptionDetails));
        const value = result.result.value;
        if (value.documentWidth > width || value.bodyWidth > width || value.h1 !== 1 || value.contrast < 4.5 ||
            value.headerTargetCount !== 4 || value.headerTargetFailures.length || value.headerSpacingFailures.length) {
          throw new Error(`${color} ${width}px ${path || '/'}: ${JSON.stringify(value)}`);
        }
        if (path === 'missing-page/' && value.title !== 'Page Not Found | trello-mcp') {
          throw new Error('Custom 404 failed');
        }
        checked++;
      }
    }
  }
  const copyPaths = ['', 'setup/', 'tools/', 'security/'];
  for (const path of copyPaths) {
    try {
      await copyChecks(origin, path);
    } catch (error) {
      throw new Error(`${path || '/'}: ${error.message}`, { cause: error });
    }
  }
  const unexpected = [...requests].filter(url => !(url.startsWith(origin + '/') && allowedPaths.has(new URL(url).pathname)));
  if (unexpected.length) throw new Error(`Unexpected page requests: ${unexpected.join(', ')}`);
  if (errors.length) throw new Error(`Browser errors: ${errors.join(', ')}`);
  console.log(`PASS: ${checked} browser checks at ${widths.join('/')}px in light/dark mode, no horizontal overflow, sampled text contrast >= 4.5:1`);
  console.log('PASS: all header links have >= 44x44px targets and >= 4px separation');
  console.log('PASS: custom 404 keeps project-root assets; requests match the same-origin allowlist; no browser exceptions');
  console.log(`PASS: copy buttons on ${copyPaths.map(path => '/trello-mcp/' + path).join(', ')} cover every code block with exact text, repeat clicks, Enter/Space, rejection, missing API and no-JS reading`);
} finally {
  clearTimeout(timeout);
  socket?.close();
  if (browser && browser.exitCode === null && browser.signalCode === null) {
    const exited = new Promise(resolve => browser.once('exit', resolve));
    browser.kill('SIGTERM');
    await exited;
  }
  await new Promise(resolve => server.close(resolve));
  // Chromium child processes can finish writing just after the parent exits.
  await rm(profile, { recursive: true, force: true, maxRetries: 10, retryDelay: 100 });
}
