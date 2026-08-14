---
paths:
  - "src/components/MarkdownPreviewOverlay*"
  - "src/components/AttachmentStrip*"
  - "src/components/ComposerAttachmentPreview*"
  - "src/components/md-preview.css"
  - "src/components/ConfirmModal*"
  - "src/components/*Modal*"
  - "src/components/*Popover*"
  - "src/components/DocCard*"
  - "src/components/BootDocPage*"
  - "src/components/SettingsPage*"
  - "src/components/DiffView*"
  - "src/components/diff-view.css"
  - "src/components/DocumentHistory*"
  - "src/lib/escapeLayers.ts"
  - "src/lib/useEscapeLayer.ts"
  - "src/lib/escapeLayerOwnership.test.ts"
  - "src/lib/lineDiff.ts"
  - "src/lib/wordDiff.ts"
  - "src/lib/shareLink.ts"
  - "visual-guards/image-zoom-pan.ct.spec.tsx"
---

# 全幅閱覽 overlay、Esc 分層、DocCard、差異呈現

> 本檔由 `frontend/CLAUDE.md` 拆出(T-9b5d)。`paths:` 的 glob **相對 `frontend/`**(rules 檔所在目錄),所以一律寫成 `src/…`;**不要寫 `frontend/…`**——那種寫法在這個位置永遠不命中(實測 89 條對 565 個真檔零命中,已整批刪除)。
> 標 📎 的段落表示「當時的量測證據已搬回該票」,本檔只留規則。

## 全幅閱覽 = 一個 overlay、三種來源(owner 2026-07-28;T-f014 收編圖片)

`MarkdownPreviewOverlay` 是**唯一**的座艙內全幅面 —— markdown **與圖片都算**,三個入口共用,
props 是 discriminated union(`url`+`attachmentId` / `source` / `imageSrc` 三者互斥),傳兩個是
compile error;`url` 少了 `attachmentId` 也是。
📎 **一次性 repro 的像素/百分比讀數、CT 斷言逐條清單、mutant 紅了幾條,已搬回
T-f014 / T-7e68 / T-043e 三張票**;這裡只留規則與方法。

- **`url`**:已存檔的 .md 附件(T-a1c4),overlay 自己 fetch,header 保留「下載」**與複製分享
  連結**;分享連結永遠用 blob 自己的 `att-` id 去 mint,不是 serve path、也不是 `ta-` 產物 id
  (T-4fdc)。
- **`source`**:呼叫端**手上已有**的文字(聊天訊息本文)。不 fetch、不進 loading 態,**也沒有
  下載鈕、沒有分享鈕**——那串位元組從來不是檔案,給一個假造的 blob url 是說謊。
- **`imageSrc`**:呼叫端手上已有的**圖片 bytes**(composer 裡還沒送出的 staged 附件,T-f014)。
  那是真的檔案,所以**下載誠實保留**;但還沒上傳、沒有 blob id,所以**沒有分享鈕**。

**T-f014:舊的 `Lightbox`(`chat__lightbox*`)已退役、連同樣式整塊刪除。** 座艙裡**只剩這一個**
全尺寸看圖面。退役前的實況比票面描述更糟:`AttachmentStrip` 早就**不讀** `onOpenImage` 了,
於是五個 call site 一邊把 handler 傳進一個忽略它的元件、一邊掛著一個永遠打不開的第二層
overlay,而**沒有任何東西是紅的**——沒用到的 prop 與到不了的元件都完全通過型別檢查。
守衛:`bin/tests/lightbox-retired-guard.sh`。

**放大必須改變 layout,不能只改 transform(T-7e68)**:縮放住在 `<img>` 的 `transform: scale()`
裡**只把像素畫大、layout box 原地不動**,於是 `.md-preview__image-wrap` 的 `overflow: auto`
永遠沒有可捲內容。⚠️ **「CSS 寫著 overflow:auto」不等於「使用者捲得動」**,別再從屬性推論可達性。
- 🔴 **先別試 `transform-origin`**:**實測不通**——transform 畫出來的溢出**不進入祖先捲動容器的
  scrollable region**,`scrollWidth - clientWidth` 仍是 0。縮放必須是真的 layout。**這是花實驗
  換到的負面結論,別再推導一次。**
