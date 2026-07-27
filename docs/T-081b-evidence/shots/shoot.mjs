// T-081b — 版面截圖(寬版/窄版、不同寬度、外區背景圖、主題包)。
// 沿用 canvas-sides.mjs / zonecheck.mjs 那套：從 frontend/package.json 取 playwright。
// 起站：frontend 的 vite dev(預設 mock adapter,AuthGate 在 mock 模式直接進畫面、
// 不需登入 —— 見 frontend/src/AuthGate.tsx)。
//   cd frontend && npm run dev -- --port 5199
//
// 用法： node shoot.mjs <group>    group ∈ builtin | canvas | pack
// 每拍一張就把實測數字寫進 measurements.jsonl。
import { createRequire } from "node:module";
import { appendFileSync, mkdirSync, writeFileSync, readFileSync } from "node:fs";
import { join } from "node:path";

const ROOT = "/Users/eva/Desktop/gofreight/OffiCraft-wt-T081b";
const require = createRequire(join(ROOT, "frontend/package.json"));
const { chromium } = require("playwright");

const OUT = join(ROOT, "docs/T-081b-evidence/shots");
const PACK = join(ROOT, "docs/T-081b-evidence/shots-pack");
const BASE = process.env.OC_URL || "http://localhost:5199/";
mkdirSync(OUT, { recursive: true });

const group = process.argv[2];
const log = (o) =>
  appendFileSync(join(OUT, "measurements.jsonl"), JSON.stringify(o) + "\n");

/** 實測幾何：外區(每側)、內容欄、有無橫向捲動。 */
const GEO = () => {
  const main = document.querySelector(".app__main");
  const de = document.scrollingElement;
  const col = Math.round(main.getBoundingClientRect().width);
  return {
    viewport: window.innerWidth,
    column: col,
    outerPerSide: Math.round((window.innerWidth - col) / 2),
    layout: document.documentElement.dataset.layout ?? "(none)",
    scrollWidth: de.scrollWidth,
    clientWidth: de.clientWidth,
    hScroll: de.scrollWidth > de.clientWidth,
  };
};

const browser = await chromium.launch();

// ── 色彩數學(WCAG)+ 兩種格式的計算色解析器 ─────────────────────────────
const srgb = (c) => (c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4);
const lum = ([r, g, b]) =>
  0.2126 * srgb(r / 255) + 0.7152 * srgb(g / 255) + 0.0722 * srgb(b / 255);
const contrast = (a, b) => {
  const [x, y] = [lum(a), lum(b)].sort((m, n) => n - m);
  return (x + 0.05) / (y + 0.05);
};
// getComputedStyle 對 color-mix() 回傳的是 `color(srgb 0.62 0.63 0.66)`(0–1 浮點),
// 不是 rgb(0–255)。兩種都要吃,否則 0.62 會被當成 0.62/255 → 幾乎全黑、對比度全錯。
const parseRGB = (s) => {
  const n = s.match(/[\d.]+/g).slice(0, 3).map(Number);
  return s.trim().startsWith("color(") ? n.map((v) => v * 255) : n;
};
const hex = ([r, g, b]) =>
  "#" + [r, g, b].map((v) => Math.round(v).toString(16).padStart(2, "0")).join("");

/** 實際「畫出來」的背景色:截這塊區域的圖,取出現次數最多的像素色。
 *  這樣連 alpha 分區底色疊在背景圖上的結果都一起算進去,不是只算 CSS。 */
async function modalPixel(page, clip) {
  const buf = await page.screenshot({ clip });
  const scratch = await browser.newContext({ viewport: { width: 200, height: 200 } });
  const sp = await scratch.newPage();
  await sp.setContent("<body></body>");
  const top = await sp.evaluate(async (uri) => {
    const img = new Image();
    img.src = uri;
    await img.decode();
    const c = document.createElement("canvas");
    c.width = img.width;
    c.height = img.height;
    const g = c.getContext("2d");
    g.drawImage(img, 0, 0);
    const d = g.getImageData(0, 0, c.width, c.height).data;
    const hist = new Map();
    for (let i = 0; i < d.length; i += 4) {
      const k = `${d[i]},${d[i + 1]},${d[i + 2]}`;
      hist.set(k, (hist.get(k) ?? 0) + 1);
    }
    const total = d.length / 4;
    return [...hist.entries()]
      .sort((a, b) => b[1] - a[1])
      .slice(0, 3)
      .map(([k, n]) => ({ rgb: k.split(",").map(Number), share: +(n / total).toFixed(3) }));
  }, "data:image/png;base64," + buf.toString("base64"));
  await scratch.close();
  return top;
}

