---
paths:
  - "src/**/*.css"
  - "src/styles/**"
  - "src/components/**"
  - "visual-guards/**"
---

# CSS / 版面陷阱

## 文字與邊界

自由文字與 markdown 的基底 .doc-md 要用 overflow-wrap: anywhere，讓長 URL、sha 與無空白 token 能收縮 min-content。沒有 markdown 繼承的自由文字欄位要自己宣告；.doc-md pre 與 table 是明確允許的橫向 scroll 子區，修整頁面 overflow 時不可把它們的 overflow-x: auto 拿掉。守衛要同時量頁面與實際 scroll container，並同時確認 pre/table 仍能滾。

固定高度、可收縮 flex item、CJK 標籤同時出現時，用 white-space: nowrap 保住單行；不要用 flex:none 代替，也不要只看 computed property。≤359px 的既有 header wrap 是洩壓閥，別擴大斷點到會讓較寬手機多一行的範圍。中文與英文的幾何不可互推。

## 浮層與 CSS ownership

絕對定位浮層不可用以視窗左緣為座標的 vw 夾寬度。讓父容器提供 left:0、right:0、width:auto，再以 max-width 收上限；量浮層自己與中間 scroll container，不要只量被壓回視窗寬的 flex parent。

使用某個 block class 的元件要自己 import 該 block 的 stylesheet；不可依賴 transitive import。styleOwnership test 是防止最後一個間接 importer 消失後整個 dialog 變原生樣式的守衛。

## lazy fetch

lazy prompt 的 fetch function 若由 wrapper inline 建立，不得直接放進 effect deps。用 ref 保存讀取函式，deps 只放真正的 cache key；in-flight 與 loaded key 分開，只有文字成功到手才蓋 loaded key。重繪不能取消仍有效的讀取，失敗要落 error state 並提供 retry。測試要在讀取途中用新 element 觸發 rerender、覆蓋成功、失敗重試與收合再展開。

## 詳情面板動作列

正職與外包詳情面板共用 .mp-identity__actions 的 column 外殼與 row buttons；更改在前、停止在後，沒在跑時只顯示喚醒。手機 media query 要按 row 形狀重新驗跨距與均分，不能只驗「元素仍存在」。

喚醒先開與更改相同的設定 dialog，預設保留原執行環境、模型、投入度與已釘機器；落地順序是 model、必要時 relocate、restart。restart 不吃 machine_id。睡著的已釘機器不能 fallback 到第一台線上機器。

正職可只儲存；外包 relocate 會 kill + re-dispatch，除非 desired_state 已 offline，所以不可把兩者 UI 強行對齊。released worker 的身分文字與入口共用，依 worker.status 判定；released 不畫生命週期卡或 dead action，offline 對照仍要保留。
