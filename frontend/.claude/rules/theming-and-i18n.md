---
paths:
  - "src/i18n/**"
  - "frontend/src/i18n/**"
  - "src/paint/**"
  - "frontend/src/paint/**"
  - "paint-guards/**"
  - "frontend/paint-guards/**"
  - "scripts/**"
  - "frontend/scripts/**"
  - "src/lib/theme*"
  - "frontend/src/lib/theme*"
  - "src/lib/paint*"
  - "frontend/src/lib/paint*"
  - "src/lib/imageCap*"
  - "frontend/src/lib/imageCap*"
  - "src/components/ThemeSettings*"
  - "frontend/src/components/ThemeSettings*"
  - "src/components/theme-settings.css"
  - "frontend/src/components/theme-settings.css"
  - "src/components/FirstRunPage*"
  - "frontend/src/components/FirstRunPage*"
  - "src/components/LoginPage*"
  - "frontend/src/components/LoginPage*"
  - "src/components/ProfileDropdown*"
  - "frontend/src/components/ProfileDropdown*"
  - "src/AuthGate.tsx"
  - "frontend/src/AuthGate.tsx"
  - "src/api/auth.ts"
  - "frontend/src/api/auth.ts"
  - "src/styles/**"
  - "frontend/src/styles/**"
---

# 首設密碼 / 伺服器設定、i18n 可覆寫文案、主題包、用詞清單、pre-paint 守衛

> 本檔由 `frontend/CLAUDE.md` 拆出(T-9b5d)。`paths:` 的 glob **相對 `frontend/`**(rules 檔所在目錄),另外並列一組 `frontend/` 開頭的同義 glob 當保險。
> 標 📎 的段落表示「當時的量測證據已搬回該票」,本檔只留規則。

## 首設密碼 + 伺服器設定(B3)
- **AuthGate 四態牆**(real mode only):有 token → App;無 token → 打 PUBLIC `GET /api/auth/status` 一次 → 未設密碼 = `FirstRunPage`(啟用碼 + 設密碼,POST set-password 成功即存 token 直接進 App;啟用碼從 `?code=` query 預填——server 首跑自動開的就是這條 URL,預填時 autoFocus 落密碼欄、code 讀到即 history.replaceState 從網址列抹掉)、已設 = `LoginPage`。mock mode 永不出牆(照舊直接進辦公室)。
- **ProfileDropdown 三 view**:main → preferences(主題/語言 + **伺服器設定**:登入有效期下拉 12h/24h/7d/30d、自動換手門檻 40–90%,經 api seam `getServerSettings`/`patchServerSettings` 即時生效)→ password(改密碼)。設定載入失敗 = 誠實不渲染該區塊。
- **⚠️ 密碼端點不走 openapi-fetch client**:client middleware 把任何 401 變成 clear-token + `oc-auth-expired`(登出彈跳)——打錯「目前密碼」/claim token 必須是 inline 表單錯誤,所以 `setPassword`/`changePassword` 走 http.ts 的 `credentialPost` 裸 fetch(丟同款 `ApiError`),成功後 `setToken` 換上 server 新發的 token(change-password 會撤銷所有舊 owner session)。settings GET/PATCH 照常走 typed client。

## i18n 語系是封閉聯集(自 `設計 token` 節保留)

i18n 兩語 `zh` / `en`(`Locale` 是封閉聯集,`locales/` 只有這兩份;`xian` 是**佈景主題**不是語系)。其餘設計 token 的字面值讀 `theme.css` / `global.css` 即可,不在本檔重述。

## i18n 帶參數文案 = 可覆寫片段 + `compose.ts`(T-081b)
字典葉子**不再寫 interpolation 函式**。白名單產生器只收字串葉子,所以任何寫成
`` (name) => `終止「${name}」嗎?` `` 的模板,裡面的字對主題包的 `wording` 覆寫是隱形的
——「終止」按鈕換得掉、確認框內文換不掉(owner 2026-07-27 回報的正是這個)。
- **寫法**:字拆成靜態葉子(`terminateConfirmBodyLead` / `…Tail`、`progressLabel`、
  `blockedByLabel` …),組裝收在 `i18n/compose.ts` 的 `makeMessages(t, language)`;
  元件寫 `const { t, msg } = useI18n()` 後叫 `msg.taskTerminateConfirmBody(title)`。
  `msg` 與 `t` 同一個 memo 來源,主題換詞立刻反映。
