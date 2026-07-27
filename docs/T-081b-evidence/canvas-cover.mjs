// T-081b — "cover" mode (owner rc-f0e23286d75e option 2). Proves the two halves
// of the feature in a real browser:
//   1. cover alone is INVISIBLE behind the chrome zones' opaque colours — the
//      finding that made this a ticket-sized change rather than a new enum value;
//   2. with the SAME theme also giving those zones translucent colours (which the
//      colour grammar already admits), the image shows through under the whole
//      cockpit, stays pinned to the viewport on a long page, and never overflows.
import { createRequire } from "node:module";
import { readFileSync } from "node:fs";
import { join } from "node:path";
const ROOT = "/Users/eva/Desktop/gofreight/OffiCraft-wt-T081b/frontend";
const require = createRequire(join(ROOT, "package.json"));
const { chromium } = require("playwright");
const SRC = join(ROOT, "src");
const css = ["styles/theme.css", "styles/global.css", "components/chrome.css"]
  .map((p) => readFileSync(join(SRC, p), "utf8")).join("\n");
const shell = (inner) =>
  `<style>${css}</style><div id="root"><div class="app"><div class="topbar">t</div><div class="nav-tabs">n</div><div class="app__main">${inner}</div></div></div>`;

const browser = await chromium.launch();
const page = await browser.newPage();
await page.setViewportSize({ width: 1440, height: 600 });
await page.setContent("<body></body>");
// A flat magenta 40x40 — any pixel of the image is unmistakable.
const img = await page.evaluate(() => {
  const c = document.createElement("canvas"); c.width = 40; c.height = 40;
  const g = c.getContext("2d"); g.fillStyle = "#ff00ff"; g.fillRect(0, 0, 40, 40);
  return c.toDataURL("image/png");
});
async function pixels(shot, pts) {
  return page.evaluate(([b64, p]) => new Promise((res) => {
    const im = new Image();
    im.onload = () => { const c = document.createElement("canvas");
      c.width = im.width; c.height = im.height;
      const g = c.getContext("2d"); g.drawImage(im, 0, 0);
      res(p.map(([x, y]) => { const d = g.getImageData(x, y, 1, 1).data;
        return `#${[d[0], d[1], d[2]].map((v) => v.toString(16).padStart(2, "0")).join("")}`; })); };
    im.src = "data:image/png;base64," + b64;
  }), [shot.toString("base64"), pts]);
}
// exactly what i18n/index.tsx writes for mode === "cover"
async function applyCover(zoneAlpha) {
  await page.evaluate(([uri, alpha]) => {
    const de = document.documentElement;
    de.style.setProperty("--canvas-bg-image", `url("${uri}")`);
    de.style.setProperty("--canvas-bg-repeat", "no-repeat");
    de.style.setProperty("--canvas-bg-position", "center center");
    de.style.setProperty("--canvas-bg-size", "cover");
    de.style.setProperty("--canvas-bg-attachment", "fixed");
    // …and what a THEME does on top: translucent zone colours (legal today —
    // the colour grammar admits #RRGGBBAA / rgba()).
    if (alpha !== null) {
      for (const tok of ["--color-topbar-bg", "--color-nav-bg", "--color-main-bg"]) {
        de.style.setProperty(tok, `rgba(25, 28, 36, ${alpha})`);
      }
    }
  }, [img, zoneAlpha]);
}
let bad = 0;
const INK = "#ff00ff";
// 1. opaque zones (a theme that only sets the image)
await page.setContent(shell("m"));
await applyCover(null);
let [gutter, main, topbar, nav] = await pixels(await page.screenshot(), [[2, 300], [720, 300], [720, 20], [720, 80]]);
const hidden = main !== INK && topbar !== INK && nav !== INK && gutter === INK;
if (!hidden) bad++;
console.log(`opaque zones : gutter ${gutter} main ${main} topbar ${topbar} nav ${nav} — ${hidden ? "IMAGE HIDDEN (only the gutter shows it)" : "UNEXPECTED"}`);

// 2. translucent zones (the theme owner asked for)
await page.setContent(shell("m"));
await applyCover(0.35);
[gutter, main, topbar, nav] = await pixels(await page.screenshot(), [[2, 300], [720, 300], [720, 20], [720, 80]]);
const tint = (c) => c !== "#191c24" && c !== INK; // blended, neither pure zone nor pure image
const through = tint(main) && tint(topbar) && tint(nav) && gutter === INK;
if (!through) bad++;
console.log(`translucent  : gutter ${gutter} main ${main} topbar ${topbar} nav ${nav} — ${through ? "IMAGE SHOWS THROUGH ALL THREE ZONES" : "FAIL"}`);

// 3. long page: pinned to the viewport, no horizontal overflow
await page.setContent(shell(`<div style="height:5000px">tall</div>`));
await applyCover(0.35);
const geo = await page.evaluate(() => { const de = document.documentElement;
  window.scrollTo(0, 2000);
  return { over: de.scrollWidth - de.clientWidth, y: window.scrollY, docH: de.scrollHeight }; });
const [scrolledGutter] = await pixels(await page.screenshot(), [[2, 300]]);
const pinned = scrolledGutter === INK && geo.over <= 0;
if (!pinned) bad++;
console.log(`long page    : doc ${geo.docH}px scrolled ${geo.y} gutter ${scrolledGutter} h-overflow ${geo.over} — ${pinned ? "STILL COVERED, NO OVERFLOW" : "FAIL"}`);

// 4. phone width: the zones are full-bleed, so a cover image IS visible there
// (unlike tile/sides, which need a gutter) — the reason owner preferred it.
await page.setViewportSize({ width: 375, height: 600 });
await page.setContent(shell("m"));
await applyCover(0.35);
const [phoneMain] = await pixels(await page.screenshot(), [[187, 300]]);
const phoneOK = tint(phoneMain);
if (!phoneOK) bad++;
console.log(`phone 375px  : content ${phoneMain} — ${phoneOK ? "VISIBLE ON PHONES TOO" : "FAIL"}`);

console.log(bad === 0 ? "ALL OK" : `${bad} FAILING ROW(S)`);
await browser.close();
