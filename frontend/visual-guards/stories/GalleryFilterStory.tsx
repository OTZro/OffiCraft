// CT story: the 「檔案與圖片」 panel's uploader filter (T-51 ②) in the panel's
// real production DOM shape — `.chat__gallery` positioned against a chat pane,
// the header/tabs above it and the file list below — so the loaded office.css,
// not a mock stylesheet, governs the geometry the guard measures.
//
// The component under test is the shipped `GallerySenderFilter`. What is
// stand-in is only the data path: the real panel reaches it through `api`,
// which resolves to the mock adapter in this harness and cannot be handed 60
// uploaders. The surrounding rows carry the panel's own class names because the
// question being asked is "does this filter leave the list its height", and the
// answer lives in the CSS box tree, not in the fetch.
import { useState } from "react";
import { I18nProvider } from "../../src/i18n";
import { GallerySenderFilter } from "../../src/components/ChatGalleryPanel";
import { FileTextIcon } from "../../src/components/icons";

export function GalleryFilterStory({ uploaders }: { uploaders: number }) {
  const senders = Array.from({ length: uploaders }, (_, i) => ({
    id: `m-sender-${i}`,
    label: `寄件者 ${i}`,
    count: uploaders - i,
  }));
  const [selected, setSelected] = useState<ReadonlySet<string>>(new Set());

  return (
    <I18nProvider>
      {/* The panel is `position: absolute; top: 64px; bottom: 0` against the
        * chat pane — a bare mount would give it no bounded height, and a filter
        * that grows without limit would then look fine. */}
      <div
        className="chat"
        style={{ position: "relative", height: "100vh", width: "100%" }}
      >
        <div className="chat__gallery" role="dialog" aria-label="檔案與圖片">
          <div className="chat__gallery-header">
            <span className="chat__gallery-title">檔案與圖片</span>
          </div>
          <div className="chat__gallery-tabs" role="tablist">
            <button
              type="button"
              role="tab"
              aria-selected
              className="chat__gallery-tab chat__gallery-tab--active"
            >
              圖片
            </button>
            <button type="button" role="tab" aria-selected={false} className="chat__gallery-tab">
              檔案
            </button>
          </div>
          <GallerySenderFilter
            senders={senders}
            selected={selected}
            onChange={setSelected}
          />
          <div className="chat__gallery-list">
            {senders.map((s) => (
              <div key={s.id} className="chat__gallery-item" role="button" tabIndex={0}>
                <span className="chat__gallery-fileicon" aria-hidden>
                  <FileTextIcon size={20} />
                </span>
                <div className="chat__gallery-meta">
                  <span className="chat__gallery-name">shot-{s.id}.png</span>
                  <span className="chat__gallery-sub">{s.label} · 9/2 10:00</span>
                </div>
              </div>
            ))}
          </div>
        </div>
      </div>
    </I18nProvider>
  );
}
