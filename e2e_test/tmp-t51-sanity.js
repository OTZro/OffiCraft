// SCRATCH FILE — T-51 AFTER hand sanity checks. NOT for commit.
const fs = require('fs');
const path = require('path');
const { chromium } = require('@playwright/test');
const BASE = process.env.OC_E2E_BASE || 'http://127.0.0.1:8791';
const STATE = path.join(__dirname, '.state');
const PASSWORD = fs.readFileSync(path.join(STATE, 'owner.password'), 'utf8').trim();
const TARGET = JSON.parse(fs.readFileSync(path.join(STATE, 't51-target.json'), 'utf8'));
const out = [];
const rec = (id, pass, obs) => { out.push({ id, pass, obs }); console.log(`${pass ? 'PASS' : 'FAIL'} ${id} :: ${obs}`); };

(async () => {
  const token = (await (await fetch(BASE + '/api/login', { method: 'POST', headers: { 'content-type': 'application/json' }, body: JSON.stringify({ password: PASSWORD }) })).json()).token;
  const browser = await chromium.launch();
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 } });
  const page = await ctx.newPage();
  await page.goto(BASE + '/');
  await page.evaluate((t) => { localStorage.setItem('oc_token', t); localStorage.setItem('oc.theme', 'office'); }, token);
  await page.reload();
  await page.locator('.member-card', { hasText: TARGET.targetName }).first().click();
  await page.locator('.chat__gallery-toggle').click();
  await page.locator('.chat__gallery-item').first().waitFor({ state: 'visible' });

  const title = () => page.locator('.md-preview__title').innerText();
  const counter = () => page.locator('.md-preview__pager-count').innerText();
  const zoom = () => page.locator('.md-preview__zoom span').innerText();

  // (a) right chevron
  await page.locator('.chat__gallery-item').first().click();
  await page.locator('.md-preview__image').waitFor({ state: 'visible' });
  const t0 = await title(), c0 = await counter();
  await page.locator('.md-preview__pager--next').click();
  await page.waitForTimeout(500);
  const t1 = await title(), c1 = await counter();
  rec('a', t1 !== t0 && c1 !== c0, `title ${t0.split('\n').join(' ')} -> ${t1.split('\n').join(' ')}; counter ${c0} -> ${c1}`);

  // (b) ArrowRight at 100%
  const z = await zoom();
  await page.locator('.md-preview').press('ArrowRight');
  await page.waitForTimeout(500);
  const t2 = await title(), c2 = await counter();
  rec('b', t2 !== t1 && c2 !== c1, `zoom ${z}; counter ${c1} -> ${c2}; title -> ${t2.split('\n').join(' ')}`);

  // (c) zoom past 100% then arrows must NOT page; chevrons still do
  const zin = page.locator('.md-preview__zoom button[aria-label="放大"]');
  await zin.click(); await zin.click();
  await page.waitForTimeout(400);
  const zAfter = await zoom();
  const scrollBefore = await page.evaluate(() => { const w = document.querySelector('.md-preview__image-wrap'); return w ? { l: w.scrollLeft, sw: w.scrollWidth, cw: w.clientWidth } : null; });
  await page.locator('.md-preview').press('ArrowRight');
  await page.locator('.md-preview').press('ArrowLeft');
  await page.waitForTimeout(500);
  const c3 = await counter(), t3 = await title();
  const pannable = await page.evaluate(() => !!document.querySelector('.md-preview__image-wrap--pannable'));
  await page.locator('.md-preview__pager--next').click();
  await page.waitForTimeout(500);
  const c4 = await counter();
  rec('c', c3 === c2 && t3 === t2 && c4 !== c3,
    `zoom ${zAfter}; arrows kept counter at ${c3} (image unchanged); wrap pannable=${pannable} scrollWidth=${scrollBefore && scrollBefore.sw} clientWidth=${scrollBefore && scrollBefore.cw}; chevron then moved ${c3} -> ${c4}`);
  await page.locator('.md-preview__close').click();
  await page.waitForTimeout(300);

  // (d) tick two uploaders
  await page.locator('.chat__gallery-sender-toggle').click();
  await page.locator('.chat__gallery-sender-menu').waitFor({ state: 'visible' });
  const opts = page.locator('.chat__gallery-sender-option');
  const n1 = (await opts.nth(0).innerText()).split('\n');
  const n2 = (await opts.nth(1).innerText()).split('\n');
  await opts.nth(0).locator('input').check();
  await opts.nth(1).locator('input').check();
  await page.waitForTimeout(400);
  const toggleText = await page.locator('.chat__gallery-sender-toggle-text').innerText();
  await page.locator('.chat__gallery-header').click();
  await page.waitForTimeout(400);
  const subs = await page.locator('.chat__gallery-sub').allInnerTexts();
  const names = new Set(subs.map((s) => s.split(' · ')[0]));
  const nameA = n1[0], nameB = n2[0];
  const expectA = parseInt(n1[n1.length - 1], 10), expectB = parseInt(n2[n2.length - 1], 10);
  const rows = await page.locator('.chat__gallery-item').count();
  rec('d', toggleText.includes('已選 2 位') && names.has(nameA) && names.has(nameB) && names.size === 2 && rows === expectA + expectB,
    `toggle reads 「${toggleText}」; rows=${rows} (=${nameA} ${expectA} + ${nameB} ${expectB}); distinct uploaders shown = ${[...names].join(', ')}`);

  // (e) switching to 檔案 re-cuts the options
  await page.locator('.chat__gallery-sender-toggle').click();
  const imgOpts = await page.locator('.chat__gallery-sender-option-name').allInnerTexts();
  const imgCounts = await page.locator('.chat__gallery-sender-option-count').allInnerTexts();
  await page.keyboard.press('Escape');
  await page.locator('.chat__gallery-tab', { hasText: '檔案' }).click();
  await page.waitForTimeout(600);
  const toggleAfterTab = await page.locator('.chat__gallery-sender-toggle-text').innerText();
  await page.locator('.chat__gallery-sender-toggle').click();
  const fileOpts = await page.locator('.chat__gallery-sender-option-name').allInnerTexts();
  const fileCounts = await page.locator('.chat__gallery-sender-option-count').allInnerTexts();
  const same = imgOpts.length === fileOpts.length && imgOpts.every((v, i) => v === fileOpts[i] && imgCounts[i] === fileCounts[i]);
  rec('e', !same, `圖片 tab: ${imgOpts.length} options (first: ${imgOpts.slice(0,3).map((v,i)=>v+'/'+imgCounts[i]).join(', ')}); 檔案 tab: ${fileOpts.length} options (first: ${fileOpts.slice(0,3).map((v,i)=>v+'/'+fileCounts[i]).join(', ')}); toggle reset to 「${toggleAfterTab}」`);

  fs.writeFileSync(path.join(STATE, 't51-sanity.json'), JSON.stringify(out, null, 2));
  await ctx.close(); await browser.close();
})();