- **兩種接法**:兩語言空格一致 → `label + " " + 參數`(值裡不留看不見的空白);參數卡在
  句中、中英標點/引號不同 → lead/tail **純串接**,標點寫進片段,兩語言各填各的、不強求對齊。
  只有空格差異用 `sp`(zh 無空格)吸收。多參數(`uninstallWarnBody1/2/3`)在鍵名標順序。
- **查表不要寫成模板**:狀態→顯示字用靜態物件葉子(`mp.effortOf`、`office.presence`,
  同 `tasks.status` / `tasks.priority` 的寫法),成員才會逐條可覆寫。
  (曾經的 `workerDetail.statusOf` 是同族的第三個例子,已隨外包狀態卡一起退場 — T-7526。)
- **護欄**:`i18n/compose.test.ts` 把每一句在 zh/en 的**逐字輸出**釘死——拆片段不准改到
  螢幕上的一個字元;新增一支 composer 沒進表會被 coverage 那條擋下。

## 主題的身分名稱不可被主題包覆寫(T-081b §6)
`themeIdentity.*` 子樹放的是**某個主題自己的 name**(主題下拉的那一列、匯出檔寫進去的
`name`、新建主題的預設名)。`gen-message-keys.mjs` **整個跳過這個子樹**——規則掛在結構上,
不是另外手維一份 key 清單。以後多一個內建主題,名字放進去就自動不可覆寫。
導覽列的「辦公室」是 `nav.office`(場所稱呼),**照舊可換**。護欄:
`i18n/messageKeys.theme-identity.test.ts`。

## 匯入主題包:不認得的用詞代碼 = 警告,不是錯誤(T-081b)
`wording` 覆寫裡不在白名單的 message code **一律丟棄、匯入照樣成功**(owner
2026-07-27:「已匯入的主題包還是要能夠運作,只是不認得的會失效」),但**不准無聲無息**。
- 丟棄的代碼經 `validateWording(wording, where, skipped)` / `validateThemeBundle(…, skipped)`
  的 **out-param 警告通道**回報(跨語言同一代碼只回報一次),`parseImportedBundle`
  再以 `skippedWording: string[]` 交給 UI。**它永遠不是回傳值(錯誤)** —— 真正不合法的
  包(顏色注入、保留 id、非法 token)照舊由回傳值擋下、留在匯入頁顯 `.set-error`。
- UI 面:匯入成功後落回主題列表,列表頂端出 `.set-warn` 黃框
  (`data-testid="theme-import-skipped"`),文案 = `msg.themeImportSkipped(count, sample)`。
  **比例原則**:只點名前 `IMPORT_SKIPPED_SAMPLE`(3)個代碼,其餘由 count 承載、尾巴接
  「等」/「…」,30 個略過也只有一行。
- 已存在的包(匯入時間早於白名單縮減)**不重驗、不清洗**:`applyWording` 的 `setPath`
  只覆寫既有 string leaf,不認得的路徑自然無作用。護欄:`i18n/index.test.tsx`
  「keeps applying an already-stored pack whose overlay holds an unrecognised code」。
- server 端(`wording_bundle.go`)維持同樣的丟棄語意,但**沒有面向使用者的**警告通道:
  PATCH 的回應形狀是凍結 wire(§13),加欄要先過 spec;而且 FE 在送出前已把不認得的
  代碼濾掉,server 這側的丟棄對使用者不可見。要讓 server 也**回應**回報,是另一張票——
  但**對 operator 一定要留痕**:每次丟棄寫一行 server log(bundle 位置+代碼)。
  server 讀取既有 DB 列時也跑同一支裁剪(只裁剪、不拒收),否則舊列會一路被 GET 回顯。
