// CT story (T-49e7): the REAL <ScheduledMessagesCard> custom-cadence form
// against the REAL member-detail.css, so what the FOUR set grids do to the
// detail column can be MEASURED.
//
// The card reads its rows through useScheduledMessages → api.listScheduledMessages,
// so the one thing faked here is that call — same seam ScheduledMessagesClampStory
// patches. One stored CUSTOM row is enough: opening its row editor puts the four
// set pickers on screen through the ordinary path.
//
// 🔴 The stored minutes are 0/5/…/55 PLUS 7. Round 2 replaced a 60-cell grid
// behind a disclosure with twelve default cells, and the one thing that grid
// must never do is swallow a minute it does not offer — so the story carries an
// off-grid value and the guard measures the cell it has to grow.
import { I18nProvider } from "../../src/i18n";
import { ScheduledMessagesCard } from "../../src/components/ScheduledMessagesCard";
import { api } from "../../src/api";
import type { ScheduledMessage } from "../../src/api/adapter";

export const CUSTOM_ID = "sch-custom";

const ALL_MONTHS = Array.from({ length: 12 }, (_, i) => i + 1);
const ALL_DAYS = Array.from({ length: 31 }, (_, i) => i + 1);
const ALL_HOURS = Array.from({ length: 24 }, (_, i) => i);

const ROWS: ScheduledMessage[] = [
  {
    id: CUSTOM_ID,
    memberId: "mira",
    label: "佇列巡檢",
    body: "看一下佇列有沒有卡住的工作。",
    cadence: "custom",
    dayOfWeek: 1,
    dayOfMonth: 1,
    hour: 9,
    minute: 0,
    // Every month, every day and every hour, listed out — which is what "every"
    // means on this wire — so the largest grid (31 days) is the worst case for
    // layout with nothing hidden.
    customMonths: ALL_MONTHS,
    customDays: ALL_DAYS,
    customHours: ALL_HOURS,
    // 7 is NOT one of the twelve offered cells: the grid has to grow one cell
    // for it, and that cell has to be on screen and hittable like any other.
    customMinutes: [0, 7, 30],
    timezone: "Asia/Taipei",
    status: "enabled",
    lastFiredSlot: "",
    lastFiredTs: 0,
    createdTs: 1754800000,
  },
];

/** `width` is the PANEL's width, not the viewport's — the card lives in the
 * detail column, and how a grid wraps is a property of that column.
 * `.app__main`'s padding is reproduced because a bare card gains ~22px of slack
 * that makes exactly this class of overflow disappear (T-49fb). */
export function ScheduledMessagesCustomStory({ width }: { width: number }) {
  api.listScheduledMessages = async () => ROWS;

  return (
    <I18nProvider>
      <div className="app__main" style={{ padding: 22 }}>
        <div
          className="mp"
          style={{ width, height: 760, overflowY: "auto" }}
          data-testid="panel"
        >
          <ScheduledMessagesCard memberId="mira" />
        </div>
      </div>
    </I18nProvider>
  );
}
