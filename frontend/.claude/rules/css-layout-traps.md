---
paths:
  - "src/**/*.css"
  - "src/styles/**"
  - "src/components/**"
  - "visual-guards/**"
---

# CSS / 版面陷阱:長 token、CJK nowrap、vw 夾、樣式所有權、effect deps、動作列

> 本檔由 `frontend/CLAUDE.md` 拆出(T-9b5d)。`paths:` 的 glob **相對 `frontend/`**(rules 檔所在目錄),所以一律寫成 `src/…`;**不要寫 `frontend/…`**——那種寫法在這個位置永遠不命中(實測 89 條對 565 個真檔零命中,已整批刪除)。
> 標 📎 的段落表示「當時的量測證據已搬回該票」,本檔只留規則。

## 長 token 溢出:單一來源在 `.doc-md` 基底(T-d451)
owner/agent 自由文字會帶**不可斷的長 token**(長 URL、40-hex sha、無空白長字)。
沒有斷點時它把容器 min-content 撐到 token 全寬,容器不肯縮、撐破手機視窗,**整頁**
就能左右滑。**修在 `.doc-md` 基底(`settings.css`)的 `overflow-wrap: anywhere`**,
17 處 render site 與**未來新增的**一起繼承——這是唯一來源,**別再逐 surface 貼**
(T-4974 就是逐處貼,結果同一個病從沒貼到的頁面復發,才有 T-d451)。
- `anywhere` 不是 `break-word`:兩者都斷已溢出的行,但**只有 `anywhere` 收縮
  min-content**,那才是容器肯縮回視窗的原因(flex/grid 宿主尤其吃這點)。
- **不渲染 markdown 的自由文字欄位收不到這個繼承**,要自己宣告(現有:
  `replies.css` 的 `.reply-option__text` / `.reply-card__answer-text`、
  `monitor.css` 手機卡片模式的 `.mon-table td`)。加新的純文字欄位時記得。
- **橫向滾動只允許出現在明確的可滾動子區**:`.doc-md pre`(`white-space: pre`
  使 `overflow-wrap` 對它無效,實測仍正常橫捲)與 `.doc-md table`。修這類問題時
  **不可**為了消滅整頁橫滑而拿掉它們的 `overflow-x: auto`。
- 護欄:`visual-guards/docmd-longtoken-wrap.ct.spec.tsx`(文件面)、
  `monitor-table-longtoken.ct.spec.tsx`(監控表格)、
  `taskcard-longtoken-wrap.ct.spec.tsx`(任務卡)。都是**雙向**契約:整頁不許滑
  **且** pre/table 仍要能滑——單向斷言會讓「修過頭」靜靜通過。
- ⚠️ 重驗 mutant 時當心**斷言互相掩護**:整頁那條先炸會中止測試,底下 per-surface
  斷言根本沒跑。要證明後者,先暫時放寬整頁斷言再跑 mutant。

## 固定高 ＋ 可壓縮 ＋ CJK 標籤 = 必須宣告 `white-space: nowrap`(T-5e79)

owner 截圖回報:判準卡的「預設」徽章,兩個中文字被折成上下兩行、**撐破**那顆膠囊;
同一張圖右側的「編輯」按鈕一樣。三個條件同時成立就會發生,**缺一不會**:

1. 元素有**固定高**(`.set-badge` 是 `height: 19px`、`.doc-btn` 是 `30px`),
2. 它是**可收縮的 flex item**(預設 `min-width: auto`,但沒有東西阻止斷行),
3. 標籤是 **CJK**。

機制在第 3 點:**中文沒有 `white-space` 宣告時,min-content 只有一個字的寬度**
——盒子可以被壓到比標籤還窄,文字於是折行,而固定高跟不上,就溢出。拉丁字的
min-content 是整個單字,所以**同一段 CSS 對 en 幾乎沒有作用**(見下)。

- **修法是宣告 `white-space: nowrap`,不是 `flex: none`。** 兩者在這裡各自都足夠
  (mutant 實測:只拿掉任一個,護欄都還是綠),所以同時加就是一條**沒有任何東西守得住
  的冗餘宣告**。留 `nowrap`:語意最直接,而且在非 flex 宿主也成立。
- ⚠️ **`.doc-btn` 只加 `white-space`、刻意不加 `flex: none`** —— 它有約 40 個 call site,
  收縮能力在別處是有用的。
- 🔴 **這類缺陷的護欄一定要真瀏覽器,jsdom 構造上零鑑別力** —— 它不套版面,
  `insight-default-badge` 在不在 DOM 兩種情況一模一樣。