- **spec 是 wire 的 SSOT**:這條「不認得 = 丟棄 + 200 + 裁剪過的 echo」寫在
  `spec/openapi.json` 的 `ThemeBundleDTO.wording` / `SettingsUpdateDTO.custom_themes`,
  行為由 `conformance/test_rest_happy.py` 釘。改這個語意 = 先改 spec 再改碼。

## 主題編輯器不准把作者打的字 trim 掉(T-081b)
`wording` 覆寫的值**逐字存**,只有「是不是空的」那個判斷才 trim。
T-081b 開放的葉子有好幾條是**句子片段**,邊界空白是有意義的
(`monitor.machine.uninstallWarnBody2` = `"」上還有 "`、`…Body3` 開頭是空白),
存之前 trim 會讓產品自己的編輯器產出「上還有3位成員」——編輯器弄壞它剛開放的字串。
護欄:`ThemeSettings.test.tsx`「keeps the boundary spaces of a sentence-fragment override」。

## 用詞清單**整份 866 列都在 DOM 裡**,不做虛擬捲動(T-8115 已由 owner 撤回)

`.ts-wording-list` 有 866 個可覆寫代碼,**全部一次掛上**。這是決定,不是還沒優化。
📎 **A/B 毫秒數、DOM 節點數表、O(N²) 查詢耗時表、jsdom↔CT mutant kill 表已搬回 T-8115 與
T-e2e9 兩張票**;這裡只留規則。

- 🔴 **owner 2026-08-02 親自裁定撤回虛擬捲動**:「這設定根本不常進去 只要不是秒等級根本沒差
  而且通常都是直接匯入」。判準是**不是秒等級就沒差**,加上主題通常直接匯入。**代價是在知情下
  付的,不是沒量過**(真 Chromium A/B 與慢機器節流都量過,數字在票上)。
  ⚠️ **觸及條件比「要改用詞才付」寬**:用詞清單與顏色、字體同在一張表單、**無條件渲染**,
  所以只想改一個顏色的人也付全額。這一點也在裁定時攤開過。
- 🔴 **不准再引入虛擬捲動、視窗化、overscan、或任何「只渲染 N 列」的上限**,除非有新的 owner
  裁定。理由是**三個能力靠「每一列都在文件裡」**,其中兩個是瀏覽器自己的功能、沒有別的方式
  重新提供:
  1. **鍵盤 Tab 與讀屏的循序順序**。虛擬捲動**必須**把持有焦點的那一列留著(卸載持有焦點的
     input,瀏覽器把焦點交還 `<body>`、游標整個消失),而那個「釘住的列」被 render 在視窗
     **之後** ⇒ 從它按 Tab **直接掉出清單**,`aria-posinset` 的循序也不再單調遞增(兩者皆實測
     發生過,不是理論)。
  2. **瀏覽器自己的「查找」(Cmd+F)** 只找得到掛載的文字。⚠️ **探針的關鍵字一定要用顯示文字
     (英文原文),不能用 message code**——列上**不渲染 code**,拿 code 去搜兩邊都是 false,零
     鑑別力(這正是先前一份獨立探針失敗的原因)。⚠️ 而**「面板自己的搜尋框已經取代 Cmd+F」這句
     話是錯的**:那個搜尋框在虛擬捲動之前就存在,**不是為這個損失做的補償**。
  3. **整頁全選複製 / 列印**。全掛載之後「把整份清單倒出來當翻譯工作表交給譯者」又做得到了
     ⇒ **不需要另做「匯出用詞對照表」那顆鈕**(那是虛擬捲動時期的補償方案,已無標的)。
- **仍然是捲動盒**:`.ts-wording-list` 維持 `max-height: 340px; overflow-y: auto`。「全部在 DOM
  裡」講的是**文件**,不是**畫面**——沒有這個 cap,面板會長到整組代碼的高度、把「取消」推出畫面。
- **列距寫在 `.ts-wording-row` 自己的 padding,不用 flex gap**。這原本是虛擬捲動的約束(spacer
  以整數 row pitch 算高);虛擬捲動走了,但**列與列之間**逐像素等價。⚠️ **只有「列間」等價**:
  首列上緣與末列下緣各差 2px。結論不變——**改回去只會讓每一列都動、換不到任何東西**。
