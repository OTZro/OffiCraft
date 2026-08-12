package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
)

// ── spec/openapi.json ↔ spec/mcp-catalog.json:結構性對質 ──────────────────────
//
// T-74f8 round-2 §1-#6 / §7-3. The MCP tool surface is written down TWICE — once
// as an OpenAPI operation (spec/openapi.json, which CI regenerates schema.ts
// from and diffs, bin/ci.sh) and once as a tools/list descriptor
// (spec/mcp-catalog.json, which ocserverd serves verbatim). Until this test
// existed, NOTHING compared the two: mcp_test.go and conformance/test_rest_happy.py
// both key on the tool NAME set only, so a parameter could exist on one side and
// not the other indefinitely.
//
// That is not hypothetical. It is exactly the half-finished state this very
// ticket shipped in: openapi.json carried handoff / handoff_note /
// handoff_task_id, the catalog did not, and every check stayed green — which
// means the gate's 422 told agents to send parameters their only view of the
// tool said did not exist. A gate that names an unreachable way out is an
// outage, so the two files agreeing is load-bearing, not tidiness.
//
// The confrontation is THREE-way, because neither spec file knows how to find
// the other: the routes table (defaultRouteSpecs) supplies tool name → method +
// path, openapi.json supplies that operation's parameter set, and the catalog
// supplies the tool's inputSchema. Drift in any one of the three reddens here.
//
// Deliberately a set comparison on property NAMES, not on types or prose: the
// two files describe the same fact in genuinely different vocabularies (JSON
// Schema vs OpenAPI 3.1 with $ref indirection), and pinning more than the
// parameter set would make this test fail on formatting rather than on drift.

// knownCatalogDrift is the drift that ALREADY existed when this confrontation
// was first written (T-74f8 round-2 rework): a parameter spec/openapi.json
// accepts, the handler genuinely READS, and the frozen catalog does not
// advertise — so an agent, whose only view of a tool is tools/list, cannot
// discover a feature that works.
//
// VERDICT ON EACH ENTRY: every one below was traced to the line that reads it,
// so all six are MISSING, not deliberately withheld. Recorded because "we could
// not tell whether this was intentional" is exactly the kind of shrug that lets
// a real gap live forever:
//
// (Anchored on handler names, not line numbers: every one of these six line
// refs had rotted by T-ccc7 — get_members.fields alone had drifted 90 → 215 —
// and a stale pointer reads as "traced" while pointing at nothing.)
//
//	list_tasks.open          → HandleListTasksApiTasksGet          trimmedOrEmpty(params.Open) == "true"
//	get_chat.peek            → HandleListChatApiChatGet            trimmedOrEmpty(params.Peek) == "true"
//	get_members.fields       → HandleListMembersApiMembersGet      trimmedOrEmpty(params.Fields) == "light"
//	update_task_manual.display_name → HandleCreateTaskManualApiTaskManualsPost + HandleUpdateTaskManualApiTaskManualsTypeKeyPost
//	ingest_telemetry.binaries/claude → HandleIngestTelemetryApiMonitoringTelemetryPost (the asObject reads)
//
// The last one deserves a note because it was initially GUESSED to be
// CLI-only: binaries and claude are read in the same handler, by the same
// asObject calls, as hardware and self_update — and those two ARE already in
// the catalog. There is no line anywhere that treats them differently. A guess
// from the parameter's NAME was simply wrong; only following the read proved it.
//
// There is no parameter-level "deliberately not on MCP" marker in this codebase
// (RouteSpec.MCPExclude is whole-route only), which is why intent has to be
// established by tracing reads. That absence is itself worth a ticket: today
// "forgotten" and "withheld on purpose" are indistinguishable by inspection.
//
// Baselined rather than fixed HERE: repairing the catalog changes the tools/list
// wire surface and therefore catalog_hash for six tools unrelated to the handoff
// gate, which needs its own ticket and its own conformance run. Baselining keeps
// this test fail-closed for anything NEW while staying loud about the debt.
//
// ONE OF THE SIX IS REPAID: list_task_manuals.view is now advertised (T-a98d).
// It was the entry that cost the most to leave sitting here — that tool's
// DEFAULT answer is every manual in full, six figures of characters, and
// ?view=list was the only way down from it. A lever that exists, works, and is
// the sole escape from an unreadable default does not exist at all while it is
// off tools/list, so it was repaid with the response-size work rather than
// waiting for a catalog-wide ticket.
//
// ALL SIX ARE NOW REPAID (T-1ba2), so this map is EMPTY. The catalog-wide
// ticket the note above was waiting for is this one: the remaining five tools'
// six parameters are advertised in spec/mcp-catalog.json with descriptions, and
// the entries that recorded them as debt were deleted in the same change. The
// map is kept (rather than deleted outright) because it is the seam a FUTURE
// deliberate baseline would go through; an entry added here still has to be
// traced to the line that reads it, and still has to be deleted the moment the
// gap is repaired.
//
// Checked in BOTH directions (see the rot assertion below): if one is fixed,
// this test fails until the entry is deleted. A stale allowlist that silently
// permits drift is the same disease as a stale comment, and this file exists
// because of a stale comment.
var knownCatalogDrift = map[string][]string{}