- 🔴 **斷言要落在使用者看得到的幾何,不要斷言 CSS 屬性字串**:`getComputedStyle` 讀回
  `nowrap` 會被「屬性設了但被 out-cascade」與「不折行了但仍撐破盒子」兩種情況滿足。
  現行護欄用 `Range.getClientRects()` **數 line box**(必須剛好 1)、要求每個 line box
  留在元素自己的 border box 內,再加三層容器的橫向溢出 ≤1px(擋「用溢出換不折行」)。
- **護欄**:`visual-guards/set-badge-nowrap.ct.spec.tsx`(四個寬度 320/375/390/1040;
  1040 是控制組,對每顆 mutant 都綠、**不計入覆蓋**)。**mutant 與「斷言互相掩護」的
  完整紀錄寫在該檔檔頭**,不在這裡——它是那份量測的家。
- ⚠️ **`.mp-lessons__head` 的 `@media (max-width:359px){flex-wrap:wrap}` 是上面兩條
  nowrap 造成的新溢出的洩壓閥**,不是原缺陷的一部分:不再可壓縮之後,zh 在 320px
  會溢出 23px。斷點**刻意不用站上的 720px**——開了 wrap 之後標題會以 max-content
  參與斷行,375/390 那些**本來排得下**的一列會被推成兩行(列高 42→61px 實測),
  而 owner 就是在那些寬度看座艙。
- 🔴 **已知缺口:上面每一個數字都是 zh 量的,en 不一樣。** 英文在 **360–380px 仍會
  溢出**(實測 headOver 22 @360、7 @375、0 @390),因為 `nowrap` 對拉丁 min-content
  沒有作用。**那是既有缺陷、不是這包造成的**——pre-fix 樹量到**逐位元組相同**的 en
  數字,而 <360 那段本包反而把 en 修好了(320: headOver 62→0)。把閥門放寬到涵蓋 en
  會把 wrap 推進 360–390,正是那個斷點存在的理由要避開的範圍 ⇒ **是取捨,不是遺漏。**
  理由與量測寫在 `member-detail.css` 那段 KNOWN GAP。

## 浮層寬度不可用 `vw` 夾(T-49fb)
`100vw` 從**視窗左緣**起算。一個 `position: absolute` 的浮層若不是從視窗左緣長出來
(幾乎都不是——它從卡片內緣起算),`width: min(Xpx, calc(100vw - g))` 就是**錯的座標
系**:它夾住了寬度,卻沒有把浮層自己的左偏移算進去,右緣照樣可以出界。T-2ca0 就是
這樣留下 375px 溢出 +2px 的尾巴。
- 正確作法:讓**兩個橫向邊界都由容器給**——`left: 0; right: 0; width: auto`,再用
  `max-width` 收上限。可用寬 = 容器寬,右緣**構造上**等於容器右內緣;容器在視窗內,
  浮層就一定在視窗內,與視窗寬無關。over-constrained 時 LTR 忽略 `right`,靠左展開
  的行為不變。
- 量測紀律:**量會溢出的元素自己**,別量它的 flex 父容器 rect(父容器 rect 常被壓回
  視窗寬,看起來沒事)。逐層比 `scrollWidth - clientWidth`,溢出停在哪一層,兇手就在
  那一層裡面。
- ⚠️ **`documentElement` 沒溢出不代表沒 bug**:任何祖先只要有 `overflow-y: auto`
  (CSS 規定 `overflow-x` 跟著變 `auto`),就會把溢出**吸進自己的橫向捲軸**。任務頁的
  `.tasks` 正是如此——owner 看到的「整頁左右滑」其實是 `.tasks` 在滑。斷言要同時涵蓋
  `documentElement` 與那個 scroll container。
- ⚠️ CT 護欄**必須重現真實祖先鏈**(`.app__main` 的 22px padding 等)。裸掛一張卡片
  會多出 ~22px 餘裕,溢出就消失——`artifacts-badge.ct.spec.tsx` 舊的 390px 斷言就是
  這樣一路綠著,卻沒攔到 owner 手機上的 bug。見
  `stories/TaskArtifactsOverflowStory.tsx`。

## column flex 的 `align-items: flex-start` 會讓子元素被自己最寬的內容綁架(T-4aa0)

`flex-direction: column` 下,`align-items` 決定的是子元素的**寬度**。設成 `flex-start`
就是叫每個子元素用 **fit-content** ——而 fit-content 的下限是 **min-content**。
於是只要子孫裡有一個不肯縮的東西(`<pre>` 的 `white-space: pre`、寬表格),整個子元素
就被撐到那個寬度、突破父容器,**而父容器的寬度限制對它無效**。

