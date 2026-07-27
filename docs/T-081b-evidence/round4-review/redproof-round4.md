# round4 — 審查者親手做的「弄壞 → 紅 → 還原 → 綠」

不看實作者的 log。每一條都自己改產品碼、自己跑測試、自己還原。
還原方式:改動前 `cp` 一份到 `/tmp`,驗證後 `cp` 回來,最後以 tracked diff 的 sha256 確認全樹回到原點。

## 抽驗 1 — 第 2 條(BLOCKER-2,optgroup)

弄壞 `frontend/src/components/ProfileDropdown.tsx`:把兩個 `<optgroup>` 換回扁平 option +
字串拼接的內建標記(`{`${t.themeIdentity.office}（${t.themeMarkers.builtinGroup}）`}`)。

```
× ProfileDropdown · preferences scope > keeps only the theme SELECTOR — no management affordances (moved to 設定/主題)
× ProfileDropdown · preferences scope > cannot be made to show two identical built-in rows by a theme's NAME
   Tests  2 failed | 7 passed (9)
```
還原後:`Test Files 1 passed (1) / Tests 9 passed (9)`。

## 抽驗 2 — 第 3 條 A(前景色納入)

弄壞 `frontend/scripts/check-token-roles.mjs`:從 `required` 移除
`["color", BADGE_TEXT, ...]` 那一列。

```
× check-token-roles > fails when a badge's text colour stops using the measured token
   Tests  1 failed | 5 passed (6)
```
還原後 6/6 綠。

## 抽驗 3 — 第 3 條 B(取每一條規則,不是第一條)

弄壞:`const rules = decls.filter(...)` 後面接 `.slice(0, 1)`(等價於還原成舊的 `find`)。

```
× check-token-roles > passes on the tree as shipped
× check-token-roles > fails when a later declaration re-paints a badge with --color-danger
   Tests  2 failed | 4 passed (6)
```
還原後 6/6 綠。

## 抽驗 4 — 第 3 條 C(只認 `:root`)

弄壞:`concreteValue` 裡 `rootDefs.get(token)` 改回全樹的 `defs.get(token)`。

```
× check-token-roles > fails when the badge fill drops below AA in :root, however it is patched elsewhere
   Tests  1 failed | 5 passed (6)
```
還原後 6/6 綠。

## 抽驗 5 — 第 4 條(1px 頁底色外框)

弄壞 `frontend/src/components/chrome.css`:刪掉 `.nav-tab__badge` 的
`outline: 1px solid var(--color-bg);`。

```
× check-token-roles > passes on the tree as shipped
  .nav-tab__badge has no outline declaration — without the page-colour ring the pill is
  measured against the wrong background (--color-indigo on an active tab is 2.74:1).
   Tests  1 failed | 5 passed (6)
```
還原後 6/6 綠。

---

## 結論

5 個切面全部成立 —— 這些測試確實咬住了它們宣稱咬住的東西,不是只有 log 上好看。

必須並列的但書:這證明的是「**這 5 種**弄壞法會被抓」。
`guard-bypass-probe.md` 裡的 A–E 五條繞法在**同一支守衛**下 exit=0 通過,
所以「守衛擋得住 CSS 優先序」這個更強的宣稱**不**成立。

---

## 一次性 probe:`validateWording` 是否接受 內建/自訂 chip 的對調

暫時放入 `frontend/src/lib/__round4probe.test.ts`(跑完即刪,未進入 git 狀態):

```ts
const w = { zh: { "settings.themeBuiltinTag": "自訂",
                  "settings.themeCustomTag":  "內建" } };
validateWording(w)
```
```
VALIDATE: null          ← 通過,沒有回報任何錯誤
AFTER: {"zh":{"settings.themeBuiltinTag":"自訂","settings.themeCustomTag":"內建"}}
                        ← 覆寫原封保留,沒有被 drop
```

對照:SHOULD-5 之後,同樣形狀的 `themeMarkers.copyTag` 覆寫**會**被 drop。
機制存在,只是沒套到這兩顆 key 上 —— 這是 review-round4.md 的 BLOCKER-A。