/** 每個 .ts-group-head:計算色 + 它實際坐的背景像素色 + WCAG 對比度。 */
async function measureHeads(page, label) {
  const heads = await page.evaluate(() => {
    const decl = [...document.querySelectorAll(".ts-group-head")];
    return decl.map((h) => {
      const cs = getComputedStyle(h);
      const r = h.getBoundingClientRect();
      const layers = [];
      for (let el = h; el; el = el.parentElement) {
        const b = getComputedStyle(el).backgroundColor;
        if (b !== "rgba(0, 0, 0, 0)")
          layers.push({ sel: el.tagName.toLowerCase() + "." + (el.className || ""), bg: b });
      }
      return {
        text: h.textContent,
        color: cs.color,
        fontSize: cs.fontSize,
        fontWeight: cs.fontWeight,
        rect: { x: r.x, y: r.y, w: r.width, h: r.height },
        bgLayers: layers.slice(0, 4),
      };
    });
  });
  const out = [];
  for (const h of heads) {
    const px = await modalPixel(page, {
      x: Math.round(h.rect.x),
      y: Math.round(h.rect.y),
      width: Math.max(2, Math.round(h.rect.w)),
      height: Math.max(2, Math.round(h.rect.h)),
    });
    const fg = parseRGB(h.color);
    const bg = px[0].rgb;
    out.push({
      theme: label,
      text: h.text,
      fgComputed: h.color,
      fgHex: hex(fg),
      bgPixel: `rgb(${bg.join(", ")})`,
      bgHex: hex(bg),
      bgPixelShare: px[0].share,
      bgLayersCSS: h.bgLayers,
      fontSize: h.fontSize,
      fontWeight: h.fontWeight,
      contrast: +contrast(fg, bg).toFixed(2),
      passAA_normal: contrast(fg, bg) >= 4.5,
    });
  }
  return out;
}

async function openApp({ width, height, wide, theme = "office" }) {
  const ctx = await browser.newContext({ viewport: { width, height } });
  await ctx.addInitScript(
    ([w, th]) => {
      localStorage.setItem("oc.language", "zh");
      localStorage.setItem("oc.theme", th);
      localStorage.setItem("oc.wide", w ? "true" : "false");
    },
    [wide, theme]
  );
  const page = await ctx.newPage();
  await page.goto(BASE, { waitUntil: "networkidle" });
  await page.waitForSelector(".app__main");
  await page.waitForTimeout(1000);
  return { ctx, page };
}

/** 監控頁 —— 內容最豐富的一頁(帳號 / 機器表格 / AI 會話表格),
 *  寬窄版的內容欄寬度差異在表格上一眼可見。 */
async function gotoMonitor(page) {
  // 走 hash route,不靠頁籤文字 —— 主題包會改寫頁籤用詞(例:小精靈村把「監控」
  // 改成「瞭望台」),用文字點會在套了包之後找不到。
  await page.evaluate(() => {
    location.hash = "#monitor";
  });
  await page.waitForTimeout(1200);
}

async function shot(page, file, note, extra = {}) {
  const geo = await page.evaluate(GEO);
  await page.screenshot({ path: join(OUT, file) });
  const row = { file, note, ...geo, ...extra };
  log(row);
  console.log(JSON.stringify(row));
  return row;
}

/** 走產品自己的匯入 UI(設定 › 主題 › 匯入)。匯入被「拒絕」→ 直接 throw。 */
async function importBundle(page, bundle) {
  await page.evaluate(() => {
    location.hash = "#settings";
  });
  await page.waitForTimeout(600);
  await openThemeSettings(page);
  await page.getByRole("button", { name: "匯入", exact: true }).first().click();
  await page.waitForTimeout(400);
  await page.locator("textarea.ts-textarea").fill(JSON.stringify(bundle));
  await page.locator(".ts-form-actions button.doc-btn--accent").click();
  await page.waitForTimeout(900);
  const errs = (await page.locator(".set-error").allTextContents()).filter(Boolean);
  if (errs.length) throw new Error(`IMPORT REJECTED (${bundle.id}): ${errs.join(" | ")}`);
  const warn = (
    await page.locator('[data-testid="theme-import-skipped"]').allTextContents()
  ).filter(Boolean);
  return warn.join(" | ") || "(無警告)";
}

/** 設定 › 主題。「主題」這個字在 ProfileDropdown 裡也有一個(profile.theme),
 *  而下拉在 topbar、DOM 順序在 .app__main 之前 —— 沒把範圍限在 .app__main 會點到它。 */
async function openThemeSettings(page) {
  await page.evaluate(() => {
    location.hash = "#settings";
  });
  await page.waitForTimeout(600);
  await page.locator(".app__main").getByText("主題", { exact: true }).first().click();
  await page.waitForTimeout(600);
}

/** 在 設定 › 主題 清單點選一個主題(名稱比對)。 */
async function pickTheme(page, name) {
  await openThemeSettings(page);
  await page.locator("button.ts-pick", { hasText: name }).first().click();
  await page.waitForTimeout(700);
}