// openapiOverweight is the OTHER direction, and it is NOT debt: openapi lists a
// field for this operation that the operation's handler never reads, so the
// catalog omitting it is CORRECT and openapi is the one describing a parameter
// that does nothing.
//
// open_gate.bind: ReplyCardCreateDTO is SHARED by two operations. Bind is read
// in exactly one place in the whole server — api_replycards.go:401, inside
// HandleCreateReplyCard — and HandleOpenTaskGate (api_tasks.go) decodes the same
// DTO and never touches it. It arrived with T-4166 on the shared DTO.
//
// Kept separate from knownCatalogDrift deliberately: filing this as "debt" would
// record a bug that does not exist, and would invite someone to "fix" it by
// advertising a lever open_gate ignores — which is the exact failure this ticket
// is about, just pointed the other way.
var openapiOverweight = map[string][]string{
	"open_gate": {"bind"},
}

// deliberatelyOffMCP is the THIRD category, and the one the comment above
// knownCatalogDrift said this codebase could not express: openapi accepts the
// parameter, the handler READS it, and the catalog omits it ON PURPOSE.
//
// ingest_telemetry.warden_shape: the value is which shape launchd is actually
// executing, and the ONLY honest source for it is the reporting process's own
// PARENT executable (cli/ocwarden/cutover.go detectShape). A warden can read
// that; an agent cannot read anything about the launchd job that supervises its
// machine, so a warden_shape arriving over MCP could only ever be invented.
// Advertising it in tools/list would be an invitation to fabricate the exact
// signal the fleet uses to decide whether a machine's migration succeeded.
//
// ingest_telemetry.cutover_effect: the same argument, one step further in. The
// verdict is computed from the ages of the tmux server processes that CARRY
// agent sessions on that machine, measured against the birth of its anchor
// inode (cli/ocwarden/cutovereffect.go). Only a process on the box can see any
// of those operands. And this field is the one that was added BECAUSE a signal
// nobody could falsify still read green while the cutover had not taken effect
// — putting a hand-typed version of it on the MCP surface would hand out the
// falsification the incident did not even need.
//
// NOT filed under knownCatalogDrift, deliberately: every entry there was traced
// to its read and confirmed MISSING (debt to be repaid), and that list is
// checked for rot precisely so it shrinks. Recording an intentional omission as
// debt would invite the next person to "repay" it by advertising this field.
// list_reply_cards.view: T-a3e4, owner-approved 2026-08-02. Unlike the two
// telemetry fields above — which an agent could only ever INVENT — an agent
// could honestly send `view=full`. It is withheld for the opposite reason: the
// LIGHT row is the agent-facing contract by owner ruling (T-3f31, 卡只需要
// title+決策), and `view=full` returns every card's body, full option list and
// untruncated answer. Advertising it in tools/list would hand every agent a
// one-call way to pull whole panes of full cards into its context — undoing
// exactly what T-3f31 shrank, and the agent path to a full card is meant to be
// get_reply_card, one card at a time, chosen deliberately. The cockpit is the
// only intended caller (it renders the whole card, so the light list forced it
// into one GET per row).
//
// NOT knownCatalogDrift: that map is debt to be repaid, and every entry there
// was traced to its read and confirmed MISSING. Filing this there would invite
// the next person to "repay" it by advertising the lever.
var deliberatelyOffMCP = map[string][]string{
	"ingest_telemetry": {"warden_shape", "cutover_effect"},
	"list_reply_cards": {"view"},
}

