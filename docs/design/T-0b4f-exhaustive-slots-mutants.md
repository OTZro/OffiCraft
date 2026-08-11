# T-0b4f mutant 驗證紀錄：插槽改成 exhaustive

**為什麼這份要落檔**：這張票的核心驗收是「**反例編不過**」——一個宣稱，而宣稱要能被獨立重放才算數。
T-7526 的獨立審查回來時，那包宣稱的 mutant 驗證在 repo 裡找不到任何紀錄，審查者只好自己重跑一遍。**沒落檔等於不存在。**

## 方法

1. 把要驗的檔案複製一份到 scratchpad 當備份，`shasum -a 256` 記下雜湊。
2. 施加 mutant（單一、明確、可讀的一處改動 —— 描述欄寫的就是那一處）。
3. 跑該範圍的檢查，記下**哪幾條紅**。
4. **從 scratchpad 備份 `cp` 回來**（🔴 **不准 `git checkout --`** —— 那會吃掉別人未提交的編輯），
   再 `shasum -a 256 -c` 驗還原後**逐位元組相同**。

全部 5 支的還原檢查都是 `OK`（`shasum -a 256 -c` 逐位元組相同，且 `git status` 空）。

🔴 **本檔的數字是 2026-08-11 在 `t-0b4f/exhaustive-slots` @ `ea28ad1`（基底 = 當時的 `origin/main` `1b21afb`）重跑的**，不是沿用更早那一輪。**理由：換基底之後「比對型」證據全部失效** —— 而這幾支 mutant 的證據形式正是 **tsc 的逐字訊息**，屬於比對型。第一輪跑在 `a8fdb42`（基底 `7246049`）上，主幹隨後動了兩次，所以那一輪的輸出已不可引用。

## 未施加 mutant 的基準（先證明它本來是綠的）

| 檢查 | 結果 |
|---|---|
| 完整 `bash bin/ci.sh` | **rc=0**，`ci.log` 末行（去 ANSI 色碼後）逐字 `[ci] all green`；17:28:16Z→17:40:21Z |
| `npm run typecheck`（含 `tsconfig.scripts.json` 與 `tsconfig.guards.json`） | rc=0 |

⚠️ **判綠沒有用這台的 `grep`**：它是包過的函式、帶 `-I`，會把有色碼的 CI log 判成非文字而**靜默跳過**（O-146 實測：連陽性對照都 0 命中，輸出跟「掃過了、很乾淨」一模一樣）。改用 python 讀 bytes 判斷，並同時證明掃描器活著：陽性對照 `[ci]` **3 命中**、陰性對照 `ZZZ_NO_SUCH_TOKEN` **0 命中**、`FAIL=[1-9]` **0 命中**。
⚠️ 另外跑前跑後各存一份 `git status --porcelain` 指紋，兩份**相同且皆為空** ⇒ 這輪驗的是 commit 那棵樹，不是被中途動過的工作樹。

## A · 核心驗收：新增一個插槽，兩邊都沒實作

**mutant**：`AGENT_DETAIL_SLOTS` 加一個 `"afterFooterCards"`，兩個 wrapper 都不動。

**tsc 紅 3 條**，逐字點名三個必須表態的地方：

```
src/components/AgentDetailPanel.slots.test.tsx(52,7): error TS2741: Property 'afterFooterCards' is missing …
src/components/MemberDetailPanel.tsx(1762,7):        error TS2741: Property 'afterFooterCards' is missing …
src/components/WorkerDetailPanel.tsx(704,7):         error TS2741: Property 'afterFooterCards' is missing …
```

## A2 · 票面逐字的那一顆：新增插槽，**只在一邊實作**

**mutant**：同上加 key，**並且**只在 `MemberDetailPanel` 補上它（型別測試檔一併補，讓殘餘的錯誤只剩「另一邊」）。

**tsc 恰好紅 1 條，而且指的正是被漏掉的那一邊**：

```
src/components/WorkerDetailPanel.tsx(704,7): error TS2741: Property 'afterFooterCards' is missing …
```

⇒ 票面「**故意新增一個插槽卻只在一邊實作 ⇒ 型別檢查必須失敗，且失敗訊息指到漏掉的那一邊**」成立。
**改動前**這顆 mutant 是完全綠的：optional prop 少傳一個不是錯誤，畫面上那一格只是靜默地空著。

