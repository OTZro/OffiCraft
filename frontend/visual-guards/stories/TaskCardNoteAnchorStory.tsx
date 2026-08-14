// CT story (T-4e39): the note disclosure inside the REAL scrolling chain.
//
// The bug this exists to make measurable is a scroll-position bug, so the story
// has to reproduce the box that actually scrolls. In production that is `.tasks`
// (`overflow-y:auto` + `height:100%`), NOT the document — measured on the live
// site, `document.scrollHeight` equals the window height at every width. A card
// mounted bare has no scrollport at all, so every correction would be a no-op
// and the guard would pass on any implementation, including none.
//
//   .app (viewport-tall) > .app__main > .tasks > .tasks__section > .tasks__list
//
// `.app` carries an explicit 100vh here because `height: 100%` needs a parent
// with a definite height, which the app shell supplies in production.
//
// Nine steps, every one of them carrying a note long enough that opening it
// cannot fit in what is left of a 390×844 phone — that is the owner's 「像在一疊
// 紙中間插進去十張」. Steps 2, 5 and 9 are the ones the guard clicks in order.
import { useLayoutEffect } from "react";
import { I18nProvider } from "../../src/i18n";
import { TaskCard } from "../../src/components/TaskCard";
import { mkTask, mkStep, MIRA, NOOP, WORKERS } from "./taskFixtures";
import { LIGHT_PACK } from "./ThemeContrastStory";

const LONG_NOTE = (n: number) =>
  [
    `第 ${n} 步做到哪:handler 已完成,conformance 三份重生一致。`,
    "",
    "下一步:補負面案例(400 / 403),再跑一次 `bin/ci.sh` 確認整輪是綠的。",
    "",
    "風險:seed 舊資料沒有 note 欄位,列表要能容忍空值;另外舊版 client 會把",
    "空字串當成「有備註但內容為空」,所以 wire 上要維持 optional。",
    "",
    "備援:如果 conformance 再紅,先回到上一顆 commit 再逐檔比對生成物。",
  ].join("\n");

// `noteRepeat` is the one-line knob that makes a note TALLER THAN THE
// SCROLLPORT. That case is not hypothetical: on this very site the step notes
// run to a 515-character median, t-fc23's longest is 1790, and T-e5b1's own
// step 4 is about 3.5k. At `noteRepeat: 8` the row measures ~1.6k px on a
// 390-wide card and ~1.0k px on a 1280-wide one, against scrollports of 820 and
// 776 — so it exercises the branch where the whole row CANNOT be revealed and
// the top edge has to win.
const makeTask = (noteRepeat: number) =>
  mkTask({
    id: "t-4e39",
    taskNo: "T-4e39",
    title: "步驟備註展開後畫面要停在你點開的那一則",
    status: "in_progress",
    description: "九個步驟,每一步都有一則夠長的備註。",
    progressDone: 3,
    progressTotal: 9,
    steps: Array.from({ length: 9 }, (_, i) =>
      mkStep({
        id: `s-${i + 1}`,
        name: `節點 ${i + 1}`,
        dod: `第 ${i + 1} 步的驗收標準。`,
        status: i < 3 ? "done" : i === 3 ? "in_progress" : "pending",
        note: Array.from({ length: noteRepeat }, () => LONG_NOTE(i + 1)).join(
          "\n\n"
        ),
      })
    ),
  });

export function TaskCardNoteAnchorStory({
  theme = "dark",
  noteRepeat = 1,
}: {
  theme?: "dark" | "light";
  noteRepeat?: number;
}) {
  const TASK = makeTask(noteRepeat);
  // The harness page is a bare <body><div id="root">, so the shell's own
  // premises have to be restated: no body margin (which would give the DOCUMENT
  // a scrollbar and make `.tasks` stop being the only scrollport) and a
  // definite height for `.app`'s height:100% chain to resolve against.
  // The light case is applied the way the product applies a theme pack —
  // --color-* custom properties on documentElement — reusing the palette of a
  // real shipped pack rather than inventing one.
  useLayoutEffect(() => {
    const root = document.documentElement;
    document.body.style.margin = "0";
    root.style.height = "100%";
    document.body.style.height = "100%";
    if (theme === "light") {
      for (const [k, v] of Object.entries(LIGHT_PACK))
        root.style.setProperty(k, v);
    } else {
      for (const k of Object.keys(LIGHT_PACK)) root.style.removeProperty(k);
    }
  }, [theme]);
  return (
    <I18nProvider>
      <div className="app" style={{ height: "100vh" }}>
        <main className="app__main">
          <div className="tasks">
            <section className="tasks__section">
              <div className="tasks__list">
                <TaskCard
                  task={TASK}
                  allTasks={[TASK]}
                  members={[MIRA]}
                  workers={WORKERS}
                  nowTs={3000}
                  onTerminate={NOOP as never}
                  onMarkDuplicate={NOOP as never}
                  onSetPriority={NOOP as never}
                  onSendMessage={NOOP as never}
                  onReassign={NOOP as never}
                  onHydrate={(async () => TASK) as never}
                />
              </div>
            </section>
          </div>
        </main>
      </div>
    </I18nProvider>
  );
}
