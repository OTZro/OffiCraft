import { beforeEach, describe, expect, it } from "vitest";
import { __resetMock, mockApi } from "./mock";

describe("mock warden onboarding credential", () => {
  beforeEach(() => __resetMock());

  it("is permanent regardless of the legacy ttlDays request option", async () => {
    const ordinary = await mockApi.onboardMachine("Mock One", { ttlDays: 1 });
    const formerlyClamped = await mockApi.onboardMachine("Mock Two", {
      ttlDays: 401,
    });

    expect(ordinary.expiresIn).toBe(0);
    expect(formerlyClamped.expiresIn).toBe(0);
  });
});