/** 寬/窄版：走 ProfileDropdown 的版面切換(不 reload —— 自訂主題只活在
 *  mock 的 server settings 記憶體裡,reload 會掉)。 */
async function setWide(page, wide) {
  const cur = await page.evaluate(() => document.documentElement.dataset.layout === "wide");
  if (cur === wide) return;
  await page.locator(".topbar button").last().click();
  await page.waitForTimeout(400);
  await page.getByText("偏好設定", { exact: true }).click();
  await page.waitForTimeout(400);
  await page.getByRole("button", { name: wide ? "寬版" : "窄版", exact: true }).click();
  await page.waitForTimeout(400);
  // 關掉下拉:它蓋在 topbar 上,留著會擋住後續的點擊(也會讓 getByText 命中它自己的
  // 「主題」標籤)。用「點外面」關 —— 下拉本身也在 .topbar 裡,拿 .topbar button 會點到
  // 下拉自己的按鈕、關不掉(App.tsx 的 outside-click 判斷是 ref.contains)。
  for (let i = 0; i < 3 && (await page.locator(".profile-dd").count()); i++) {
    await page.locator(".app__main").click({ position: { x: 4, y: 4 }, force: true });
    await page.waitForTimeout(300);
  }
  if (await page.locator(".profile-dd").count()) {
    throw new Error("profile dropdown stayed open");
  }
}

// ── group builtin：內建「辦公室」主題,寬窄版 / 不同寬度 / 手機寬度 ────────
if (group === "builtin") {
  const plan = [
    ["01-wide-1440-office.png", 1440, 900, true, "寬版 1440 · 內建辦公室主題 · 監控頁"],
    ["02-narrow-1440-office.png", 1440, 900, false, "窄版 1440(預設版面)· 同一畫面對照"],
    ["03-wide-1280-office.png", 1280, 900, true, "寬版 1280"],
    ["04-wide-1040-office.png", 1040, 900, true, "寬版 1040"],
    ["05-narrow-480-office.png", 480, 900, false, "窄版 480(手機寬度)"],
    ["06-narrow-375-office.png", 375, 812, false, "窄版 375(手機寬度)"],
  ];
  for (const [file, w, h, wide, note] of plan) {
    const { ctx, page } = await openApp({ width: w, height: h, wide });
    await gotoMonitor(page);
    await shot(page, file, note);
    await ctx.close();
  }
}

// ── group canvas：外區背景圖(自造小圖,非任何真實素材)────────────────
if (group === "canvas") {
  const boot = await browser.newContext({ viewport: { width: 400, height: 400 } });
  const bp = await boot.newPage();
  await bp.setContent("<body></body>");
  const img = await bp.evaluate(() => {
    const c = document.createElement("canvas");
    c.width = 160;
    c.height = 160;
    const g = c.getContext("2d");
    g.fillStyle = "#12321f";
    g.fillRect(0, 0, 160, 160);
    g.strokeStyle = "#2f7d5b";
    g.lineWidth = 6;
    for (let i = -160; i < 320; i += 40) {
      g.beginPath();
      g.moveTo(i, 0);
      g.lineTo(i + 160, 160);
      g.stroke();
    }
    g.fillStyle = "#6fd6b0";
    g.beginPath();
    g.arc(80, 80, 22, 0, Math.PI * 2);
    g.fill();
    return c.toDataURL("image/png");
  });
  await boot.close();
  console.log("self-made canvas image, data-URI bytes:", img.length);

  const cover = {
    id: "canvas-cover",
    name: "畫布封面測試",
    colors: {
      "--color-bg": "#12321f",
      "--color-topbar-bg": "rgba(15, 22, 30, 0.55)",
      "--color-nav-bg": "rgba(15, 22, 30, 0.55)",
      "--color-main-bg": "rgba(15, 22, 30, 0.74)",
    },
    backgrounds: { canvas: img },
    backgroundModes: { canvas: "cover" },
  };
  const sides = {
    id: "canvas-sides",
    name: "畫布兩側測試",
    colors: { "--color-bg": "#0f161e" },
    backgrounds: { canvas: img },
    backgroundModes: { canvas: "sides" },
  };
  writeFileSync(join(PACK, "canvas-cover.theme.json"), JSON.stringify(cover, null, 2));
  writeFileSync(join(PACK, "canvas-sides.theme.json"), JSON.stringify(sides, null, 2));

  const { ctx, page } = await openApp({ width: 1440, height: 900, wide: false });
  const wCover = await importBundle(page, cover);
  const wSides = await importBundle(page, sides);
  console.log("import warnings:", { cover: wCover, sides: wSides });

  for (const [file, name, warn, w, h, wide, note] of [
    ["07-canvas-bg-wide-1440-cover.png", cover.name, wCover, 1440, 900, true,
      "外區背景圖(自造圖)· cover 鋪法 · 寬版 1440"],
    ["08-canvas-bg-narrow-375-cover.png", cover.name, wCover, 375, 812, false,
      "外區背景圖(自造圖)· cover 鋪法 · 窄版 375(手機)"],
    ["09-canvas-bg-narrow-1440-sides.png", sides.name, wSides, 1440, 900, false,
      "外區背景圖(自造圖)· sides 鋪法 · 窄版 1440(唯一真的有外區可畫的組合)"],
  ]) {
    await page.setViewportSize({ width: w, height: h });
    await pickTheme(page, name);
    await setWide(page, wide);
    await gotoMonitor(page);
    await shot(page, file, note, { importWarning: warn });
  }
  await ctx.close();
}

