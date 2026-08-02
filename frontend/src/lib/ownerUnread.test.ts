import { describe, expect, it } from "vitest";
import type { SseDelta } from "../api/adapter";
import { burstMovesNoOwnerUnread } from "./ownerUnread";

function batch(deltas: SseDelta[], unnamed = false) {
  return { deltas, unnamed };
}

describe("burstMovesNoOwnerUnread fail-open boundary", () => {
  it.each(["member", "outsource_worker"])(
    "a named %s lifecycle delta still requires a refetch",
    (topic) => {
      expect(
        burstMovesNoOwnerUnread(
          batch([{ topic, names: { id: "live-1" }, ids: ["live-1"] }]),
          [topic]
        )
      ).toBe(false);
    }
  );

  it("an unrelated coalesced topic does not defeat a proven unread no-op", () => {
    expect(
      burstMovesNoOwnerUnread(
        batch([
          {
            topic: "chat",
            names: { from: "m-1", to: "m-2" },
            ids: ["m-1", "m-2"],
          },
          { topic: "task", names: { id: "t-1" }, ids: ["t-1"] },
        ]),
        ["chat"]
      )
    ).toBe(true);
  });

  it.each(["chat", "chat_read"])(
    "an unnamed %s delta fails open to a refetch",
    (topic) => {
      expect(
        burstMovesNoOwnerUnread(
          batch([{ topic, names: {}, ids: [] }], true),
          [topic]
        )
      ).toBe(false);
    }
  );
});