- 實例:任務卡內文被撐到 479px,遠寬於卡片給得起的寬度,`.tasks` 因此橫滑
  (重現真實祖先鏈後 mutant 量到 +148 @390)。拿掉那個 `align-items` 就好了,
  `<pre>` 也回到自己內部捲動。
  ⚠️ 數字要註明**是在哪一條祖先鏈上量的**:同一個 bug 裸掛只有 +104,補上
  `.app__main` 的 22px padding 才是 +148。**裸掛量到的數字比真畫面鬆。**
- 🔴 **`min-width: 0` 在這裡沒有用**(子元素、任一祖先、整條鏈都試過,實測無變化)——
  那條規則治的是 flex item 的**自動最小尺寸**,而這裡壞的是 **cross size**。兩者不同。
- 🔴 **不要從「哪一段看起來爆出去」推兇手**:視覺上最明顯的那段(有底色、有左線的
  引用區塊)溢出是 0,它只是填滿了被別人撐開的容器。**逐一隱藏子元素、看誰不見了
  溢出就消失**,那是因果判定;看外觀是猜。
- 這類容器若曾經放過「不該被拉寬」的按鈕,`flex-start` 可能是那時留下的。按鈕移除後
  沒人回來收,它就變成一顆等著被踩的地雷。

## 用了哪份 CSS 的 class,就要自己 import 那份 CSS(T-7526)
`machine-picker.css` 全 repo 只有 `MachinePicker.tsx` 一個 importer,而它只透過
`WorkerDetailPanel → useRelocateMachine → MachinePicker` 進入 module graph。**兩個詳情面板
都用 `.machine-picker*` 畫自己的設定 dialog,卻都沒有 import 那份 CSS** —— 一直在搭那條
transitive import 的便車。外包面板不再驅動那個 hook 的那一刻,最後一個 production importer
跟著消失,**兩邊的 dialog 全部變成無樣式**:沒有置中的框、沒有暗底,機器欄變成瀏覽器原生
`<select>`。**連沒被那次改動碰到的正職面板也一起壞了。**
🔴 **沒有任何自動檢查抓得到**:jsdom 不算 CSS,所以整套 vitest 全綠;`tsc` 看不出 class
字串和 stylesheet 的關係;唯一render 過 machine picker 的 CT guard 又在同一張票裡退場。
**它是靠人看截圖發現的。**
⇒ 護欄 `src/components/styleOwnership.test.ts`:某個元件的 markup 用到 `<block>__*`,
就必須自己 `import "./<block>.css"`。**樣式的所有權跟著 class 名字走,不跟著 transitive
import 的偶然走。**

## lazy fetch:別把 inline arrow 放進 effect deps(T-7526)
`AgentDetailPanel` 的初始 PROMPT 卡曾經**永遠停在「載入中…」**,而且關掉重開救不回來。
兩個成因缺一不可,修的時候也必須兩個都修:
- **不穩定的 deps**:`vm.prompt.fetch` 在兩個 wrapper 都是**每次 render 重建的 inline
  arrow**(正職是 `async () => (await api.getBootstrap(member.role)).context`,外包是
  OfficePage 重建的 `onFetchBootContext`)。它一進 deps,**任何一次重繪**(一個 SSE
  delta 就夠)就把 effect 拆掉,cleanup 的 `alive = false` 讓 `.then` 與 `.catch`
  **兩條都寫不了 state**。⇒ 讀取函式走 **ref**,deps 只留真正該重讀的東西(換一個
  agent = `cacheKey`);重繪不是重讀的理由,也不是取消的理由。
- **在「開始讀」時就蓋已載入章**:`loadedKeyRef` 原本在 fetch **啟動**時就寫,所以
  effect 重跑一律早退——收合再展開也早退。⇒ **只在文字真的到手時蓋章**,in-flight 另
  用一個 ref 擋重複發射;過期與否**比對 key**,不用會被重繪翻掉的 `alive` flag。
- **失敗要說失敗**:`.catch` 必須落到錯誤態 + 一顆重試鈕(`*-prompt-error` /
  `*-prompt-retry`),停在「載入中…」會被讀成「還在跑」而且無處可按。
⚠️ **測試要「讀到一半觸發重繪」才看得到這個病**:render 一次就斷言的測試對它完全是盲的
(它就是這樣上線這麼久沒被抓到)。而 `rerender` **必須傳一個新的 element**——傳同一個
element 物件 React 會 bail out、根本不重繪,測試會對著沒修的碼變綠。
護欄:`MemberDetailPanel.initial-prompt.test.tsx` + `WorkerDetailPanel.test.tsx`
的 initial-prompt 段。**同一段程式,但兩個 wrapper 各自證明三件事**(重繪中、失敗重試、
收合再展開)——它們的 `vm.prompt.fetch` 是兩條不同的 arrow(正職在 vm 物件字面量裡、
外包經 OfficePage 的 prop),只證一邊等於沒證另一邊那條線。
mutant 紀錄:`docs/design/worker-panel-parity-mutants.md`。

