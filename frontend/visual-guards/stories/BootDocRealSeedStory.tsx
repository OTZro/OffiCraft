// CT story for T-791e — the boot-context block page rendering THE REAL
// `seeds/system_interaction.md`, at phone widths.
//
// Why a second boot-doc story: `BootDocSectionRowStory` seeds a hand-written
// four-section document, so everything measured against it is measured on a
// page four rows tall. The system-interaction seed splits into SEVENTY-odd
// sections and carries the things a synthetic fixture does not — fenced
// blocks, tables, long CJK headings, cited routes and ids — and that page had
// never been in a browser. "It works at 4 sections" says nothing about it.
//
// Nothing is seeded here on purpose. `api/mock.ts` already SERVES the real
// seed for (system_interaction, global) — the same `?raw` bytes `api/seeds.ts`
// imports from the repo-root file — so writing a copy in would replace the
// input with a fake of itself and mark the document owner-edited besides.
// `EXPECTED_SECTIONS` is computed from those same bytes rather than written
// down, so a seed edit moves the guard's own expectation with it instead of
// silently making the number a lie.
//
// The ancestor chain is reproduced BY CLASS, per frontend/CLAUDE.md 〈浮層寬度
// 不可用 vw 夾〉: a bare card mounted at x≈0 carries ~22px of slack it does not
// have in the app. Production is
//   .app > .app__main (max-width 1040 + 22px side padding) > .settings > card.
import { I18nProvider } from "../../src/i18n";
import { BootDocPage } from "../../src/components/BootDocPage";
import { SEED_SYSTEM_INTERACTION_MD } from "../../src/api/seeds";
import { splitDocSections } from "../../src/lib/docSections";
import { zh } from "../../src/i18n/locales/zh";

/** How many rows the page OUGHT to render, derived from the same bytes the
 * mock serves and the same splitter the page uses. Published into the DOM so
 * the spec can assert the rendered count against it — a hard-coded 74 in the
 * spec would go stale on the next seed edit and would go stale QUIETLY. */
export const EXPECTED_SECTIONS = splitDocSections(
  SEED_SYSTEM_INTERACTION_MD.trim()
).length;

export function BootDocRealSeedStory() {
  return (
    <I18nProvider>
      <div className="app">
        <main className="app__main">
          <div data-testid="story-expected-sections">{EXPECTED_SECTIONS}</div>
          <BootDocPage
            kind="system_interaction"
            docKey="global"
            title={zh.settings.systemName}
            historyTitle={zh.settings.historyBootSystemTitle}
            crumbs={[{ label: zh.settings.title }]}
          />
        </main>
      </div>
    </I18nProvider>
  );
}