- **`aria-setsize` / `aria-posinset` 仍然明寫**,不交給 AT 自己數:它們是「第 431 項,共 866 項」
  為真的來源,而且**搜尋過後**要跟著當前結果集的序號走。
- ⚠️ **`resetWordingScroll()` 只剩一件事**:換搜尋字時把真實元素的 `scrollTop` 歸零。它**不再有**
  window state 要重設。

### 還剩下的、以及被撤回的成本紀錄

- 🔴 **那個測試逾時的根因是查詢、不是 render,而且差三個數量級(T-e2e9,owner 裁定
  `rc-cf2a2982f31d` 選①)。`EDIT_VIEW_TIMEOUT_MS` 已整個刪除**,12 條 `it()` 全部回到 vitest 預設。
  機制:`dom-testing-library` 的 label 查詢對每個 labelable 元素讀 `input.labels`,jsdom 每次都
  重走整份 document ⇒ **O(N²)**;真正 render 866 個 input 只佔一小部分成本。
  🔴 **放大門檻被試過而且不夠,別再往上加。**
  ⚠️ **`it()` 報的 duration 含 hooks,`testTimeout` 只綁 body**,所以報出來的數字是 body 的
  **上界**,別拿它直接減預設值講餘裕。
  ⚠️ 另一個會誤導的觀察:**耗時更久的跑反而通過、較短的跑失敗**——逾時只在 `await` 邊界檢查,
  同步查詢不會被打斷,所以過不過取決於檢查落在哪個 await,與總耗時無關。**這就是它看起來隨機
  的機制。**
  ⇒ **不要新增「在 866 列都掛著時對整個 container 跑 `getByLabelText` / `getByRole`」的查詢**。
  現行寫法一律**先縮到一個小容器**(`ThemeSettings.test.tsx` 檔頭的 `colourRow` /
  `canvasBgSlots` / `formActions` 三個 helper,以及 `within(row).getByRole("textbox")`)或直接
  `querySelector('[data-wording-code=…]')`。照直覺改成整頁查詢就會把逾時招回來。
  🔴 **縮小 scope,不要改用 id 定位。** `*ByLabelText` 除了找到元素,**本身就在證明 input 與它的
  label 真的綁在一起**(讀屏軟體念得出欄位名靠的就是這個);換成 `getElementById` 會快,但 label
  綁錯或掉了測試照樣全綠——而 owner 撤回虛擬捲動換回來的三個能力裡有兩個就是無障礙。唯一的例外是
  `.ts-wording-search`(它的容器就是那 866 列,沒有更小的 scope),那一處改成**斷言** `aria-label`。
  ⚠️ **這是測試環境成本,不是使用者成本**,別拿這些數字當「拿掉虛擬捲動讓產品變慢」的證據。
- 🔴 **舊文那個 jsdom 專屬的加速比數字,永遠不准拿去講使用者體感**——這條規則跟虛擬捲動一起留著,
  因為它是關於怎麼引用數字,不是關於實作。
- ⛔ **已作廢、不要再引用的舊記載**:本檔上一版把「釘住的列 Tab 會掉出清單、讀屏序號跳回第 1 項」
  記成**「已知、刻意接受的缺口」**,也把 Cmd+F 與整頁複製列印記成刻意接受的缺口。**那個立場已經
  不存在了,三個缺口都隨虛擬捲動一起消失。** 留這句是因為錯的事實會長腳:看到任何地方還寫著
  「這個缺口存在」或「我們決定不修」,那是舊文,直接改掉。

### 護欄兩層,各答一半

- **`src/components/ThemeSettings.test.tsx`「wording list is browsable in full」四條**(jsdom 看得到
  的那半:文件裡有什麼、DOM 順序):(a) 866 個代碼**全部在文件裡**;(b) 搜尋結果一個不少;
  (c) 打字不准把該列移走;(d) **捲走之後整組還在、`aria-posinset` 單調遞增、焦點還在同一個元素、
  零 pinned / 零 pad**。