type openapiSpec struct {
	Paths map[string]map[string]struct {
		Summary string `json:"summary"`
		XMCP    struct {
			Include     bool   `json:"include"`
			Description string `json:"description"`
		} `json:"x-mcp"`
		Parameters []struct {
			Name string `json:"name"`
		} `json:"parameters"`
		RequestBody struct {
			Content map[string]struct {
				Schema map[string]any `json:"schema"`
			} `json:"content"`
		} `json:"requestBody"`
	} `json:"paths"`
	Components struct {
		Schemas map[string]struct {
			Properties map[string]any `json:"properties"`
		} `json:"schemas"`
	} `json:"components"`
}

// Keep this false legacy prose out of the map literal: gitleaks' generic-api-key
// rule misclassifies the inline summary entry whose prose begins `Submit/replace`.
const legacySubmitPlanDescription = "Submit/replace the workflow plan (done and answered-card steps are kept)."

// knownToolDescriptionDrift records the known-inaccurate prose that predates
// T-960d. Every nonempty entry is an already-known FALSE statement; an empty
// entry records a missing summary. Neither is a correct example to copy. This
// is a debt ledger, NOT a list of correct descriptions, and MUST shrink as
// T-7524 aligns each source with
// x-mcp.description. This guard deliberately rejects an empty map to catch a
// vacuous baseline; when the last debt is repaid, that PR must explicitly
// replace this transitional baseline shape with a strict no-drift assertion.
// Each value is the measured false prose itself, so a baselined slot changing
// to a different false claim also fails. This is deliberately reviewed test
// data: it prevents accidental drift, while an intentional baseline change
// remains a visible, reviewable PR change. This guard compares prose only: it
// cannot detect a handler's behavior drifting while its descriptions stay put.
var knownToolDescriptionDrift = map[string]map[string]string{
	"add_task_artifact": {
		"openapi_summary": "",
		"route_summary":   "Register a deliverable (file/image/link) onto the task's artifact set.",
	},
	"answer_reply_card": {
		"openapi_summary": "Answer a waiting reply card — the only way a card closes.",
	},
	"claim_task": {
		"openapi_summary": "Take over a reassigned task (the new executor claims it -- clears the reassigning lock).",
		"route_summary":   "Take over a reassigned task (the new executor claims it — clears the reassigning lock).",
	},
	"create_reply_card": {
		"openapi_summary": "Open a reply card: an ask the owner must answer (options ≤4, [0]=AI pick).",
		"route_summary":   "Open a reply card: an ask the owner must answer (options ≤4, [0]=AI pick). Auto-binds to your single active task's current step when unambiguous — that step (and usually the task) enters waiting_owner until the owner answers.",
	},
	"create_role": {
		"openapi_summary": "Create a custom role + its founding member (one pair per call).",
		"route_summary":   "Create a custom role + its founding member (one pair per call).",
	},
	"create_task": {
		"openapi_summary": "Create a task (dedupes on the manual's key; ad-hoc when type_key omitted).",
		"route_summary":   "Create a task (dedupes on the manual's key; ad-hoc when type_key omitted).",
	},
	"create_task_manual": {
		"openapi_summary": "Create a task type: pass display_name; the server mints and returns the tm- type_key id (legacy explicit type_key still accepted; duplicate → 409; assignee = owner/admin agent).",
		"route_summary":   "Create a task type: pass display_name; the server mints and returns the tm- type_key id (legacy explicit type_key still accepted; duplicate → 409; assignee = owner/admin agent).",
	},
	"expire_reply_card": {
		"openapi_summary": "Mark a waiting card expired (its author, the owner, or an admin agent; not an answer; terminal).",
	},
	"get_chat": {
		"openapi_summary": "List the chat stream (?with=<id>&limit=<n>; oldest→newest).",
		"route_summary":   "List the chat stream (?with=<id>&limit=<n>; oldest→newest).",
	},
	"get_chat_attachment_share_link": {
		"openapi_summary": "Mint a permanent single-file share link (?sig= HMAC; grants read of this one attachment only).",
		"route_summary":   "Mint a permanent single-file share link (?sig= HMAC; grants read of this one attachment only).",
	},
	"get_document_seed": {
		"openapi_summary": "Read the shipped default of an editable document.",
		"route_summary":   "Read the shipped default of an editable document.",
	},
	"get_insight": {
		"openapi_summary": "Read a per-role insight doc (per role_key; may have a PER-ROLE factory seed).",
		"route_summary":   "Read a per-role insight doc (per role_key; may have a PER-ROLE factory seed).",
	},
	"get_member_resume_summary": {
		"openapi_summary": "Bounded LIGHT wake snapshot for a TARGET member (admin_agent+; same shape as resume_summary).",
		"route_summary":   "Bounded LIGHT wake snapshot for a TARGET member (admin_agent+; same shape as resume_summary).",
	},
	"get_members": {
		"openapi_summary": "List members, including outsource members by default; fields=light preserves kind.",
		"route_summary":   "List the owner's roster (presence-derived MemberDTO[]).",
	},
	"get_my_task": {
		"openapi_summary": "Outsource worker's claim: read the task bound to the caller.",
		"route_summary":   "Outsource worker's claim: read the task bound to the caller.",
	},
	"get_task_manual": {
		"openapi_summary": "Read one task manual (purpose/fields/SOP/learnings/assignee).",
		"route_summary":   "Read one task manual (purpose/fields/SOP/learnings/assignee).",
	},
	"get_version": {
		"openapi_summary": "Build identity: version + git sha + MCP catalog hash.",
		"route_summary":   "Build identity: version + git sha + MCP catalog hash.",
	},
	"hire_member": {
		"openapi_summary": "Hire a member (server mints the id). Pure seam, no UI (§9.1).",
		"route_summary":   "Hire a member (server mints the id). Pure seam, no UI (§9.1).",
	},
	"list_document_history": {
		"openapi_summary": "List retained versions of an editable document.",
		"route_summary":   "List the retained history of an editable document.",
	},
	"list_reply_cards": {
		"openapi_summary": "List reply cards — LIGHT rows (?status=waiting|answered|expired; ?limit= caps; get_reply_card for full).",
		"route_summary":   "List reply cards — LIGHT rows: summary+decision digest, no body/options (?status=waiting|answered|expired; ?limit= caps; get_reply_card for full).",
	},
	"list_task_manuals": {
		"openapi_summary": "List task types (match by display_name/purpose; address by type_key).",
		"route_summary":   "List task types (match by display_name/purpose; address by type_key).",
	},
	"list_tasks": {
		"openapi_summary": "List tasks (?executor=&type=&status=; light list items — get_task for full).",
		"route_summary":   "List tasks (?executor=&type=&status=; light list items — get_task for full).",
	},
	"mark_duplicate": {
		"openapi_summary": "Mark a task duplicated, pointing at the original (executor/owner; terminal).",
		"route_summary":   "Mark a task duplicated, pointing at the original (executor/owner; terminal).",
	},
	"open_gate": {
		"openapi_summary": "Arm a gate step: opens the reply card the owner must answer.",
		"route_summary":   "Arm a gate step: opens the reply card the owner must answer.",
	},
	"patch_insight": {
		"openapi_summary": "Patch a per-role insight doc by unique anchors ({edits:[{old,new}]}).",
		"route_summary":   "Patch a per-role insight doc by unique anchors ({edits:[{old,new}]}).",
	},
	"patch_task_learnings": {
		"openapi_summary": "Patch a type's learnings by unique anchors ({edits:[{old,new}]}).",
		"route_summary":   "Patch a type's learnings by unique anchors ({edits:[{old,new}]}).",
	},
	"peek_doc_sizes": {
		"openapi_summary": "Size-only overview of every capped document (each against its own cap; NO content; role lessons = DEFAULT bucket only).",
		"route_summary":   "Size-only overview of every capped document (each against its own cap; NO content; role lessons = DEFAULT bucket only).",
	},
	"peek_resume_summary_size": {
		"openapi_summary": "Size-only PEEK of the wake snapshot (overview counts/sizes + estimated_total_chars, NO content).",
		"route_summary":   "Size-only PEEK of the wake snapshot (identity-locked; overview counts/sizes + estimated_total_chars, NO content) — size resume_summary before pulling it.",
	},
	"post_chat": {
		"openapi_summary": "Post a chat message (sender = verified JWT sub; auto SSE fan-out).",
		"route_summary":   "Post a chat message (sender = verified JWT sub; auto SSE fan-out).",
	},
	"reassign_task": {
		"openapi_summary": "Reassign a task to a member or a fresh outsource worker (the task's executor or an admin; an outsource target lands the task unassigned for the scheduler to spawn under the global parallel cap; enters the reassigning handover state).",
		"route_summary":   "Reassign a task to a member or a fresh outsource worker (the task's executor or an admin; outsource targets pass the owner-approval gate; enters the reassigning handover state).",
	},
	"refocus_outsource_worker": {
		"openapi_summary": "Refocus (換手) an outsource worker's context (owner/admin agent, online-only else 409).",
	},
	"relocate_member": {
		"openapi_summary": "Relocate a member to a machine (placement only; never touches desired_state). Also accepts an outsource-worker id: the same move-one-agent verb relocates the worker.",
		"route_summary":   "Relocate a member to a machine (placement only; never touches desired_state). Also accepts an outsource-worker id: the same move-one-agent verb relocates the worker.",
	},
	"remove_task_artifact": {
		"openapi_summary": "",
		"route_summary":   "Remove one artifact from a task's set (executor/owner/admin).",
	},
	"replace_global_context": {
		"openapi_summary": "Whole-block replace of the user-custom additive block ({text}).",
		"route_summary":   "Whole-block replace of the user-custom additive block ({text}).",
	},
	"replace_insight": {
		"openapi_summary": "Whole-doc replace of a per-role insight doc ({text}).",
		"route_summary":   "Whole-doc replace of a per-role insight doc ({text}).",
	},
	"replace_lessons": {
		"openapi_summary": "Whole-doc replace of a per-role lessons doc ({text}).",
		"route_summary":   "Whole-doc replace of a per-role lessons doc ({text}).",
	},
	"reset_insight": {
		"openapi_summary": "Reset a per-role insight doc to its factory seed (idempotent tombstone overlay).",
		"route_summary":   "Reset a per-role insight doc to its factory seed (idempotent tombstone overlay).",
	},
	"resume_summary": {
		"openapi_summary": "Bounded LIGHT wake snapshot for the caller (what it carries is enumerated in the description, not here).",
		"route_summary":   "Bounded LIGHT wake snapshot for the caller (identity-locked; what it carries is enumerated in the description, not here).",
	},
	"set_task_priority": {
		"openapi_summary": "Set a task's priority (owner/admin agent any value; the executor any value on their own task — frozen included, T-6020).",
		"route_summary":   "Set a task's priority (owner/admin agent any value; the executor any value on their own task — frozen included, T-6020).",
	},
	"stop_outsource_worker": {
		"openapi_summary": "Stop (停止) an outsource worker (owner/admin agent; kill + hold down).",
	},
	"submit_plan": {
		"openapi_summary": legacySubmitPlanDescription,
		"route_summary":   "Submit/replace the workflow plan (done steps are kept).",
	},
	"uninstall_warden_on_server_host": {
		"openapi_summary": "Teardown on server: run ocwarden teardown on the server's OWN host; machine_id is not a target selector and every target is currently refused (409).",
		"route_summary":   "Teardown on server: run ocwarden teardown on the server's OWN host; machine_id is not a target selector and every target is currently refused (409).",
	},
	"update_member": {
		"openapi_summary": "Edit a member (name / model / effort). Blank name / bad effort → 422.",
		"route_summary":   "Edit a member (name / model / effort). Blank name / bad effort → 422.",
	},
	"update_settings": {
		"route_summary": "Edit settings (owner and agent token TTLs / handover threshold); live immediately.",
	},
	"update_step_note": {
		"openapi_summary": "Write a step's working note (any status; wholesale replace).",
		"route_summary":   "Write a step's working note (any status; wholesale replace).",
	},
	"update_step_status": {
		"openapi_summary": "Report a step status (pending/in_progress/done).",
		"route_summary":   "Report a step status (pending/in_progress/done).",
	},
	"update_task_description": {
		"openapi_summary": "Correct a task's description (executor/admin; closed tasks included).",
		"route_summary":   "Correct a task's description (executor/admin; closed tasks included).",
	},
	"update_task_manual": {
		"openapi_summary": "Edit a task manual (partial; content fields agent-editable; assignee = owner/admin agent).",
		"route_summary":   "Edit a task manual (partial; content fields agent-editable; assignee = owner/admin agent).",
	},
	"update_task_title": {
		"openapi_summary": "Correct a task's title (executor/admin; closed tasks included).",
		"route_summary":   "Correct a task's title (executor/admin; closed tasks included).",
	},
	"write_task_learnings": {
		"openapi_summary": "Whole-doc replace of a type's learnings (task-close write-back).",
		"route_summary":   "Whole-doc replace of a type's learnings (task-close write-back).",
	},
}

