// CT story (T-59): the COMPARE attachment opened in the shared preview overlay.
//
// Mounts the real MarkdownPreviewOverlay with the real diff mime, so the
// component resolves the pointer pair and renders the real DiffView inside the
// overlay panel — the one geometry the jsdom suite cannot see. The pair itself
// arrives as a data: URL (authedAttachmentUrl leaves non-"/" URLs alone); the
// two SIDES are ordinary "/api/chat/attachment/…" reads, which the spec serves
// with page.route so the story stays a pure component mount.
import { MarkdownPreviewOverlay } from "../../src/components/MarkdownPreviewOverlay";
import { I18nProvider } from "../../src/i18n";
import "../../src/components/office.css";

const PAIR = JSON.stringify({
  before: { attachment_id: "att-0123456789ab", label: "改動前" },
  after: { attachment_id: "att-fedcba987654", label: "改動後" },
});

export function DiffAttachmentOverlayStory() {
  return (
    <I18nProvider>
      <MarkdownPreviewOverlay
        title="lineDiff.before.ts → lineDiff.after.ts"
        url={"data:application/json;charset=utf-8," + encodeURIComponent(PAIR)}
        attachmentId="att-aaaaaaaaaaaa"
        mime="application/vnd.officraft.diff"
        onClose={() => {}}
      />
    </I18nProvider>
  );
}
