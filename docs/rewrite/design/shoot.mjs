#!/usr/bin/env node
// NCD 2 — design review harness.
//
// Captures every surface the gallery marks with `data-surface`, at 1x and 2x,
// and prints the paths. Optional audit: fonts actually loaded, text that
// overflows its box without an ellipsis, component heights off the 4px grid,
// numbers without tabular figures, and console errors.
//
//   node docs/rewrite/design/shoot.mjs <baseUrl> [outDir] [options]
//
//   <baseUrl>          e.g. http://127.0.0.1:1430  (the harness opens <baseUrl>/gallery)
//   [outDir]           default: $TMPDIR/ncd-shots/<yyyymmdd-hhmmss>
//   --route <path>     gallery route, default /gallery
//   --only <text>      capture only surfaces whose id contains <text> (repeatable)
//   --isolated         also load <route>?surface=<id> for each surface and capture it alone
//   --scales 1,2       device scale factors, default 1,2
//   --reduced-motion   emulate prefers-reduced-motion: reduce (default: no-preference, animations paused)
//   --viewport WxH     default 1600x1000
//   --audit            print the audit as JSON after the shots (also written to <outDir>/audit.json)
//
// Gallery contract (docs/rewrite/design/README.md):
//   - every capturable surface has data-surface="<kind>/<widget|variant>[/<page|state>]"
//     e.g. dock/v-normal, dial/crit, panel/docker/now, toast/stack
//   - the page sets data-gallery-ready on <html> or <body> once fonts are loaded and
//     every surface has rendered its first snapshot
//   - <route>?surface=<id> renders only that surface on the --bg canvas
//
// Playwright is resolved from: the current project, $NCD_PLAYWRIGHT (a directory
// containing node_modules/playwright). Chromium from
// $NCD_CHROME, else Playwright's own, else the newest ~/.cache/ms-playwright/chromium-*.

import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { createRequire } from 'node:module';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));

// ---------- args ----------
const argv = process.argv.slice(2);
if (!argv.length || argv.includes('-h') || argv.includes('--help')) {
  console.log(fs.readFileSync(fileURLToPath(import.meta.url), 'utf8').split('\n').slice(1, 32).map(l => l.replace(/^\/\/ ?/, '')).join('\n'));
  process.exit(argv.length ? 0 : 2);
}
const opt = { route: '/gallery', only: [], isolated: false, scales: [1, 2], reduced: false, viewport: [1600, 1000], audit: false };
const pos = [];
for (let i = 0; i < argv.length; i++) {
  const a = argv[i];
  if (a === '--route') opt.route = argv[++i];
  else if (a === '--only') opt.only.push(argv[++i]);
  else if (a === '--isolated') opt.isolated = true;
  else if (a === '--scales') opt.scales = argv[++i].split(',').map(Number);
  else if (a === '--reduced-motion') opt.reduced = true;
  else if (a === '--viewport') opt.viewport = argv[++i].split('x').map(Number);
  else if (a === '--audit') opt.audit = true;
  else pos.push(a);
}
const base = pos[0].replace(/\/$/, '');
const stamp = new Date().toISOString().replace(/[-:]/g, '').replace('T', '-').slice(0, 15);
const out = path.resolve(pos[1] || path.join(os.tmpdir(), 'ncd-shots', stamp));
fs.mkdirSync(out, { recursive: true });

// ---------- playwright ----------
function loadPlaywright() {
  const tries = [
    process.cwd(),
    process.env.NCD_PLAYWRIGHT,
  ].filter(Boolean);
  for (const dir of tries) {
    try { return createRequire(path.join(dir, 'package.json'))('playwright'); } catch {}
  }
  throw new Error('playwright not found; set NCD_PLAYWRIGHT to a directory that has node_modules/playwright');
}
function chromePath() {
  if (process.env.NCD_CHROME) return process.env.NCD_CHROME;
  const cache = path.join(os.homedir(), '.cache/ms-playwright');
  try {
    const dirs = fs.readdirSync(cache).filter(d => /^chromium-\d+$/.test(d))
      .sort((a, b) => +b.split('-')[1] - +a.split('-')[1]);
    for (const d of dirs) {
      const p = path.join(cache, d, 'chrome-linux64/chrome');
      if (fs.existsSync(p)) return p;
    }
  } catch {}
  return undefined;
}
const { chromium } = loadPlaywright();
let browser;
try { browser = await chromium.launch(); }
catch { browser = await chromium.launch({ executablePath: chromePath() }); }

// ---------- capture ----------
const safe = id => id.replace(/[^a-z0-9._-]+/gi, '_');
const wanted = id => !opt.only.length || opt.only.some(t => id.includes(t));
const written = [];
const audit = { base, route: opt.route, when: new Date().toISOString(), console: [], fonts: [], surfaces: {} };

async function ready(page) {
  await page.waitForFunction(() => document.documentElement.hasAttribute('data-gallery-ready') || document.body?.hasAttribute('data-gallery-ready'), null, { timeout: 15000 })
    .catch(() => console.error('warn: data-gallery-ready never set; capturing after network idle'));
  await page.evaluate(() => document.fonts.ready);
  // freeze CSS animations at a deterministic frame unless reduced motion is requested
  if (!opt.reduced) await page.addStyleTag({ content: '*,*::before,*::after{animation-play-state:paused!important;animation-delay:-1.2s!important;caret-color:transparent!important}' });
}