## B · 守衛自己會不會壞：把保證拿掉

**mutant**：`AgentDetailSlots` 從 `Record<…>` 放寬成 `Partial<Record<…>>`（＝退回舊語意）。

**tsc 紅**，而且第一條就是守衛在喊自己沒事做：

```
src/components/AgentDetailPanel.slots.test.tsx(64,1): error TS2578: Unused '@ts-expect-error' directive.
src/components/AgentDetailPanel.tsx(415,17): error TS2345: Argument of type 'AgentDetailSlot | undefined' …
（同型另外 4 條，每個插槽渲染點各一）
```

⇒ 這是這份型別測試存在的理由：`@ts-expect-error` 只有在**真的有錯誤**時才安靜，
所以「型別哪天不再拒絕漏掉的 key」會**當場**變成一條紅，而不是一份看起來還很有道理的舊測試。

## C · 誤殺方向：把合法的插槽也一起關掉

**mutant**：`slotNode` 改成 `s.on ? null : null`（＝所有插槽都不渲染）。

**vitest 轉紅 81 條**，而且**兩邊都有證人**（節錄）：

```
× both detail panels render the 定期訊息 card > renders it on the member panel
× both detail panels render the 定期訊息 card > renders it on the outsource worker panel, bound to the ow- id
× OutsourcePanel > clicking the avatar opens the worker detail panel (not the chat)
× MemberDetailPanel · webhook platform + signing secret > …（整組 webhook 卡）
× MemberDetailPanel — unified wake/change settings > …（整組設定對話框，走 overlays）
```

⇒ 「**正例不被誤傷**」那一側有真的哨兵：把插槽渲染打壞，正職與外包**各自**都有測試會紅。
（這一顆刻意不是「拿掉修法」的方向——那一側由 A/A2 蓋住；紀律要求**兩個方向各自被證明**。）

## D · 「不要」不可以變成畫面上的字

**mutant**：`slotNode` 改成 `s.on ? s.node : s.why`（把開發者寫的理由渲染出去）。

`AgentDetailPanel.slots.test.tsx` **恰好紅 1 條**（另一條照常綠）：

```
× AgentDetailPanel slot map > renders nothing — not even the reason — for a slot this side declined
```

⇒ 編譯期看不到這個方向：`{ on: false }` 帶著一段中文說明，把它印出來型別完全合法。

## E · 我改寫的那句 mutant 指示，自己也驗過

改寫文件時新寫下的指示同樣是一個**待驗證的宣稱**，所以照樣跑一次：

**mutant**：把 `WorkerDetailPanel` 的 `extraExpandCards: slot(scheduleCard)` 換成 `notHere("mutant")`。

`ScheduledMessagesCard.test.tsx` **恰好紅 1 條，而且是外包那一條**（另外 23 條全綠）：

```
× both detail panels render the 定期訊息 card > renders it on the outsource worker panel, bound to the ow- id
```

⇒ 該檔註解改寫後的那句「Turn either wrapper's `extraExpandCards` into `notHere(...)` and exactly one of
these two reddens」成立。
⚠️ 對稱的另一半（改正職那一邊 ⇒ 只有正職那條紅）**未實跑**，是由對稱性推論的。

## 這份紀錄沒有涵蓋的（誠實界線）

- **`frontend/visual-guards/` 不在任何一份 tsconfig 的 `include` 裡**（`tsconfig.guards.json` 只收 `paint-guards`
  與兩支 playwright 設定），所以那個資料夾**從未被型別檢查過**——而 `visual-guards/stories/AgentDetailConvergenceStory.tsx`
  是共用面板的**第三個呼叫端**。⇒ 上面 A/A2 那道「加一個插槽，全部呼叫端都會紅」的保證，**看不到那一個**。
  本包已把該 story 改成新形狀（所以 CT 不會壞），但**那個洞本身沒有補**：owner 於卡 `rc-72209ca50748` 裁定範圍。
- CT（真瀏覽器）那一層**沒有為本包新增護欄**：本包不動任何版面或樣式，插槽的渲染位置與順序一個字沒改。