## 兩個詳情面板的動作列 = 一個形狀(T-7526, owner 2026-07-31)
身分卡右上角**永遠是一列**:`.mp-identity__actions`(column,只裝 `DispatchAlert`)
裡面包一個 `.mp-identity__buttons`(row) —— **更改 ＋ 停止** 並排,沒在跑的時候整列收成
一顆 **喚醒**。正職外包同一份 CSS、同一個順序(更改在前)。
- ⛔ **改這一列的 flex 方向時,≤720px 的 media query 要一起想**。舊規則是為了撐開一個
  **column**;原封不動套在 row 上,`justify-content: flex-end` 會讓兩顆鍵擠在右邊界
  ——「東西還在、但擺錯了」,而且**每一條「同一列/沒溢出」的斷言都還是綠的**。
  護欄因此量的是**跨距與均分**,不是存在性:`visual-guards/identity-actions-row.ct.spec.tsx`。
- **喚醒 = 先開設定再送,兩邊都是**。外包的喚醒以前是 `POST …/restart` 直接派工、什麼都不問;
  現在開的是與 更改 **同一份** dialog,四格(執行環境/模型/投入度/機器)預設成**它原本的值**、
  **且都可以改**。落地順序 `/model` → `/relocate`(機器有改才打) → `/restart`,**全是既有端點**。
  ⚠️ `/restart` **不吃 machine_id**,所以釘選只能由 relocate 寫 —— 這是外包與正職(它的
  `activate(machineId)` 自己帶機器)唯一的形狀差異,別把兩邊的順序抄來抄去。
- 🔴 **釘住的機器只是「睡著」時,seed 一律逐字保留那一台**,不准 fallback「第一台線上的機器」。
  否則 `machineChanged` 對一個停在睡著機器上的 agent **恆為 true**,開設定只想改模型的人會被
  默默搬走。兩個面板的 `openSettings` 都有這條,**改一邊要改兩邊**。
  ⚠️ 測這一條時 fixture **必須有一台線上機器**,否則那個 mutant 無處可去、測試在壞碼上照樣綠。
- **外包沒有「只儲存,不喚醒」,而且這是刻意不對齊**:正職的「只儲存」是 PATCH ＋
  placement-only relocate,都不啟動;外包的 relocate **會 kill + re-dispatch**(除非
  `desired_state` 已是 offline),所以那顆鍵對外包會是假話。要有它得新增 pin-only 端點＝動凍結 wire。
- **外包沒有「狀態」卡**(owner:「外包為什麼需要工作狀態這個UI介面」):五個狀態字裡四個是
  `LifecycleDot` 的複述,「已釋放」由聊天室橫幅承擔。**但離線原因(`worker-detail-stuck-reason`)
  留著**,搬到那顆點下面 —— 它不是狀態字的複述,而且 `最近操作` 卡是 `hasLastOp` gated 的,
  「一次都沒派出去」正好是它不渲染的情況。
- **「重啟」這個字已退場**,兩邊一律 `t.lifecycle.action.spawn`＝「喚醒」。
  ⛔ **REST 路徑仍是 `/restart`**(凍結 wire),`api.restartWorker` 也不改名 —— 退場的是字,不是契約。
- 🔴 **「已結案(released)」由身分那一層講,兩個入口同一句話**(owner 2026-07-31:
  「為什麼從不同進入頁面會有不同的顯示方式?不是應該要一致嗎」)。released worker 被 server
  從 LIVE 名單濾掉,所以只剩**兩個**入口看得到它:**聊天室**與**直接開的詳情面板**。
  - 文案只有一個家:`office.outsource.releasedTitle` / `releasedSub`。
    ⛔ **不准為某一邊加第二份字串** —— 舊名字是 `releasedChatSub`,那個 `Chat` 就是病灶。
    措辭必須**與入口無關**(「以下為歷史對話」對面板是假話);**沒有測試會擋措辭,只擋副本**。
  - 判 released 一律看 `worker.status === "released"`。
    ⛔ **不要動 `presenceVisual` 的五態 no-default switch**:`presence` 對 released 與
    對「從沒派工過」**都是 `undefined`**,那顆點分不出來,而拓寬它會波及正職 roster。
  - released 面板**不畫共用卡片、不留任何生命週期按鍵**:server 對 released worker 的
    `/stop` `/restart` `/model` `/relocate` `/refocus` **一律 404**,留著就是 dead affordance;
    八張全是 dash 的卡也只是把那句話埋了。
  - 測這一條要有**真的 released fixture** ＋ **一條 offline(非 released)對照**,
    否則只證明了「有字」,沒證明「分得出來」。
mutant 紀錄:`docs/design/worker-panel-parity-mutants.md` 第五、六批。
