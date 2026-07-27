package main

// api_monitoring.go — the observation channel (handlers.
// handle_ingest_agent_context / handle_ingest_telemetry /
// handle_get_monitoring): the two IN-MEMORY ingest stores (restart amnesia is
// contract, lifecycle.md §3) keyed on the VERIFIED token sub, the durable
// command_result fold onto member.last_op*, and the three-section monitoring
// fold that never fabricates a number.

import (
	"fmt"
	"math"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

// commandResultLogMax re-clamps the folded command_result log (the warden
// already truncates to 4 KB; the body is untrusted).
const commandResultLogMax = 4096

// commandResultReasonMax caps the folded command_result reason — a one-line
// structured "<code>: <detail>" summary (SpawnOutcome.Reason), NOT the log
// dump, so it gets a much tighter bound (the body is untrusted).
const commandResultReasonMax = 512

// POST /api/agent/context — ingest the caller's context gauge. Non-numeric
// context_pct → flat 400 (never 422). MERGES onto the prior entry so the
// session boot_ts anchor survives.
func (s *apiServer) HandleIngestAgentContextApiAgentContextPost(w http.ResponseWriter, r *http.Request) {
	var body AgentContextIngestDTO
	if !decodeJSONBody(w, r, &body) {
		return
	}
	pct, ok := body.ContextPct.(float64) // JSON numbers land as float64; bool/str/nil fail
	if !ok {
		writeError(w, http.StatusBadRequest, "context_pct must be a number")
		return
	}
	var compactions *int
	if body.CompactionCount != nil {
		value, ok := body.CompactionCount.(float64)
		if !ok || value < 0 || value != math.Trunc(value) {
			writeError(w, http.StatusBadRequest, "compaction_count must be a non-negative integer")
			return
		}
		count := int(value)
		compactions = &count
	}
	agentID := currentActor(r)
	rateLimits := map[string]any{}
	if body.RateLimits != nil {
		for k, v := range *body.RateLimits {
			rateLimits[k] = v
		}
	}
	now := nowSecs()
	entry := s.gauge.Get(agentID)
	if entry == nil {
		entry = map[string]any{}
	}
	entry["context_pct"] = pct
	entry["rate_limits"] = rateLimits
	entry["ts"] = now
	entry["context_pct_ts"] = now
	if compactions != nil {
		entry["compaction_count"] = *compactions
	}
	s.gauge.Set(agentID, entry)
	// No agent consumes the context signal on the wire (it drives the
	// server-side context-high band, not fan-out); owner cockpit only.
	s.hub.Publish("context", "signal", "context", agentID, nil, audienceOwnerOnly(), requestTrigger(r))
	writeJSON(w, http.StatusOK, agentContextDTO{
		AgentID:         agentID,
		ContextPct:      pct,
		CompactionCount: compactions,
		RateLimits:      rateLimits,
		TS:              now,
	})
}

// teleNum shapes a telemetry numeric: bool / non-number / negative sentinel
// (-1 = 未量到) → nil, NEVER a fabricated 0 (handlers._tele_num).
func teleNum(value any) *float64 {
	n, ok := value.(float64)
	if !ok || n < 0 {
		return nil
	}
	return &n
}

// teleBool shapes a telemetry boolean: absent / non-bool stays honest-nil.
func teleBool(value any) *bool {
	b, ok := value.(bool)
	if !ok {
		return nil
	}
	return &b
}

// commandResultAtEpoch parses a command_result "at" (RFC3339 from the warden;
// a bare epoch number accepted for robustness; garbage → 0.0 so a bad
// timestamp can never shortcut presence).
func commandResultAtEpoch(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case string:
		text := strings.TrimSpace(v)
		if text == "" {
			return 0.0
		}
		if t, err := time.Parse(time.RFC3339, text); err == nil {
			return float64(t.UnixNano()) / 1e9
		}
		if t, err := time.ParseInLocation("2006-01-02T15:04:05", text, time.Local); err == nil {
			return float64(t.UnixNano()) / 1e9
		}
	}
	return 0.0
}

// stopNoopReasonPrefix is the prefix of the warden stop receipt Reason
// (cli/ocwarden/command.go rpcStop/rpcWorkerStop) when the robust stop was an
// idempotent NO-OP: the addressed session did not exist on that warden and no
// member process was found — nothing was actually killed. Twin of the
// spawnClobberReasonPrefix cross-module contract (reconcile.go).
const stopNoopReasonPrefix = "no_such_session"

// isStopNoopReceipt reports whether a command_result receipt is a no-op stop:
// an OK stop whose reason carries the no_such_session code. Such a receipt
// proves only that ONE warden's tmux view held no session — it is NOT evidence
// the member was killed (identity sweeps broadcast stop to every other warden;
// a mis-routed / already-dead stop no-ops the same way), so folding it over
// last_op would forge a "successfully stopped" story onto a member whose live
// session was never touched (T-9adc, the 2026-07-20 incident's misleading
// last_op=stop/ok=true). Callers SKIP the last_op fold for these receipts.
func isStopNoopReceipt(rpc string, ok *bool, reason string) bool {
	if rpc != "stop" && rpc != "worker_stop" {
		return false
	}
	if ok == nil || !*ok {
		return false // a FAILED stop is always folded — failure must stay visible
	}
	return strings.HasPrefix(reason, stopNoopReasonPrefix)
}

