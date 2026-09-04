// Fixtures for the T-48 forward-walk guard, in their own module so that BOTH
// the story and the spec can import them (Playwright CT rewrites a component
// import into its own registry declaration, so a plain value exported from the
// story module collides at collect time and the spec reports "No tests found").
export const OWNER = "owner";
export const PEER = "m-aaaaaaaaaaaa";
/** The message the jump lands on — deep enough that many pages sit below it. */
export const TARGET_ID = "a100";
export const TOTAL = 200;
/** Where the story publishes every `?start_id=` request it has answered, so the
 * spec can count them from the page. */
export const FORWARD_COUNT_KEY = "__t48ForwardCalls";