// ── group pack：小精靈村主題包(本次改動之前做的包)─────────────────
if (group === "pack") {
  const bundle = JSON.parse(
    readFileSync(join(PACK, "smurf-village.theme.json"), "utf8")
  );
  const { ctx, page } = await openApp({ width: 1440, height: 900, wide: false });
  const warn = await importBundle(page, bundle);
  console.log("smurf import warning:", warn);
  writeFileSync(join(OUT, "smurf-import-warning.txt"), warn + "\n");
  await page.screenshot({ path: join(OUT, "10-smurf-import-warning.png") });

  for (const [file, w, h, wide, note] of [
    ["11-smurf-wide-1440.png", 1440, 900, true, "小精靈村主題包 · 寬版 1440"],
    ["12-smurf-narrow-1440.png", 1440, 900, false, "小精靈村主題包 · 窄版 1440"],
    ["13-smurf-narrow-480.png", 480, 900, false, "小精靈村主題包 · 窄版 480"],
    ["14-smurf-narrow-375.png", 375, 812, false, "小精靈村主題包 · 窄版 375"],
  ]) {
    await page.setViewportSize({ width: w, height: h });
    await pickTheme(page, bundle.name);
    await setWide(page, wide);
    await gotoMonitor(page);
    await shot(page, file, note, { importWarning: warn });
  }
  await ctx.close();
}

// ── group pack2：小精靈村「最終包」(完整套用新架構:72 色槽 + 185 用語 +
// backgrounds.canvas 走 cover 整頁鋪滿)。預期匯入零警告 —— 有警告就 throw。
if (group === "pack2") {
  const bundle = JSON.parse(
    readFileSync(join(PACK, "smurf-village.theme.json"), "utf8")
  );
  const { ctx, page } = await openApp({ width: 1440, height: 900, wide: false });
  const warn = await importBundle(page, bundle);
  console.log("final pack import warning:", warn);
  writeFileSync(join(OUT, "smurf-final-import-warning.txt"), warn + "\n");
  await page.screenshot({ path: join(OUT, "smurf-final-import.png") });
  if (warn !== "(無警告)") {
    throw new Error(`EXPECTED ZERO WARNINGS, GOT: ${warn}`);
  }

  for (const [file, w, h, wide, note] of [
    ["15-smurf-final-wide-1440.png", 1440, 900, true, "小精靈村最終包 · 寬版 1440"],
    ["16-smurf-final-narrow-1440.png", 1440, 900, false, "小精靈村最終包 · 窄版 1440"],
    ["17-smurf-final-narrow-480.png", 480, 900, false, "小精靈村最終包 · 窄版 480"],
    ["18-smurf-final-narrow-375.png", 375, 812, false, "小精靈村最終包 · 窄版 375"],
  ]) {
    await page.setViewportSize({ width: w, height: h });
    await pickTheme(page, bundle.name);
    await setWide(page, wide);
    await gotoMonitor(page);
    await shot(page, file, note, { importWarning: warn });
  }
  await ctx.close();
}

