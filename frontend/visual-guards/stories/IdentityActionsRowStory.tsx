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
import { I18nProvider } from "../../src/i18n";
import { MemberActionButtons } from "../../src/components/MemberActionButtons";
import "../../src/styles/theme.css";
import "../../src/components/office.css";
import "../../src/components/member-detail.css";

export function IdentityActionsRowStory() {
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
              <MemberActionButtons status="online-awake" onStop={() => {}} />
            </div>
          </div>
        </div>
      </div>
    </I18nProvider>
  );
}