type catalogSpec struct {
	Tools []struct {
		Name        string `json:"name"`
		InputSchema struct {
			Properties map[string]any `json:"properties"`
		} `json:"inputSchema"`
	} `json:"tools"`
}

// openapiParamsFor returns the parameter-name set an operation accepts: its
// path/query parameters plus the property names of its JSON request body
// ($ref resolved through components/schemas).
//
// The second result distinguishes the two reasons there may be nothing to
// compare, because collapsing them is how this leg rots silently: "the route is
// not in openapi at all" is a REAL failure (a live MCP tool with no spec entry),
// while "the body is multipart" is a legitimate skip. An earlier version of this
// helper returned the same bare false for both, which meant a route quietly
// disappearing from openapi looked exactly like an upload endpoint and reddened
// nothing — the same enumerate-and-hope shape this whole ticket is about.
type openapiLookup int

const (
	openapiFound       openapiLookup = iota // compare it
	openapiMissing                          // route exists, spec does not — FAIL
	openapiNotJSONBody                      // multipart upload — genuinely skip
)

func openapiParamsFor(spec openapiSpec, method, path string) (map[string]bool, openapiLookup) {
	ops, hit := spec.Paths[path]
	if !hit {
		return nil, openapiMissing
	}
	op, hit := ops[strings.ToLower(method)]
	if !hit {
		return nil, openapiMissing
	}
	out := map[string]bool{}
	for _, p := range op.Parameters {
		out[p.Name] = true
	}
	if len(op.RequestBody.Content) > 0 {
		body, isJSON := op.RequestBody.Content["application/json"]
		if !isJSON {
			return nil, openapiNotJSONBody // multipart upload — nothing to compare
		}
		if ref, isRef := body.Schema["$ref"].(string); isRef {
			named := ref[strings.LastIndex(ref, "/")+1:]
			for name := range spec.Components.Schemas[named].Properties {
				out[name] = true
			}
		} else if inline, isInline := body.Schema["properties"].(map[string]any); isInline {
			for name := range inline {
				out[name] = true
			}
		}
	}
	return out, openapiFound
}