// wakeTimeoutReasonCode is the reason CODE stampWakeObservability writes
// (reconcile.go) when a START lapsed its start window. It is the only
// dispatch-level — as opposed to execution-level — writer of last_op_reason
// today, and naming it here is the cross-module contract that lets the receipt
// fold recognise what it is about to overwrite.
const wakeTimeoutReasonCode = "wake_timeout"

// supersededDispatchClue returns a one-line carry-forward of the member's
// CURRENT last_op_reason when that reason is a dispatch-level diagnosis (the
// "nothing ever came back" story) about to be replaced by an execution receipt
// (the "the machine acted and here is what happened" story). Empty when there is
// nothing worth preserving — a receipt superseding a receipt is ordinary.
//
// Deliberately in-place inside the existing five last_op* fields: a separate
// durable slot would grow MemberDTO, and the wire is frozen (CLAUDE.md §13).
// This follows the isStopNoopReceipt precedent — the fold already knows that not
// every receipt deserves the slot on its own terms.
func supersededDispatchClue(m Member) string {
	if !strings.HasPrefix(m.LastOpReason, wakeTimeoutReasonCode+":") {
		return ""
	}
	return fmt.Sprintf("[superseded dispatch diagnosis @%.0f] %s",
		m.LastOpAt, m.LastOpReason)
}

// foldCommandResult folds ONE warden command_result receipt onto the
// addressed member's last_op* fields (handlers._fold_command_result).
// Fail-safe: a missing/blank member_id or an unknown member is ignored; any
// storage fault is logged and swallowed (an observation fold must never 500).
func (s *apiServer) foldCommandResult(commandResult map[string]any, trigger string) {
	// T-9ccf: a worker receipt keys on worker_id (a worker has no roster member) —
	// route it to the worker fold FIRST. The warden sends exactly one id per
	// receipt (command.go), so worker_id present ⇒ this is a worker receipt.
	workerIDRaw, _ := commandResult["worker_id"].(string)
	if workerID := strings.TrimSpace(workerIDRaw); workerID != "" {
		s.foldWorkerCommandResult(workerID, commandResult, trigger)
		return
	}
	memberIDRaw, _ := commandResult["member_id"].(string)
	memberID := strings.TrimSpace(memberIDRaw)
	if memberID == "" {
		return
	}
	m, err := s.dal.GetMember(memberID)
	if err != nil {
		fmt.Fprintf(os.Stderr,
			"[monitoring] command_result fold failed for member %q: %v\n", memberID, err)
		return
	}
	if m == nil {
		fmt.Fprintf(os.Stderr,
			"[monitoring] command_result for unknown member %q — ignored\n", memberID)
		return
	}
	// P5b convergence: a worker start/stop now rides the member verbs, so its
	// receipt arrives keyed member_id == the ow- id. Route it to the worker fold
	// (PutOutsourceWorker + the owner-only outsource_worker delta) — never the
	// member putMember fold, whose member-topic fan-out would leak an outsource
	// row onto the staff roster wire.
	if m.Kind == KindOutsource {
		s.foldWorkerCommandResult(memberID, commandResult, trigger)
		return
	}
	rpc, _ := commandResult["rpc"].(string)
	logText, isLog := commandResult["log"].(string)
	if !isLog {
		logText, _ = commandResult["reason"].(string)
	}
	if len(logText) > commandResultLogMax {
		logText = logText[:commandResultLogMax]
	}
	// The structured cause ("<code>: <detail>" — SpawnOutcome.Reason), persisted
	// SEPARATELY from the log so the owner-facing 最近操作 block can show a
	// one-line WHY without parsing the log dump. A receipt without one (old
	// warden / successful op) folds "" — the FE then shows status-only as before.
	reason, _ := commandResult["reason"].(string)
	if len(reason) > commandResultReasonMax {
		reason = reason[:commandResultReasonMax]
	}
	var okPtr *bool
	if ok, isBool := commandResult["ok"].(bool); isBool {
		okPtr = &ok
	}
	// T-9adc: a NO-OP stop receipt (idempotent ok over a session that was never
	// there) must not overwrite last_op — get_member's 最近操作 must reflect
	// what actually HAPPENED, not what one session-less warden politely 200'd.
	if isStopNoopReceipt(rpc, okPtr, reason) {
		fmt.Fprintf(os.Stderr,
			"[monitoring] no-op stop receipt for member %q (%s) — last_op NOT folded\n",
			memberID, reason)
		return
	}
	// T-66a2: the five last_op* fields are ONE slot with TWO blind writers —
	// this fold (an EXECUTION outcome: the machine received the order and acted)
	// and stampWakeObservability (a DISPATCH-level diagnosis: nothing ever came
	// back). The second is the clue that decides whether to go look at that
	// machine at all, and until now the next spawn receipt erased it outright:
	// not archived, not superseded, just gone in one putMember. The receipt is
	// genuinely newer and must still win the slot — but the clue it displaces is
	// carried into last_op_log rather than destroyed. Prefixed, not appended, so
	// it survives the commandResultLogMax clamp of a long log dump; bounded to
	// ONE hop because the new last_op_reason is the receipt's own, so the next
	// fold has no dispatch diagnosis left to carry.
	if clue := supersededDispatchClue(*m); clue != "" {
		logText = clue + "\n" + logText
		if len(logText) > commandResultLogMax {
			logText = logText[:commandResultLogMax]
		}
		fmt.Fprintf(os.Stderr,
			"[monitoring] member %q: %s receipt supersedes a dispatch diagnosis — "+
				"carried into last_op_log (%s)\n", memberID, rpc, m.LastOpReason)
	}
	m.LastOp = rpc
	m.LastOpOK = okPtr
	m.LastOpLog = logText
	m.LastOpReason = reason
	m.LastOpAt = commandResultAtEpoch(commandResult["at"])
	// UNINSTALL CONVERGENCE: an ok uninstall receipt folds the machine
	// lifecycle intent back to offline (record kept — re-installable).
	if m.LastOp == "uninstall" && m.LastOpOK != nil && *m.LastOpOK {
		m.DesiredState = DesiredStateOffline
	}
	if err := s.putMember(*m, trigger); err != nil {
		fmt.Fprintf(os.Stderr,
			"[monitoring] command_result fold failed for member %q: %v\n", memberID, err)
	}
}

