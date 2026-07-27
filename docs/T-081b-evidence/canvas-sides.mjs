// T-081b — outer-canvas background image, "sides" display mode (owner 2026-07-27).
// Renders the real chrome CSS, applies the four vars EXACTLY as i18n/index.tsx
// does for mode="sides", then screenshots and reads back real pixels:
//   * ONE copy of the image against each viewport edge — no repeat in EITHER
//     axis (owner: "為什麼還會有重複的樹" — a second copy is the defect),
//   * pinned to the VIEWPORT, so scrolling a long page cannot bring another in,
//   * the content column is never leaked into, and no width gains h-scroll.
import { createRequire } from "node:module";
import { readFileSync } from "node:fs";
import { join } from "node:path";

const ROOT = "/Users/eva/Desktop/gofreight/OffiCraft-wt-T081b/frontend";
const require = createRequire(join(ROOT, "package.json"));
const { chromium } = require("playwright");

const SRC = join(ROOT, "src");
const css = ["styles/theme.css", "styles/global.css", "components/chrome.css"]
  .map((p) => readFileSync(join(SRC, p), "utf8"))
  .join("\n");
const shell = (inner) =>
  `<style>${css}</style><div id="root"><div class="app"><div class="topbar">t</div><div class="nav-tabs">n</div><div class="app__main">${inner}</div></div></div>`;
const html = shell("m");
// The worst case for "does the canvas scroll": a pane that does not self-scroll.
const tallHtml = shell(`<div style="height:5000px">tall</div>`);

const IMG_W = 20;
const IMG_H = 8;
const VH = 600;
const browser = await chromium.launch();
const page = await browser.newPage();

// 20x8 quadrant checker — both axes differ, so a repeat in either shows up.
await page.setContent("<body></body>");
const img = await page.evaluate(
  ([w, h]) => {
    const c = document.createElement("canvas");
    c.width = w;
    c.height = h;
    const g = c.getContext("2d");
    g.fillStyle = "#ff00ff";
    g.fillRect(0, 0, w, h);
    g.fillStyle = "#00ff00";
    g.fillRect(0, 0, w / 2, h / 2);
    g.fillRect(w / 2, h / 2, w / 2, h / 2);
    return c.toDataURL("image/png");
  },
  [IMG_W, IMG_H]
);

async function pixels(shot, points) {
  return page.evaluate(
    ([b64, pts]) =>
      new Promise((res) => {
        const im = new Image();
        im.onload = () => {
          const c = document.createElement("canvas");
          c.width = im.width;
          c.height = im.height;
          const g = c.getContext("2d");
          g.drawImage(im, 0, 0);
          res(
            pts.map(([x, y]) => {
              const d = g.getImageData(x, y, 1, 1).data;
              return `#${[d[0], d[1], d[2]]
                .map((v) => v.toString(16).padStart(2, "0"))
                .join("")}`;
            })
          );
        };
        im.src = "data:image/png;base64," + b64;
      }),
    [shot.toString("base64"), points]
  );
}

// byte-for-byte what i18n/index.tsx writes for mode === "sides"
async function applySides(layout) {
  await page.evaluate(
    ([lay, uri]) => {
      const de = document.documentElement;
      if (lay === "wide") de.setAttribute("data-layout", "wide");
      else de.removeAttribute("data-layout");
      const url = `url("${uri}")`;
      de.style.setProperty("--canvas-bg-image", `${url}, ${url}`);
      de.style.setProperty("--canvas-bg-repeat", "no-repeat, no-repeat");
      de.style.setProperty("--canvas-bg-position", "left bottom, right bottom");
      de.style.setProperty("--canvas-bg-attachment", "fixed, fixed");
    },
    [layout, img]
  );
}

const INK = new Set(["#ff00ff", "#00ff00"]);
const BG = "#191c24";
let bad = 0;

