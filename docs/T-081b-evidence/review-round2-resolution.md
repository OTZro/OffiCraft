# T-081b 獨立 review 第二輪 — 處置結論

**審查者**:兩個未參與實作的 actor,各帶一種 lens(A=正確性/相容性、B=安全/使用者可見/i18n),
報告全文見同目錄 `review-round2-lensA.md` / `review-round2-lensB.md`。

**快照**:本處置檔對應的樹 = base `origin/main@8545b8e` + 未 commit 變更,
`git diff | shasum -a 256` 前 12 碼 **`93e0f32051f7`**(2026-07-27)。

⚠️ **兩位審查者看的是更早的快照**(報告內各自註明)。本票範圍在審查期間仍被 owner 擴大
(`cover` 鋪法、寬版外區撤回、透明度可調),因此**下列每一條都已對上面這個最終快照重新確認**,
不是照抄報告結論。

## BLOCKER — 全數修掉

| # | 發現 | 處置 | 證據 |
| --- | --- | --- | --- |
| A-B1 | 七個新槽用**字面**預設值,已匯入的**淺色**主題包升級後靜默壞掉(實測 16.28:1 → 1.29:1) | 預設值改成 `var(--母槽)` alias;連帶移除 lint 中前提錯誤的「不得 alias 回母槽」那條,並讓 `expandVars` 在新槽處停止展開 | `legacy-pack-compat.mjs` / `-report.txt`:同一份 pre-T-081b 淺色包餵進 base 與 head,七個受影響計算色**逐一相同**;無主題包時內建深色亦逐一相同 |
| A-B2 | 版面分區 token 在產品自己的主題編輯器裡編不到(匯出刻意跳過 alias 預設,而編輯器只列匯出有的) | 編輯器改成「bundle 既有的槽 ∪ 所有預設跟隨別人的槽」,未動過的維持空值不寫死 | `ThemeSettings.test.tsx` 新增兩例(可編到 `--color-main-bg`;未動的 `--color-nav-bg`/`--color-knob` 不進 bundle) |
| B-B1 | `cover` 當時只加進 TS 白名單 | 已補齊 Go 驗證、openapi(重跑 gen-ocapi)、zh/en 文案、下拉第三個選項 | conformance 新增 `test_settings_custom_theme_background_and_mode_round_trip` |
| B-B2 | `sides` 語意改過但 spec/註解/文案仍寫「只向下重複」 | spec 兩段、`theme.css`/`global.css`/`i18n` 註解、zh+en 使用者文案全部改寫 | `grep -rn "repeat-y\|向下重複"` 於產品碼與 spec 均無殘留 |

## SHOULD — 修掉的

| # | 發現 | 處置 |
| --- | --- | --- |
| B | 被丟棄的用詞 code 原封進 `log.Printf`,可**偽造整行 server log** | 改 `%q` escape + 單碼長度截斷;新增 `TestDropUnknownWordingCodesLogIsForgeProof`,**實測拿掉 escape 會紅** |
| A | lint 第 7/8 種繞法:`color-mix(token 100%, transparent)`、`transparent 0%` 語法像疊層、實則不透明 | `veilOnly` 改為看百分比;兩種各自實測會紅,合法的 40% 疊層仍綠(無誤判) |
| A | lint 第 9 種:alias 鏈超過 8 跳直接放行(fail-open) | 改 fail-closed:超深即記一條違規;10 跳鏈實測會紅 |
| A | `backgrounds`/`backgroundModes` 零 conformance 覆蓋 | 新增往返 + 封閉值域 + 「有鋪法沒有圖」三段 |
| B | 背景圖提示只講「手機不受影響」,但**寬版**外區也是 0 | zh/en 提示改寫,明列三種看不到的情境,並說明 `cover` 不受此限 |
| A/B | 交付項「寬版保留左右外區」已被 owner 當日撤回 | 拆法定案書 §7 已改寫,chrome.css 保留「為什麼不能只寫 calc()」的知識 |

## SHOULD — 不修,列為已知取捨

| 發現 | 為什麼不在本票修 |
| --- | --- |
| 主題名稱不擋控制字元/RTL,且自訂主題可自稱「辦公室」 | 兩者都**早於本票**(名稱欄位與其驗證未被本票改動);修它要動主題名稱的驗證規則,屬獨立範圍。已在拍板卡向 owner 列明 |
| ProfileDropdown 沒有「內建」標記 | 同上,屬既有 UI 的資訊呈現,非本票造成 |
| 內建深色主題未讀徽章 2.85:1 | 既有缺口,因「像素不變」的驗收條件必須原封保留;修它會改變內建主題外觀 |

## 兩位審查者共同確認為乾淨的部分

- 注入面:對背景圖值構造 23 種攻擊(引號/括號/換行/編碼/RTL/svg/mime 變體)全被擋;用詞覆寫走 React children,無 `dangerouslySetInnerHTML`。
- 寬容政策**沒有外溢**:顏色/字型/頭像/頁籤圖示/背景 zone 仍整包拒收,只有用詞 code 被丟。
- 主題名稱 bug 確實修好(結構性排除,非手維第二份清單)。
- 前後端驗證判決一致(16 組 JSON 逐一比對)。
- 內建深色主題像素在 10 種寬度×版面組合下完全相同。
- 對比度改動前後失敗組合完全相同 ⇒ **沒有引入新的低對比**。
- round-1 的 3 BLOCKER + 6 SHOULD 經獨立重驗,**全部真的修好**(含六種 lint 繞法)。