- **`visual-guards/wording-list-full.ct.spec.tsx`(真 Chromium)四條** ——⚠️ 檔名從
  `wording-list-window.ct.spec.tsx` 改過來了,因為已經沒有 window。它守的是 **jsdom 做不到的那半**:
  jsdom 按不出真的 Tab、也**完全沒有「查找」**。
- 上面那條 (d) **刻意**放在 jsdom。當初的理由是「CT 不在雲端 gate 裡」——⚠️ **那個理由自 T-0fef
  起已經作廢**(`test:ct` 現在跑在雲端自己一格)。**但 (d) 不要搬走**:jsdom 那條問的是「文件裡有
  沒有」,CT 那條問的是「焦點與瀏覽器查找」,兩層答的不是同一題。
  ⚠️ **CT 從來沒有在 Linux 上被量過**,而 T-4d88 起雲端已經沒有 Linux 那一格 ⇒ 這個 repo 對 Linux
  字型堆疊零量測。⚠️ **不要數 job 數量**;判準是「哪一個 job 真的呼叫到 `test:ct`」,對號讀兩處:
  `grep -n 'run-checks' .github/workflows/ci.yml` ＋ `grep -nE '^[a-z][a-z0-9-]*:' Makefile`。
- 🔴 **四條 CT 裡有一條是「不可靠、負載相依」的偵測器,不要把它算進覆蓋**:「Tab walks from row to
  row」——**單獨跑通常綠、並行負載下真的會紅**。⚠️ **把它讀成「間歇」,不要讀成某個比率**(n=5
  撐不起一個數字)。機制:overscan 本來就是為了讓循序走訪能動,但**機器較忙時視窗跟不上焦點驅動的
  捲動**。⚠️ **在 HEAD 上它是確定性綠**,所以那個紅是 mutant 專屬的,不是這條分支引入的 flake。
  **真正擋住「虛擬捲動回來」的是另外三條**;它留著的正當理由是**對硬上限確定性有效**。
  🔴 **本檔上一版把它寫成「零鑑別力/照樣綠」,那是錯的,而且錯的方向是低估自己的測試**——寫錯會讓
  下一個人把一個**真的間歇紅**當成「本來就這樣」,或反過來去追一個不存在的 bug。
- ⚠️ **一個量測陷阱**(實測踩到):`el.scrollTop = …` 之後**立刻**用 `page.evaluate` 讀 DOM,讀到的
  是 **React 還沒重繪的舊樹**。要嘛用會自動重試的 `expect(locator)`,要嘛先等一個能證明重繪發生的
  斷言。一次性探針特別容易寫出這種假綠。

## 匯出不烘 alias 預設值(T-081b)
theme.css 裡定義值是裸 `var(--other)` 的 token(分區三槽)是「跟隨」不是顏色。
`getComputedStyle` 會把它解析掉,所以匯出/播種前必須先跳過**還坐在 alias 上**的槽,
否則每個新主題的分區都被釘死在內建色。名單由 `gen-theme-tokens.mjs` 從 theme.css 推導
(`THEME_ALIAS_DEFAULT_TOKENS`),不是手寫三個名字。護欄:`lib/themeExport.test.ts`。

## 最外層畫布可吃背景圖(T-081b,rc-1e78b3b19082 選項 2)
主題包新增 optional `backgrounds: { canvas: "data:image/…;base64,…" }`(spec 已凍結入
`openapi.json` 的 ThemeBundleDTO)。**只有最外層畫布**(內容欄兩側那塊)有這一槽。
- **為何是 zone map 而不是像 `logo` 的裸字串**:key 就是「哪一區」,把「只有畫布能吃圖」
  這條規則寫進結構——`backgrounds.topbar` 會被具名擋下(422 / `only canvas`),不是
  靠註解約定。頂列/頁籤列/內容區底下都坐著文字,**文字壓在花紋上沒有可讀性保證**,
  所以不開放,不要「順手」加進 key set。
- **圖片驗證的「安全」那半原封不動重用頭像那道閘**:同一份 mime 白名單
  (PNG/JPEG/WEBP,SVG 永遠拒)、同一組 magic byte、同一套嚴格 base64 字母集。
  **這半永遠不准為背景放寬**,它跟大小完全正交。