// foldWorkerCommandResult folds ONE warden worker command_result receipt
// (worker_start / worker_stop, T-9ccf) onto the addressed outsource_worker
// row's last_op* fields — the worker twin of foldCommandResult's member fold,
// reusing the SAME clamps and three-valued ok. Fail-safe: an unknown worker or
// any storage fault is logged and swallowed (an observation fold must never
// 500), and it fans an owner-only outsource_worker delta so the cockpit sees
// the fresh reason immediately. It deliberately does NOT touch lifecycle
// (status / released_ts) — a receipt is an observation, never a state change.
//
// Holds s.outsourceMu for the whole read-modify-write-publish: the worker row is
// also read-modify-written by notifyWorkerSpawn (the spawn stamp) under the same
// lock, so without it the two full-row upserts race and the later write silently
// clobbers the earlier (a spawn stamp could erase a just-folded failure reason,
// or vice versa — the "失敗可見" DoD's exact hazard). The telemetry HTTP handler
// that reaches here holds no scheduler lock, so acquiring it is deadlock-free.
func (s *apiServer) foldWorkerCommandResult(workerID string, commandResult map[string]any, trigger string) {
	s.outsourceMu.Lock()
	defer s.outsourceMu.Unlock()

	w, err := s.dal.GetOutsourceWorker(workerID)
	if err != nil {
		fmt.Fprintf(os.Stderr,
			"[monitoring] worker command_result fold failed for %q: %v\n", workerID, err)
		return
	}
	if w == nil {
		fmt.Fprintf(os.Stderr,
			"[monitoring] worker command_result for unknown worker %q — ignored\n", workerID)
		return
	}
	rpc, _ := commandResult["rpc"].(string)
	logText, isLog := commandResult["log"].(string)
	if !isLog {
		logText, _ = commandResult["reason"].(string)
	}
	if len(logText) > commandResultLogMax {
		logText = logText[:commandResultLogMax]
	}
	reason, _ := commandResult["reason"].(string)
	if len(reason) > commandResultReasonMax {
		reason = reason[:commandResultReasonMax]
	}
	var okVal *bool
	if ok, isBool := commandResult["ok"].(bool); isBool {
		v := ok
		okVal = &v
	}
	// T-9adc: a NO-OP stop receipt never overwrites the worker's last_op —
	// same honesty rule as the member fold (identity sweeps broadcast stop to
	// every other warden; their polite idempotent OKs are not kill evidence).
	if isStopNoopReceipt(rpc, okVal, reason) {
		fmt.Fprintf(os.Stderr,
			"[monitoring] no-op stop receipt for worker %q (%s) — last_op NOT folded\n",
			workerID, reason)
		return
	}
	w.LastOp = rpc
	w.LastOpOK = okVal
	w.LastOpLog = logText
	w.LastOpReason = reason
	w.LastOpAt = commandResultAtEpoch(commandResult["at"])
	if err := s.dal.PutOutsourceWorker(*w); err != nil {
		fmt.Fprintf(os.Stderr,
			"[monitoring] worker command_result fold failed for %q: %v\n", workerID, err)
		return
	}
	// DoD② 換機: a REFUSED start means the last spawn target could not boot
	// this worker (RAM/creds/ghost) — bench that machine for it so the next
	// re-spawn rotates to a different warden instead of re-picking the same bad
	// one. The target comes from the in-memory spawn map (notifyWorkerSpawn
	// stamped it under this same lock; durable spawn columns retired in P7d).
	// Both the converged member verb (`start`, P5b) and the legacy worker verb
	// (an old warden in the transition window) count.
	if (rpc == reconcileCmdStart || rpc == legacyWardenCmdWorkerStart) &&
		okVal != nil && !*okVal {
		s.benchWorkerMachine(w.ID, s.workerSpawnTarget[w.ID], nowSecs())
	}
	s.publishOutsourceWorker(*w, trigger)
}

