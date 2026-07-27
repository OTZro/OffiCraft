# T-081b 第四輪修正 — 進度報告

工作區 `OffiCraft-wt-T081b`,分支 `feat/T-081b-theme-token-split`,全部未 commit。
紅證 log 在 `docs/T-081b-evidence/round4-fix-run/`。

## 必修 A — 設定頁主題清單的偽造(BLOCKER-A) ✅

改動檔案:

| 檔案 | 用途 |
|---|---|
| `frontend/src/i18n/locales/{en,zh}.ts` | 刪掉 `settings.themeBuiltinTag` / `themeCustomTag`;chip 改讀不可覆寫的 `themeMarkers.builtinGroup` / `customGroup`(與 ProfileDropdown 的 optgroup 同一個語意來源) |
| `frontend/src/components/ThemeSettings.tsx` | 主題清單結構化成「內建 / 自訂」兩個 `role="group"` + `aria-labelledby` 的分組;chip 改讀 themeMarkers |
| `frontend/src/styles/theme.css` | 新增不可覆寫色槽 `--color-marker-{builtin,custom,surface,fg}`,值等同內建深色主題原本的 `--color-seg-fill` / `--color-icon-violet-bg` / `--color-bg` / `--color-text`,color-mix 算式與百分比原封不動 |
| `frontend/scripts/gen-theme-tokens.mjs` | 結構規則:`--color-marker-*` 整族排除在 `THEME_COLOR_TOKENS` / `themeColorTokens` 白名單之外(命名規則,不另維清單) |
| `frontend/src/components/theme-settings.css` | chip 改吃 `--color-marker-*`;新增 `.ts-group-head` |
| 產生器輸出 | `npm run gen:tokens` + `gen:msgkeys` 重跑,drift 歸零(`themeTokens.generated.ts` / `theme_colornames_gen.go` / `messageKeys.generated.ts` / `message_keys_gen.go`) |
| 測試 | `ThemeSettings.test.tsx`(3 條新測試)、`ProfileDropdown.settings.test.tsx`、`compose.test.ts` 跟改 |

測試與紅證(`round4-fix-run/redproof-A.txt`):

| 弄壞什麼 | 紅的測試 | 訊息 |
|---|---|---|
| chip 文字搬回可覆寫的 `settings` 子樹 | `cannot be made to show two identical built-in rows by a theme's wording, colours or name` | `expected '偽造' to be '內建'` |
| chip 顏色改回 `--color-seg-fill` / `--color-icon-violet-bg` / `--color-bg` / `--color-text` | 同上 | `expected … not to contain 'var(--color-seg-fill)'` |
| 拿掉兩個分組標題 | `puts the built-in and the custom rows in separate labelled groups` + 上一條 | `expected undefined to be '內建'` |
| 產生器不再排除 `--color-marker-*` | `keeps the marker colour slots out of the pack-settable token whitelist` | `expected true to be false` |

還原後 18/18 綠。

## 必修 B — 隱形字元改用 Unicode 類別規則 ✅

改動檔案:

| 檔案 | 用途 |
|---|---|
| `frontend/src/lib/themeBundle.ts` | `BIDI_FORMAT_CODEPOINTS` / `ZERO_WIDTH_NAME_CODEPOINTS` 兩份手列表刪除,改成 `u` 旗標的 property escapes `/[\p{Cc}\p{Cf}\p{Co}\p{Cs}\p{Zl}\p{Zp}\p{Zs}]/u`,U+0020 例外放行 |
| `server/ocserverd/theme_bundle.go` | `bidiFormatRunes` / `zeroWidthNameRunes` 兩份手列表刪除,改成標準庫 `unicode.{Cc,Cf,Co,Cs,Zl,Zp,Zs}` RangeTable + `unicode.In`,U+0020 例外放行 |
| 兩端錯誤訊息 | 同步改成 `name must not contain control, formatting, private-use, surrogate, separator or non-ASCII space characters` |
| `frontend/src/lib/themeName.cases.json` | 把審查者的 57 組名稱語料收成**追蹤的 fixture**(原本只在未追蹤的 evidence 目錄) |
| `frontend/src/lib/themeName.parity.test.ts` | **新檔:兩端同時餵、逐條比對判決的安全網**。TS 端直接呼叫 `validateThemeBundle`;Go 端由同一支測試 spawn `go test -run ^TestThemeNameVerdictsEmit$` 產出判決 JSON;只正規化錯誤訊息的路徑前綴,其餘逐字比對 |
| `server/ocserverd/theme_bundle_test.go` | 新增 `TestThemeNameVerdictsEmit`(由兩個 env var 驅動,沒設就 skip,不影響一般 `go test ./...`);拒收表加入 U+00AD / U+180E / U+E0041 / U+E000 / U+2028 / U+2029 / U+00A0 / U+3000 / U+1680 與純全形空白;接受表移除 U+3000 名稱、加入 VS16 emoji、阿拉伯文、希伯來文、越南文 |
| `frontend/src/lib/themeBundle.test.ts` | 同一份 twin 表格同步 |

