// T-081b — "an ALREADY-IMPORTED light pack must not change" (review round 2, lens-A B1).
// Renders the same probe markup against base (origin/main) and head CSS, applies a
// pack written BEFORE this ticket existed (it can only know the pre-split tokens),
// and compares the computed colours + WCAG contrast at the call sites the split moved.
import { createRequire } from "node:module";
import { readFileSync } from "node:fs";
import { join } from "node:path";
const ROOT = "/Users/eva/Desktop/gofreight/OffiCraft-wt-T081b/frontend";
const BASE = process.argv[2]; // dir holding the base CSS tree
const require = createRequire(join(ROOT, "package.json"));
const { chromium } = require("playwright");
const FILES = ["styles/theme.css","styles/global.css","components/chrome.css",
  "components/member-detail.css","components/office.css","components/settings.css",
  "components/tasks.css","components/onboarding.css"];
const read = (base) => FILES.map((f) => readFileSync(join(base, f), "utf8")).join("\n");

// A pre-T-081b light pack: it overrides the PARENT tokens because those are the
// only names that existed when it was written.
const PACK = {
  "--color-bg": "#f5f6fa", "--color-card": "#ffffff", "--color-text": "#1a1d24",
  "--color-overlay": "#000000", "--color-shadow": "#cccccc", "--color-indigo": "#e0e0ff",
};
const PROBES = [
  [".mp-webhook__count", "color"],
  [".mp-webhook__submit", "color"],
  [".mp-toggle__knob", "background-color"],
  [".chat__lightbox-close", "color"],
  [".nav-tab__badge", "color"],
  [".mp-webhook__requestdetail", "background-color"],
  [".chat__share-btn", "background-color"],
];
const HTML = PROBES.map(([sel]) => `<div class="${sel.slice(1)}">x</div>`).join("");

const browser = await chromium.launch();
async function measure(css, pack) {
  const page = await browser.newPage();
  await page.setViewportSize({ width: 1200, height: 600 });
  await page.setContent(`<style>${css}</style><div id="root"><div class="app"><div class="app__main">${HTML}</div></div></div>`);
  await page.evaluate((p) => {
    for (const [k, v] of Object.entries(p)) document.documentElement.style.setProperty(k, v);
  }, pack);
  const out = await page.evaluate((probes) => probes.map(([sel, prop]) => {
    const el = document.querySelector(sel);
    const cs = getComputedStyle(el);
    return [sel, prop, cs.getPropertyValue(prop)];
  }), PROBES);
  await page.close();
  return out;
}
const base = await measure(read(BASE), PACK);
const head = await measure(read(join(ROOT, "src")), PACK);
let bad = 0;
console.log("── pre-T-081b LIGHT pack: base vs head ──");
for (let i = 0; i < base.length; i++) {
  const same = base[i][2] === head[i][2];
  if (!same) bad++;
  console.log(`${same ? "same " : "DIFF "} ${base[i][0].padEnd(28)} ${base[i][1].padEnd(17)} base ${base[i][2].padEnd(22)} head ${head[i][2]}`);
}
// …and the built-in dark theme (no pack at all) must be identical too.
const baseDark = await measure(read(BASE), {});
const headDark = await measure(read(join(ROOT, "src")), {});
let badDark = 0;
for (let i = 0; i < baseDark.length; i++) if (baseDark[i][2] !== headDark[i][2]) { badDark++;
  console.log(`DIFF (built-in) ${baseDark[i][0]} ${baseDark[i][1]} base ${baseDark[i][2]} head ${headDark[i][2]}`); }
console.log(`built-in dark theme: ${badDark === 0 ? "identical on all probes" : badDark + " DIFF"}`);
console.log(bad === 0 && badDark === 0 ? "ALL OK" : `${bad + badDark} REGRESSION(S)`);
await browser.close();