// POST /api/monitoring/telemetry — ingest one warden telemetry report:
// partial-report MERGE onto the in-memory entry; an all-absent body or a
// wrong-typed field is a flat 400 (never 422); command_result additionally
// folds durably onto the addressed member.
func (s *apiServer) HandleIngestTelemetryApiMonitoringTelemetryPost(w http.ResponseWriter, r *http.Request) {
	var body AgentTelemetryIngestDTO
	if !decodeJSONBody(w, r, &body) {
		return
	}
	if body.RateLimits == nil && body.Tokens == nil && body.Hardware == nil &&
		body.Binaries == nil && body.Claude == nil && body.Cost == nil &&
		body.Effort == nil && body.Runtime == nil && body.Runtimes == nil &&
		body.SelfUpdate == nil && body.CommandResult == nil {
		writeError(w, http.StatusBadRequest,
			"rate_limits, tokens, hardware, binaries, claude, cost, effort, runtime, runtimes, "+
				"self_update or command_result is required")
		return
	}
	asObject := func(v any, name string) (map[string]any, bool) {
		if v == nil {
			return nil, true
		}
		obj, ok := v.(map[string]any)
		if !ok {
			writeError(w, http.StatusBadRequest, name+" must be an object")
			return nil, false
		}
		return obj, true
	}
	// hardware / claude / runtimes DECLARE their nested shape in the frozen spec
	// (T-90be), so codegen types them as *map[string]interface{} instead of the
	// bare interface{} the undeclared blocks still get. The declaration is what
	// lets the CI guard see a nested rename; it deliberately does NOT close the
	// object (additionalProperties stays true), so an undeclared nested key is
	// still accepted and stored exactly as before — a warden that grows a probe
	// must never have its WHOLE report refused (that is the a7fa594 outage).
	// Dereferencing here keeps every downstream line reading the same
	// map[string]any it always did.
	declaredObject := func(p *map[string]any) map[string]any {
		if p == nil {
			return nil
		}
		return *p
	}
	rateLimits, ok := asObject(body.RateLimits, "rate_limits")
	if !ok {
		return
	}
	tokens, ok := asObject(body.Tokens, "tokens")
	if !ok {
		return
	}
	hardware := declaredObject(body.Hardware)
	binaries, ok := asObject(body.Binaries, "binaries")
	if !ok {
		return
	}
	claude := declaredObject(body.Claude)
	runtimes := declaredObject(body.Runtimes)
	for name, raw := range runtimes {
		if !ValidRuntime(name) {
			writeError(w, http.StatusBadRequest, "runtimes keys must be 'claude' or 'codex'")
			return
		}
		capability, isObj := raw.(map[string]any)
		if !isObj {
			writeError(w, http.StatusBadRequest, "runtimes."+name+" must be an object")
			return
		}
		if v, exists := capability["installed"]; exists {
			if _, valid := v.(bool); !valid {
				writeError(w, http.StatusBadRequest, "runtimes."+name+".installed must be a boolean")
				return
			}
		}
		if v, exists := capability["logged_in"]; exists && v != nil {
			if _, valid := v.(bool); !valid {
				writeError(w, http.StatusBadRequest, "runtimes."+name+".logged_in must be a boolean or null")
				return
			}
		}
		if v, exists := capability["version"]; exists && v != nil {
			if _, valid := v.(string); !valid {
				writeError(w, http.StatusBadRequest, "runtimes."+name+".version must be a string or null")
				return
			}
		}
	}
	var runtime *string
	if body.Runtime != nil {
		text, isStr := body.Runtime.(string)
		if !isStr || !ValidRuntime(text) {
			writeError(w, http.StatusBadRequest, "runtime must be 'claude' or 'codex'")
			return
		}
		runtime = &text
	}
	var cost *float64
	if body.Cost != nil {
		n, isNum := body.Cost.(float64)
		if !isNum {
			writeError(w, http.StatusBadRequest, "cost must be a number")
			return
		}
		cost = &n
	}
	var effort *string
	if body.Effort != nil {
		text, isStr := body.Effort.(string)
		if !isStr {
			writeError(w, http.StatusBadRequest, "effort must be a string")
			return
		}
		effort = &text
	}
	selfUpdate, ok := asObject(body.SelfUpdate, "self_update")
	if !ok {
		return
	}
	commandResult, ok := asObject(body.CommandResult, "command_result")
	if !ok {
		return
	}

	agentID := currentActor(r)
	entry := s.telemetry.Get(agentID)
	if entry == nil {
		entry = map[string]any{}
	}
	if body.RateLimits != nil {
		entry["rate_limits"] = rateLimits
	}
	if body.Tokens != nil {
		entry["tokens"] = tokens
	}
	if body.Hardware != nil {
		entry["hardware"] = hardware
		// Stamp WHEN this hardware sample was taken, separately from the entry's
		// `ts`. The entry ts advances on EVERY report — a command_result receipt or
		// an identity-only heartbeat carries no hardware, yet would refresh `ts` and
		// make a long-dead CPU reading look like it arrived a second ago. The read
		// path's freshness verdict must be about the hardware sample itself, so it
		// gets its own stamp (same shape as the gauge's context_pct_ts).
		entry["hardware_ts"] = nowSecs()
	}
	if body.Binaries != nil {
		entry["binaries"] = binaries
	}
	if body.Claude != nil {
		entry["claude"] = claude
	}
	if body.Runtimes != nil {
		entry["runtimes"] = runtimes
		// Same per-sample stamp as hardware_ts, same reason (T-b36a): the entry
		// ts advances on every report, so a receipt carrying no capability probe
		// would make an arbitrarily old "codex not logged in" look freshly
		// measured. Placement (machineSupportsRuntime) deliberately does NOT
		// consult this — expiring the map there would silently reclassify a quiet
		// machine as a legacy warden and hand it Claude work; freshness here is a
		// question about what the COCKPIT may present as current.
		entry["runtimes_ts"] = nowSecs()
	}
	if runtime != nil {
		entry["runtime"] = *runtime
	}
	if cost != nil {
		entry["cost"] = *cost
	}
	if effort != nil {
		entry["effort"] = *effort
	}
	if selfUpdate != nil {
		entry["self_update"] = selfUpdate
		fmt.Fprintf(os.Stderr,
			"[monitoring] warden self-update: agent=%s binary=%v %v->%v at=%v\n",
			agentID, orUnknown(selfUpdate["binary"]), orUnknown(selfUpdate["old_hash"]),
			orUnknown(selfUpdate["new_hash"]), orUnknown(selfUpdate["at"]))
	}
	if commandResult != nil {
		entry["command_result"] = commandResult
	}
	// Machine attribution comes from AUTH first (the token's machine_id
	// placement claim — caller-identity-convention.md: facts derive from the
	// verified token, not a self-report). The payload machine is only a
	// fallback for claim-less tokens (/api/mint long-lived tokens and
	// outsource-worker tokens mint machine_id "none" by design; a member
	// without desired_machine_id boots claim-less too).
	if claim := currentMachineClaim(r); claim != "" {
		entry["machine"] = claim
	} else if machine, isStr := body.Machine.(string); isStr && machine != "" {
		entry["machine"] = machine
	}
	// Account keys belong to a runtime-specific identity space, so the key, its
	// provenance stamp and its reporter label move as ONE unit through the
	// partial merge — see applyAccountReport (account_display.go) for the three
	// fail-closed rules that keep the pairing true across the whole sequence of
	// reports, not just the happy path.
	applyAccountReport(entry, body.Account, body.AccountLabel, runtime)
	entry["ts"] = nowSecs()
	s.telemetry.Set(agentID, entry)
	// No agent consumes the monitoring signal on the wire; owner cockpit only.
	s.hub.Publish("monitoring", "signal", "monitoring", agentID, nil, audienceOwnerOnly(), requestTrigger(r))

	if commandResult != nil {
		s.foldCommandResult(commandResult, requestTrigger(r))
	}

	writeJSON(w, http.StatusOK, agentTelemetryDTO{
		AgentID:       agentID,
		Machine:       entryStr(entry, "machine"),
		Account:       entryStr(entry, "account"),
		RateLimits:    entryObj(entry, "rate_limits"),
		Tokens:        entryObj(entry, "tokens"),
		Hardware:      entryObj(entry, "hardware"),
		Binaries:      entryObj(entry, "binaries"),
		Claude:        entryObj(entry, "claude"),
		Runtime:       entryStr(entry, "runtime"),
		Runtimes:      entryObj(entry, "runtimes"),
		Cost:          entryNum(entry, "cost"),
		Effort:        entryStr(entry, "effort"),
		SelfUpdate:    entryObj(entry, "self_update"),
		CommandResult: entryObj(entry, "command_result"),
		TS:            entry["ts"].(float64),
	})
}

