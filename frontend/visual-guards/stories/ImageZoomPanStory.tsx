// CT story for T-7e68 — zoomed image panning. The image is deliberately much
// wider and taller than the overlay frame and carries a distinctly coloured
// square in each corner, so "can the user reach the corner" is a question about
// real geometry rather than about which CSS property is written down.
//
// The page around the overlay is made tall on purpose: wheel-zoom must not take
// the page with it, and that is only observable when there is a page to scroll.
import { I18nProvider } from "../../src/i18n";
import { MarkdownPreviewOverlay } from "../../src/components/MarkdownPreviewOverlay";

const W = 1600;
const H = 1000;
const M = 120;
const corner = (x: number, y: number, fill: string) =>
  `%3Crect x='${x}' y='${y}' width='${M}' height='${M}' fill='${fill}'/%3E`;

export const ZOOM_PAN_IMAGE =
  `data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='${W}' height='${H}'%3E` +
  `%3Crect width='100%25' height='100%25' fill='%23163a5f'/%3E` +
  corner(0, 0, "%23ff3b30") +
  corner(W - M, 0, "%2334c759") +
  corner(0, H - M, "%23ffcc00") +
  corner(W - M, H - M, "%23af52de") +
  `%3C/svg%3E`;

export function ImageZoomPanStory() {
  return (
    <I18nProvider>
      <div style={{ height: "3000px" }} />
      <MarkdownPreviewOverlay
        title="wide-shot.png"
        url={ZOOM_PAN_IMAGE}
        attachmentId="att-zoom-pan"
        mime="image/png"
        onClose={() => {}}
      />
    </I18nProvider>
  );
}