測試與紅證(`round4-fix-run/redproof-B.txt`):

| 弄壞什麼 | 紅的測試 | 訊息 |
|---|---|---|
| TS 端的類別集合拿掉 `\p{Zs}`(單邊漂移) | `theme name validation · Go/TS parity > returns the identical verdict on both ends for every name` | 6 組分歧,`go: REJECT… / ts: ACCEPT` |
| Go 端的類別集合拿掉 `unicode.Zs`(另一邊) | 同上 + `TestValidateThemeBundles` | `go: ACCEPT / ts: REJECT…`;Go `name " Office " must be rejected, got <nil>` |
| 兩端一起退回第三輪的碼位黑名單 | `rejects a name carrying control, formatting, …`(TS)、`rejects every invisible-category name …`、Go `TestValidateThemeBundles` | `tag_char: expected 'ACCEPT' to match /^REJECT…/`;Go `name "Off­ice" must be rejected, got <nil>` |
| 兩端一起把 `Mn` 也擋掉(誤擋合法名稱) | `accepts every legitimate name shape…`(兩端) | `Heart ❤️: expected … to be null`;`emoji_vs16_only: expected 'REJECT…' to be 'ACCEPT'` |

還原後 TS 46/46 綠、Go `ok ocserverd`。

## 應修 C — 守衛的 5 條繞法 ✅

改動檔案:`frontend/scripts/check-token-roles.mjs`(選擇器判定 + 掃描範圍 + 屬性族)、
`frontend/scripts/check-token-roles.test.ts`(6 條新測試)。

三處修法:

1. **`targets(prelude, wanted)`** 取代 `selector.split(" ").at(-1) === selector`:逐一走 selector list 的**每一段**,取該段的 subject compound,再拆成 simple selector 逐個比對 —— `:root:root`、`.nav-tab__badge.is-hot`、`.nav-tab__badge, .zz` 三種都算命中。
2. **`:root` 定義掃描範圍改為所有 CSS 檔**(不再只有 theme.css、不再只認字面 `:root`),並依「specificity → 載入順序」排序後由 `.at(-1)` 取勝者;`atRuleFree()` 維持第三輪 SHOULD-3 C 的 at-rule 排除,兩個方向都不看 at-rule。載入順序刻意**不是**檔案走訪順序:`main.tsx` 先載 theme.css 再載其他,所以 theme.css 是同 specificity 中最弱的一份。
3. **屬性族**取代單一 prop:`background`/`background-color`、`outline`/`outline-color` —— shorthand 必須存在,且族內每一條宣告都要用到該 token。

測試與紅證(`round4-fix-run/redproof-C.txt`):

| 弄壞什麼 | 紅的測試 |
|---|---|
| `:root` 掃描縮回「theme.css + 字面 `:root`」 | `fails when a higher-specificity :root:root overrides the badge fill`、`fails when a later stylesheet's :root overrides the badge fill` |
| badge 規則比對退回 `split(" ").at(-1)` | `fails when a compound selector re-paints a badge`、`fails when a selector LIST re-paints a badge` |
| 屬性族收回只認 shorthand | `fails when the outline-color longhand removes the badge's ring` |

另加 `still ignores a compliant value parked in an at-rule`,確認放寬掃描範圍沒有把第三輪 SHOULD-3 C 的修法賠掉。
還原後 12/12 綠、`node scripts/check-token-roles.mjs` exit=0。

## 應修 D — 新測試檔進入型別檢查 ✅

改動檔案:

| 檔案 | 用途 |
|---|---|
| `frontend/package.json` | devDependencies 加 `@types/node@^22.20.1`;`typecheck` 改成 `tsc --noEmit && tsc --noEmit -p tsconfig.scripts.json`;`build` 改成 `npm run typecheck && vite build`(兩個 tsconfig 都跑到) |
| `frontend/package-lock.json` | 一併更新 |
| `frontend/tsconfig.scripts.json` | **新檔**:`include: ["scripts/**/*.ts"]`,node 環境(node 型別、無 DOM、vitest globals),沿用 `tsconfig.node.json` 的既有慣例。刻意不用 project reference —— reference 的 project 不能 `noEmit`(TS6310),而這裡要的就是純檢查 |
| `frontend/tsconfig.json` | `types` 加上 `"node"`:`src/` 下新增的兩支測試(`themeName.parity.test.ts`、`ThemeSettings.test.tsx`)會用到 `node:fs` / `node:child_process` |