- 🔴 **但「多大」那半已經分家了(T-72da,owner 2026-08-03)。本檔上一版寫著
  「不准為背景另開一套規則、不准調高 cap」——那條裁定已經被 owner 自己推翻,
  不要照著它把兩個數字統一回去。**
  當時那句的前提是**背景與頭像共用同一道 gate**,所以「調高背景」必然等於「調高頭像」。
  這個前提已經不成立:閘的兩個 size cap 現在是**參數**
  (Go `validImageValue(v, maxDecoded, maxValueLen)` / TS `isValidImageValue`),
  兩個 purpose 各有一組 thin wrapper。
  - **頭像 / logo / 導覽圖示 = 64 KiB**(`maxAvatarBytes` / `MAX_AVATAR_BYTES`),
    字串長度 96 KiB。**一個字都沒動**,30–40px 的小圓圖不需要更多。
  - **背景圖 = 512 KiB**(`maxBackgroundBytes` / `MAX_BACKGROUND_BYTES`),
    字串長度 704 KiB。理由:它是**鋪滿整個視窗**的圖,owner 的實際背景貼在 64 KiB
    上限、他連講三次「太糊」。
  - 🔴 **字串長度那一層一定要跟著動**:它跑在 base64 解碼**之前**,留在 96 KiB 的話
    512 KiB 的圖會在那裡就被 `data URI is too long` 擋掉,解碼那層永遠執行不到。
    **只放寬解碼那層 = 完全沒有效果**,這是這件事最容易做半套的地方。
  - **TS/Go 兩側仍是 twin,而且不再只靠註解**:`bin/tests/fixtures/image-cap-cases.tsv`
    是唯一的真相表,兩側各自對它斷言
    (`server/ocserverd/image_cap_mirror_test.go` / `frontend/src/lib/imageCap.test.ts`),
    任一側漂掉都會紅**而且訊息點得出是哪一側**。照 `doc-cap-cases.tsv` 的先例做。
  - **座艙那道 UI 閘也要跟著分流**:`ThemeSettings.tsx` 的 `readValidatedImage`
    是四個 picker 共用的,背景那個 picker 要傳 `isValidBackgroundValue`,
    否則會出現「後端說可以、座艙說圖片無效」。
- **色與圖並存**:`global.css` 的 body 改成 `background-color: var(--color-bg)` +
  `background-image: var(--canvas-bg-image, none)` + `background-repeat: repeat`。
  沒有圖 = 完全等同從前的純色;舊主題包(沒有這個欄位)一個像素都不會變。
- **CSS 變數刻意不叫 `--color-*`**:它的值是 `url("data:…")` 不是顏色,掛進顏色契約
  會被 `themeExport.ts` 的 `isValidColorValue` 濾掉、也過不了 bundle 的顏色文法。
  因此圖片走**自己的 bundle 欄位**(與 avatars/logo 同路)round-trip,
  套用則在 `i18n/index.tsx` 的 apply effect 以 `setProperty` 推 `--canvas-bg-image`,
  並登記進 `appliedTokensRef` 讓換主題時清得掉。護欄:`lib/themeExport.test.ts`
  「round-trips a canvas background image through serialize → import」+
  「no colour token holds a url()」、`i18n/index.test.tsx` 的 apply 用例。
- **手機不受影響**(owner 特別交代):視窗 ≤1136px 時 gutter 歸零,三個分區的不透明
  底色蓋滿整幅,圖看不見也不影響版面;background 不參與 layout,所以不可能產生橫向捲動。
  實測 narrow 1440/1040/900/720/480/375 與 wide 1440/1280/1040 皆 h-scroll = 0。

## 主題快取的三道守衛:三個宿主,零個新 CI 關卡(T-1500)

