// T-081b — 抓「主題下拉」真正被點開時的**原生彈出層**。
// 原生 <select> 的彈出層由作業系統(WindowServer)繪製,不在網頁畫面裡,所以
// Playwright 的 page.screenshot() 一定拍不到。改用:headed 瀏覽器 + macOS 的
// screencapture,而且只截**瀏覽器視窗那塊區域**(-R),不截整個桌面 —— 桌面上有
// 使用者的私人內容,不該進到要交給別人看的截圖裡。
//
// 這個 shell 位在 launchd 的 Background session,直接跑 screencapture 會得到
// "could not create image from display";要用 `launchctl asuser <uid>` 丟進 Aqua
// session 才畫得出來。
import { createRequire } from "node:module";
import { execFileSync } from "node:child_process";
import { readFileSync, mkdirSync } from "node:fs";
import { join } from "node:path";

const ROOT = "/Users/eva/Desktop/gofreight/OffiCraft-wt-T081b";
const require = createRequire(join(ROOT, "frontend/package.json"));
const { chromium } = require("playwright");
const OUT = join(ROOT, "docs/T-081b-evidence/shots");
const PACK = join(ROOT, "docs/T-081b-evidence/shots-pack");
mkdirSync(OUT, { recursive: true });
const UID = String(process.getuid());
const cap = (file, region) =>
  execFileSync("launchctl", [
    "asuser", UID, "screencapture", "-x", "-t", "png",
    "-R", region, file,
  ]).toString();

const browser = await chromium.launch({
  headless: false,
  args: ["--window-position=40,80", "--window-size=1200,860"],
});
const ctx = await browser.newContext({ viewport: null });
await ctx.addInitScript(() => {
  localStorage.setItem("oc.language", "zh");
  localStorage.setItem("oc.theme", "office");
  localStorage.setItem("oc.wide", "false");
});
const page = await ctx.newPage();
await page.goto("http://localhost:5199/", { waitUntil: "networkidle" });
await page.waitForSelector(".app__main");
await page.waitForTimeout(1200);

// 匯入最終包,讓下拉裡同時有「內建 → 辦公室」與「自訂 → 精靈村」兩組。不切換主題。
const bundle = JSON.parse(readFileSync(join(PACK, "smurf-village.theme.json"), "utf8"));
await page.evaluate(() => {
  location.hash = "#settings";
});
await page.waitForTimeout(700);
await page.locator(".app__main").getByText("主題", { exact: true }).first().click();
await page.waitForTimeout(600);
await page.getByRole("button", { name: "匯入", exact: true }).first().click();
await page.waitForTimeout(400);
await page.locator("textarea.ts-textarea").fill(JSON.stringify(bundle));
await page.locator(".ts-form-actions button.doc-btn--accent").click();
await page.waitForTimeout(900);
const errs = (await page.locator(".set-error").allTextContents()).filter(Boolean);
if (errs.length) throw new Error("IMPORT REJECTED: " + errs.join(" | "));

await page.evaluate(() => {
  location.hash = "#monitor";
});
await page.waitForTimeout(1000);
await page.locator(".topbar button").last().click();
await page.waitForTimeout(500);
await page.getByText("偏好設定", { exact: true }).click();
await page.waitForTimeout(700);

const win = await page.evaluate(() => ({
  x: window.screenX,
  y: window.screenY,
  w: window.outerWidth,
  h: window.outerHeight,
}));
const region = `${win.x},${win.y},${win.w},${win.h}`;
console.log("browser window region:", region);

// 先截一張「未展開」的對照(同一個 headed 視窗、同一條 screencapture 路徑),
// 用來證明後面那張的差別真的是彈出層,不是別的東西。
cap(join(OUT, "native-popup-before.png"), region);

// 點開下拉。原生彈出層是 modal 的:Playwright 的 click 會一直等到它關掉才 resolve,
// 所以**不能 await** —— 開著它的當下才是我們要截的畫面。
const sel = page.locator("select.profile-dd__input");
const pending = sel.click({ force: true, timeout: 8000 }).catch(() => {});
await new Promise((r) => setTimeout(r, 2500));
cap(join(OUT, "22-theme-select-native-popup.png"), region);

// 關掉彈出層,讓 click 的 promise 收乾淨
await page.keyboard.press("Escape").catch(() => {});
await Promise.race([pending, new Promise((r) => setTimeout(r, 3000))]);

await browser.close();
console.log("done");