測試與紅證(`round4-fix-run/redproof-D.txt`):

| 弄壞什麼 | 結果 |
|---|---|
| 在 `scripts/check-token-roles.test.ts` 塞一個型別錯誤 | `npm run typecheck` **exit=2**,`check-token-roles.test.ts(202,7): error TS2322` —— 證明這支檔案真的進入型別閘 |
| 拿掉 `@types/node` 套件本身(SHOULD-D 的根因) | `error TS2688: Cannot find type definition file for 'node'`,typecheck 非 0 |

(另記:只改 `types` 陣列**不足以**造成紅 —— 顯式 `import "node:fs"` 不受 `types` 陣列限制,所以第一次的 sabotage 是無效的,已改用移除套件本身。log 內保留這一段紀錄。)
還原後 `npm run typecheck` 綠。

## 已知取捨(明確記錄)

1. **`辦公室` + 變體選擇符(U+FE0F 等,類別 `Mn`)仍會渲染成與內建同名**。變體選擇符維持**接受**是刻意的:它是合法 emoji 名稱(「Heart ❤️」)必用的字元,而 `Mn` 整個類別還裝著越南文、希伯來文母音點、天城文的組合符 —— 擋掉會大面積誤傷正常名稱。代價是「保留內建名」這條規則對 `Mn` 這一類仍可被繞過;殘餘防線是設定頁與快選單的**結構化分組**(內建組永遠只有一列,自訂列進不去),那才是本輪 BLOCKER-A 真正的修法。
2. **全形空白(U+3000)包夾或純全形空白的名稱從此被拒收**(`Zs` 類別)。使用者改用一般空格即可。這順帶把第四輪的 NIT-E(全空白名稱被收下)一起解掉。同類被連帶拒收的還有 U+00A0 / U+1680 等所有非 U+0020 的 `Zs`。
3. **既有主題包的 chip 顏色會變**。定案要求「新色槽的預設值必須讓內建深色主題與所有既有主題包外觀不變」,但這兩件事在「色槽不可覆寫」的前提下**互斥**:只要主題包蓋掉 `--color-seg-fill` / `--color-icon-violet-bg` / `--color-bg` / `--color-text`,舊行為就是 chip 跟著變色,而那正是偽造的來源。實作採用定案的方向(專用不可覆寫色槽),並把預設值設成內建深色主題原本算出來的同一組值,所以:**內建深色主題與任何沒動那四顆 token 的主題包外觀一個像素不變**(142 個 Playwright CT 視覺守衛全綠佐證);**有動那四顆 token 的既有淺色包,chip 從此固定為那組深色**。這是本次唯一偏離「外觀完全不變」字面要求的地方,原因是硬衝突,方案未自行更換。
4. **lone surrogate 的兩端行為不完全同構**。TS 端 `\p{Cs}` 會擋下落單代理;Go 的 `encoding/json` 在解碼時就把落單代理換成 U+FFFD(類別 `So`),所以 Go 端看到的已經不是代理了。57 組語料裡沒有這個案例,兩端在該語料上 57/57 一致;此差異只影響「傳一個落單代理進來」的邊界,結果是前端先拒、後端收下並存成 U+FFFD,不構成仿冒。

## CI

`bash bin/ci.sh` → **`[ci] all green`**(exit 0)。log:`docs/T-081b-evidence/round4-fix-run/ci-run-1.log`。

- go:`ok ocserverd 49.169s` / `ok ocwarden 36.330s` / `ok ocagent 0.829s`
- frontend:`Test Files 165 passed (165)` / `Tests 1317 passed (1317)`(前一輪為 164 / 1305),`tsc --noEmit` 兩份 tsconfig 都綠
- Playwright CT 視覺守衛:`142 passed (33.1s)` —— 內建深色主題外觀無位移
- `[token-roles] ok — … 4.52:1 vs --color-on-danger / 3.76:1 vs --color-bg (its 1px ring)`
- 五道產生器 drift gate(theme-token / message-key / font / contract / gen-ocapi)全部無 drift
- conformance:`975 passed in 16.46s`,`[conformance] all green`
