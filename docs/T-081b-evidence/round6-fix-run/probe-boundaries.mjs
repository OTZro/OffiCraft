// T-081b round 6 追加 1/2 — 實測探針。
//   ① 頂列輸入框(.inline-edit__input,唯一坐在 --color-topbar-bg 上的輸入框)
//      與其所在分區底色之間的「邊界」是否可辨識。
//   ② 登入/首啟頁 .login__hint 的對比。
// 兩者都在「內建深色」與「一份代表性淺色主題(精靈村)」下各量一次。
//
// 起站：cd frontend && npm run dev -- --port 5199
// 用法：node probe-boundaries.mjs
import { createRequire } from "node:module";
import { join } from "node:path";
import { readFileSync } from "node:fs";

const ROOT = "/Users/eva/Desktop/gofreight/OffiCraft-wt-T081b";
const require = createRequire(join(ROOT, "frontend/package.json"));
const { chromium } = require("playwright");
const BASE = process.env.OC_URL || "http://localhost:5199/";

const LIGHT = JSON.parse(
  readFileSync(join(ROOT, "docs/T-081b-evidence/shots-pack/smurf-village.theme.json"), "utf8")
).colors;

// ── colour maths (same formulas the CT visual guards use) ──
const parse = (s) => {
  const m = s.match(/rgba?\(([^)]+)\)/i);
  if (m) {
    const p = m[1].split(/[,/]/).map((x) => parseFloat(x.trim()));
    return { r: p[0], g: p[1], b: p[2], a: p[3] === undefined ? 1 : p[3] };
  }
  const c = s.match(/color\(\s*srgb\s+([^)]+)\)/i);
  if (c) {
    const [chans, alpha] = c[1].split("/").map((x) => x.trim());
    const v = chans.split(/\s+/).map(parseFloat);
    return { r: v[0] * 255, g: v[1] * 255, b: v[2] * 255, a: alpha === undefined ? 1 : parseFloat(alpha) };
  }
  throw new Error("unparseable " + s);
};
const over = (f, b) => ({
  r: f.r * f.a + b.r * (1 - f.a),
  g: f.g * f.a + b.g * (1 - f.a),
  b: f.b * f.a + b.b * (1 - f.a),
  a: 1,
});
const lum = ({ r, g, b }) => {
  const l = (v) => {
    const c = v / 255;
    return c <= 0.03928 ? c / 12.92 : Math.pow((c + 0.055) / 1.055, 2.4);
  };
  return 0.2126 * l(r) + 0.7152 * l(g) + 0.0722 * l(b);
};
const ratio = (f, b) => {
  const [hi, lo] = lum(f) >= lum(b) ? [lum(f), lum(b)] : [lum(b), lum(f)];
  return (hi + 0.05) / (lo + 0.05);
};
const hex = (c) =>
  "#" + [c.r, c.g, c.b].map((v) => Math.round(v).toString(16).padStart(2, "0")).join("");

// Sample an element: own background, its border colour, and the composited
// background of the nearest painted ancestor.
const SAMPLE = (sel) => {
  const el = document.querySelector(sel);
  if (!el) return null;
  const cs = getComputedStyle(el);
  const layers = [];
  let n = el.parentElement;
  while (n) {
    layers.push(getComputedStyle(n).backgroundColor);
    n = n.parentElement;
  }
  return {
    color: cs.color,
    background: cs.backgroundColor,
    borderTopColor: cs.borderTopColor,
    borderTopWidth: cs.borderTopWidth,
    borderTopStyle: cs.borderTopStyle,
    outlineColor: cs.outlineColor,
    outlineWidth: cs.outlineWidth,
    outlineStyle: cs.outlineStyle,
    boxShadow: cs.boxShadow,
    ancestorLayers: layers,
  };
};

function fold(layers) {
  let bg = { r: 25, g: 28, b: 36, a: 1 };
  for (let i = layers.length - 1; i >= 0; i--) {
    const l = parse(layers[i]);
    if (l.a === 0) continue;
    bg = over(l, bg);
  }
  return bg;
}

const browser = await chromium.launch();

async function open({ light, hash }) {
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 900 } });
  await ctx.addInitScript(
    ([lightColors]) => {
      localStorage.setItem("oc.language", "zh");
      localStorage.setItem("oc.theme", "office");
      if (lightColors) {
        window.__applyLight = () => {
          for (const [k, v] of Object.entries(lightColors))
            document.documentElement.style.setProperty(k, v);
        };
      }
    },
    [light ? LIGHT : null]
  );
  const page = await ctx.newPage();
  await page.goto(BASE + (hash || ""), { waitUntil: "networkidle" });
  if (light) await page.evaluate(() => window.__applyLight && window.__applyLight());
  await page.waitForTimeout(400);
  return { ctx, page };
}

const rows = [];

// ── ① topbar input ──
for (const light of [false, true]) {
  const { ctx, page } = await open({ light });
  await page.waitForSelector(".topbar");
  await page.click(".topbar__brand .inline-edit__iconbtn");
  await page.waitForSelector(".topbar .inline-edit__input");
  const s = await page.evaluate(SAMPLE, ".topbar .inline-edit__input");
  const zone = fold(s.ancestorLayers);
  const fill = over(parse(s.background), zone);
  const border = over(parse(s.borderTopColor), fill);
  rows.push({
    probe: "topbar-input",
    theme: light ? "精靈村(淺)" : "內建(深)",
    zone: hex(zone),
    fill: hex(fill),
    fillVsZone: +ratio(fill, zone).toFixed(2),
    border: hex(border),
    borderWidth: s.borderTopWidth,
    borderStyle: s.borderTopStyle,
    borderVsZone: +ratio(border, zone).toFixed(2),
    borderVsFill: +ratio(border, fill).toFixed(2),
  });
  await ctx.close();
}

// ── ② login hint ──
for (const light of [false, true]) {
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 900 } });
  await ctx.addInitScript(
    ([lightColors]) => {
      localStorage.setItem("oc.language", "zh");
      localStorage.setItem("oc.theme", "office");
      window.__applyLight = lightColors
        ? () => {
            for (const [k, v] of Object.entries(lightColors))
              document.documentElement.style.setProperty(k, v);
          }
        : () => {};
    },
    [light ? LIGHT : null]
  );
  const page = await ctx.newPage();
  await page.goto(BASE, { waitUntil: "networkidle" });
  // Render a .login__hint on a .login__card exactly as FirstRunPage does.
  await page.evaluate(() => {
    const wrap = document.createElement("div");
    wrap.className = "login";
    wrap.innerHTML =
      '<div class="login__card"><p class="login__hint" id="probe-hint">提示文字</p></div>';
    document.body.appendChild(wrap);
  });
  await page.evaluate(() => window.__applyLight());
  await page.waitForTimeout(200);
  const s = await page.evaluate(SAMPLE, "#probe-hint");
  const zone = fold(s.ancestorLayers);
  const fg = over(parse(s.color), zone);
  rows.push({
    probe: "login-hint",
    theme: light ? "精靈村(淺)" : "內建(深)",
    card: hex(zone),
    fg: hex(fg),
    contrast: +ratio(fg, zone).toFixed(2),
  });
  await ctx.close();
}

for (const r of rows) console.log(JSON.stringify(r));
await browser.close();
