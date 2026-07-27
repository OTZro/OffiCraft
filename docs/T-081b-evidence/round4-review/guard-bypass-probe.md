# round4 — check-token-roles.mjs 繞法實測

方法:把 `frontend/src` 複製到 temp 樹,只改 temp 樹,用 `TOKEN_ROLES_SRC` 指過去跑守衛。
**真實工作區未被修改。**

baseline(temp 樹未動):exit=0,`4.52:1 vs --color-on-danger / 3.76:1 vs --color-bg`。

| # | 追加的 CSS | 檔 | 守衛 | CSS 上是否真的生效 |
|---|---|---|---|---|
| A | `:root:root { --color-danger-badge: #f0736b; }` | styles/theme.css | **exit=0(沒抓到)** | 生效。specificity (0,2,0) > (0,1,0),螢幕上是 #f0736b(2.85:1),守衛仍印 4.52:1 |
| B | `:root { --color-danger-badge: #f0736b; }` | styles/global.css | **exit=0(沒抓到)** | 生效。`main.tsx:5-6` 先 import theme.css 再 import global.css,同 specificity 由後者勝 |
| C | `.nav-tab__badge.is-hot { background: var(--color-danger); }` | components/chrome.css | **exit=0(沒抓到)** | 生效(需 markup 加 class)。`selector.split(" ").at(-1)` = `.nav-tab__badge.is-hot` ≠ `.nav-tab__badge` |
| D | `.nav-tab__badge, .zz { background: var(--color-danger); }` | components/chrome.css | **exit=0(沒抓到)** | 生效。selector list 的 `.at(-1)` 取到 `.zz`,整條規則被跳過 |
| E | `.nav-tab__badge { outline-color: transparent; }` | components/chrome.css | **exit=0(沒抓到)** | 生效。shorthand 先設、longhand 後蓋 → 環消失,守衛只檢查 `prop === "outline"` |
| F(對照) | `.nav-tab__badge { outline: none; }` | components/chrome.css | exit=1 ✅ | — |
| G(對照) | `.nav-tab__badge { background: var(--color-danger) !important; }` | components/chrome.css | exit=1 ✅ | — |

F/G 證明守衛本身是活的(不是全域失效),A–E 是選擇器/屬性比對邏輯的具體漏洞。

- A、B 是 round3 SHOULD-3 C(「合規值被停在不生效的地方」)的**鏡像**:這次是**不合規值被停在守衛不看的地方**。
  修 C 用的是「只認 `rel === theme.css && selector === ":root"`」,但這個條件對 A/B 而言太窄。
- D 是 SHOULD-3 B(`find`→`filter`)沒補完的部分:改成看「每一條規則」了,但「一條規則裡的多個選擇器」仍漏。
- E 是 SHOULD-4 的環自帶的漏洞:環只被 `outline` 這一個 prop 名釘住。