// ── group labels：拿掉每列重複標籤之後的樣子(commit 650754c)。
// 匯入小精靈村包讓清單同時有「內建」「自訂」兩列,但**不切換主題** —— 維持內建深色,
// 才跟 smurf-final-import.png 那張舊圖同條件可比。
if (group === "labels") {
  const bundle = JSON.parse(
    readFileSync(join(PACK, "smurf-village.theme.json"), "utf8")
  );
  const { ctx, page } = await openApp({ width: 1440, height: 900, wide: false });
  const warn = await importBundle(page, bundle);
  console.log("import warning:", warn);

  // 19：設定 › 主題 清單。停在匯入後的清單頁,主題仍是內建「辦公室」。
  await shot(
    page,
    "19-theme-list-no-row-tags.png",
    "設定 › 主題 清單(拿掉每列標籤後)· 內建主題 · 窄版 1440",
    { importWarning: warn, activeTheme: await page.evaluate(() => localStorage.getItem("oc.theme")) }
  );

  // 20：主題下拉。它是原生 <select>,原生下拉的彈出層由作業系統畫、不會進頁面截圖,
  // 所以用 size 把同一顆 select 攤成 in-page 清單框 —— DOM 與 <optgroup> 都是產品真的
  // 渲染出來的那一份,只是強制展開。
  await page.locator(".topbar button").last().click();
  await page.waitForTimeout(400);
  await page.getByText("偏好設定", { exact: true }).click();
  await page.waitForTimeout(500);
  const opt = await page.evaluate(() => {
    const sel = document.querySelector("select.profile-dd__input");
    sel.size = sel.querySelectorAll("option").length + sel.querySelectorAll("optgroup").length;
    // .profile-dd__input 給的是固定行高,攤開後會被裁掉,放開高度讓兩組都露出來
    sel.style.height = "auto";
    sel.style.minHeight = "0";
    sel.style.overflow = "visible";
    return [...sel.querySelectorAll("optgroup")].map((g) => ({
      group: g.label,
      options: [...g.querySelectorAll("option")].map((o) => o.textContent),
    }));
  });
  console.log("optgroups:", JSON.stringify(opt, null, 1));
  await page.waitForTimeout(300);
  await shot(page, "20-theme-select-optgroups.png", "主題下拉(展開)· <optgroup> 分成內建/自訂兩組", {
    importWarning: warn,
    optgroups: opt,
  });
  await ctx.close();
}

// ── group prefs：偏好設定面板「正常」的樣子 —— 主題下拉維持未展開的原始外觀,
// 不做任何 size/height 手腳,連同語言、版面等其他偏好項目一起入鏡。
if (group === "prefs") {
  const bundle = JSON.parse(
    readFileSync(join(PACK, "smurf-village.theme.json"), "utf8")
  );
  const { ctx, page } = await openApp({ width: 1440, height: 900, wide: false });
  const warn = await importBundle(page, bundle);
  console.log("import warning:", warn);
  await gotoMonitor(page); // 背後停在一般畫面,不要停在設定頁
  await page.locator(".topbar button").last().click();
  await page.waitForTimeout(400);
  await page.getByText("偏好設定", { exact: true }).click();
  await page.waitForTimeout(600);
  const panel = await page.evaluate(() => {
    const sel = document.querySelector("select.profile-dd__input");
    return {
      selectSize: sel.size,
      selectValue: sel.value,
      options: [...sel.querySelectorAll("optgroup")].map((g) => ({
        group: g.label,
        options: [...g.querySelectorAll("option")].map((o) => o.textContent),
      })),
      sections: [...document.querySelectorAll(".profile-dd__section-label")].map(
        (e) => e.textContent
      ),
    };
  });
  console.log("panel:", JSON.stringify(panel));
  await shot(page, "21-preferences-panel.png", "偏好設定面板(正常狀態,主題下拉未展開)· 窄版 1440", {
    importWarning: warn,
    ...panel,
  });
  await ctx.close();
}

// ── group flat:拿掉 <optgroup> 之後的偏好設定面板(round 7)。條件與第 21 張同:
// 先匯入小精靈村最終包讓清單同時有內建與自訂,**匯入後不切換主題**,窄版 1440。
// 只拍收合態 —— 原生下拉的展開層由作業系統畫,已有第 22 張。
if (group === "flat") {
  const bundle = JSON.parse(
    readFileSync(join(PACK, "smurf-village.theme.json"), "utf8")
  );
  const { ctx, page } = await openApp({ width: 1440, height: 900, wide: false });
  const warn = await importBundle(page, bundle);
  console.log("import warning:", warn);
  await gotoMonitor(page); // 背後停在一般畫面,不要停在設定頁
  await page.locator(".topbar button").last().click();
  await page.waitForTimeout(400);
  await page.getByText("偏好設定", { exact: true }).click();
  await page.waitForTimeout(600);
  const panel = await page.evaluate(() => {
    const sel = document.querySelector("select.profile-dd__input");
    return {
      optgroupCount: sel.querySelectorAll("optgroup").length,
      selectValue: sel.value,
      // 順序就是防線本身:內建必須是第 0 顆。
      optionOrder: [...sel.querySelectorAll("option")].map((o) => ({
        value: o.value,
        text: o.textContent,
      })),
      sections: [...document.querySelectorAll(".profile-dd__section-label")].map(
        (e) => e.textContent
      ),
    };
  });
  console.log("panel:", JSON.stringify(panel, null, 1));
  if (panel.optgroupCount !== 0) throw new Error("optgroup still present");
  if (panel.optionOrder[0].value !== "office") throw new Error("built-in is not first");
  await shot(
    page,
    "23-preferences-panel-flat-select.png",
    "偏好設定面板(收合態)· 主題下拉已改平面清單、內建在前 · 窄版 1440",
    { importWarning: warn, ...panel }
  );
  await ctx.close();
}