func orUnknown(v any) any {
	if v == nil {
		return "?"
	}
	return v
}

func entryStr(entry map[string]any, key string) *string {
	if s, ok := entry[key].(string); ok {
		return &s
	}
	return nil
}

func entryObj(entry map[string]any, key string) map[string]any {
	obj, _ := entry[key].(map[string]any)
	return obj
}

func entryNum(entry map[string]any, key string) *float64 {
	if n, ok := entry[key].(float64); ok {
		return &n
	}
	return nil
}

// telemetryFreshSecs is how long a reported telemetry SAMPLE stays serveable —
// the hardware snapshot and the runtime capability probes both ride the same
// warden heartbeat, so they get the same window rather than two knobs that can
// disagree about what "recent" means.
//
// The warden heartbeat cadence is 30s (cli/ocwarden: reportThrottle), and a
// heartbeat the server ACCEPTS resets the loop straight back to that cadence, so
// a healthy machine restamps every ~30s plus request latency. 90s = three
// cadences: two heartbeats may be lost (a sleeping laptop, a network blip, a
// server restart mid-cycle) without a healthy machine ever flickering to "no
// data", while the window in which the cockpit can show a number that is no
// longer true is bounded at a minute and a half instead of being unbounded.
//
// Deliberately NOT tied to presence: "the warden's SSE dropped" and "nobody has
// measured this box lately" are different facts, and the second is the one these
// numbers depend on. A machine can be online with a wedged collector, and it can
// be briefly offline with a 5-second-old sample that is still perfectly true.
const telemetryFreshSecs = 90.0

// runtimeCapabilitiesStampOf reads WHEN the entry's capability probe was taken.
// Same fail-closed reading as hardwareStampOf: a map with no stamp has an
// unknown age, and unknown age is not freshness.
func runtimeCapabilitiesStampOf(entry map[string]any) float64 {
	ts, _ := entry["runtimes_ts"].(float64)
	return ts
}