- **正解:縮放 = 圖片自己的 `width`/`height`**(量到的 100% fit box × zoom),**同時把 stylesheet
  的百分比上限關掉**(inline `maxWidth/maxHeight: none`)——那兩條 cap 是「包住它的盒子」的
  百分比,留著就會讓圖再放大一次,readout 直接說謊。**100% 時不下 inline 尺寸**——那正是
  `measureFit` 讀 fit box 的狀態,釘死等於凍結第一次量測。
  ⚠️ 這裡的**結構重整不是必要條件**:前一代那個 stage div 的骨架本來就是對的,最小修法其實只有
  兩行。現行形狀少一層間接、比較好推理,但別把它講成「非重構不可」。
- **resize 要重量 fit box,而且不能只在 100% 量**:兩條 cap 都是 viewport 相對,只在 `zoom === 1`
  掛 resize listener 會讓倍率說謊(舊的 transform 版沒有這個漂移,所以漏掉它是**回歸**、不是
  遺留)。`measureFit` 量之前會**先把 inline 尺寸拔掉**,否則量到的只是 zoom 自己、每次重算都在
  前一次上複利。
- **兩條到得了溢出的路,不是一條**:pointer drag 與原生捲動都驅動**同一個**
  `scrollLeft/scrollTop`,沒有第二份偏移要同步,回到 100% 也就沒有殘留。
- **縮放控制列不可以住在捲動容器裡**:scroll container 的 `position: absolute` 子元素是對它的
  **padding box** 定位的,所以會跟著內容跑,使用者失去把自己帶到那裡的 −/+。因此多一層
  `.md-preview__image-viewport` 當共用定位父層。
- **矮視窗只准有一條捲軸**:frame 的兩條 cap 是 vh,但 frame 拿不到整個 viewport——overlay 的
  inset、panel 邊框、header、body padding 先扣掉。不扣就會讓 `.md-preview__body` 長出**自己那條**
  捲軸躲在 frame 後面。⚠️ **只放寬 `min-height` 不夠**,兩條 cap 都要扣。
- **手勢歸屬按手指數拆兩半,兩半的理由相反**(owner 2026-07-31 要求手機可拉動):
  - **單指歸瀏覽器**:縮放變成真 layout 之後,`.md-preview__image-wrap` 本身就是個可捲容器,
    **單指原生就能拖**(附帶慣性與回彈),背後頁面由 `overscroll-behavior: contain` 擋住。所以
    `onPanPointerDown` 對 `pointerType === "touch"` **早退是刻意的**:再跑一次我們的 drag 會把
    同一段位移**套用兩次**。**兩者只能有一個負責移動**——想「補上觸控支援」而把那個早退刪掉,
    就是把 double-apply 裝回去。
  - 🔴 **雙指歸我們,不再交給 UA(T-043e;owner:「在手機上二指撐開,要放大的是圖片本身,
    頁面不動」)**。交給 UA 時 pinch 縮放的是 **visual viewport**,而這個彈窗是 `position: fixed`
    貼 **layout viewport** ⇒ 放大的是「整個彈窗」而不是照片。**正解是兩件事,缺一不可**:
    (a) frame 上宣告 `touch-action: pan-x pan-y`——**不是** `manipulation`(那個仍把 pinch 交給
    UA);(b) 元件自己接 `touchstart`/`touchmove`(non-passive,雙指才 `preventDefault`)把兩指
    距離比例換成自己的 zoom state。**兩者單獨都不夠**(實測分別是「頁面沒被放大但什麼都沒動」
    與「圖片放大了但頁面照樣被縮放」)。
    - **iOS Safari 另接 WebKit 專屬的 `gesturestart`/`gesturechange`** 當第二條路;兩條路用
      `pinching` flag 互斥——iOS 兩種事件會同時發,兩條都改 zoom 就是套用兩次。
    - 🔴 **絕對不准**用 `user-scalable=no` / `maximum-scale=1` 達成:owner 已明確否決(全站失去
      放大能力是無障礙倒退)。要的是「這個元素把手勢**接管**」,不是「對整個 document **禁用**」。
    - ⚠️ **驗不到的部分**:守衛只有 headless Chromium。**Chromium 綠不等於 iPhone 上會動**——
      WebKit 的 driver 開不出 `gesture*` 事件,真正的驗收在 owner 的手機上。