async function auditSurface(page, id) {
  return page.evaluate((sel) => {
    const root = document.querySelector(sel);
    if (!root) return null;
    const find = [];
    const cs = el => getComputedStyle(el);
    for (const el of root.querySelectorAll('*')) {
      const s = cs(el);
      if (s.display === 'none' || s.visibility === 'hidden') continue;
      // text clipped without an ellipsis. `text-overflow` does nothing on a
      // flex or grid container (its text is an anonymous item), so an element
      // that holds text directly and is flex/grid counts as un-ellipsized.
      const ownText = [...el.childNodes].some(n => n.nodeType === 3 && n.textContent.trim());
      const flexish = /flex|grid/.test(s.display);
      if (ownText && el.scrollWidth > el.clientWidth + 1 && s.overflow !== 'visible' && (s.textOverflow !== 'ellipsis' || flexish))
        find.push({ kind: flexish ? 'clip-in-flex' : 'clip', text: el.textContent.trim().slice(0, 60), w: el.clientWidth, need: el.scrollWidth });
      // digits in a proportional face without tabular figures (a mono face is tabular already)
      if (el.childElementCount === 0 && /\d/.test(el.textContent) && !/tabular-nums/.test(s.fontVariantNumeric) && !/mono/i.test(s.fontFamily))
        find.push({ kind: 'proportional-digits', text: el.textContent.trim().slice(0, 40), font: s.fontFamily.split(',')[0] });
    }
    // 4px grid on the components that define rhythm
    // (component roots carry their component name as a class: .Row, .Section, …)
    for (const el of root.querySelectorAll('.Row,.Section,.PanelHeader,.PanelFooter,.PillButton,.Segmented,.Dial,.Toast,.Alert,.Stats,.Chip')) {
      const h = Math.round(el.getBoundingClientRect().height * 100) / 100;
      if (h > 0 && h % 4 !== 0) find.push({ kind: 'off-grid-height', el: (el.getAttribute('class') || '').slice(0, 40), h });
    }
    // anything wider than the surface
    const r = root.getBoundingClientRect();
    for (const el of root.querySelectorAll('*')) {
      const b = el.getBoundingClientRect();
      if (b.width && (b.right > r.right + 1 || b.left < r.left - 1)) { find.push({ kind: 'escapes-surface', el: (el.getAttribute('class') || el.tagName).slice(0, 40), text: el.textContent.trim().slice(0, 40) }); break; }
    }
    return { w: Math.round(r.width), h: Math.round(r.height), findings: find };
  }, `[data-surface="${id}"]`);
}

for (const scale of opt.scales) {
  const ctx = await browser.newContext({
    viewport: { width: opt.viewport[0], height: opt.viewport[1] },
    deviceScaleFactor: scale,
    reducedMotion: opt.reduced ? 'reduce' : 'no-preference',
    colorScheme: 'dark',
  });
  const page = await ctx.newPage();
  page.on('console', m => { if (m.type() === 'error' || m.type() === 'warning') audit.console.push(`${m.type()}: ${m.text()}`); });
  page.on('pageerror', e => audit.console.push(`pageerror: ${e.message}`));
  await page.goto(base + opt.route, { waitUntil: 'networkidle' });
  await ready(page);

  if (scale === opt.scales[0]) {
    audit.fonts = await page.evaluate(() => [...document.fonts].filter(f => f.status === 'loaded').map(f => `${f.family} ${f.weight}`));
    const full = path.join(out, `gallery@${scale}x.png`);
    await page.screenshot({ path: full, fullPage: true }); written.push(full);
  }

  const ids = (await page.$$eval('[data-surface]', els => els.map(e => e.getAttribute('data-surface')))).filter(wanted);
  if (!ids.length) console.error('warn: no [data-surface] elements found on', base + opt.route);
  for (const id of ids) {
    // A live gallery (mock data that keeps changing) may replace a surface's
    // element between lookup and capture: resolve it at capture time, and
    // retry a few times if it is swapped mid-shot.
    const loc = page.locator(`[data-surface="${id}"]`).first();
    const file = path.join(out, `${safe(id)}@${scale}x.png`);
    for (let attempt = 1; ; attempt++) {
      try { await loc.screenshot({ path: file, animations: 'disabled', timeout: 5000 }); break; }
      catch (e) { if (attempt >= 4) throw e; await page.waitForTimeout(150); }
    }
    written.push(file);
    if (opt.audit && scale === opt.scales[0]) audit.surfaces[id] = await auditSurface(page, id);
  }

  if (opt.isolated) {
    for (const id of ids) {
      const p2 = await ctx.newPage();
      await p2.goto(`${base}${opt.route}?surface=${encodeURIComponent(id)}`, { waitUntil: 'networkidle' });
      await ready(p2);
      const loc = (await p2.locator(`[data-surface="${id}"]`).count()) ? p2.locator(`[data-surface="${id}"]`).first() : p2.locator('body');
      const file = path.join(out, `${safe(id)}.isolated@${scale}x.png`);
      await loc.screenshot({ path: file, animations: 'disabled', timeout: 5000 }); written.push(file);
      await p2.close();
    }
  }
  await ctx.close();
}
await browser.close();

for (const f of written) console.log(f);
if (opt.audit) {
  const need = ['Manrope', 'IBM Plex Mono'];
  audit.missingFonts = need.filter(n => !audit.fonts.some(f => f.includes(n)));
  fs.writeFileSync(path.join(out, 'audit.json'), JSON.stringify(audit, null, 2));
  console.log(JSON.stringify({ out, missingFonts: audit.missingFonts, console: audit.console.length,
    findings: Object.fromEntries(Object.entries(audit.surfaces).map(([k, v]) => [k, v ? v.findings.length : 'missing'])) }, null, 2));
}
