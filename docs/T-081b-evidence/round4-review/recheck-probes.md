# round4 複驗 — 實測紀錄

版本快照 `dfd611a02fa1c317`。所有守衛實驗都在 `frontend/src` 的 **temp 複本**上進行
(`TOKEN_ROLES_SRC` 指過去),真實工作區未被寫入;其餘 sabotage 皆以 `cp` 備份/還原。

---

## A — 設定頁偽造:再攻一次

一次性 probe(`frontend/src/lib/__rc.test.ts`,跑完即刪):

```
V1 wording 覆寫 themeMarkers.builtinGroup/customGroup 對調
   validate: null   AFTER: {"zh":{}}            ← 覆寫被 DROP(上一輪是原封保留)

V2 colors 指定 --color-marker-custom / --color-marker-builtin
   validate: "theme: \"--color-marker-custom\" is not a theme colour token (see theme.css)"

V3 名字(先前可用來仿冒的形狀)
   "辦公室　"(U+3000)  -> REJECT: name must not contain control, formatting, private-use,
                                  surrogate, separator or non-ASCII space characters
   "辦公室 "(U+00A0)   -> REJECT: 同上
   "辦公室"+ZWSP       -> REJECT: 同上
   "辦公室"+U+E0041    -> REJECT: 同上

V3b 已知取捨(Mn 類別仍接受)
   "辦公室"+U+FE0F     -> null(ACCEPT)         ← 渲染成「辦公室」
   "辦公室"+U+0301     -> null(ACCEPT)
   "辦公室"(純)        -> REJECT: name "辦公室" is reserved for a built-in theme

V4 --color-text-muted(分組標題的顏色)
   validate: null(ACCEPT)                       ← 主題可覆寫
```

白名單清點(出現次數):

```
themeMarkers      : messageKeys.generated.ts=0  message_keys_gen.go=0
themeBuiltinTag   : 0 / 0        themeCustomTag : 0 / 0
--color-marker-*  : themeTokens.generated.ts=0  theme_colornames_gen.go=0
                    (theme.css 中定義 5 處 → 全數被產生器排除)
```

結論:文字、顏色、名字三條路全斷。V3b/V4 是殘餘缺口,見結論檔 §1(判 NIT)。

---

## C — 守衛:舊繞法複驗 + 新繞法

`node scripts/check-token-roles.mjs`(TOKEN_ROLES_SRC 指 temp 樹)。baseline exit=0 / 4.52:1。

### 上一輪的 5 條,全部已堵

| # | 追加的 CSS | 位置 | 這輪結果 |
|---|---|---|---|
| R1 | `:root:root { --color-danger-badge: #f0736b; }` | theme.css | **exit=1 ✅** |
| R2 | `:root { --color-danger-badge: #f0736b; }` | global.css | **exit=1 ✅** |
| R3 | `.nav-tab__badge.is-hot { background: var(--color-danger); }` | chrome.css | **exit=1 ✅** |
| R4 | `.nav-tab__badge, .zz { background: var(--color-danger); }` | chrome.css | **exit=1 ✅** |
| R5 | `.nav-tab__badge { outline-color: transparent; }` | chrome.css | **exit=1 ✅** |
| R6 | round3 回歸:合規值停在 `@media print { :root {…} }`,`:root` 放不合規值 | theme.css | **exit=1 ✅**(第三輪 SHOULD-3 C 的修法沒被放寬掃描範圍賠掉) |

### 這輪新找到的 3 條(仍可繞)

| # | 構造 | 守衛 | 螢幕實際 |
|---|---|---|---|
| **N1** | theme.css `:root { --color-danger-badge: #f0736b !important; }` + global.css `:root { …: #ba5953; }` | **exit=0,印 4.52:1** | `!important` 勝 → **2.85:1** |
| **N2** | theme.css `:root:root:root { …: #f0736b; }` + global.css `:root:root { …: #ba5953; }` | **exit=0,印 4.52:1** | (0,3,0) > (0,2,0) → theme.css 勝 → **2.85:1** |
| **N3** | theme.css `:root` 放不合規值、global.css `:root` 放合規值,**再把 `main.tsx` 的兩行 import 對調** | **exit=0,印 4.52:1**(與未對調時**逐字相同**) | 對調後 theme.css 後載入而勝 → **2.85:1** |

N3 的直接證據:

```
--- 原順序 (theme.css 先, global.css 後) ---   guard: 4.52:1, exit=0   ← 這時是對的
--- 對調後 (global.css 先, theme.css 後) ---   guard: 4.52:1, exit=0   ← 這時是錯的
```

守衛從未讀取 `main.tsx`:

```
$ grep -c "readFileSync.*main" frontend/scripts/check-token-roles.mjs
0
```

排序是 `cascadeRank(d) = (compoundRoot(d) ? 2 : 0) + (d.rel === THEME ? 0 : 1)` ——
「theme.css 最先載入所以最弱」是**寫死的推導**,不是從 `main.tsx` 讀來的,也沒有任何測試釘住這個前提。
`compoundRoot` 只分「是否為複合」二元值,不計算真正的 specificity;`!important` 完全未納入模型。

---

## B — parity 測試的可信度:四種「靜默變綠」手法

| # | 弄壞什麼 | 結果 |
|---|---|---|
| S1 | Go 端 `TestThemeNameVerdictsEmit` **改名**(`-run` 比對不到,`go test` exit 0) | **紅** `ENOENT: … verdicts.json` |
| S2 | Go 端守門改成 `if true { t.Skip(...) }` | **紅** `ENOENT: … verdicts.json` |
| S3 | `go` 執行檔移出 `PATH` | **紅** `spawnSync go ENOENT` |
| S4 | TS 端類別集合拿掉 `\p{Cf}`(真實單邊漂移) | **紅**,逐條印出 `go: REJECT… / ts: ACCEPT` |

還原後 3/3 綠。fail-closed 的關鍵:Go 端寫檔到**每次新建的 mkdtemp**,TS 端**必須讀到**該檔;
`-count=1` 擋快取,`Object.keys(go).length === cases.length` 擋半截檔,
`cases.length >= 57` 擋語料縮水。

---

## D — 型別閘

```
$ npm run typecheck                       # "tsc --noEmit && tsc --noEmit -p tsconfig.scripts.json"
exit=0

# D1 在 scripts/check-token-roles.test.ts 尾端加 `const __probe: number = "not a number";`
exit=2
scripts/check-token-roles.test.ts(202,7): error TS2322: Type 'string' is not assignable to type 'number'.

# D2 在 src/lib/themeName.parity.test.ts 尾端加 `const __p: string = readFileSync(CASES);`
src/lib/themeName.parity.test.ts(155,7): error TS2322: Type 'NonSharedBuffer' is not assignable to type 'string'.

# 還原後 exit=0
```

`@types/node@^22.20.1` 已在 devDependencies(package.json:30);
`tsconfig.scripts.json` `include: ["scripts/**/*.ts"]`、`types: ["node","vitest/globals"]`;
`tsconfig.json` 的 `types` 也補上了 `"node"`;`build` 改為 `npm run typecheck && vite build`,
兩份 tsconfig 都在 build 路徑上。**兩個方向都實測會紅。**
