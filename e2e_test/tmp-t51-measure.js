// SCRATCH FILE — T-51 baseline measurement only. NOT for commit.
// Measures the CURRENT (unmodified) 「檔案與圖片」 gallery panel on first open,
// against the isolated e2e server seeded by tmp-t51-seed.js.
//   node e2e_test/tmp-t51-measure.js
const fs = require('fs');
const path = require('path');
const { chromium } = require('@playwright/test');

const BASE = process.env.OC_E2E_BASE || 'http://127.0.0.1:8791';
const STATE = path.join(__dirname, '.state');
const PASSWORD = fs.readFileSync(path.join(STATE, 'owner.password'), 'utf8').trim();
const TARGET = JSON.parse(fs.readFileSync(path.join(STATE, 't51-target.json'), 'utf8'));
const VIEWPORT = { width: 1440, height: 900 };
const RUNS = 3;

async function ownerToken() {
  const res = await fetch(`${BASE}/api/login`, {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ password: PASSWORD }),
  });
  return (await res.json()).token;
}

(async () => {
  const token = await ownerToken();
  const browser = await chromium.launch();
  const results = [];
  for (let run = 1; run <= RUNS; run++) {
    const ctx = await browser.newContext({ viewport: VIEWPORT });
    const page = await ctx.newPage();

    let attachmentReqs = 0;
    let attachmentDone = 0;
    let lastReqAt = 0;
    page.on('request', (r) => {
      if (r.url().includes('/api/chat/attachment/')) { attachmentReqs++; lastReqAt = Date.now(); }
    });
    page.on('requestfinished', (r) => {
      if (r.url().includes('/api/chat/attachment/')) attachmentDone++;
    });

    await page.goto(BASE + '/');
    await page.evaluate((t) => localStorage.setItem('oc_token', t), token);
    await page.reload();
    await page.locator('.member-card', { hasText: TARGET.targetName }).first().click();
    await page.locator('.chat__gallery-toggle').waitFor({ state: 'visible' });

    // counters reset so we only count what the PANEL causes
    attachmentReqs = 0;
    attachmentDone = 0;
    const t0 = Date.now();
    lastReqAt = t0;
    await page.locator('.chat__gallery-toggle').click();
    await page.locator('.chat__gallery-item').first().waitFor({ state: 'visible' });
    const tFirstRow = Date.now() - t0;
    const reqsAtFirstRow = attachmentReqs;

    // networkidle never fires here: the cockpit holds an open SSE stream.
    // "Idle" is therefore defined as 2s with no new /api/chat/attachment/
    // request, and reported as the time of the LAST such request.
    while (Date.now() - lastReqAt < 2000) {
      await new Promise((r) => setTimeout(r, 100));
      if (Date.now() - t0 > 120000) break;
    }
    const tIdle = lastReqAt - t0;

    const m = await page.evaluate(() => {
      const panel = document.querySelector('.chat__gallery');
      const senders = document.querySelector('.chat__gallery-senders');
      const list = document.querySelector('.chat__gallery-list');
      const px = (el) => (el ? el.getBoundingClientRect() : null);
      const p = px(panel), s = px(senders), l = px(list);
      return {
        items: document.querySelectorAll('.chat__gallery-item').length,
        thumbs: document.querySelectorAll('img.chat__gallery-thumb').length,
        chips: document.querySelectorAll('.chat__gallery-sender-chip').length,
        senderToggles: document.querySelectorAll('.chat__gallery-sender-toggle').length,
        sendersHeight: s ? Math.round(s.height) : null,
        sendersScrollHeight: senders ? senders.scrollHeight : null,
        panelHeight: p ? Math.round(p.height) : null,
        panelTop: p ? Math.round(p.top) : null,
        listTop: l ? Math.round(l.top) : null,
        listHeight: l ? Math.round(l.height) : null,
        panelBottom: p ? Math.round(p.bottom) : null,
        viewportH: window.innerHeight,
        // is the first row below the viewport fold / below the panel bottom?
        firstRowTop: (() => {
          const r = document.querySelector('.chat__gallery-item');
          return r ? Math.round(r.getBoundingClientRect().top) : null;
        })(),
        domNodesInPanel: panel ? panel.querySelectorAll('*').length : null,
      };
    });

    results.push({
      run,
      viewport: `${VIEWPORT.width}x${VIEWPORT.height}`,
      msToFirstRowVisible: tFirstRow,
      msToLastAttachmentRequest: tIdle,
      attachmentRequestsAtFirstRow: reqsAtFirstRow,
      attachmentRequestsTotal: attachmentReqs,
      attachmentRequestsFinished: attachmentDone,
      ...m,
      listBelowFold: m.listTop !== null ? m.listTop >= m.viewportH : null,
      listStartsBelowPanelBottom: m.listTop !== null && m.panelBottom !== null ? m.listTop >= m.panelBottom : null,
    });
    console.log(JSON.stringify(results[results.length - 1], null, 2));
    await ctx.close();
  }

  // ---- ONE-OFF (task 3): the filter popover OPEN.
  {
    const ctx = await browser.newContext({ viewport: VIEWPORT });
    const page = await ctx.newPage();
    await page.goto(BASE + '/');
    await page.evaluate((t) => localStorage.setItem('oc_token', t), token);
    await page.reload();
    await page.locator('.member-card', { hasText: TARGET.targetName }).first().click();
    await page.locator('.chat__gallery-toggle').click();
    await page.locator('.chat__gallery-item').first().waitFor({ state: 'visible' });
    const before = await page.evaluate(() => {
      const p = document.querySelector('.chat__gallery').getBoundingClientRect();
      const s = document.querySelector('.chat__gallery-senders').getBoundingClientRect();
      const l = document.querySelector('.chat__gallery-list').getBoundingClientRect();
      return { panelHeight: Math.round(p.height), sendersHeight: Math.round(s.height), listHeight: Math.round(l.height), listTop: Math.round(l.top) };
    });
    await page.locator('.chat__gallery-sender-toggle').click();
    await page.locator('.chat__gallery-sender-menu').waitFor({ state: 'visible' });
    const open = await page.evaluate(() => {
      const p = document.querySelector('.chat__gallery').getBoundingClientRect();
      const s = document.querySelector('.chat__gallery-senders').getBoundingClientRect();
      const opts = document.querySelector('.chat__gallery-sender-options');
      const l = document.querySelector('.chat__gallery-list');
      const or = opts.getBoundingClientRect();
      return {
        panelHeight: Math.round(p.height),
        sendersHeight: Math.round(s.height),
        optionsHeight: Math.round(or.height),
        optionsClientHeight: opts.clientHeight,
        optionsScrollHeight: opts.scrollHeight,
        optionsScrolls: opts.scrollHeight > opts.clientHeight,
        optionRows: document.querySelectorAll('.chat__gallery-sender-option').length,
        checkboxes: document.querySelectorAll('.chat__gallery-sender-option input[type=checkbox]').length,
        searchBoxes: document.querySelectorAll('.chat__gallery-sender-search').length,
        listHeight: l ? Math.round(l.getBoundingClientRect().height) : null,
        listTop: l ? Math.round(l.getBoundingClientRect().top) : null,
      };
    });
    const popover = { before, open, panelHeightUnchanged: before.panelHeight === open.panelHeight };
    console.log('POPOVER ' + JSON.stringify(popover, null, 2));
    fs.writeFileSync(path.join(STATE, 't51-popover.json'), JSON.stringify(popover, null, 2));
    await ctx.close();
  }

  await browser.close();
  fs.writeFileSync(path.join(STATE, 't51-measurements.json'), JSON.stringify(results, null, 2));
})();