// ── group markers2(第八輪):與 markers 同條件重量一次 —— 標題色改吃可覆寫的
// --color-text-muted 之後,內建深色 / 套用精靈村兩組對比度各是多少。截第 27 張,
// 與第 24 張同視窗、同流程、同位置,可直接對照。
//
// ── group markers:套用「精靈村」(淺色自訂包)之後的 設定 › 主題 清單 + 偏好設定面板,
// 外加「內建/自訂」群組標題的實測對比度。標題色吃不可覆寫的 --color-marker-*,
// 那組值是為內建深色主題定的 —— 淺色包底下會不會對比不足,這一組就是去量它。
if (group === "markers" || group === "markers2") {
  const bundle = JSON.parse(
    readFileSync(join(PACK, "smurf-village.theme.json"), "utf8")
  );

  const { ctx, page } = await openApp({ width: 1440, height: 900, wide: false });
  const warn = await importBundle(page, bundle);
  console.log("import warning:", warn);
  if (warn !== "(無警告)") throw new Error(`EXPECTED ZERO WARNINGS, GOT: ${warn}`);

  // 先量「內建深色主題」的對照組(此時還沒切主題)。
  await openThemeSettings(page);
  const darkHeads = await measureHeads(page, "built-in dark (辦公室)");
  console.log("DARK:", JSON.stringify(darkHeads, null, 1));
  log({ file: "(measure only)", note: "群組標題對比度 · 內建深色主題", heads: darkHeads });

  // 還有誰吃 marker 色槽?掃過所有樣式表的每一條規則。
  const markerUsers = await page.evaluate(() => {
    const hits = [];
    for (const ss of document.styleSheets) {
      let rules;
      try {
        rules = ss.cssRules;
      } catch {
        continue;
      }
      const walk = (list) => {
        for (const r of list) {
          if (r.cssRules) walk(r.cssRules);
          if (r.style) {
            for (const p of r.style) {
              const v = r.style.getPropertyValue(p);
              if (v.includes("--color-marker-"))
                hits.push({ selector: r.selectorText, prop: p, value: v.trim() });
            }
          }
        }
      };
      walk(rules);
    }
    return hits;
  });
  console.log("marker slot users:", JSON.stringify(markerUsers, null, 1));
  log({ file: "(measure only)", note: "讀 --color-marker-* 的所有 CSS 規則", markerUsers });

  // 切到精靈村(淺色自訂包)。
  await pickTheme(page, bundle.name);
  const activeTheme = await page.evaluate(() => localStorage.getItem("oc.theme"));
  if (activeTheme !== bundle.id) throw new Error(`theme not applied: ${activeTheme}`);

  await openThemeSettings(page);
  const lightHeads = await measureHeads(page, "精靈村 (custom, light)");
  console.log("LIGHT:", JSON.stringify(lightHeads, null, 1));
  await shot(
    page,
    group === "markers2" ? "27-smurf-applied-theme-list-round8.png" : "24-smurf-applied-theme-list.png",
    "設定 › 主題 清單 · **已套用精靈村** · 窄版 1440(重點:內建/自訂兩個群組標題在淺色底下的樣子)",
    { activeTheme, importWarning: warn, groupHeads: lightHeads }
  );

  if (group === "markers2") {
    await ctx.close();
    await browser.close();
    process.exit(0);
  }

  // 25:偏好設定面板(收合態),背後停在一般畫面。
  await gotoMonitor(page);
  await page.locator(".topbar button").last().click();
  await page.waitForTimeout(400);
  await page.getByText("偏好設定", { exact: true }).click();
  await page.waitForTimeout(600);
  const panel = await page.evaluate(() => {
    const sel = document.querySelector("select.profile-dd__input");
    return {
      optgroupCount: sel.querySelectorAll("optgroup").length,
      selectSize: sel.size,
      selectValue: sel.value,
      optionOrder: [...sel.querySelectorAll("option")].map((o) => ({
        value: o.value,
        text: o.textContent,
      })),
      sections: [...document.querySelectorAll(".profile-dd__section-label")].map(
        (e) => e.textContent
      ),
    };
  });
  console.log("panel:", JSON.stringify(panel));
  if (panel.selectSize !== 0) throw new Error("select was tampered with");
  await shot(
    page,
    "25-smurf-applied-preferences-panel.png",
    "偏好設定面板(收合態:主題/語言/版面)· **已套用精靈村** · 窄版 1440",
    { activeTheme, ...panel }
  );

  // 26:手機 375 的主題清單。
  await page.setViewportSize({ width: 375, height: 812 });
  await page.waitForTimeout(400);
  await openThemeSettings(page);
  const mobileHeads = await measureHeads(page, "精靈村 (custom, light) @375");
  console.log("MOBILE:", JSON.stringify(mobileHeads, null, 1));
  await shot(
    page,
    "26-smurf-applied-theme-list-375.png",
    "設定 › 主題 清單 · **已套用精靈村** · 手機 375",
    { activeTheme, groupHeads: mobileHeads }
  );

  await ctx.close();
}

