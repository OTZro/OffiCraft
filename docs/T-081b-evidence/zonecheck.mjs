// T-081b 分區 token 驗證：用真的瀏覽器載入產品自己的 CSS，量三個區域的計算背景色。
// 兩件事要證明：
//   (1) 不填分區 token 時，三區與頁底完全同色 → 既有主題包外觀零變化
//   (2) 填了分區 token 時，三區各自變色 → 分層真的生效
// 兩種版面都跑：窄版（預設，內容欄 max-width 1040）與寬版（data-layout="wide"）。
import { createRequire } from "node:module";
import { readFileSync } from "node:fs";
import { join } from "node:path";
const require = createRequire(
  "/Users/eva/Desktop/gofreight/OffiCraft-wt-T081b/frontend/package.json"
);
const { chromium } = require("playwright");

const SRC = join("/Users/eva/Desktop/gofreight/OffiCraft-wt-T081b/frontend", "src");
const css = ["styles/theme.css", "styles/global.css", "components/chrome.css"]
  .map((p) => readFileSync(join(SRC, p), "utf8"))
  .join("\n");

const page_html = (overrides) => `
<style>${css}</style>
<style>:root{${overrides}}</style>
<div class="app">
  <div class="topbar"><span class="topbar__org">org</span></div>
  <div class="nav-tabs"><div class="nav-tabs__seg"></div></div>
  <div class="app__main">main</div>
</div>`;

const ZONES = [
  ["body", "body"],
  [".topbar", "頂列"],
  [".nav-tabs", "頁籤列"],
  [".app__main", "內容區"],
];

const WIDTHS = { narrow: [1440, 1040, 900, 720, 480, 375], wide: [1440, 1280, 1040] };

const browser = await chromium.launch();
const ctx = await browser.newContext();
const page = await ctx.newPage();

async function sample(overrides, layout, width) {
  await page.setViewportSize({ width, height: 800 });
  await page.setContent(page_html(overrides));
  await page.evaluate((l) => {
    if (l === "wide") document.documentElement.setAttribute("data-layout", "wide");
    else document.documentElement.removeAttribute("data-layout");
  }, layout);
  const out = {};
  for (const [sel, label] of ZONES) {
    out[label] = await page.$eval(sel, (el) => getComputedStyle(el).backgroundColor);
  }
  // 內容欄實際寬度：用來確認寬版真的解除了上限、窄版真的有 gutter
  out.__mainWidth = await page.$eval(".app__main", (el) => Math.round(el.getBoundingClientRect().width));
  return out;
}

let fail = 0;
console.log("=== (1) 不填分區 token — 應全部同色（既有主題包零變化）===");
for (const layout of ["narrow", "wide"]) {
  for (const w of WIDTHS[layout]) {
    const r = await sample("", layout, w);
    const vals = ZONES.map(([, l]) => r[l]);
    const same = new Set(vals).size === 1;
    if (!same) fail++;
    console.log(
      `  ${layout.padEnd(6)} ${String(w).padStart(4)}px  gutter=${String(w - r.__mainWidth).padStart(3)}px  ` +
        `${same ? "✅ 四層同色" : "❌ 不同色"}  ${vals[0]}`
    );
  }
}

console.log("\n=== (2) 填入分區 token — 應各自分層 ===");
const OV = "--color-topbar-bg:#112233;--color-nav-bg:#445566;--color-main-bg:#778899;";
for (const layout of ["narrow", "wide"]) {
  for (const w of WIDTHS[layout]) {
    const r = await sample(OV, layout, w);
    const distinct = new Set(ZONES.map(([, l]) => r[l])).size;
    const ok = distinct === 4;
    if (!ok) fail++;
    console.log(
      `  ${layout.padEnd(6)} ${String(w).padStart(4)}px  gutter=${String(w - r.__mainWidth).padStart(3)}px  ` +
        `${ok ? "✅ 四層分明" : "❌ 只有 " + distinct + " 色"}  ` +
        `頂列 ${r["頂列"]} / 頁籤 ${r["頁籤列"]} / 內容 ${r["內容區"]}`
    );
  }
}

await browser.close();
console.log(`\n結果：${fail === 0 ? "✅ 全部通過" : `❌ ${fail} 項失敗`}`);
process.exit(fail === 0 ? 0 : 1);
