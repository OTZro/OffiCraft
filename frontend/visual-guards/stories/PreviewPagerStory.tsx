// CT story: the preview overlay carrying a pager (T-51 ①), with the caller's
// list state held here — exactly as `ChatGalleryPanel` holds it — so a step
// really re-renders the overlay with a new item instead of merely calling a spy.
//
// It exists for one question jsdom cannot answer: **does the keyboard survive
// using the buttons?** Stepping to the last item disables the button under the
// pointer, and a real browser blurs a focused element when it is disabled —
// jsdom does not, so only a real Chromium can tell the two bindings apart.
import { useState } from "react";
import { I18nProvider } from "../../src/i18n";
import { MarkdownPreviewOverlay } from "../../src/components/MarkdownPreviewOverlay";

/** A 1x1 PNG per item, so the overlay renders a real <img> without a server. */
const PNG =
  "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==";

export function PreviewPagerStory({ start = 0 }: { start?: number }) {
  const items = ["one.png", "two.png", "three.png"];
  const [index, setIndex] = useState(start);
  return (
    <I18nProvider>
      <MarkdownPreviewOverlay
        title={items[index]}
        imageSrc={PNG}
        pager={{ index, total: items.length, onGo: setIndex }}
        onClose={() => {}}
      />
    </I18nProvider>
  );
}
