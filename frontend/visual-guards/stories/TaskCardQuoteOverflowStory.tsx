// CT story (T-4aa0): an expanded TaskCard whose description carries the shape
// the owner photographed on his phone — a markdown BLOCKQUOTE of quoted CJK
// prose with inline code, plus the neighbouring surfaces (table, fenced code,
// long URL) that the same fix has to leave alone.
//
// The screenshot's own text is reproduced rather than invented: the quote he
// circled is T-9b5d's 由來 block, whose lines are 「…」-wrapped sentences mixing
// CJK with latin words and `code` spans. Real TaskCard + real tasks.css, so the
// guard measures production layout (jsdom applies no layout at all).
import { I18nProvider } from "../../src/i18n";
import { TaskCard } from "../../src/components/TaskCard";
import { mkTask, mkStep, MIRA, NOOP, WORKERS } from "./taskFixtures";

const DESC = [
  "## 由來（owner 2026-08-14 聊天，原話逐字）",
  "",
  "> 「你可以看出自己開機中呼叫了多少次 mcp 載入多少 context 嗎」 「我發現你一起來就吃到 20% 以上」 「我需要你處理前端規範 他就算有規範應該也要有結構 不是全部放進去一個 claude.md 應該是隨需載入」 「而且你自己可以從程式碼去看規範嗎」 「前端規範重整後 可以也順道把它結構化嗎？ 我記得 claude code 是可以有類似 path map」 「你真的改到某一塊 才需要看那一個區塊的」",
  "",
  "## 我量到的（2026-08-14，親自量）",
  "",
  "| 文件 | 字數 |",
  "| --- | --- |",
  "| `frontend/CLAUDE.md` | 90,367 |",
  "| 根 `CLAUDE.md` | 30,084 |",
  "",
  "```bash",
  "wc -m frontend/CLAUDE.md CLAUDE.md docs/dev/*.md | sort -rn | head",
  "```",
  "",
  "參考：https://example.com/a/very/long/path/that/never/breaks/anywhere/at/all/because-it-has-no-spaces-in-it-at-all",
].join("\n");

const QUOTE_TASK = mkTask({
  id: "t-4aa0f",
  taskNo: "T-4aa0",
  title: "前端開發規範要拆成有結構、隨需載入",
  status: "waiting_owner",
  description: DESC,
  progressDone: 6,
  progressTotal: 7,
  steps: [
    mkStep({
      status: "done",
      dod: "> 引用區塊也會出現在步驟的 DoD 裡，所以同一個容器要一起量。",
    }),
    mkStep({ status: "pending" }),
  ],
});

export function TaskCardQuoteOverflowStory() {
  return (
    <I18nProvider>
      <TaskCard
        task={QUOTE_TASK}
        allTasks={[QUOTE_TASK]}
        members={[MIRA]}
        workers={WORKERS}
        nowTs={3000}
        onTerminate={NOOP as never}
        onMarkDuplicate={NOOP as never}
        onSetPriority={NOOP as never}
        onSendMessage={NOOP as never}
        onReassign={NOOP as never}
        onHydrate={(async () => QUOTE_TASK) as never}
      />
    </I18nProvider>
  );
}