- **護衛**:`visual-guards/image-zoom-pan.ct.spec.tsx`(真 Chromium,**每一條斷言都量座標**、
  沒有一條靠屬性/class 就能滿足)。故事有**兩種長寬比**——高度受限與寬度受限,**只守前者會整個
  漏掉倍率說謊那條**。
  ⚠️ 寫「某某沒有發生」(例如 `visualViewport.scale` 維持 1)這種斷言前,**先把修補拿掉量一次**
  ——否則你只是寫了一條擋不住任何東西的斷言。
- ⚠️ **寫這類守衛的三個陷阱**(都真的發生過):(a) **方向要選有東西可失去的那一邊**——恆真的
  斷言沒有意義;(b) **不要用固定按鍵次數校準捲動距離**——各引擎每次捲多少不同;(c) 🔴 **也不要
  改成「按到 offset 不再變為止」**——Safari **會吃掉聚焦後的第一次方向鍵**,停在第一次沒動就等於
  在暖身期放棄;而且 WebKit **對鍵盤捲動有動畫**,停在某次沒動可能停在動畫中間。**正解是直接問
  最終問題**:「按方向鍵(有上限)直到那個角落進入可視框」——與引擎無關,而且真的不會捲的實作
  依然過不了。
- **各引擎差異不能外推(實測,Chromium vs WebKit)**:①上面那條鍵盤差異;②**transform 畫出來的
  溢出在 WebKit 是進入捲動區的、在 Chromium 不是**——所以原始那個 bug 在 Safari 上只是「拖不動」,
  在 Chrome 上才是四角全部碰不到。修完之後兩者行為一致;守這類 bug 的護欄必須兩個引擎都跑。

**這個 overlay `createPortal` 到 `document.body`(T-76cd),而那是正確性、不是整潔**。
`z-index: 1100` 只值它「最近的 stacking context 祖先」那麼多:就地 render 時它住在任務
產物彈窗裡,而 `.task-artifacts { z-index: 40 }` 把那個 1100 圈在那顆盒子內 ⇒ 頁首與
分頁列畫在預覽上面、關閉鈕點不到(owner 的 iPhone:「看不到按關閉的按鈕, 被擋住了
且上面的 tab 全部都不能按」)。**每一個宿主都有同樣的曝險,只是還沒發作**——祖先只要有
z-index、opacity、`isolation`,或 `transform`(它連 fixed 的 containing block 都一起
困住),就會再圈一次。portal 讓它的 root stacking context **構造上**就是根,所以沒有
宿主圈得住它、也沒有宿主需要承諾不圈。
- 🔴 **代價是 DOM containment 沒了,而有一個 caller 靠它**:`TaskArtifactsPopover` 的
  click-outside 判定本來白吃這件事——overlay 住在 `anchorRef` 裡,點它的灰底就算
  「在裡面」,產物面板因此不關(owner 2026-07-20:「點其他地方都不會自動關閉,一定要
  點 X」)。portal 之後 `contains()` 對預覽的**每一個點**都是 false,所以那條規則改成
  **按選擇器**認(`closest(".md-preview")`)。**任何用祖先關係推論這個 overlay 的地方
  都要同樣改**。
- ⚠️ **測試面一起變了,而失敗的樣子會誤導**:`render()` 交還的 `container`、CT 的
  `cmp` 都不含這個 overlay,`container.querySelector(".md-preview…")` 回 null、讀起來
  像「overlay 根本沒 render」。jsdom 走 `document.body` / `screen`,CT 走 `page`。
- 護欄:`visual-guards/artifacts-stacking.ct.spec.tsx`(真 Chromium,量
  `elementFromPoint`)。**它守的是兩件互相拉扯的事**——彈窗要壓過**自己那張卡**
  (`z-index: 40` 必須在),預覽要壓過**頁首/分頁列**(不可被祖先圈住)。b59c753 只顧了
  後者(把 z-index 拿掉)就把前者弄壞了:那顆 commit 的成本表量了卡片**外**的四個層、
  卡片**內**一個都沒量,而 owner 截圖裡蓋在面板上的正是同一張卡的身分列、頭像與送出鈕。
  所以那條斷言**列舉、不點名**:走遍 `.task-card` 的每一個後代,與面板矩形有交集的都
  要 hit-test 回面板內部。**⚠️ 射程只有 390×780 那一個寬度**——獨立審查在 768×1024 的
  mutant 下量到 4 個 offender,那個尺寸今天沒有任何護欄守著。

