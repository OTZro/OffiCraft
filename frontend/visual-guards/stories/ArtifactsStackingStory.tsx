// T-76cd — the STACKING story for the artifact popover's preview overlay.
//
// Every earlier guard for this overlay mounted it with no app chrome around it,
// and that is exactly why four rounds of geometry measurement stayed green
// while owner's phone showed the panel tucked under the tab bar: with no
// `.topbar` and no `.nav-tabs` in the tree there is NOTHING for the overlay to
// lose a stacking comparison to. A competitor has to exist before "who paints
// on top" is even a question.
//
// So this story carries the real chain AND the two competitors by class:
//   .app > [ .topbar, .nav-tabs, .app__main > .tasks > … > .task-card ]
// The card sits at its production x-offset for the same reason
// TaskArtifactsOverflowStory does.
import { I18nProvider } from "../../src/i18n";
import { TaskCard } from "../../src/components/TaskCard";
import { mkTask, MIRA, NOOP, WORKERS } from "./taskFixtures";

const MD = ["# Global Context", "", "AI 工作室・成員 boot context", "", "x".repeat(40)].join("\n");
const DATA_URL = "data:text/markdown;charset=utf-8," + encodeURIComponent(MD);

const TASK = mkTask({
  id: "t-76cd",
  taskNo: "T-76cd",
  title: "stacking",
  artifactCount: 1,
  artifacts: [
    {
      id: "ta-md",
      kind: "file",
      url: DATA_URL,
      label: "Global Context.md",
      filename: "Global Context.md",
      mime: "text/markdown",
      isImage: false,
      attachmentId: "att-md",
      createdTs: 0,
      createdBy: "mira",
    },
  ],
});

export function ArtifactsStackingStory() {
  return (
    <I18nProvider>
      <div className="app">
        <header className="topbar">
          <div className="topbar__brand">OffiCraft</div>
        </header>
        <nav className="nav-tabs">
          <div className="nav-tabs__seg">
            <button type="button" className="nav-tab nav-tab--active">
              <span>辦公室</span>
            </button>
            <button type="button" className="nav-tab">
              <span>任務</span>
            </button>
          </div>
        </nav>
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
                  onRemoveArtifact={NOOP as never}
                />
              </div>
            </section>
          </div>
        </main>
      </div>
    </I18nProvider>
  );
}