// hardwareStampOf reads WHEN the entry's hardware sample was taken. Fail-closed:
// an entry carrying hardware with no hardware_ts has an UNKNOWN age, and unknown
// age is not freshness — it reads as the epoch, i.e. stale. Every writer of
// entry["hardware"] stamps this alongside it (see the ingest handler), so the
// zero case means "written by something that does not know when it measured",
// which must never be presented as a live reading.
func hardwareStampOf(entry map[string]any) float64 {
	ts, _ := entry["hardware_ts"].(float64)
	return ts
}

// monitoringActor is the ONE thing the account/machine value folds need from an
// actor: who it is (telemetry key), what runtime it is currently on (the
// provenance gate's other half — NOT read off the entry, see telemetryAccount),
// where it was observed, and what it has already banked. Members and outsource
// workers project onto it identically; nothing downstream of the projection can
// tell them apart, which is precisely the point (T-fc2f).
type monitoringActor struct {
	id      string
	runtime string
	host    string
	banked  float64
}

// GET /api/monitoring — the three-section fold (sessions / machines /
// accounts) over the roster + gauge + warden telemetry. NEVER fabricates a
// number: unmeasured stays null / honest-empty.
func (s *apiServer) HandleGetMonitoringApiMonitoringGet(w http.ResponseWriter, r *http.Request) {
	all, err := s.dal.ListMembers()
	if err != nil {
		internalError(w, err)
		return
	}
	var members []Member
	for _, m := range all {
		if m.RosterStatus != RosterStatusRemoved {
			members = append(members, m)
		}
	}
	telemetry := s.telemetry.Snapshot()
	gauge := s.gauge.Snapshot()
	now := nowSecs()
	machineNames, err := s.dal.MachineDisplayNames()
	if err != nil {
		internalError(w, err)
		return
	}
	accountNames, err := s.dal.AccountDisplayNames()
	if err != nil {
		internalError(w, err)
		return
	}
	resolveDisplay := func(overlay map[string]string, raw string) string {
		if name := overlay[raw]; name != "" {
			return name
		}
		return raw
	}
	tele := func(memberID string) map[string]any {
		return telemetry[memberID] // nil map reads are safe
	}

	// account_label overlay (T-260e): the freshest reporter-supplied
	// human-readable label per account key (oauthAccount email/org), owner-only
	// (PII gate inside the shared fold — empty for any non-owner caller). Scans
	// the WHOLE telemetry snapshot, so a label reported by an outsource-worker
	// session resolves here too (T-ba6b). The owner-edited alias (accountNames)
	// ALWAYS wins over the reported label (never overwritten).
	acctLabels := accountLabelOverlay(telemetry, s.principalOfRequest(r) == principalOwner)
	// Session rows serve a READABLE name or "" — never the raw credential key
	// (T-ba6b: the raw hash/uuid must not reach the member detail panel, which
	// joins its Claude Account cell from this field). The accounts fold below
	// keeps its raw-key fallback: that row is the aliasing surface.
	resolveSessionAccount := func(raw string) string {
		return resolveAccountDisplay(accountNames, acctLabels, raw)
	}

	// actors = members ∪ LIVE outsource workers. The three VALUE folds below
	// (machine attribution / rate-limit windows / cost) run over THIS list, not
	// over `members` alone — `dal.ListMembers()` is `WHERE kind != 'outsource'`,
	// so a member-only fold cannot see a single outsource session.
	//
	// That was the owner-reported bug (T-fc2f): the accounts overview HAPPILY
	// grew a row for an outsource-held key — the raw-key loop further down scans
	// the WHOLE telemetry snapshot — while machine / cost / five_hour / seven_day
	// all came from folds that had never looked at a worker. A key held by BOTH a
	// member and a worker (seth-m5-claude) hid it for months; a key held ONLY by a
	// worker (eva-m5-claude) rendered as a green card with three dashes.
	//
	// NOT a widening of attribution: telemetryAccount's provenance gate still
	// decides whether each entry's key may be read under that actor's runtime
	// (T-69bc / 2eb6590 — an account must never be borrowed from an older
	// runtime). This only fixes WHICH actors get asked.
	//
	// Members and workers are disjoint by construction (kind != 'outsource' vs
	// kind = 'outsource'), so each actor — and each actor's banked_cost —
	// contributes exactly once.
	workers, err := s.dal.ListOutsourceWorkers()
	if err != nil {
		internalError(w, err)
		return
	}
	actors := make([]monitoringActor, 0, len(members)+len(workers))
	for _, m := range members {
		actors = append(actors, monitoringActor{
			id: m.ID, runtime: m.Runtime, host: s.observedHost(m), banked: m.BankedCost,
		})
	}
	for _, wk := range workers {
		// Released workers are the worker twin of RosterStatusRemoved, which the
		// member list above filters out — a retired session's spend is history,
		// not current usage.
		if wk.Status == WorkerStatusReleased {
			continue
		}
		actors = append(actors, monitoringActor{
			id:      wk.ID,
			runtime: wk.Runtime,
			host:    s.observedWorkerHost(wk.ID, telemetry[wk.ID]),
			banked:  wk.BankedCost,
		})
	}

	sessions := []monitoringSessionDTO{}
	for _, m := range members {
		entry := tele(m.ID)
		roleName, err := s.memberRoleName(m)
		if err != nil {
			internalError(w, err)
			return
		}
		effort := ""
		if e, ok := entry["effort"].(string); ok {
			effort = e
		}
		// Runtime facts fold through the SAME foldActorRuntime the outsource
		// worker DTO reads (P7b read-path convergence — one fold, two wires).
		rt := foldActorRuntime(entry, gauge[m.ID], m.BankedCost, m.Runtime)
		sessions = append(sessions, monitoringSessionDTO{
			ID:              m.ID,
			Name:            m.Name,
			Role:            roleName,
			Runtime:         NormalizeRuntime(m.Runtime),
			Model:           m.Model,
			Effort:          effort,
			Machine:         resolveDisplay(machineNames, s.observedHost(m)),
			Account:         resolveSessionAccount(rt.account),
			Presence:        PresenceState(m, now, s.hub.IsOnline(m.ID)),
			ContextPct:      rt.contextPct,
			CompactionCount: rt.compactionCount,
			Cost:            rt.cost,
			BankedCost:      rt.bankedCost,
			Tokens:          entryObj(entry, "tokens"),
		})
	}

	// Machines: freshest hardware per OBSERVED host; CPU/RAM point-in-time,
	// never summed.
	hostCounts := map[string]int{}
	hwByHost := map[string]map[string]any{}
	hwTS := map[string]float64{}
	acctByHost := map[string]map[string]bool{}
	// Over `actors`, not `members`: an account observed only on an outsource
	// session must still attribute to the box it is burning on, and a host that
	// carries nothing but workers must still get a row for that account to hang
	// off. The agent count follows for the same reason — a row claiming 0 agents
	// while naming an account observed there would contradict itself. The
	// hardware fold is a provable no-op for workers: the agent-side reporter
	// (cli/ocagent contextreport telemetryBody) has no `hardware` field at all —
	// only the per-machine warden samples hardware.
	for _, a := range actors {
		entry := tele(a.id)
		host := a.host
		hostCounts[host]++
		if hw, ok := entry["hardware"].(map[string]any); ok {
			// Track the freshest sample per host REGARDLESS of age; whether its
			// numbers may be served is decided per row below. Keeping the stamp
			// for an expired sample is what lets the cockpit say "nobody has
			// measured this box for an hour" instead of showing the same blank
			// row as a box that has never reported hardware at all.
			if ts := hardwareStampOf(entry); ts > 0 {
				if prior, seen := hwTS[host]; !seen || ts > prior {
					hwTS[host] = ts
					hwByHost[host] = hw
				}
			}
		}
		if account := telemetryAccount(entry, a.runtime); account != "" {
			if acctByHost[host] == nil {
				acctByHost[host] = map[string]bool{}
			}
			acctByHost[host][account] = true
		}
	}
	hosts := make([]string, 0, len(hostCounts))
	for host := range hostCounts {
		hosts = append(hosts, host)
	}
	sort.Strings(hosts)
	machines := []monitoringMachineDTO{}
	for _, host := range hosts {
		hw := hwByHost[host]
		accounts := []string{}
		for account := range acctByHost[host] {
			accounts = append(accounts, account)
		}
		sort.Strings(accounts)
		// The host string IS the warden's member id (machines are warden
		// members), so the registry verdicts apply verbatim here.
		claudeVersion, claudeCredSource, claudeSubReadable := s.machineClaudeInfo(host)
		row := monitoringMachineDTO{
			Machine:             host,
			DisplayName:         resolveDisplay(machineNames, host),
			Agents:              hostCounts[host],
			Accounts:            accounts,
			BinStatus:           s.machineBinStatus(host),
			ClaudeVersion:       claudeVersion,
			ClaudeCredSource:    claudeCredSource,
			ClaudeSubReadable:   claudeSubReadable,
			RuntimeCapabilities: s.machineRuntimeCapabilities(host),
		}
		// A hardware sample is only SERVEABLE while it is fresh. Telemetry is
		// never cleared on disconnect (only on dismissal), so without this gate a
		// machine that reported once and then went away kept serving its last
		// CPU/RAM/battery numbers forever — beside an "offline" badge, with
		// nothing on the wire saying how old they were. That reads as a confident
		// live measurement and has already been misread. Past the TTL the numbers
		// go back to the SAME honest nulls a machine that never reported hardware
		// serves; the stamp stays on the wire so the two cases remain telling
		// apart. See telemetryFreshSecs for the threshold.
		if ts := hwTS[host]; ts > 0 {
			stamp := ts
			stale := now-ts > telemetryFreshSecs
			row.HardwareTS = &stamp
			// The verdict rides the wire next to the stamp for the same reason
			// runtime_capabilities_stale does: the window lives HERE, and a
			// cockpit that re-derived it from `now - hardware_ts` would be a
			// second home for the threshold, judged against a clock this server
			// has never seen. Without it the only way to render "expired" is to
			// guess from all-null values — which is wrong for a fresh sample
			// whose probes all failed (hardware {} is a legal report).
			row.HardwareStale = &stale
			if hw != nil && !stale {
				row.CpuPct = teleNum(hw["cpu_pct"])
				row.RamPct = teleNum(hw["ram_pct"])
				row.BatteryPct = teleNum(hw["battery_pct"])
				row.ACPower = teleBool(hw["ac_power"])
			}
		}
		// Capability probes carry the same age question with a different answer:
		// the values are KEPT past the window and marked instead of blanked,
		// because "codex was not logged in as of 3h ago" is the only surface that
		// explains a worker parked on machine_unavailable — deleting it would
		// trade one silent screen for another. Only the confidence is withdrawn.
		if entry := s.telemetry.Get(host); entry != nil {
			if ts := runtimeCapabilitiesStampOf(entry); ts > 0 {
				stamp := ts
				stale := now-ts > telemetryFreshSecs
				row.RuntimeCapabilitiesTS = &stamp
				row.RuntimeCapabilitiesStale = &stale
			} else if len(row.RuntimeCapabilities) > 0 {
				// A map of unknown age must not read as current either.
				stale := true
				row.RuntimeCapabilitiesStale = &stale
			}
		}
		machines = append(machines, row)
	}

	// Accounts: freshest rate_limits per account + Σ(live cost + banked_cost);
	// machine = the observed host set, display-resolved and comma-joined.
	acctHosts := map[string]map[string]bool{}
	for host, accts := range acctByHost {
		for account := range accts {
			if acctHosts[account] == nil {
				acctHosts[account] = map[string]bool{}
			}
			acctHosts[account][host] = true
		}
	}
	freshRL := map[string]map[string]any{}
	rlTS := map[string]float64{}
	acctCost := map[string]float64{}
	acctHasCost := map[string]bool{}
	// Same `actors` list, same reason: the freshest rate-limit window and the
	// account's total spend are ACCOUNT-wide facts, and an outsource session
	// burns the same quota and the same money as a member one. The agent-side
	// reporter is identity-agnostic — cli/ocagent's contextreport POSTs
	// rate_limits/cost keyed on nothing but its JWT sub — so a worker's entry
	// carries exactly the same fields a member's does.
	for _, a := range actors {
		entry := tele(a.id)
		account := telemetryAccount(entry, a.runtime)
		if account == "" {
			continue
		}
		ts, _ := entry["ts"].(float64)
		if rl, isObj := entry["rate_limits"].(map[string]any); isObj {
			if prior, seen := rlTS[account]; !seen || ts > prior {
				rlTS[account] = ts
				freshRL[account] = rl
			}
		}
		if cost, isNum := entry["cost"].(float64); isNum {
			acctCost[account] += cost
			acctHasCost[account] = true
		}
		// One banked balance per ACTOR, and members/workers are disjoint sets, so
		// a key held by both a member and a worker sums two distinct balances
		// rather than counting either one twice.
		if a.banked != 0 {
			acctCost[account] += a.banked
			acctHasCost[account] = true
		}
	}
	accountKeys := map[string]bool{}
	// An identified account is still useful observability even before Codex has
	// supplied a rate-limit window or a billable-cost estimate.  Keeping it in
	// the fold lets the cockpit show the bound ChatGPT account honestly instead
	// of presenting the misleading "no account usage data" empty state.
	for account := range acctHosts {
		accountKeys[account] = true
	}
	for account := range freshRL {
		accountKeys[account] = true
	}
	for account := range acctCost {
		accountKeys[account] = true
	}
	// The account overview is global owner observability, not a member-account
	// attribution cell. Keep every reported key visible here even when it is
	// deliberately withheld from a mismatched session/machine fold.
	for _, entry := range telemetry {
		if account, _ := entry["account"].(string); account != "" {
			accountKeys[account] = true
		}
	}
	sortedAccounts := make([]string, 0, len(accountKeys))
	for account := range accountKeys {
		sortedAccounts = append(sortedAccounts, account)
	}
	sort.Strings(sortedAccounts)
	accounts := []monitoringAccountDTO{}
	for _, account := range sortedAccounts {
		windows := ShapeWindows(anyOrNil(freshRL[account]), now)
		hostLabels := []string{}
		for host := range acctHosts[account] {
			hostLabels = append(hostLabels, resolveDisplay(machineNames, host))
		}
		sort.Strings(hostLabels)
		var cost *float64
		if acctHasCost[account] {
			rounded := round4(acctCost[account])
			cost = &rounded
		}
		// account_label passthrough: same owner-only overlay as the
		// display_name fold (acctLabels is empty for non-owner callers), so
		// the PII gate is reused verbatim. Absent label → field omitted.
		var accountLabel *string
		if label := acctLabels[account]; label != "" {
			accountLabel = &label
		}
		// Raw-key fallback stays HERE only: the accounts row is where the
		// owner aliases a key, so the key itself is the information.
		displayName := resolveAccountDisplay(accountNames, acctLabels, account)
		if displayName == "" {
			displayName = account
		}
		accounts = append(accounts, monitoringAccountDTO{
			Account:      account,
			AccountLabel: accountLabel,
			DisplayName:  displayName,
			Machine:      strings.Join(hostLabels, ", "),
			Cost:         cost,
			FiveHour:     windows["five_hour"],
			SevenDay:     windows["seven_day"],
		})
	}

	writeJSON(w, http.StatusOK, monitoringDTO{
		Sessions: sessions,
		Machines: machines,
		Accounts: accounts,
	})
}

// anyOrNil widens a possibly-nil typed map to `any` so ShapeWindows sees a
// true nil (a typed nil inside any is not nil to a type switch on map).
func anyOrNil(m map[string]any) any {
	if m == nil {
		return nil
	}
	return m
}

// round4 mirrors Python round(x, 4) (banker's rounding).
func round4(x float64) float64 {
	return math.RoundToEven(x*10000) / 10000
}
