package main

// account_display.go — the ONE readable-name fold for raw Claude account keys
// (credential hash / oauth uid-org), shared by the monitoring fold
// (api_monitoring.go) and the outsource worker projection (api_outsource.go /
// wire.go). T-ba6b: the worker DTO used to serve the raw telemetry key
// verbatim, so the 外包 detail panel showed credential hashes; both surfaces
// now resolve through the same precedence chain.

import "net/http"

// accountRuntimeKey is an internal provenance stamp paired with a telemetry
// account key. It is deliberately not part of any API DTO: account identity is
// runtime-specific, while the rest of telemetry remains one shared fold.
const accountRuntimeKey = "account_runtime"

// accountLabelOverlay folds the freshest reporter-supplied `account_label`
// (oauthAccount email/org — T-260e) per account key across EVERY telemetry
// entry — members and outsource workers alike (the pre-T-ba6b fold scanned
// only roster members, so an account reported by a worker-only session never
// picked up its label — recon §6-4/§6-6). PRIVACY GATE: the label is PII and
// OWNER-FACING ONLY — callers must pass isOwner=false for any non-owner
// caller, and the overlay then stays empty so every fold degrades honestly.
func accountLabelOverlay(telemetry map[string]map[string]any, isOwner bool) map[string]string {
	labels := map[string]string{}
	if !isOwner {
		return labels
	}
	labelTS := map[string]float64{}
	for _, entry := range telemetry {
		account, _ := entry["account"].(string)
		label, _ := entry["account_label"].(string)
		if account == "" || label == "" {
			continue
		}
		ts, _ := entry["ts"].(float64)
		if prior, seen := labelTS[account]; !seen || ts > prior {
			labelTS[account] = ts
			labels[account] = label
		}
	}
	return labels
}

// telemetryAccount returns the account key only when it belongs to the actor's
// current runtime. This is the sole runtime-aware decision in the account READ
// path: acquisition differs (Codex obtains a ChatGPT key; Claude obtains its
// OAuth key), but telemetry ingestion, folding, display resolution, and both
// UI wires share this accessor.
//
// The provenance stamp is the ONLY admissible proof, and it is written by
// applyAccountReport in the same partial-merge as the key itself. There is
// deliberately NO fallback to the entry's ordinary `runtime` field: that field
// is mutated by every later heartbeat, so reading it would let an account whose
// own provenance is missing silently inherit whichever runtime reported last —
// exactly the "account borrowed from some older runtime" bug this path exists
// to prevent. Unstamped ⇒ unproven ⇒ empty for BOTH runtimes.
func telemetryAccount(entry map[string]any, actorRuntime string) string {
	account, _ := entry["account"].(string)
	if account == "" {
		return ""
	}
	reported, _ := entry[accountRuntimeKey].(string)
	if reported == "" || NormalizeRuntime(reported) != NormalizeRuntime(actorRuntime) {
		return ""
	}
	return account
}

// clearAccountPairing retires the WHOLE account unit — key, provenance stamp
// and reporter label — from a telemetry entry. Partial removal is never right:
// a surviving key with no stamp is unreadable anyway, and a surviving label
// would outlive the key it describes.
func clearAccountPairing(entry map[string]any) {
	delete(entry, "account")
	delete(entry, accountRuntimeKey)
	delete(entry, "account_label")
}

// applyAccountReport merges one report's account facts into the actor's durable
// (in-memory, partial-merge) telemetry entry. It is the single writer of the
// account unit, so the key and its provenance can never drift apart across the
// REAL sequence of reports — not merely in the happy path. Three fail-closed
// rules:
//
//  1. a report whose runtime disagrees with the stored stamp RETIRES the stored
//     pairing before anything else: that key belonged to the runtime the actor
//     has just left, so it must not survive to be served under the new one
//     (the member row's runtime can lag a live switch by a whole reconcile);
//  2. an account reported WITHOUT a runtime is unprovable — it is neither
//     stored NOR allowed to leave the previous pairing standing. Leaving it
//     standing is what let a later runtime-only heartbeat be served an older
//     runtime's key;
//  3. only an account reported WITH a runtime writes a new pairing, and the
//     reporter label rides along with it (label alone, on a proven report,
//     still attaches to the standing key — that is the T-260e overlay).
//
// Every shipped reporter already sends `runtime` alongside `account`
// (cli/ocagent contextreport.go always sets it; cli/ocwarden codex_session.go
// posts runtime+account together), so rule 2 costs nothing in practice and
// self-heals on the next report.
func applyAccountReport(entry map[string]any, rawAccount, rawLabel any, runtime *string) {
	account, _ := rawAccount.(string)
	label, _ := rawLabel.(string)
	if runtime != nil {
		if stamped, _ := entry[accountRuntimeKey].(string); stamped != "" &&
			NormalizeRuntime(stamped) != NormalizeRuntime(*runtime) {
			clearAccountPairing(entry)
		}
	}
	if account != "" && runtime == nil {
		clearAccountPairing(entry)
		return
	}
	if runtime == nil {
		return
	}
	if account != "" {
		entry["account"] = account
		entry[accountRuntimeKey] = NormalizeRuntime(*runtime)
	}
	// account_label (T-260e): the reporter's human-readable label for the
	// account key (oauthAccount email/org — PII). Folded into the entry for the
	// OWNER-FACING monitoring fold only; it is never echoed on the
	// agent-readable ingest response and never joins the stable key.
	if label != "" {
		entry["account_label"] = label
	}
}

// resolveAccountDisplay maps a raw account key to its human-readable name:
// ① the owner's hand-set alias (accounts table) — highest precedence, never
//
//	overwritten by a reported label, visible to every caller rank;
//
// ② the reported account_label overlay (empty for non-owner callers);
// ③ nothing readable → "" — the caller picks its own honest fallback. The
//
//	worker projection and the monitoring session row serve the empty string
//	(the panel renders a bare dash — NEVER the raw credential hash); only the
//	monitoring ACCOUNTS row falls back to the raw stable key, because that
//	row is the aliasing surface where the key itself is the information.
func resolveAccountDisplay(aliases, labels map[string]string, raw string) string {
	if name := aliases[raw]; name != "" {
		return name
	}
	if label := labels[raw]; label != "" {
		return label
	}
	return ""
}

// accountDisplayFold builds the per-request raw→readable resolver over the
// given telemetry snapshot (pass the SAME snapshot the handler already took,
// so the overlay and the fold read one consistent view). The label overlay is
// owner-gated by the caller's verified principal.
func (s *apiServer) accountDisplayFold(
	r *http.Request, telemetry map[string]map[string]any,
) (func(string) string, error) {
	aliases, err := s.dal.AccountDisplayNames()
	if err != nil {
		return nil, err
	}
	labels := accountLabelOverlay(telemetry, s.principalOfRequest(r) == principalOwner)
	return func(raw string) string {
		return resolveAccountDisplay(aliases, labels, raw)
	}, nil
}