func TestFrozenCatalogAgreesWithOpenapiOnEveryToolsParameters(t *testing.T) {
	rawAPI, err := os.ReadFile("../../spec/openapi.json")
	if err != nil {
		t.Fatalf("read openapi: %v", err)
	}
	var api openapiSpec
	if err := json.Unmarshal(rawAPI, &api); err != nil {
		t.Fatalf("parse openapi: %v", err)
	}
	rawCat, err := os.ReadFile("../../spec/mcp-catalog.json")
	if err != nil {
		t.Fatalf("read catalog: %v", err)
	}
	var cat catalogSpec
	if err := json.Unmarshal(rawCat, &cat); err != nil {
		t.Fatalf("parse catalog: %v", err)
	}
	catalogProps := map[string]map[string]bool{}
	for _, tool := range cat.Tools {
		props := map[string]bool{}
		for name := range tool.InputSchema.Properties {
			props[name] = true
		}
		catalogProps[tool.Name] = props
	}

	index := mcpToolIndex(defaultRouteSpecs())
	if len(index) == 0 {
		t.Fatalf("no MCP tools in the routes table — this test would pass vacuously")
	}
	compared := 0
	for name, spec := range index {
		want, lookup := openapiParamsFor(api, spec.Method, spec.Path)
		switch lookup {
		case openapiNotJSONBody:
			continue // multipart upload — genuinely nothing to compare
		case openapiMissing:
			t.Errorf("tool %q is a live MCP tool on the routes table but %s %s is "+
				"NOT in spec/openapi.json — the routes≡openapi leg of this "+
				"confrontation has broken for it, and every parameter check "+
				"below would have skipped it in silence",
				name, spec.Method, spec.Path)
			continue
		}
		got, listed := catalogProps[name]
		if !listed {
			t.Errorf("tool %q is on the routes table (and in openapi as %s %s) but "+
				"MISSING from spec/mcp-catalog.json — agents reach tools only "+
				"through tools/list, so this tool does not exist for them",
				name, spec.Method, spec.Path)
			continue
		}
		compared++
		baseline := map[string]bool{}
		for _, p := range knownCatalogDrift[name] {
			baseline[p] = true
		}
		overweight := map[string]bool{}
		for _, p := range openapiOverweight[name] {
			overweight[p] = true
		}
		offMCP := map[string]bool{}
		for _, p := range deliberatelyOffMCP[name] {
			offMCP[p] = true
		}
		var missing, extra []string
		for p := range want {
			if got[p] {
				// Recorded but actually present now: the debt was paid, or the
				// "openapi is overweight" call was wrong. Either way the map has
				// to shrink, or it starts hiding real drift.
				if baseline[p] {
					t.Errorf("stale baseline: tool %q advertises %q again — delete "+
						"it from knownCatalogDrift. An allowlist nobody prunes "+
						"stops being a record of debt and becomes a hole.", name, p)
				}
				if overweight[p] {
					t.Errorf("stale entry: tool %q now advertises %q, so it was NOT "+
						"an unread field — delete it from openapiOverweight and "+
						"work out which side changed.", name, p)
				}
				if offMCP[p] {
					t.Errorf("tool %q now advertises %q, which is recorded as "+
						"deliberately off-MCP. Either the catalog gained a field "+
						"agents cannot honestly produce, or the decision changed — "+
						"read the note on deliberatelyOffMCP before deleting it.", name, p)
				}
				continue
			}
			if baseline[p] || overweight[p] || offMCP[p] {
				continue
			}
			missing = append(missing, p)
		}
		for p := range got {
			if !want[p] {
				extra = append(extra, p)
			}
		}
		sort.Strings(missing)
		sort.Strings(extra)
		if len(missing) > 0 || len(extra) > 0 {
			t.Errorf("DRIFT on tool %q (%s %s): spec/openapi.json and "+
				"spec/mcp-catalog.json disagree about its parameters.\n"+
				"  in openapi but not in the catalog: %v (agents cannot send these)\n"+
				"  in the catalog but not in openapi: %v (agents are told to send "+
				"these and the server does not read them)\n"+
				"  Fix BOTH files; they are the same fact written twice.",
				name, spec.Method, spec.Path, missing, extra)
		}
	}
	if compared < 20 {
		t.Fatalf("only %d tools were actually compared — the routes/openapi join "+
			"has broken and this test has stopped discriminating (dead assertion, "+
			"the failure mode this file exists to prevent)", compared)
	}
	t.Logf("confronted %s tool(s) across routes table ≡ openapi ≡ frozen catalog",
		fmt.Sprint(compared))
}