for (const [layout, widths] of [
  ["narrow", [1920, 1440, 1200, 1040, 900, 720, 480, 375]],
  ["wide", [1920, 1440, 1280, 1100, 1040, 720, 375]],
]) {
  for (const w of widths) {
    await page.setViewportSize({ width: w, height: VH });
    await page.setContent(html);
    await applySides(layout);
    const r = await page.evaluate(() => {
      const el = document.querySelector(".app__main");
      const de = document.documentElement;
      const cs = getComputedStyle(document.body);
      return {
        col: Math.round(el.getBoundingClientRect().width),
        scrollOver: de.scrollWidth - de.clientWidth,
        repeat: cs.backgroundRepeat,
        attachment: cs.backgroundAttachment,
        layers: cs.backgroundImage.split(/\),\s*url/).length,
      };
    });
    const shot = await page.screenshot();
    const gutter = Math.round((w - r.col) / 2);
    // bottom-left ink / bottom-right ink / one row ABOVE the image (must be bare
    // — proves no vertical repeat) / past the image horizontally, both sides /
    // middle of the content column
    const [lBot, rBot, above, lPast, rPast, mid] = await pixels(shot, [
      [2, VH - 3],
      [w - 3, VH - 3],
      [2, VH - IMG_H - 4],
      [Math.min(IMG_W + 5, w - 1), VH - 3],
      [Math.max(w - IMG_W - 6, 0), VH - 3],
      [Math.round(w / 2), VH - 3],
    ]);

    const painted = gutter > 0;
    const okEdges = painted
      ? INK.has(lBot) && INK.has(rBot)
      : !INK.has(lBot) && !INK.has(rBot);
    // ONE copy only: nothing above it vertically…
    const okNoVRepeat = !painted || above === BG;
    // …and nothing beside it horizontally (where the gutter is wide enough).
    const roomL = gutter > IMG_W + 5;
    const okNoHRepeat = !roomL || (lPast === BG && rPast === BG);
    const okMid = !INK.has(mid);
    const okScroll = r.scrollOver <= 0;
    const okLayers = r.layers === 2;
    const ok =
      okEdges && okNoVRepeat && okNoHRepeat && okMid && okScroll && okLayers;
    if (!ok) bad++;
    console.log(
      `${layout.padEnd(6)} ${String(w).padStart(4)}px  col ${String(r.col).padStart(4)}  ` +
        `gutter ${String(gutter).padStart(3)}px/side  h-scroll ${okScroll ? "none" : `OVERFLOW ${r.scrollOver}px`}  ` +
        `bottom L${lBot} R${rBot} ${painted ? (okEdges ? "PINNED" : "BAD") : "(no gutter — invisible, expected)"}  ` +
        `above ${above} ${painted ? (okNoVRepeat ? "SINGLE" : "V-REPEATED!") : "-"}  ` +
        `past-image L${lPast} R${rPast} ${roomL ? (okNoHRepeat ? "NO-H-REPEAT" : "H-REPEATED!") : "(gutter too narrow to tell)"}  ` +
        `column ${mid} ${okMid ? "(opaque)" : "LEAKED"}  ${ok ? "" : "<<< FAIL"}`
    );
  }
}

// The case a tall image cannot solve: a page that actually scrolls. Pinned to
// the viewport, the art must stay at the bottom edge and stay single.
await page.setViewportSize({ width: 1440, height: VH });
await page.setContent(tallHtml);
await applySides("narrow");
const scrolled = await page.evaluate(() => {
  const de = document.documentElement;
  const scrolls = de.scrollHeight > de.clientHeight;
  window.scrollTo(0, 2000);
  return { scrolls, y: window.scrollY, docH: de.scrollHeight };
});
const longShot = await page.screenshot();
const [sBot, sMidGutter] = await pixels(longShot, [
  [2, VH - 3],
  [2, Math.round(VH / 2)],
]);
const longOK = scrolled.scrolls && INK.has(sBot) && sMidGutter === BG;
if (!longOK) bad++;
console.log(
  `long page (doc ${scrolled.docH}px, scrolled to ${scrolled.y}): bottom ${sBot} mid-gutter ${sMidGutter} — ` +
    (longOK ? "STILL PINNED + SINGLE" : "FAIL")
);

// Control 1: mode="tile" (the default vars) must be byte-identical to the
// pre-existing behaviour — repeat in both axes, scrolling with the document.
await page.setViewportSize({ width: 1440, height: VH });
await page.setContent(html);
await page.evaluate((uri) => {
  const de = document.documentElement;
  de.style.setProperty("--canvas-bg-image", `url("${uri}")`);
  de.style.setProperty("--canvas-bg-repeat", "repeat");
  de.style.setProperty("--canvas-bg-position", "0 0");
  de.style.setProperty("--canvas-bg-attachment", "scroll");
}, img);
const tileShot = await page.screenshot();
const [t1, tPast, tAbove] = await pixels(tileShot, [
  [2, 300],
  [IMG_W + 5, 300],
  [2, 100],
]);
const tileOK = INK.has(t1) && INK.has(tPast) && INK.has(tAbove);
if (!tileOK) bad++;
console.log(
  `control tile  1440px: ${t1}/${tPast}/${tAbove} — ${tileOK ? "REPEATS BOTH AXES (unchanged)" : "FAIL"}`
);

// Control 2: no image at all → plain colour, i.e. a theme that names no
// background is untouched by any of this.
await page.setContent(html);
const plain = await page.screenshot();
const [p1] = await pixels(plain, [[2, 300]]);
if (p1 !== BG) bad++;
console.log(`control none  1440px: gutter px ${p1} — expected ${BG} ${p1 === BG ? "OK" : "FAIL"}`);

console.log(bad === 0 ? "ALL OK" : `${bad} FAILING ROW(S)`);
await browser.close();
