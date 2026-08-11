// CT story (T-49e7): the REAL <ScheduledMessagesCard> custom-cadence form
// against the REAL member-detail.css, so what the 60 minute checkboxes do to
// the detail column can be MEASURED.
//
// The card reads its rows through useScheduledMessages → api.listScheduledMessages,
// so the one thing faked here is that call — same seam ScheduledMessagesClampStory
// patches. One stored CUSTOM row is enough: opening its row editor puts the three
// set pickers on screen through the ordinary path, with the sets already full,
// which is the worst case for layout (every box ticked, nothing hidden).
import { I18nProvider } from "../../src/i18n";
import { ScheduledMessagesCard } from "../../src/components/ScheduledMessagesCard";
import { api } from "../../src/api";
import type { ScheduledMessage } from "../../src/api/adapter";

export const CUSTOM_ID = "sch-custom";

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
    // Every day and every hour, listed out — which is what "every" means on
    // this wire — and minutes that match no shortcut, so the detail grid opens
    // itself and the guard measures the 60 boxes rather than a collapsed stub.
    customDays: ALL_DAYS,
    customHours: ALL_HOURS,
    customMinutes: [3, 17, 41],
    timezone: "Asia/Taipei",
    status: "enabled",
    lastFiredSlot: "",
    lastFiredTs: 0,
    createdTs: 1754800000,
  },
];

/** `width` is the PANEL's width, not the viewport's — the card lives in the
 * detail column, and how a 60-box grid wraps is a property of that column.
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
