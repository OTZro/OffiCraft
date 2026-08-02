// The one derivation from a backup-health answer to what the cockpit says
// (T-da06).
//
// 🔴 WHAT THIS IS DEFENDING. The defect this ticket exists to remove is
// SILENCE: a studio with no retreat point looked exactly like one that had it.
// The cheapest way to reintroduce that defect is a fall-through — any branch
// here that ends up at `healthy` when we do not actually know turns the whole
// feature back into a lie, and it would do it QUIETLY, because a green light is
// what a working system looks like.
//
// So the table below is written round the wrong way on purpose: it does not
// check that healthy is healthy (a stuck-green implementation passes that), it
// checks that every ignorant state is NOT green.

import { describe, it, expect } from "vitest";
import {
  backupIndicatorState,
  backupStatusLabel,
  backupReasonText,
} from "./backupHealth";
import { zh } from "../i18n/locales/zh";
import type { BackupHealthView } from "../types";

const d = zh.backupHealth;

function view(over: Partial<BackupHealthView> = {}): BackupHealthView {
  return {
    status: "healthy",
    code: "",
    detail: "",
    newestBackupTs: 1785600000,
    newestBackupAgeSecs: 3600,
    staleAfterSecs: 43200,
    sinceTs: null,
    checkedTs: 1785603600,
    ...over,
  };
}

describe("backup health derivation", () => {
  it("never reads green when we do not know", () => {
    // Each of these is a DIFFERENT way of not knowing, and every one of them
    // used to be indistinguishable from a working backup.
    expect(backupIndicatorState(null, false)).toBe("unknown"); // still loading
    expect(backupIndicatorState(null, true)).toBe("unknown"); // fetch rejected
    expect(backupIndicatorState(view({ status: "unknown" }), false)).toBe(
      "unknown",
    ); // the watchdog itself cannot tell
    // A failed fetch must not let a previously green answer stand.
    expect(backupIndicatorState(view({ status: "healthy" }), true)).toBe(
      "unknown",
    );
  });

  it("passes the server's verdict through when there is one", () => {
    expect(backupIndicatorState(view(), false)).toBe("healthy");
    expect(
      backupIndicatorState(view({ status: "unhealthy", code: "stale" }), false),
    ).toBe("unhealthy");
  });

  it("gives each alarm its OWN sentence, from the code and not from the server's English", () => {
    const reasons = new Set(
      (["never_ran", "stale", "failed"] as const).map((code) =>
        backupReasonText(
          d,
          view({
            status: "unhealthy",
            code,
            detail: "SERVER DIAGNOSTIC THAT MUST NOT BE THE SENTENCE",
          }),
          false,
        ),
      ),
    );
    // Three distinct alarms, three distinct sentences — collapsing them would
    // send the reader to look at the wrong thing.
    expect(reasons.size).toBe(3);
    for (const r of reasons) {
      expect(r).not.toContain("SERVER DIAGNOSTIC");
      expect(r.trim()).not.toBe("");
    }
  });

  it("separates 'the server says it cannot tell' from 'we could not ask the server'", () => {
    const serverUnknown = backupReasonText(d, view({ status: "unknown" }), false);
    const cannotAsk = backupReasonText(d, null, true);
    // Two different things to go and look at, so they must not share wording.
    expect(serverUnknown).not.toBe(cannotAsk);
    expect(cannotAsk.trim()).not.toBe("");
  });

  it("says nothing when healthy, rather than inventing filler", () => {
    expect(backupReasonText(d, view(), false)).toBe("");
  });

  it("stays red, and honest, on an unhealthy verdict whose code it does not recognise", () => {
    const text = backupReasonText(
      d,
      // A future server code this build has never heard of.
      view({ status: "unhealthy", code: "corrupt" as never }),
      false,
    );
    expect(text.trim()).not.toBe("");
    expect(
      backupIndicatorState(view({ status: "unhealthy", code: "corrupt" as never }), false),
    ).toBe("unhealthy");
  });

  it("labels the three states differently", () => {
    const labels = new Set(
      (["healthy", "unhealthy", "unknown"] as const).map((s) =>
        backupStatusLabel(d, s),
      ),
    );
    expect(labels.size).toBe(3);
  });
});