// ── group remeasure(rebase 到 12b84d1 之後重量)────────────────────────
// CI 蓋不到的那一塊:對比度實算 + 對照截圖。四項:
//   ① .ts-group-head(內建深色 / 套用精靈村)
//   ② 未讀徽章(白字對徽章底;徽章底對頁底 / 卡片 / active 分頁)
//   ③ .login__hint(內建深色 / 精靈村)
//   ④ 手機 375 無橫向捲動
// 截圖 28(主題清單,已套精靈村)與 29(偏好設定面板,已套精靈村),窄版 1440。
if (group === "remeasure") {
  const bundle = JSON.parse(
    readFileSync(join(PACK, "smurf-village.theme.json"), "utf8")
  );

  // alpha 版解析 + 疊色(半透明前景/卡片底要先壓在底下那層上才算得準)。
  const parseA = (s) => {
    const n = s.match(/[\d.]+/g).map(Number);
    const scale = s.trim().startsWith("color(") ? 255 : 1;
    return { rgb: n.slice(0, 3).map((v) => v * scale), a: n.length > 3 ? n[3] : 1 };
  };
  const over = (f, b) => f.rgb.map((v, i) => v * f.a + b[i] * (1 - f.a));
  const flat = (s, base) => {
    const p = parseA(s);
    return p.a === 1 ? p.rgb : over(p, base);
  };

  /** 未讀徽章:白字 vs 徽章底,以及徽章底 vs 三種它真的會坐上去的底色。 */
  async function measureBadge(page, label) {
    const raw = await page.evaluate(() => {
      const host = document.createElement("div");
      host.id = "probe-badges";
      host.style.cssText = "position:fixed;left:0;top:0;z-index:99999;opacity:0";
      host.innerHTML = `
        <div id="p-page" style="background:var(--color-bg);padding:8px">
          <span class="nav-tab__badge" id="b-page">7</span></div>
        <div id="p-card" class="member-card" style="background:var(--color-card);padding:8px">
          <span class="member-card__unread" id="b-card">7</span></div>
        <div id="p-tab" class="nav-tab nav-tab--active" style="padding:8px">
          <span class="nav-tab__badge" id="b-tab">7</span></div>`;
      document.body.appendChild(host);
      const cs = (id) => getComputedStyle(document.getElementById(id));
      const b = cs("b-page");
      const out = {
        fill: b.backgroundColor,
        text: b.color,
        outlineColor: b.outlineColor,
        outlineWidth: b.outlineWidth,
        pageBg: cs("p-page").backgroundColor,
        cardBg: cs("p-card").backgroundColor,
        activeTabBg: cs("p-tab").backgroundColor,
      };
      host.remove();
      return out;
    });
    const base = [25, 28, 36];
    const fill = flat(raw.fill, base);
    const text = flat(raw.text, fill);
    const ring = flat(raw.outlineColor, base);
    const bgs = {
      "頁底 --color-bg": flat(raw.pageBg, base),
      "卡片 --color-card": flat(raw.cardBg, base),
      "active 分頁 --color-indigo": flat(raw.activeTabBg, base),
    };
    return {
      theme: label,
      fillHex: hex(fill),
      textHex: hex(text),
      ringHex: hex(ring),
      outlineWidth: raw.outlineWidth,
      textOnFill: +contrast(text, fill).toFixed(2),
      fillVs: Object.fromEntries(
        Object.entries(bgs).map(([k, v]) => [k, +contrast(fill, v).toFixed(2)])
      ),
      ringVs: Object.fromEntries(
        Object.entries(bgs).map(([k, v]) => [k, +contrast(ring, v).toFixed(2)])
      ),
      bgHex: Object.fromEntries(Object.entries(bgs).map(([k, v]) => [k, hex(v)])),
    };
  }

  /** 登入/首啟頁提示文字:與 FirstRunPage 相同的結構,量它在卡片底上的對比。 */
  async function measureLoginHint(page, label) {
    const raw = await page.evaluate(() => {
      const wrap = document.createElement("div");
      wrap.className = "login";
      wrap.style.cssText = "position:fixed;left:0;top:0;z-index:99999;opacity:0";
      wrap.innerHTML =
        '<div class="login__card"><p class="login__hint" id="probe-hint">提示文字</p></div>';
      document.body.appendChild(wrap);
      const hint = document.getElementById("probe-hint");
      const card = hint.parentElement;
      const out = {
        color: getComputedStyle(hint).color,
        cardBg: getComputedStyle(card).backgroundColor,
        pageBg: getComputedStyle(document.body).backgroundColor,
      };
      wrap.remove();
      return out;
    });
    const base = [25, 28, 36];
    const page0 = flat(raw.pageBg, base);
    const card = flat(raw.cardBg, page0);
    const fg = flat(raw.color, card);
    return {
      theme: label,
      cardHex: hex(card),
      fgComputed: raw.color,
      fgHex: hex(fg),
      contrast: +contrast(fg, card).toFixed(2),
      passAA_normal: contrast(fg, card) >= 4.5,
    };
  }

  const { ctx, page } = await openApp({ width: 1440, height: 900, wide: false });
  const warn = await importBundle(page, bundle);
  console.log("import warning:", warn);
  if (warn !== "(無警告)") throw new Error(`EXPECTED ZERO WARNINGS, GOT: ${warn}`);

  // ① 內建深色主題(還沒切主題)。
  await openThemeSettings(page);
  const darkHeads = await measureHeads(page, "built-in dark (辦公室)");
  console.log("DARK HEADS:", JSON.stringify(darkHeads, null, 1));
  log({ file: "(measure only)", note: "REBASE 重量 · 群組標題對比度 · 內建深色主題", heads: darkHeads });

  // ② + ③ 內建深色下的徽章與 login hint。
  const darkBadge = await measureBadge(page, "built-in dark (辦公室)");
  const darkHint = await measureLoginHint(page, "built-in dark (辦公室)");
  console.log("DARK BADGE:", JSON.stringify(darkBadge, null, 1));
  console.log("DARK HINT:", JSON.stringify(darkHint, null, 1));
  log({ file: "(measure only)", note: "REBASE 重量 · 未讀徽章 · 內建深色主題", badge: darkBadge });
  log({ file: "(measure only)", note: "REBASE 重量 · login hint · 內建深色主題", hint: darkHint });

  // 切到精靈村(淺色自訂包)。
  await pickTheme(page, bundle.name);
  const activeTheme = await page.evaluate(() => localStorage.getItem("oc.theme"));
  if (activeTheme !== bundle.id) throw new Error(`theme not applied: ${activeTheme}`);

  await openThemeSettings(page);
  const lightHeads = await measureHeads(page, "精靈村 (custom, light)");
  console.log("LIGHT HEADS:", JSON.stringify(lightHeads, null, 1));
  const lightBadge = await measureBadge(page, "精靈村 (custom, light)");
  const lightHint = await measureLoginHint(page, "精靈村 (custom, light)");
  console.log("LIGHT BADGE:", JSON.stringify(lightBadge, null, 1));
  console.log("LIGHT HINT:", JSON.stringify(lightHint, null, 1));
  log({ file: "(measure only)", note: "REBASE 重量 · 未讀徽章 · 精靈村", badge: lightBadge });
  log({ file: "(measure only)", note: "REBASE 重量 · login hint · 精靈村", hint: lightHint });

  // 28:主題清單(與第 27 張同角度)。
  await shot(
    page,
    "28-smurf-applied-theme-list-rebase.png",
    "設定 › 主題 清單 · **已套用精靈村** · 窄版 1440(rebase 到 12b84d1 後重拍,與第 27 張同角度)",
    { activeTheme, importWarning: warn, groupHeads: lightHeads }
  );

  // 29:偏好設定面板(收合態),背後停在一般畫面。
  await gotoMonitor(page);
  await page.locator(".topbar button").last().click();
  await page.waitForTimeout(400);
  await page.getByText("偏好設定", { exact: true }).click();
  await page.waitForTimeout(600);
  const panel = await page.evaluate(() => {
    const sel = document.querySelector("select.profile-dd__input");
    return {
      optgroupCount: sel.querySelectorAll("optgroup").length,
      selectSize: sel.size,
      selectValue: sel.value,
      optionOrder: [...sel.querySelectorAll("option")].map((o) => ({
        value: o.value,
        text: o.textContent,
      })),
      sections: [...document.querySelectorAll(".profile-dd__section-label")].map(
        (e) => e.textContent
      ),
    };
  });
  console.log("panel:", JSON.stringify(panel));
  if (panel.selectSize !== 0) throw new Error("select was tampered with");
  await shot(
    page,
    "29-smurf-applied-preferences-panel-rebase.png",
    "偏好設定面板(收合態:主題/語言/版面)· **已套用精靈村** · 窄版 1440(rebase 到 12b84d1 後重拍)",
    { activeTheme, ...panel }
  );

  // ④ 手機 375:有沒有橫向捲動。
  await page.setViewportSize({ width: 375, height: 812 });
  await page.waitForTimeout(400);
  await page.keyboard.press("Escape");
  await gotoMonitor(page);
  const geo375 = await page.evaluate(GEO);
  console.log("GEO375:", JSON.stringify(geo375));
  log({ file: "(measure only)", note: "REBASE 重量 · 手機 375 橫向捲動檢查(已套精靈村)", ...geo375 });

  await ctx.close();
}

await browser.close();
