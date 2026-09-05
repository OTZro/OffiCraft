# T-f014 mutant 驗證紀錄

**為什麼這份要落檔**：前一次獨立審查回來時，某包宣稱的 mutant 驗證在 repo 裡找不到任何紀錄，
審查者只好自己重跑一遍。**沒落檔等於不存在。** 下面每一列都可以被獨立重放。

同源方法論見 `docs/design/worker-panel-parity-mutants.md`（T-7526）。

## 方法

1. 把要驗的檔案複製一份到 scratchpad 當備份，`shasum -a 256` 記下雜湊。
2. 施加 mutant（單一、明確、可讀的一處改動 —— 描述欄寫的就是那一處）。
3. 跑該範圍的測試／守衛，記下**哪幾條紅、以及紅的訊息說的是不是我打的那個目標**。
4. **從 scratchpad 備份 `cp` 回來**（🔴 不准 `git checkout --`），
   再 `shasum -a 256 -c` 驗還原後逐位元組相同。

全部 12 支跑完，五個被動過的檔的還原檢查都是 `OK`。

🔴 **每一列的「紅了哪條」都附了斷言訊息**，不是只寫「失敗了」。
理由：只斷言「有東西壞掉」的守衛，在因為無關原因壞掉時照樣給綠燈 —— 這個病這個 repo 犯過兩次。
M1 的第一版正是這個坑：它紅的是 `TypeError: Cannot read properties of null`，
那是「元素不見了」和「元素指錯 bytes」共用的同一種爆法。
測試改成具名的 `expect(image, "…").not.toBeNull()` 之後，紅的訊息才真的指認目標。

## 第一批：共用彈窗吃下 staged 圖片（`MarkdownPreviewOverlay.tsx`）

| # | Mutant（改了什麼） | 紅了哪條（斷言訊息） |
|---|---|---|
| M1 | `image` 判定不看 `imageSrc`（staged bytes 掉進文字分支） | `previews a staged image…` → `the staged bytes must render through the image branch: expected null not to be null`；`opens a staged composer image…` → `the shared overlay must render the staged bytes as an image` |
| M2 | 分享鈕的閘放寬成 `attachmentId \|\| imageSrc`（替還沒上傳的 bytes 造一條分享連結） | 同兩條 → `expected <button …> to be null` |
| M3 | `downloadHref` 丟掉 staged 的 `data:` URI | `previews a staged image…` → `staged bytes are a real file — the download stays: expected null not to be null` |
| M4 | 縮放群組的 label 退回寫死英文 `"image zoom controls"` | `renders an image in the shared header shell…` → `Unable to find an accessible element with the role "group" and name "縮放圖片"` |
| M5 | 放大鈕拿掉 `aria-label` | 同上 → `Unable to find an accessible element with the role "button" and name "放大"` |
| M6 | 圖片 `alt` 退回泛用字串、不用檔名 | 同上 → `expected '聊天圖片' to be 'shot.png'` |

M2 是**防過度修正的哨兵**：M3 證明「staged 要有下載」，M2 證明「staged 不准有分享」。
兩半都承重 —— 只留一半的話，把兩顆鈕一起開或一起關都能矇混過關。

## 第二批：樣式所有權（`styleOwnership.test.ts`）

這一批對應本票最大的風險：`.md-preview*` 原本坐在 office.css 中段，
而畫它的 `MarkdownPreviewOverlay.tsx` **自己沒有 import 那張表**——
它是靠 OfficePage / RepliesPage / TasksPage 的 transitive import 搭便車。
T-7526 今晚才因為同一類事故壞掉過沒被碰到的畫面，而 jsdom 不算 CSS、`tsc` 看不出
class 字串與 stylesheet 的關係 ⇒ 所有自動檢查都會是綠的。

| # | Mutant | 紅了哪條 |
|---|---|---|
| M7 | `MarkdownPreviewOverlay.tsx` 拿掉 `import "./md-preview.css"` | `every component using .md-preview__* imports ./md-preview.css` |
| M8 | 在 office.css 塞回一條 `.md-preview__panel` 規則（便車復活） | `.md-preview__* rules live in md-preview.css and nowhere else` |
| M9 | `OWNED_SHEETS` 加一個沒人畫的 block（vacuous-green 哨兵） | `every component using .nobody-uses-this__* imports ./nobody-uses-this.css`（`users.length > 0` 斷言） |

M9 是**守衛自己的守衛**：把 block 改名、或把元件刪掉，迴圈就會掃到空集合並「通過」。
`expect(users.length).toBeGreaterThan(0)` 讓「沒東西可檢查」變成紅的，而不是綠的。
M8 釘的是另一半：所有權只有在 block **只有一個家**時才成立；規則若散在兩張表，
元件自己 import 也不夠，拿掉它會只壞一半、看起來像沒壞。

## 第三批：舊看圖層退役守衛（已刪除）

`bin/tests/lightbox-retired-guard.sh` 連同 `bin/tests/run.sh` 裡派它的那一段
已依 owner 裁定刪除（這道掃描器不值得留），原本記在這裡的 M10／M11／M12
三支 mutant 也一併作廢。

**被抽掉之後什麼不再被守**：production source 裡再出現 `.chat__lightbox`
樣式塊（`frontend/src/components/office.css`）或第二個全螢幕圖片覆蓋層元件，
CI 一律綠 —— T-f014 修掉的「同一次點擊有兩個 overlay、其中一個永遠打不開」
那個形狀可以被寫回來而沒有任何掃描會說話。唯一倖存的是**一條行為斷言**：
`frontend/src/components/ChatArea.image.test.tsx` 的
「opens a staged composer image in the same shell」最後那句
`expect(container.querySelector(".chat__lightbox")).toBeNull()` ——
一個元件的一條路徑，不是對整棵 source tree 的掃描。
`frontend/src/components/office.css` 裡 `.chat__msg-image--clickable`
上方的註解記著同一句。

## 這份紀錄涵蓋不到的

- **CSS 抽檔有沒有改到畫面**：靠 `frontend/visual-guards/` 底下的 Playwright CT
  visual guard 在真瀏覽器裡跑過（**不要在這裡寫數量** —— 原本這行寫「162 條」，
  T-51 的獨立審查實查是四百多條，那個數字從寫下的那天起就在過期；要知道現在幾條就
  `npx playwright test -c playwright-ct.config.ts --list`）
  ⚠️ **這裡原本列著一份「有哪些 guard」的清單，已經拿掉** —— T-51 期間它一口氣落後
  兩支（`t51-gallery-sender-filter`、`t51-preview-pager`），而清單過期不會讓任何東西
  變紅。要知道現在有哪些，跑
  `ls frontend/visual-guards/*.ct.spec.tsx`。
  唯一值得留在這裡的是一則**歷史更正**：`t-c645-attachment-preview` 原本斷言
  `md-preview__action-label` 在窄寬度 hidden，**該 label 與那條 media query 已於
  T-51 ④ 移除**（owner 2026-09-02 要三顆頂列按鈕一律只留圖示），所以那條斷言改成釘
  「頂列沒有可見文字、每顆仍有可觸及名稱」。
- **staged 圖片在真瀏覽器裡長什麼樣**：沒有對應的 CT 故事，只有 jsdom 測試。