**樣式所有權**:`.md-preview*` 已從 office.css 抽成 `md-preview.css`,由
`MarkdownPreviewOverlay.tsx` **自己 import**。原本它靠頁面的 transitive import 搭便車,正是
T-7526 那個「唯一 importer 一消失、連沒被碰到的畫面也一起壞」的形狀。護欄:
`src/components/styleOwnership.test.ts`。

**單一換行:`source` 開 `breaks`、`url` 不開**(Seth 2026-07-28 review PR #18)。聊天泡泡是
Enter=換行的介面,同一段文字被拿到全幅面讀時如果落回標準 markdown 的 soft-wrap,一則普通多行
訊息就被重排成一條長句;已存檔的 .md 是文件,維持標準 soft-wrap 跟其他文件面一致。這是刻意的
**分家**,兩邊都有測試釘住(改一邊、另一邊會紅)。

**render 一定要戴 `.doc-md`**:overlay 原本只掛自己的 `md-preview__md`,結果標題/程式碼/表格/
callout/連結全落回 UA 預設值——面板是深色主題、內容卻沒上色(owner 2026-07-28 回報)。`.doc-md`
是所有文件面共用的文件皮膚,少戴一個 class 就等於自成一格,別再犯。

**聊天訊息的「放大閱讀」角落鈕**(`.chat__msg-expand`):只長在**對方(incoming)且有本文**的
氣泡上;hover(或鍵盤 focus)才現形,coarse pointer 恆顯低不透明度。⚠️ 位置是**氣泡讓位**
(`.chat__msg-bubble--expandable` 的 padding-right),**不是**浮在文字上:氣泡會 shrink-wrap
內容,單行訊息時浮動鈕會正好蓋掉最後一個字。Slack 那種浮動作法只有在全寬列上才成立。

## Esc 只給最內層:`lib/escapeLayers.ts` + `useEscapeLayer`(T-esc)
每個可關閉的面以前**各自** `window.addEventListener("keydown")`,於是**一次 Esc 被送給
全部的人**,每個人自己判斷該不該關。任務產物彈窗因此得去問「有沒有覆蓋層開著」
(`onPreviewChange` → `attachmentPreviewOpen`),而它問的時候答案**已經被拆掉了**:
DOM listener 依註冊順序觸發,覆蓋層先關掉自己、把 `false` 回報上來,彈窗的 listener
才跑。誰先誰後取決於誰先註冊,所以 `artifacts-badge.ct.spec.tsx` 那條 `.md chip` 守衛
**時綠時紅**。實測(乾淨 worktree,`origin/main` 原碼,n=15):**12 紅 3 綠**,而且
**與負載的相關性是反的** —— 紅落在 load 1.4–17.8、3 次綠都落在 load 17–18。
⚠️ **它的紅是 `mdChip.focus()` 等滿 30 秒逾時**,那個逾時**就是這個 bug**
(彈窗被連帶關掉 ⇒ chip 永遠不會回來),**不是負載造成的假紅、不准調寬逾時**。
- **機制**:全 app 只有**一個** window listener,Esc 交給**最內層**那一層。下面的層
  根本收不到,所以**沒有人需要去問上面有誰**。
- 🔴 **誰在最上層由 DOM 包含關係決定,不是註冊順序**。React 的**子元件 effect 先於
  父元件**,所以巢狀面若與宿主**同一個 commit** mount 就會**先**註冊 —— 用註冊順序排
  會**完全顛倒**(三層巢狀時 Esc 給最外層,最內層永遠收不到)。「巢狀面一定是後續互動
  才開的」**沒有任何東西在維持**,deep-link 就會踩到。註冊順序只留給**互不包含**的兩
  個面(兩個並排 dialog)當 tie-break:後開的在上。
  ⚠️ **產物彈窗 + 預覽這一對自 T-76cd 起就是走 tie-break 那條,不是包含關係那條**:
  overlay portal 到 `document.body` 之後,兩個根節點互不包含,`topLayer()` 因此落到
  「後註冊的在上」。**今天答案仍然對,但不是因為方向是對的——方向其實是倒的**:
  `MarkdownPreviewOverlay` 在 React 樹裡仍是 `ArtifactsPopover` 的**後代**,所以兩者若
  同一個 commit mount,**child 的 effect 先跑 ⇒ 預覽先註冊 ⇒ Esc 反而給彈窗**。今天不
  可達,只是因為預覽的初始 state 是 null、必然晚一個 commit 才出現。**別把它讀成
  「這一對是安全的」——它是「條件還沒湊齊」。⚠️ 這一段是讀碼推論,沒有人構造出可執行
  的反例。** 今天的行為由實測釘住:`artifacts-stacking.ct.spec.tsx` 的
  「Esc closes the preview first and the popover second」(真瀏覽器,兩層都開著按兩次)
  加上 `TaskArtifactsPopover.test.tsx` 既有的兩層用例。
- **用法**:`useEscapeLayer(onEscape, ref)` —— `ref` 指向這個面的根節點,巢狀關係就是
  從它讀的,**會被別的面包住的都要傳**;常駐元件裡的子視窗傳第三個參數 `active`
  (關著就不佔位)。handler 身分每次 render 變都沒關係,**層的位置不會動**。
- **層內要吞掉 Esc 就在 handler 裡判**(`ConfirmModal` 送出中即如此),別讓它漏到下面。
- **element-level 的 Esc 要 `preventDefault()`**:輸入框自己的 `onKeyDown`(`InlineEdit`
  / 角色新增列 / 手冊新增列 / 機器 onboard 列)與 layer **兩個都會跑**,不擋就會「取消
  編輯」**順便**把它所在的那個面也關掉。dispatcher 認 `e.defaultPrevented`,四個
  handler 各自 `preventDefault`。⚠️ 六個 layer **沒有一個真的做 focus trap**(只宣告了
  `aria-modal`),所以按 Tab 是回得到後面的輸入框的 —— 這條不是理論。
- 🔴 護欄的重點是 `lib/escapeLayerOwnership.test.ts`:**只有 `lib/escapeLayers.ts` 可以
  綁 window keydown**。理由是實測的 —— 把彈窗改回舊機制,**整套 1489 條 jsdom 只有這一條
  紅**,而唯一抓得到的 CT 守衛每跑只有約 1/3 機率紅 ⇒ 沒有它,單次 CI 偵測率約 33%。
- 其餘護欄:`lib/escapeLayers.test.ts`(派發規則,含三層同 commit 的顛倒案)、
  `lib/useEscapeLayer.test.tsx`(同 commit 巢狀/掛載/強制卸載/gated/重繪保位)、
  `TaskArtifactsPopover.test.tsx` 的兩層用例、`visual-guards/artifacts-badge.ct.spec.tsx`
  的 `.md chip` 那條(真瀏覽器)。

## 可編輯文件 = 一個外殼 `DocCard`,body 才是可換的那一層(T-c33e)

owner 2026-08-14:「一致性是最重要的 這樣會影響到使用者的觀感」「**我們甚至希望這些
component 是同一個 component reuse,讓他成為 single source of truth**」。

`components/DocCard.tsx` 是**設定頁上可編輯長文件的外殼**:breadcrumb、標題、
`.doc-card__head`(預設徽章 / 字數 / 按鈕組)、版本紀錄入口、超上限擋下、還原出廠版、
儲存確認、錯誤行。它就是以前藏在 `SettingsPage.tsx` 裡**沒有 export 的 `DocDetail`**
——**沒有 export 正是第二份實作長出來的原因**:`BootDocPage` 拿不到它,只好把 markup
抄一份,兩邊就這樣漂成同一張卡的兩個形狀。

- **今天的 caller**:角色定義 / 使用者自訂(`SettingsPage`)、系統互動 / 兩份啟動程序
  (`BootDocPage`,**自己零編輯器狀態**——沒有 draft、沒有 textarea、沒有按鈕組;
  `BootDocPage.test.tsx` 有一條讀原始碼的守衛釘這件事,因為「又長回自己的編輯器」的頁
  照樣 import 得到 `DocCard`,畫面層看不出來)。
- **`InsightCard` / `LessonsCard` / 任務手冊那兩份還沒遷過來**(另一張票,owner 未點頭)。
  外殼**已經接得住它們**(`renderBody` 就是為此存在),但**不要順手遷**。
- 🔴 **新增能力一律是 optional prop,不傳 = 這張卡一直以來的行為**。`errorNote` /
  `confirmSave` / `replaceNote` / `requireDirty` / `renderBody` 全部如此,所以三塊開機
  說明帶進來的東西**不會落到角色定義頭上**。
  對照斷言在 `DocCard.test.tsx`(透過真的 `SettingsPage` 走進去,不是自己組 props)。
  ⚠️ `above` 與 `factoryReset` 這兩個 prop **已經不存在**(見下面那條):`above` 畫的是
  開機說明頂端那三條說明,`factoryReset` 畫的是頂層還原鈕,owner 兩件都收掉之後它們一個
  呼叫端都不剩。**沒有呼叫端的 prop 與到不了的分支,看起來跟活碼一模一樣,而型別檢查與
  測試都不會叫**——所以是跟著同一顆 commit 刪的,不是留著等下一個人。
- 🔴 **超上限那一格是修 bug,不是順手改**。舊 `DocDetail.commit()` 是 `try/finally`
  **沒有 `catch`**:實測(未改動的樹)角色定義草稿 4,000 字對上 1,000 字的上限,完成編輯
  **按得下去**、字數讀數**凍在已存的 310 / 1000**、寫入照送、server 的 400 只變成一個
  unhandled rejection,螢幕上**一個字都沒有**。現在:編輯中讀數跟著**草稿**走、超上限
  在座艙就擋(兩個數字都在)、真的失敗了印 server 原話。**沒傳 `usage` 的文件完全不受
  影響**(使用者自訂真的沒有 cap)。
- **儲存語意沒動,仍是整份取代**;變的是**畫面上要講出來**(`replaceNote`)。三塊開機說明
  的編輯器從逐段換成單一編輯框之後,原本由段落列隱含說出的事必須明講——把一段提案貼到
  45,000 字的文件上再儲存,是靜默且只能靠版本紀錄救回來的。
- **`lib/docSections.ts` 已刪**(連同它的測試):逐段 UI 是它唯一的消費者,沒有 splitter
  也就沒有「split→join 不等長」這條資料損毀風險了。**不要因為那條 round-trip 斷言看起來
  很重要就把檔案留著**——沒有消費者的碼不是防線。
- 🔴 **owner 2026-08-14 收掉了兩樣東西,而兩樣都不要「順手」加回來**:
  1. **頂端那三條說明**(生效範圍 / 保留期數 / 字數上限)。他的理由可一般化:「若這說明
     有必要,每個 context 區塊都得加,而別的都沒有」。他隨後對兩個去處(搬進使用說明、
     儲存後跳提醒視窗)回了**「先不用」** ⇒ **只移除、什麼都不補**。
  2. **頂層的還原出廠版**(卡 `rc-f1950f4d286e` 選②「完全照 insight」)⇒ 還原只剩編輯
     模式裡版本紀錄的**初始版本**那一列。**代價他知道且選了**:讀取失敗 ⇒ 進不了編輯
     模式 ⇒ 那頁沒有任何救援出口,而這頁的失敗本來就是靜默的。**這是知情取捨,不是遺漏。**
  隨之整族退場:`above` / `factoryReset` 兩個 prop、`ConfirmModal` 的 reset 分支、
  **`boot-doc.css` 整份**(唯一 block `.boot-doc__notes` 沒人畫了)、`.doc-card__recover`
  兩條規則、6 個孤兒 i18n 葉子。
- **樣式**:`.doc-card*` 的家是 `settings.css`(含 T-c33e 新增的 `.doc-card__note`);
  **`boot-doc.css` 已不存在**,`BootDocPage` 只 import `settings.css`。
- **真瀏覽器守衛換人不換題,但其中一題縮水了**:
  `visual-guards/boot-doc-section-row.ct.spec.tsx` → `boot-doc-card.ct.spec.tsx`。
  它原本量的「**救援鈕必須在畫面內**」是量在頂層還原鈕上,那顆鈕沒了 ⇒ 現在量的是
  **同一個幾何主張搬到還原真正住的地方**:編輯模式 → 版本紀錄入口 → 初始版本那一列,
  每一格在手機寬度下都要按得到。**這比退休的那條弱,弱掉的正好是 owner 選擇放棄的那一段。**
  `boot-doc-real-seed.ct.spec.tsx` 留在原地,量的還是真 seed 的整條 ancestor chain。
  兩支的鑑別力現在都來自 `.doc-md` 的 `overflow-wrap: anywhere`(實測 mutant:card 那支
  320/375 紅、real-seed 那支 320 紅 +45px)。

## 差異呈現:三層各自只負責一件事(T-40f0;全文 `docs/design/T-40f0-history-diff-ux.md`)

`lib/lineDiff.ts`(**哪幾行**不同)→ `lib/wordDiff.ts`(一對取代列之間**哪些字**動了)→
`components/DiffView.tsx`(怎麼畫)。**只有第三層知道 owner 挑了什麼樣式**,前兩層是純函數。
- 🔴 **`lineDiff` 是行結構的唯一權威,`wordDiff` 不准碰到它的產出**:拿掉 `wordDiff`,差異照樣
  畫得出來、列與行號一個都不會變,只是少了字級底色。要加字級規則就改 `wordDiff`,別下沉到 `lineDiff`。
- 🔴 **摺疊只在呈現層退場,不是在 `lineDiff` 裡拿掉**(owner:「showing entire content」):
  `DiffView` **渲染 `result.rows`(恆為完整 edit script)、從不讀 `result.hunks`**,並且
  **不再渲染 `@@` 分隔列**;`lineDiff` 的 `collapse()` 仍是該模組自己的 API、仍有測試。
  `DiffView` 的 `options` 因此窄成自有的 `DiffViewOptions`(只剩 `maxLines`)——留著
  `collapseUnchanged` 這個 knob 等於在介面上廣告一個這個面拒絕擁有的行為。
  ⚠️ **「固定送 `collapseUnchanged: false`」不是這條的執行機制,別把它讀成保證**
  (本檔上一版就是這麼寫的):`collapseUnchanged` 只塑形 `hunks`,而這個面**一個
  consumer 都沒有** ⇒ 把那個引數翻成 `true`,渲染出來的列一行都不會變。**實測**:翻成
  `true` → `DiffView.test.tsx` **15 條全綠**;改成渲染 `hunks.flatMap(h => h.rows)`
  → 「no collapse separator」那條**立刻紅**。要找那條不准動的線,看 render 裡的
  `result.rows`,不是那個引數。
- **字級標亮有兩個刻意的「不標」**,兩者都不是缺口:①兩行**毫無共同 token** 時整行不標(整列的顏色
  已經說完「這行被整個換掉」,再逐 token 標會讓它與「只改幾個字」長得一樣);②每側 **400 token 上限**
  (token LCS 是 O(n·m)、每對變更列各跑一次,一行 base64 只能少一層底色、不能讓 tab 卡死)。
  ⚠️ 相似度**只數非空白 token**——`"  "` 對上 `"  "` 在任何兩行之間都成立,算進去就會把兩行無關的
  縮排文字整片標亮。
- 🔴 **兩種「看起來空白」的狀態永不合流**:`diff-view-empty`(兩版相同)與 `diff-view-too-large`
  (太大、拒絕比對且**報出行數**)。兩條測試各自**同時斷言另一個不在場**,所以「拒絕」不可能偽裝成
  「沒有差異」。**不要為了少一個分支把它們併成一個空面板。**
- **顏色的斷言在真引擎**:jsdom 把 `color-mix(...)` 原樣吐回,兩個 kind 指到同一個 token 在 jsdom
  是綠的 —— 那一半由 `visual-guards/diff-view.ct.spec.tsx` 守;jsdom 那邊守的是「tint 掛在 `<tr>` 上、
  兩個行號格與 marker 格都在它裡面」(整列上色的結構條件)。
