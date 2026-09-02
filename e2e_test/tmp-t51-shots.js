// SCRATCH FILE — T-51 AFTER screenshots only. NOT for commit.
//   node e2e_test/tmp-t51-shots.js
// Default (dark/office) theme only — the owner ruled light theme out of scope.
const fs = require('fs');
const path = require('path');
const { chromium } = require('@playwright/test');

const BASE = process.env.OC_E2E_BASE || 'http://127.0.0.1:8791';
const STATE = path.join(__dirname, '.state');
const OUT = '/Users/eva/.officraft/agents/ow-1f2cc271ab1c/work/shots/after';
const PASSWORD = fs.readFileSync(path.join(STATE, 'owner.password'), 'utf8').trim();
const TARGET = JSON.parse(fs.readFileSync(path.join(STATE, 't51-target.json'), 'utf8'));
const VPS = { '1440': { width: 1440, height: 900 }, '390': { width: 390, height: 844 } };

async function api(method, url, token, body) {
  const res = await fetch(BASE + url, {
    method,
    headers: { 'content-type': 'application/json', ...(token ? { Authorization: `Bearer ${token}` } : {}) },
    ...(body ? { body: JSON.stringify(body) } : {}),
  });
  const txt = await res.text();
  if (!res.ok) throw new Error(`${method} ${url} -> ${res.status} ${txt}`);
  return txt ? JSON.parse(txt) : null;
}

async function openGallery(page, token) {
  await page.goto(BASE + '/');
  await page.evaluate((t) => {
    localStorage.setItem('oc_token', t);
    localStorage.setItem('oc.theme', 'office');
  }, token);
  await page.reload();
  await page.locator('.member-card', { hasText: TARGET.targetName }).first().click();
  await page.locator('.chat__gallery-toggle').click();
  await page.locator('.chat__gallery-item').first().waitFor({ state: 'attached' });
  await page.waitForTimeout(1500);
}

(async () => {
  fs.mkdirSync(OUT, { recursive: true });
  const { token } = await api('POST', '/api/login', null, { password: PASSWORD });
  const browser = await chromium.launch();

  for (const [label, vp] of Object.entries(VPS)) {
    const ctx = await browser.newContext({ viewport: vp });
    const page = await ctx.newPage();
    await openGallery(page, token);
    let f = path.join(OUT, `gallery-${label}.png`);
    await page.screenshot({ path: f });
    console.log('wrote', f);
    await page.locator('.chat__gallery-sender-toggle').click();
    await page.locator('.chat__gallery-sender-menu').waitFor({ state: 'visible' });
    await page.waitForTimeout(500);
    f = path.join(OUT, `filter-open-${label}.png`);
    await page.screenshot({ path: f });
    console.log('wrote', f);
    await ctx.close();
  }

  // Preview overlay on an image, 1440 only.
  const ctx = await browser.newContext({ viewport: VPS['1440'] });
  const page = await ctx.newPage();
  await openGallery(page, token);
  await page.locator('.chat__gallery-item').first().click();
  await page.locator('.md-preview__panel').waitFor({ state: 'visible' });
  await page.locator('.md-preview__image').waitFor({ state: 'visible' });
  await page.waitForTimeout(1200);
  let f = path.join(OUT, 'preview-1440.png');
  await page.screenshot({ path: f });
  console.log('wrote', f, '| counter =', await page.locator('.md-preview__pager-count').innerText());

  const zoomIn = page.locator('.md-preview__zoom button[aria-label="放大"]');
  await zoomIn.click();
  await zoomIn.click();
  await page.waitForTimeout(600);
  const zoomText = await page.locator('.md-preview__zoom span').innerText();
  f = path.join(OUT, 'preview-zoomed-1440.png');
  await page.screenshot({ path: f });
  console.log('wrote', f, '| zoom =', zoomText);
  await ctx.close();
  await browser.close();
})();
