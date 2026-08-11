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

🔴 **每一列都標它最後一次被實跑的 SHA，不要問「這份文件是哪個版本的」——那個問題永遠會過期。** 理由：換基底之後「比對型」證據全部失效，而這幾支 mutant 的證據形式正是 **tsc 的逐字訊息**，屬於比對型。

⚠️ **這份文件自己犯過它要防的錯，兩次，所以這段留著**：第一版的數字跑在 `a8fdb42`（基底 `7246049`），主幹動了兩次就失效；改寫後的第二版標成 `ea28ad1`（基底 `1b21afb`），而**那顆 commit 的全部存在理由就是修「數字跑在一個已經不存在的基底上」**——它交出去時 HEAD 已經是 `cabfbf4`（基底 `6a96e56`），同一個缺陷原地重演。**判準不是「作者再仔細一點」，是「每列自帶它自己的 SHA」。**

| 列 | 最後一次實跑於 | 誰跑的 |
|---|---|---|
| A / A2 / B / C / D | `cabfbf4`（基底 `6a96e56`） | **獨立審查者**（不同 actor）逐列重跑，行號與條數與本檔一致 |
| E / E′ | `cabfbf4` | 獨立審查者（E′ 是本檔原本承認沒跑的對稱那半） |
| F / G（下方新增） | 見該列 | 實作者，回應獨立審查的 finding M1 |

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

`ScheduledMessagesCard.test.tsx` **恰好紅 1 條，而且是外包那一條**（另外 **25** 條全綠，該檔共 26 條）：

⚠️ 本檔原本寫「另外 23 條全綠」，**那是我數錯，而且寫下當時就錯**（獨立審查實查：該檔在 `ea28ad1` 與 `cabfbf4` 都是 26 條 `it(`）。一份**存在理由就是逐列可重放**的文件裡的錯數字，比別處的錯數字嚴重——因為讀者會拿它去對帳。

```
× both detail panels render the 定期訊息 card > renders it on the outsource worker panel, bound to the ow- id
```

⇒ 該檔註解改寫後的那句「Turn either wrapper's `extraExpandCards` into `notHere(...)` and exactly one of
these two reddens」成立。
**E′（對稱的另一半，本檔原本承認未跑）**：獨立審查把它跑掉了——改 `MemberDetailPanel` 那一邊 ⇒ **恰好紅 1 條，且是 `renders it on the member panel`**，25 條全綠。⇒ 對稱性現在是**量到的**，不是推論的。

## F · 面板側：宣告了、卻沒 resolve（獨立審查 finding M1 的前半）

**這一顆是獨立審查做出來的，而它當時是全綠的。** 形狀：加一個插槽、**兩個 wrapper 都乖乖填了真的卡**、清單斷言也跟著更新（那是最自然的下一步編輯），**但忘了在 `AgentDetailPanel` 裡渲染它** ⇒ 卡片在兩個面板上都不會出現，而 **tsc rc=0、2072 條全綠**。
⇒ 那正是本票要根治的病（一張卡靜默地不在），**從 wrapper 側搬到面板側**。本包原本把 wrapper 側守得很緊、面板側完全沒守。

**修法**：面板把每個 key 在一個 `rendered` 物件裡解析，並用 `satisfies Record<AgentDetailSlotKey, ReactNode>` 釘住。

**mutant（`cabfbf4` 之後、修完當下實跑）**：加 `afterFooterCards` 到 `AGENT_DETAIL_SLOTS`、**不動 `rendered`** ⇒

```
src/components/AgentDetailPanel.tsx(286,5): error TS1360: Type '{ overlays: ReactNode; … }'
  does not satisfy the expected type 'Record<"overlays" | … | "afterFooterCards", ReactNode>'.
```

⇒ 現在**面板自己也會編不過**，不是只有兩個 wrapper。

## G · 面板側：resolve 了、卻沒放上畫面（M1 的後半，`satisfies` 答不出來）

`satisfies` 只證明「每個 key 都被解析過」，**不證明它到得了 DOM**——把值算出來丟在地上照樣編得過。

**修法**：`AgentDetailPanel.slots.test.tsx` 新增一條哨兵：每個 key 塞一個**各自不同的 marker**，掛載後斷言五個 marker 全部出現在 `container.textContent`。斷言的是 **DOM**，不是「`slotNode` 被呼叫過幾次」——後者對一個算完就丟掉的面板照樣成立。

**mutant（修完當下實跑）**：加 `afterFooterCards`、**在 `rendered` 裡解析它**、但**不放進 JSX** ⇒ 該哨兵紅，而且**點名是哪一個插槽**：

```
× AgentDetailPanel slot map > puts every declared slot's content on the screen
  → expected [ 'afterFooterCards' ] to deeply equal []
```

⇒ 訊息刻意設計成印出「漏掉的那個 key」而不是「expected 4 to be 5」——它紅的時候，唯一該問的問題就是「是哪一個」。

## 這份紀錄沒有涵蓋的（誠實界線）

- **`frontend/visual-guards/` 不在任何一份 tsconfig 的 `include` 裡**（`tsconfig.guards.json` 只收 `paint-guards`
  與兩支 playwright 設定），所以那個資料夾**從未被型別檢查過**——而 `visual-guards/stories/AgentDetailConvergenceStory.tsx`
  是共用面板的**第三個呼叫端**。⇒ 上面 A/A2 那道「加一個插槽，全部呼叫端都會紅」的保證，**看不到那一個**。
  本包已把該 story 改成新形狀（所以 CT 不會壞），但**那個洞本身沒有補**：owner 於卡 `rc-72209ca50748` 裁定範圍。
- CT（真瀏覽器）那一層**沒有為本包新增護欄**：本包不動任何版面或樣式，插槽的渲染位置與順序一個字沒改。
