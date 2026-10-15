// Optional local browser checks. No packages or runtime site JS.
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
const types = { '.html': 'text/html', '.css': 'text/css', '.svg': 'image/svg+xml', '.png': 'image/png' };
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
  function command(method, params = {}, useSession = true) {
    return new Promise((resolve, reject) => {
      const id = ++nextId;
      pending.set(id, { resolve, reject });
      socket.send(JSON.stringify({ id, method, params, ...(useSession && session ? { sessionId: session } : {}) }));
    });
  }
  const target = await command('Target.createTarget', { url: 'about:blank' }, false);
  session = (await command('Target.attachToTarget', { targetId: target.targetId, flatten: true }, false)).sessionId;
  await command('Page.enable');
  await command('Runtime.enable');
  await command('Network.enable');
  const paths = ['', 'install/', 'clients/', 'tools/', 'security/', 'faq/', '404.html', 'missing-page/'];
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
            value.headerTargetCount !== 6 || value.headerTargetFailures.length || value.headerSpacingFailures.length) {
          throw new Error(`${color} ${width}px ${path || '/'}: ${JSON.stringify(value)}`);
        }
        if (path === 'missing-page/' && value.title !== 'Page Not Found | trello-mcp') {
          throw new Error('Custom 404 failed');
        }
        checked++;
      }
    }
  }
  if ([...requests].some(url => !url.startsWith(origin + '/'))) throw new Error('Unexpected external page request');
  if (errors.length) throw new Error(`Browser errors: ${errors.join(', ')}`);
  console.log(`PASS: ${checked} browser checks at ${widths.join('/')}px in light/dark mode, no horizontal overflow, sampled text contrast >= 4.5:1`);
  console.log('PASS: all header links have >= 44x44px targets and >= 4px separation');
  console.log('PASS: custom 404 keeps project-root assets; no external page requests or browser exceptions');
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
