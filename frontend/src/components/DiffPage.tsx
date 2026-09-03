// components/DiffPage.tsx — the compare url opened as ITS OWN PAGE (T-59).
//
// This is the half of the promise that has to work for someone who is NOT in
// the studio: the link was pasted into Slack, or typed into a browser, and the
// signature it carries is the only credential involved. So the page draws the
// comparison and NOTHING that would need a session — no nav, no unread badges,
// no polling of authed endpoints, and no auth wall in front of it (see
// main.tsx, which mounts this ahead of AuthGate when the url carries a ?sig=).
//
// The comparison itself is DiffScreen, the same component the in-studio modal
// puts inside the preview overlay. This file is a page shell around it and
// nothing more — one compare screen, two hosts.

import { useEffect } from "react";
import { useI18n } from "../i18n";
import type { DiffParams } from "../lib/diffLink";
import { DiffScreen } from "./DiffScreen";
import "./diff-screen.css";

export function DiffPage({ params }: { params: DiffParams }) {
  const { t } = useI18n();
  // The browser tab has to say what it is holding. The studio's own title is
  // the org name (App.tsx), which this page has no business reading: it is
  // server-backed and owner-gated, and this reader may have no session at all.
  useEffect(() => {
    document.title = t.diff.ariaLabel;
  }, [t]);

  return (
    <main className="diff-page">
      <div className="diff-page__frame">
        <DiffScreen params={params} />
      </div>
    </main>
  );
}