pre-React 上色(`src/paint/prePaint.ts` 由 `vite.config.ts` 的 `inlinePrePaint()`
編成 IIFE inline 進 `index.html`)有三個**互不重疊**的性質要守。它們刻意**分住三處**
——設計原本要開一個新的 `4b4` 一次跑完,但那樣「一個關卡被砍掉、三道守衛同時消失」:
1. **記錄驗證** → `src/lib/themePaint.test.ts`(jsdom,既有 4b)。
2. **產物形狀** → `src/lib/paintArtifact.test.ts`(jsdom + 一次 `vite build`,既有 4b,
   **不需要瀏覽器**)。
3. **真實載入的每一幀** → `paint-guards/*.paint.spec.ts`(真 Chromium,
   `playwright-paint.config.ts`,由 `npm run test:ct` 串在既有 4c 之後)。

`MALICIOUS_PAINT_CASES` / `VALID_RICH_BUNDLE` 的**權威定義**在 `src/lib/paintFixtures.ts`,
jsdom 與瀏覽器兩層共用 ⇒ 加一個 payload 兩層同時守得到。
⚠️ 但**不是「全世界只有一份」**:`src/lib/paintFixtures.theme.json` 是給 stub 伺服器吃的
twin(它是 JSON、不能 import TS)。那份 twin 由 `themePaint.test.ts` 的
「matches the JSON copy the stub server serves」做 deep-compare 守著,所以漂了會紅
——但別把它講成不存在,下一個人會照著「只有一份」的字面去改其中一邊。

🔴 **兩層各擋哪顆 mutant,別記反(獨立覆核實測)**:
- **挖掉 `readValidatedPaint` 本身** → `themePaint.test.ts` 紅 6 + `paintCache.test.tsx` 紅 4。
- **驗證器留著,只讓 inline script 繞過它** → **jsdom 三個檔 40/40 全綠、tsc 乾淨**,
  只有 `payloadInjection.paint.spec.ts` 紅 5(6 個 payload 中的 5 個;第 6 個是 CSSOM
  擋的、fixture 自己標了不算覆蓋)。
⇒ **jsdom 那層擋不住 inline 繞過。** 這句話的用途是擋掉「jsdom 已經守住了,4c 可以砍」
這個推論——那正是想省成本時最容易講出口的一句話。

### 🔴 frame 量測一律在「登入態 + 伺服器認得該主題」下做,而且要**斷言**它成立
`reconcileFromServer()` 在 `i18n/index.tsx` 是 `if (hasToken())` 閘住的。**沒有 token
⇒ reconcile 永不執行 ⇒ `themesLoaded` 永遠 false ⇒ 只有「保留快取」那一條分支被跑到。**
實測同一個 build:沒種 token 讀 `BAD_FRAMES=0`,種了 token 讀 **231/233/249**
(伺服器不認得該主題 ⇒ `writePaint(active, [])` 把記錄**刪掉**)。
- ⇒ `zeroFlash.paint.spec.ts` 每條測試都**在頻帶內證明前提**:`/api/settings` 真的回過
  200、body 真的帶著這個主題、而且**播下去的記錄用的是不同的 `name`,跑完必須變成伺服器
  那個 name**——只有 reconcile 真的跑完才會成立。前提不成立就以 setup error 紅掉,
  不准空跑變綠。**meta-mutant 驗過**:把種 token 那行拿掉 ⇒ 紅在
  「GET /api/settings never answered 200」。
- ⇒ 量測用的 build 是 **`VITE_USE_MOCK=false`**(`bin/build` 出貨的那個)。預設 build 帶
  的是 in-memory mock,`custom_themes` 恆為 `[]` 且 0 ms 回話——**節流對它完全無效**,
  而這張票要修的閃爍窗**就是**等 `/api/settings` 的那段。伺服器由
  `paint-guards/settingsStub.mjs` 扮,`--delay 400` 不是填充,是那個窗本身。
- ⚠️ mock **無法**用來測 happy path:`mockServerSettings` 是 module-level、每次重整就重置,
  所以「重整後伺服器仍記得這個主題」在 mock 下不可能發生。