func TestEveryMCPToolDescriptionAgreesWithItsSources(t *testing.T) {
	rawAPI, err := os.ReadFile("../../spec/openapi.json")
	if err != nil {
		t.Fatalf("read openapi: %v", err)
	}
	var api openapiSpec
	if err := json.Unmarshal(rawAPI, &api); err != nil {
		t.Fatalf("parse openapi: %v", err)
	}

	index := mcpToolIndex(defaultRouteSpecs())
	if len(index) == 0 {
		t.Fatalf("no MCP tools in the routes table — this test would pass vacuously")
	}
	if len(knownToolDescriptionDrift) == 0 {
		t.Fatalf("description-drift baseline is empty — the comparison below would be vacuous")
	}

	compared := 0
	baselineEntries := 0
	for _, sources := range knownToolDescriptionDrift {
		baselineEntries += len(sources)
	}
	if baselineEntries == 0 {
		t.Fatalf("description-drift baseline has no source entries — the comparison below would be vacuous")
	}
	observedBaselineEntries := 0
	for name, route := range index {
		ops, ok := api.Paths[route.Path]
		if !ok {
			t.Errorf("tool %q is on the routes table but %s %s is missing from openapi", name, route.Method, route.Path)
			continue
		}
		op, ok := ops[strings.ToLower(route.Method)]
		if !ok {
			t.Errorf("tool %q is on the routes table but %s %s is missing from openapi", name, route.Method, route.Path)
			continue
		}
		if !op.XMCP.Include {
			t.Errorf("tool %q is on the MCP routes table but %s %s is not x-mcp.include", name, route.Method, route.Path)
			continue
		}
		if op.XMCP.Description == "" {
			t.Errorf("tool %q has an empty x-mcp.description", name)
			continue
		}
		compared++

		baseline := knownToolDescriptionDrift[name]
		actual := map[string]string{
			"route_summary":   route.Summary,
			"openapi_summary": op.Summary,
		}
		for source, got := range actual {
			drifting := got != op.XMCP.Description
			expectedLegacyProse, baselined := baseline[source]
			if drifting && !baselined {
				t.Errorf("NEW description drift for tool %q (%s): %q disagrees with x-mcp.description %q", name, source, got, op.XMCP.Description)
			}
			if drifting && baselined && got != expectedLegacyProse {
				t.Errorf("changed description-drift baseline for tool %q (%s): its known false prose changed — align it to x-mcp.description or explicitly review the baseline entry", name, source)
			}
			if !drifting && baselined {
				t.Errorf("stale description-drift baseline for tool %q (%s): it now agrees with x-mcp.description — delete this entry", name, source)
			}
		}
		for source := range baseline {
			if _, known := actual[source]; !known {
				t.Errorf("description-drift baseline for tool %q names unknown source %q", name, source)
				continue
			}
			observedBaselineEntries++
		}
	}
	if compared != len(index) {
		t.Fatalf("only %d/%d MCP tools were compared for descriptions — the routes/openapi join has stopped discriminating", compared, len(index))
	}
	if observedBaselineEntries != baselineEntries {
		t.Fatalf("only %d/%d description-drift baseline entries joined a live tool/source — the baseline has stopped being evidence", observedBaselineEntries, baselineEntries)
	}
}
