// DiffPage — the compare url opened as ITS OWN page (T-59).
//
// The reader here may have arrived from Slack with no session at all, so what
// this pins is as much about what is ABSENT as what is drawn: the comparison,
// a tab title that says what the page is, and nothing that would need a login
// to render.

import { describe, it, expect, vi, afterEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { I18nProvider } from "../i18n";
import { zh } from "../i18n/locales/zh";
import { api } from "../api";
import { DiffPage } from "./DiffPage";

const PARAMS = {
  before: "att-0123456789ab",
  after: "att-fedcba987654",
  sig: "signature",
};

describe("DiffPage", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("draws the comparison and nothing that would need a session", async () => {
    const getDiff = vi.spyOn(api, "getDiff").mockResolvedValue({
      before: { address: "att-0123456789ab", text: "alpha\nbravo", label: "改動前", gone: false },
      after: { address: "att-fedcba987654", text: "alpha\nBRAVO", label: "改動後", gone: false },
    });

    render(
      <I18nProvider>
        <DiffPage params={PARAMS} />
      </I18nProvider>,
    );

    await waitFor(() => expect(screen.getByTestId("diff-screen")).toBeTruthy());
    // The signature travels with the read — it is the page's whole credential.
    expect(getDiff).toHaveBeenCalledWith(expect.objectContaining({ sig: "signature" }));
    // No nav, no badges: nothing on this page asks the server who is looking.
    expect(screen.queryByText(zh.nav.office)).toBeNull();
    expect(document.title).toBe(zh.diff.ariaLabel);
  });
});
