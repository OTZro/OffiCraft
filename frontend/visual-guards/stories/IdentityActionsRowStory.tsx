// CT story for the identity card's action row (owner 2026-07-31「全部變成左右
// 並排」). The pair 更改 ＋ 停止 used to be a COLUMN (`.mp-identity__actions {
// flex-direction: column }` — a leftover from the era when the action group sat
// stacked over the retired 「更換機器」 button, and 更改 later inherited that
// lower slot). It is a row now, and the narrow-screen media query has to keep it
// one — the old ≤720px rules stretched the COLUMN, which would crush a row into
// two crammed halves if left unadapted.
//
// jsdom can answer none of that: it resolves no flex direction and never
// evaluates @media, so every unit assertion about this is structurally blind
// (the vitest suite pins the DOM shape — same parent, 更改 first — which is the
// other half of the contract).
//
// Uses the REAL MemberActionButtons for the stop half, so the group's own
// `.member-actions` flex/gap participates exactly as it does in the panel; the
// 更改 button is the panel's own markup, verbatim.
//
// 🔴 The row is NOT one shape, and since owner 2026-08-21 「按了才出現」 it is
// not even a fixed COUNT: the ladder is REVEALED one rung at a time, so the
// cluster grows from TWO buttons (更改 ＋ 停止, nothing winding down) to FIVE
// (更改 + the 喚醒 wedge rescue + all three rungs, on an accelerated `stopping`
// member — the panel keeps 更改 there because mappers folds presence "stopping"
// onto status "online"). Both ends have to be measured: five is the widest the
// card ever has to hold, and two is the shape 375px now spends most of its time
// in. The story therefore takes the SAME (status, stage) pair the panels derive
// from the wire, not a status alone.
import { I18nProvider } from "../../src/i18n";
import {
  MemberActionButtons,
  type StopLadderStage,
} from "../../src/components/MemberActionButtons";
import "../../src/styles/theme.css";
import "../../src/components/office.css";
import "../../src/components/member-detail.css";

export function IdentityActionsRowStory({
  status = "online-awake",
  stage = "none",
}: {
  status?: "online-awake" | "stopping";
  stage?: StopLadderStage;
}) {
  return (
    <I18nProvider>
      {/* The real ancestor chain: the identity card is a flex row holding the
          avatar+body and, at its end, the action cluster. Without the card the
          row would have the whole viewport to spread into and the narrow case
          could never reproduce. */}
      <div style={{ width: "100%", padding: 22 }}>
        <div className="mp-card mp-identity" data-testid="story-identity">
          <div className="mp-identity__body">
            <div className="mp-identity__line">
              <span className="mp-identity__name">Mira</span>
              <span className="badge mp-identity__id">m-0001</span>
            </div>
          </div>
          <div className="mp-identity__actions">
            <div className="mp-identity__buttons">
              <button
                type="button"
                className="btn btn--accent-ghost"
                data-testid="mp-change"
              >
                更改
              </button>
              {/* Every rung that EXISTS at this stage gets a handler, so none is
                  disabled for a reason the panel would not have — a disabled
                  button still lays out, but its title tooltip is the only thing
                  that differs and the measurements must not depend on it.
                  Mounting straight at `stage` also arms the row immediately
                  (LADDER_ARM_MS only fires on a stage CHANGE), so the geometry
                  is measured in its settled state. */}
              <MemberActionButtons
                status={status}
                stage={stage}
                onSpawn={() => {}}
                onStop={() => {}}
                onAcceleratedStop={() => {}}
                onForceStop={() => {}}
              />
            </div>
          </div>
        </div>
      </div>
    </I18nProvider>
  );
}