### 🔴 正向斷言:要驗「該套上的真的套上了」,不是「不含某個值」
只驗「某個禁字沒出現」的套件,會被**「applier 靜默不再套用 fonts 與 canvas」**整個繞過
——實測那顆 mutant 通過 tsc、build、產物 A–E、`paintCache.test.tsx` 的決策測試,以及一套 6 case 的
absence-only 瀏覽器探針,**6/6 全綠**。所以 `VALID_RICH_BUNDLE` 一定帶
colours **＋ fonts ＋ canvas 圖 ＋ canvas mode**,`EXPECT_APPLIED` / `EXPECT_APPLIED_VALUES`
逐條斷言它們真的到 DOM。
- **而且要歸因給 inline script**:`frameCarryingBeforeMount()` 要求那些值出現在
  **React 還沒 mount** 的幀上。裸的 `frameCarrying` 分不出 inline script 與 React 自己那條
  `!themesLoaded` fallback(它也呼叫 `readValidatedPaint()`)——實測 inline plugin 整個拿掉,
  裸版正向控制**照樣綠**。
- ⚠️ 這條需要**節流**才量得到:未節流時 React 早於 sampler 的第一個 rAF 就掛載完
  (實測第一筆取樣在 24.4 ms、`mounted` 已是 true),pre-mount 幀數為 **0**、斷言無從成立。

### 🔴 探針必須有 exit code,而且「取樣數」要有下限不是 `> 0`
- 前一代 frame 探針**只 `console.log` 數字、零個 `process.exit`**:實測 `BAD_FRAMES=1`
  而 shell exit code **0** ⇒ 接進 `set -e` 的 CI 永遠綠。現在寫成 Playwright spec,
  exit code 由 runner 保證。
- `SAMPLES > 0` **擋不住**上一輪真正燒到的那個失效:把逐幀改成「載入後單次讀」,
  `SAMPLES=1` 仍 `> 0`,而同一顆 mutant H 從 4 紅變 **6/6 假綠**。所以門檻是
  `MIN_SAMPLES`(80;健康的 3 秒窗是 200–260 幀)。**meta-mutant 驗過**:把 rAF 的
  re-arm 拿掉 ⇒ 11 條全紅、訊息是「only 1 frames sampled」。
- `({}).polluted === undefined` 是**恆真**的(applier 只呼叫 `setProperty`,沒有任何以
  payload 為鍵的賦值 sink;連驗證全拔的 mutant H 六個 case 都是 false)。它留著只為了
  「哪天這件事不再成立時被看到」,**不計入覆蓋**,註解已寫明。

### 注入案例要跑在「伺服器不認得任何主題」那台(:4319)
對著 happy-path 那台跑會有三條假紅:那台會回真主題,React 於是**合法**套上
`--canvas-bg-image` 與 `--color-bg: #010203`(實測 ~2038 ms),而那正是 `svg-canvas-bg` /
`illegal-canvasMode` / wording 三個 case 的禁字 ⇒ 斷言分不出「pre-paint 洩漏」與
「伺服器給了真主題」。`custom_themes: []` 那台上,自訂屬性的唯一寫入者就只有 pre-paint,
每次出現都可歸因。

### 儲存鍵只有一份:斷言在**原始碼**上,不只在產物上
產物斷言「找模組裡 `LS_THEME` 的值」只抓得到**兩步**漂移(先改回寫死字面量、再改常數值)。
**單步**——`prePaint.ts` 改回 `localStorage.getItem("oc.theme")`、乾淨移除 import、常數不動
——實測 tsc / build / 產物斷言**全綠**。所以 `paintArtifact.test.ts` 另外直接掃
`prePaint.ts` 與 `i18n/index.tsx` 的原始碼,禁止出現 `"oc.theme"` / `"oc.themePaint"`
字面量,在還來得及 review 的那一步就紅。
**探針自己也不准寫死鍵**:`frameProbe.ts` 的 `seedSession()` / `readStoredPaint()` 從
`api/auth` 的 `TOKEN_KEY` 與 `lib/themePaint` 的 `LS_THEME` / `LS_THEME_PAINT` 取值,
再當參數送進 `page.evaluate`。自帶一份 `"oc.theme"` 的探針在改名後**照樣綠**
——它只是在種一個沒人讀的鍵,然後斷言它從沒種進去的主題沒有出現。
