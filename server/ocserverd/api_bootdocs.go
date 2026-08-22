package main

// api_bootdocs.go — the two boot-context document kinds the owner may now
// edit (T-791e):
// 系統互動 (system_interaction/global) and 啟動程序 (boot_sequence/claude,
// boot_sequence/codex).
//
// WHY THIS EXISTS (owner, 2026-08-13, verbatim): 「我們可以把系統互動改成可以修改
// 嗎 跟銀月的 insight 一樣是有 history / restore to default」「不用每次都改 code」
// 「啟動程序也是一樣」. Of the three segments the boot context is assembled from,
// only the middle one (使用者自訂) had an owner-editable representation; the other
// two were go:embed seeds, so correcting one sentence cost a release.
//
// 🔴 THREE DOCUMENTS, NOT ONE WITH A VARIANT FIELD — and the reason is NOT that
// the texts differ. Step 3 of the two boot sequences says OPPOSITE things (the
// claude one tells the agent to mount its own `ocagent listen`; the codex one
// forbids exactly that, because the App Server sidecar owns the listener).
// Serving the wrong one leaves the agent unable to come online, and that failure
// is SILENT: nothing that never boots is around to report it. Which is also why
// the runtime→document choice is made in exactly one place
// (bootSequenceSeedName / bootSequenceDocKey, assets.go) and never re-derived.
//
// 🔴 EDITING NEVER TOUCHES THE SEED. The stored edit is an OVERLAY row; the
// factory text stays in the binary's go:embed copy. "Restore to default" is
// therefore answered from a source no write path can reach, and it needs no
// agent, no MCP client and no member identity — the cockpit's owner token alone
// walks the whole way back (api_bootdocs_reset_t791e_test.go pins that: an owner
// token whose sub is on nobody's roster).

import (
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"
)

// bootDocSpec addresses ONE editable boot-context block: which document-history
// series it is, which seed file backs it, and which cap judges its writes.
// Resolved once per request and passed around, so the three handlers of a block
// cannot disagree about any of the four.
type bootDocSpec struct {
	Kind     string
	Key      string
	SeedFile string
	Cap      int
	// DocName is what a refusal calls this document. It goes into
	// docCapRefusal, which is the message a caller reads when its write is
	// rejected — naming the wrong document sends the reader to edit the wrong
	// one (the defect insightWriteAuthz exists to avoid on the authz face).
	DocName string
	// Vars are the {name} variables this document may use. nil opts the kind
	// out of variable validation entirely (the three documents that shipped
	// before T-3201 do — see doc_vars.go); a non-nil empty slice means the
	// document is validated and allows none.
	Vars []string
	// ReadOnly marks a document the owner may READ but never edit. The owner's
	// ruling, verbatim: 「以前 global context 是固定內容我們也是會顯示 只是不給改」
	// — so these still have a seed, still fold, still reach the cockpit, and
	// only the write faces refuse.
	ReadOnly bool
}

// bootDocReg is ONE row per editable boot-context document kind: everything
// bootDocSpecFor needs to answer for it, in one place.
//
// 🔴 A TABLE, NOT A SWITCH, AND THE REASON IS MEASURED. Adding the offboard
// document (bfe95d1f) meant editing eight hand-maintained switches and lists
// scattered over six files, and four of them have NO gate at all — a missing
// arm compiles, serves 200, and shows the wrong text or none. Kinds resolved
// from one slice cannot be in it for the specFor face and absent from the
// history face, which is the class of defect that produced this comment.
type bootDocReg struct {
	Kind string
	// Keys are the document keys this kind serves, in the order a refusal
	// should name them. Every kind but boot_sequence has exactly one.
	Keys []string
	// SeedFor answers the seed filename for one of Keys. A func rather than a
	// field because boot_sequence's two keys have two different seeds, and the
	// two contradict each other in step 3 — serving the wrong one is a silent
	// failure to boot (see bootSequenceSeedName).
	SeedFor  func(key string) string
	DocName  func(key string) string
	Cap      func(s *apiServer) int
	Vars     []string
	ReadOnly bool
}

// taskEventDocVars name the same facts today's Go string literals interpolate,
// spelled the way the seed files spell them. They are declared per kind rather
// than shared so a name that only makes sense for one event cannot silently be
// used by another.
var bootDocRegistry = []bootDocReg{{
	Kind:    docKindSystemInteraction,
	Keys:    []string{systemInteractionDocKey},
	SeedFor: func(string) string { return systemInteractionSeedMD },
	DocName: func(string) string { return "system interaction block" },
	Cap:     func(s *apiServer) int { return s.systemInteractionCap() },
}, {
	Kind: docKindBootSequence,
	Keys: []string{bootSequenceKeyClaude, bootSequenceKeyCodex},
	// bootSequenceSeedName, not a literal map: it is documented as the one
	// place in the tree that decides which runtime gets which sequence, and it
	// holds that title only as long as nobody writes a second one beside it.
	SeedFor: bootSequenceSeedName,
	DocName: func(key string) string { return "boot sequence (" + key + ")" },
	Cap:     func(s *apiServer) int { return s.bootSequenceCap() },
}, {
	// 下線程序 (T-c9c0). A SINGLETON: being collected is the same procedure for
	// every agent and every runtime, so there is deliberately no runtime axis
	// here to get wrong.
	Kind:    docKindOffboard,
	Keys:    []string{offboardDocKey},
	SeedFor: func(string) string { return offboardSeedMD },
	DocName: func(string) string { return "offboard sequence" },
	Cap:     func(s *apiServer) int { return s.offboardCap() },
}, {
	// 加速停止 (T-3201). Shares the offboard cap on purpose, the same way the
	// two boot sequences share one: it is the same procedure under a shorter
	// clock, and a second ceiling would be a second number to keep in step
	// without a second thing to say about it.
	Kind:    docKindAcceleratedStop,
	Keys:    []string{acceleratedStopDocKey},
	SeedFor: func(string) string { return acceleratedStopSeedMD },
	DocName: func(string) string { return "accelerated stop sequence" },
	Cap:     func(s *apiServer) int { return s.offboardCap() },
	Vars:    []string{"where", "closer", "deadline"},
}, {
	Kind:    docKindTaskCloseout,
	Keys:    []string{taskCloseoutDocKey},
	SeedFor: func(string) string { return taskCloseoutSeedMD },
	DocName: func(string) string { return "task close-out procedure" },
	Cap:     func(s *apiServer) int { return s.taskEventCap() },
	Vars:    []string{"task_no", "status", "type_key", "manual_label"},
}, {
	Kind:    docKindTaskReassignPredecessor,
	Keys:    []string{taskReassignPredecessorDocKey},
	SeedFor: func(string) string { return taskReassignPredecessorSeedMD },
	DocName: func(string) string { return "task reassign procedure (predecessor)" },
	Cap:     func(s *apiServer) int { return s.taskEventCap() },
	Vars:    []string{"task_no", "new_executor_label"},
}, {
	Kind:    docKindTaskTakeoverWithPredecessor,
	Keys:    []string{taskTakeoverWithPredecessorDocKey},
	SeedFor: func(string) string { return taskTakeoverWithPredecessorSeedMD },
	DocName: func(string) string { return "task takeover procedure (with predecessor)" },
	Cap:     func(s *apiServer) int { return s.taskEventCap() },
	Vars:    []string{"task_no", "title", "predecessor_label", "old_executor_id", "note"},
}, {
	// 🔴 READ-ONLY. Owner's ruling, verbatim: 「以前 global context 是固定內容
	// 我們也是會顯示 只是不給改」. It is a document rather than a string literal
	// so the owner can SEE what an agent is told; the write faces refuse.
	Kind:     docKindTaskTakeoverFresh,
	Keys:     []string{taskTakeoverFreshDocKey},
	SeedFor:  func(string) string { return taskTakeoverFreshSeedMD },
	DocName:  func(string) string { return "task takeover procedure (new assignment)" },
	Cap:      func(s *apiServer) int { return s.taskEventCap() },
	Vars:     []string{"task_no", "title", "note"},
	ReadOnly: true,
}, {
	Kind:     docKindTaskUnblocked,
	Keys:     []string{taskUnblockedDocKey},
	SeedFor:  func(string) string { return taskUnblockedSeedMD },
	DocName:  func(string) string { return "dependency-released notice" },
	Cap:      func(s *apiServer) int { return s.taskEventCap() },
	Vars:     []string{"blocked_task_no", "blocker_task_no", "blocker_title", "blocker_status"},
	ReadOnly: true,
}}

// bootDocRegFor finds the row for a kind.
func bootDocRegFor(kind string) (bootDocReg, bool) {
	for _, reg := range bootDocRegistry {
		if reg.Kind == kind {
			return reg, true
		}
	}
	return bootDocReg{}, false
}

func (reg bootDocReg) serves(key string) bool {
	for _, k := range reg.Keys {
		if k == key {
			return true
		}
	}
	return false
}

func (s *apiServer) systemInteractionSpec() bootDocSpec {
	return s.mustBootDocSpec(docKindSystemInteraction, systemInteractionDocKey)
}

func (s *apiServer) offboardSpec() bootDocSpec {
	return s.mustBootDocSpec(docKindOffboard, offboardDocKey)
}

// mustBootDocSpec resolves a pair this binary is built with. It panics rather
// than returning ok=false because every caller passes a constant pair from the
// registry itself: a false here would mean the binary shipped with a kind whose
// own accessor cannot address it, and degrading that to an empty spec would
// hand a caller a document with no seed, no cap and no name.
func (s *apiServer) mustBootDocSpec(kind, key string) bootDocSpec {
	spec, ok := s.bootDocSpecFor(kind, key)
	if !ok {
		panic("boot document " + kind + "/" + key + " is not in bootDocRegistry")
	}
	return spec
}

// bootSequenceSpecFor resolves the boot-sequence document for a runtime key as
// it arrives on the URL. ok=false means the key names no document this server
// serves — the caller answers 404 rather than quietly falling back to claude,
// because falling back is precisely how a codex reader ends up holding the
// sequence that keeps it from booting.
func (s *apiServer) bootSequenceSpecFor(runtimeKey string) (bootDocSpec, bool) {
	return s.bootDocSpecFor(docKindBootSequence, runtimeKey)
}

// bootDocSpecFor resolves ANY (kind, key) pair naming an editable boot-context
// block — the form the document-history faces address documents in. ok=false
// means the pair names none of them.
func (s *apiServer) bootDocSpecFor(kind, key string) (bootDocSpec, bool) {
	reg, ok := bootDocRegFor(kind)
	if !ok || !reg.serves(key) {
		return bootDocSpec{}, false
	}
	return bootDocSpec{
		Kind:     reg.Kind,
		Key:      key,
		SeedFile: reg.SeedFor(key),
		Cap:      reg.Cap(s),
		DocName:  reg.DocName(key),
		Vars:     reg.Vars,
		ReadOnly: reg.ReadOnly,
	}, true
}

// bootDocHistoryKeyKnown is bootDocSpecFor's server-free half: does this
// (kind, key) name one of these documents at all? The document-history faces
// ask this BEFORE they list or restore, so an address this server does not
// serve is refused rather than answered with an empty version list — "you used
// the wrong key" and "this document has no versions yet" must not look the same.
func bootDocHistoryKeyKnown(kind, key string) bool {
	reg, ok := bootDocRegFor(kind)
	return ok && reg.serves(key)
}

// unknownBootDocKeyMsg names the keys that DO exist for this kind, for the same
// reason writeUnknownBootSequence does: a caller holding a typo needs to be able
// to tell it from a document that is simply empty.
//
// 🔴 IT READS THE REGISTRY, NOT A SECOND LIST. It used to be a switch that
// answered the system-interaction key for every kind that was not
// boot_sequence, so a fourth kind was described by a key that did not belong to
// it; the switch was then taught each kind by hand, which is the same defect
// one edit later. A kind nobody registered says so instead of naming a key.
func unknownBootDocKeyMsg(kind, key string) string {
	reg, ok := bootDocRegFor(kind)
	if !ok {
		return "document history kind '" + kind + "' names no editable document on this server"
	}
	quoted := make([]string, 0, len(reg.Keys))
	for _, k := range reg.Keys {
		quoted = append(quoted, "'"+k+"'")
	}
	return "document history key '" + key + "' does not name a " + kind +
		" document — the key is " + strings.Join(quoted, " or ")
}

// foldBootDocDTO folds one block: overlay ⊕ the embedded seed.
func (s *apiServer) foldBootDocDTO(spec bootDocSpec) (*bootDocDTO, error) {
	overlay, err := s.dal.GetBootDocument(spec.Kind, spec.Key)
	if err != nil {
		return nil, err
	}
	seedMD, hasSeed, err := s.root.seedBlockMD(spec.SeedFile)
	if err != nil {
		return nil, err
	}
	text, isDefault := FoldBootDocument(overlay, seedMD, hasSeed)
	return &bootDocDTO{
		SizeChars:     utf8.RuneCountInString(text),
		CapChars:      spec.Cap,
		Kind:          spec.Kind,
		Key:           spec.Key,
		Text:          text,
		OwnerID:       wireOwnerID,
		SchemaVersion: wireSchemaVersion,
		IsDefault:     isDefault,
		HasSeed:       hasSeed,
	}, nil
}

// systemInteractionText / bootSequenceText are what the BOOT FOLDS read
// (buildBootContext for staff, worker_sharedcore.go for outsource). They are the
// reason this ticket is worth anything: before T-791e both were bare
// readSeedFile calls, so an edit could exist in the database and still never
// reach an agent.
//
// The runtime→document choice goes through bootSequenceDocKey, which reads the
// answer of bootSequenceSeedName instead of testing the runtime again — one
// decision point for staff and outsource alike.
func (s *apiServer) systemInteractionText() (string, error) {
	dto, err := s.foldBootDocDTO(s.systemInteractionSpec())
	if err != nil {
		return "", err
	}
	return dto.Text, nil
}

// offboardText is what the SERVER carries into the offboard notice itself
// (T-a9d6): the owner ruled that the steps must ride the notice the server
// pushes, not be fetched back by the agent — 「改回真的推播」. It answers "" on
// any fault, and every caller degrades to the sentence alone rather than going
// silent: losing the checklist is survivable, losing the notice is not.
func (s *apiServer) offboardText() string {
	dto, err := s.foldBootDocDTO(s.offboardSpec())
	if err != nil || dto == nil {
		return ""
	}
	return dto.Text
}

func (s *apiServer) bootSequenceText(runtime string) (string, error) {
	spec, ok := s.bootSequenceSpecFor(bootSequenceDocKey(runtime))
	if !ok {
		// Unreachable by construction: bootSequenceDocKey only ever answers a
		// key bootSequenceSeedForKey accepts. Fail closed rather than serve a
		// blank boot sequence, which would look like a successful boot.
		return "", errNotFound
	}
	dto, err := s.foldBootDocDTO(spec)
	if err != nil {
		return "", err
	}
	return dto.Text, nil
}

// bootDocSnapshotIn is what SaveWithDocumentHistory calls from INSIDE the write
// transaction — the same posture insightSnapshotIn takes, and for the same
// reason: the retained revision must be the state THIS write replaced, not a
// value the handler folded earlier, or two racing writers retain one common
// ancestor and the version written between them becomes unrecoverable.
func bootDocSnapshotIn(kind, key string) func(sqlQuerier) (string, error) {
	return func(q sqlQuerier) (string, error) {
		current, err := getBootDocumentOn(q, kind, key)
		if err != nil {
			return "", err
		}
		return bootDocHistorySnapshot(current)
	}
}

func bootDocHistorySnapshot(current *BootDocument) (string, error) {
	if current == nil {
		return "{}", nil
	}
	return historyJSON(map[string]string{
		"text": current.Text, "tombstoned": strconv.FormatBool(current.Tombstoned),
	})
}

// publishBootDoc fans the change on the `global_context` topic.
//
// Deliberately NOT a new topic: the closed vocabulary is enforced at the publish
// seam and a topic outside it is dropped SILENTLY (see sseTopics), so adding one
// means teaching every consumer at once or fanning nothing at all. All three
// blocks are one surface to a reader — the cockpit's 全域情境 pane renders them
// together — so the frame that already means "that surface moved, re-read it" is
// the honest one to send. Same key as the 使用者自訂 writes for the same reason.
func (s *apiServer) publishBootDoc(r *http.Request) {
	s.hub.Publish("global_context", "patch", "global_context", wireOwnerID, nil,
		audienceOwnerOnly(), requestTrigger(r))
}

// writeBootDoc is the one write path shared by replace and reset, and the one
// place the no-op rule lives.
//
// 🔴 A WRITE THAT CHANGES NOTHING RETAINS NOTHING (owner's ruling for these three
// documents). These blocks are now editable from a text box, so idle saves are
// expected — and every idle save that retained a revision would push the version
// the owner actually wants ("the one from before I broke it") one step closer to
// falling off the end of the list. The comparison is on the FOLDED text, so
// saving the seed's own bytes back over an untouched document is a no-op too.
// role_def and the task manual already work this way (roleDefHistoryStreams /
// taskManualHistoryStreams); global_context deliberately does NOT, and that is a
// known gap on that document, not a precedent to copy.
//
// Returns whether anything was written.
func (s *apiServer) writeBootDoc(r *http.Request, spec bootDocSpec, current *bootDocDTO, next BootDocument, nextText string) (bool, error) {
	// "Nothing changed" is BOTH halves: the text a reader would get, and whether
	// the document reads as the shipped default afterwards (next.Tombstoned) as
	// it does now (current.IsDefault). Comparing only the text would swallow the
	// one gesture that changes nothing visible but everything about the next
	// reset — adopting the seed's own bytes as an edit — and comparing only the
	// flag would let a genuine rewrite pass as a no-op.
	if current.Text == nextText && current.IsDefault == next.Tombstoned {
		return false, nil
	}
	if err := s.dal.SaveWithDocumentHistory(spec.Kind, spec.Key, currentActor(r),
		bootDocSnapshotIn(spec.Kind, spec.Key), func(ex sqlExecer) error {
			return putBootDocumentOn(ex, next)
		}); err != nil {
		return false, err
	}
	s.publishBootDoc(r)
	return true, nil
}

// replaceBootDoc is the whole-document replace shared by both blocks.
func (s *apiServer) replaceBootDoc(w http.ResponseWriter, r *http.Request, spec bootDocSpec, text string, allowShrink bool) {
	if spec.ReadOnly {
		writeError(w, http.StatusMethodNotAllowed, bootDocReadOnlyRefusal(spec))
		return
	}
	current, err := s.foldBootDocDTO(spec)
	if err != nil {
		internalError(w, err)
		return
	}
	// Wipe guard, the posture replace_global_context / replace_insight carry:
	// emptying a document that had content has to be said out loud. It matters
	// more here than anywhere else — an empty boot sequence is not a small
	// document, it is an agent with no instructions.
	if !allowShrink && WholeDocWipeBlocked(current.Text, text) {
		writeError(w, http.StatusBadRequest,
			docWipeRefusal(spec.DocName, ", or reset it to the shipped default"))
		return
	}
	// Hard cap, checked UNCONDITIONALLY: allow_shrink governs the opposite
	// direction and is not a bypass. The refusal names three numbers (what you
	// wrote, the cap, what is stored) because being refused is otherwise the
	// only way to learn any of them.
	if DocCapBlocked(spec.Cap, current.Text, text) {
		writeError(w, http.StatusBadRequest, docCapRefusal(spec.Cap, spec.DocName, current.Text, text))
		return
	}
	// Variable validation (T-3201). Last of the three because it is the only
	// one that judges the CONTENT rather than the size: a caller whose write is
	// both oversized and misspelt learns about the size first, which is the one
	// it can act on without re-reading the document.
	if bad := DocVarsUndeclared(text, spec.Vars); len(bad) > 0 {
		writeError(w, http.StatusBadRequest, docVarWriteRefusal(spec.DocName, bad, spec.Vars))
		return
	}
	if _, err := s.writeBootDoc(r, spec, current,
		BootDocument{Kind: spec.Kind, Key: spec.Key, Text: text, Tombstoned: false}, text); err != nil {
		internalError(w, err)
		return
	}
	dto, err := s.foldBootDocDTO(spec)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

// resetBootDoc tombstones the overlay so the folded read falls back to the
// SHIPPED seed.
//
// 🔴 NO CAP IS CHECKED HERE, matching reset_role and reset_insight. The factory
// text is part of the product, not something the caller wrote, so a ceiling the
// owner set afterwards must never be able to block the way back to it. This is
// the path that has to work when a bad edit has stopped agents booting, and at
// that moment there is nobody online to ask.
//
// 404 when the seed file is missing: there must be a factory version to reset
// TO, and the same 404 is what GET .../seed answers, so a surface offering the
// reset and a surface offering the comparison agree.
func (s *apiServer) resetBootDoc(w http.ResponseWriter, r *http.Request, spec bootDocSpec) {
	// 🔴 A READ-ONLY DOCUMENT IS REFUSED HERE TOO, and 405 rather than a
	// success is the deliberate choice. "Restore to default" on a document that
	// can never leave the default is not a harmless no-op: this path is a
	// WRITE — it tombstones an overlay row, retains a history revision and fans
	// a global_context frame — so answering 200 would put a revision and a
	// refresh on every surface for a document nothing changed, and would tell
	// a caller that a reset face exists for it. There is nothing to restore TO
	// that is not already what is being read.
	if spec.ReadOnly {
		writeError(w, http.StatusMethodNotAllowed, bootDocReadOnlyRefusal(spec))
		return
	}
	seedMD, hasSeed, err := s.root.seedBlockMD(spec.SeedFile)
	if err != nil {
		internalError(w, err)
		return
	}
	if !hasSeed {
		writeError(w, http.StatusNotFound,
			"document '"+spec.Kind+"/"+spec.Key+"' has no shipped default to reset to")
		return
	}
	current, err := s.foldBootDocDTO(spec)
	if err != nil {
		internalError(w, err)
		return
	}
	if _, err := s.writeBootDoc(r, spec, current,
		BootDocument{Kind: spec.Kind, Key: spec.Key, Tombstoned: true}, seedMD); err != nil {
		internalError(w, err)
		return
	}
	dto, err := s.foldBootDocDTO(spec)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

// GET /api/system-interaction — the folded 系統互動 block.
func (s *apiServer) HandleGetSystemInteractionApiSystemInteractionGet(w http.ResponseWriter, r *http.Request) {
	dto, err := s.foldBootDocDTO(s.systemInteractionSpec())
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

// POST /api/system-interaction — whole-document replace ({text}).
func (s *apiServer) HandleReplaceSystemInteractionApiSystemInteractionPost(w http.ResponseWriter, r *http.Request) {
	var body BootDocumentReplaceDTO
	if !decodeJSONBodyStrict(w, r, &body, "text") {
		return
	}
	s.replaceBootDoc(w, r, s.systemInteractionSpec(), body.Text,
		body.AllowShrink != nil && *body.AllowShrink)
}

// POST /api/system-interaction/reset — back to the shipped seed.
func (s *apiServer) HandleResetSystemInteractionApiSystemInteractionResetPost(w http.ResponseWriter, r *http.Request) {
	s.resetBootDoc(w, r, s.systemInteractionSpec())
}

// GET /api/offboard — the folded 下線程序 block.
func (s *apiServer) HandleGetOffboardApiOffboardGet(w http.ResponseWriter, r *http.Request) {
	dto, err := s.foldBootDocDTO(s.offboardSpec())
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

// POST /api/offboard — whole-document replace ({text}).
func (s *apiServer) HandleReplaceOffboardApiOffboardPost(w http.ResponseWriter, r *http.Request) {
	var body BootDocumentReplaceDTO
	if !decodeJSONBodyStrict(w, r, &body, "text") {
		return
	}
	s.replaceBootDoc(w, r, s.offboardSpec(), body.Text,
		body.AllowShrink != nil && *body.AllowShrink)
}

// POST /api/offboard/reset — back to the shipped seed.
func (s *apiServer) HandleResetOffboardApiOffboardResetPost(w http.ResponseWriter, r *http.Request) {
	s.resetBootDoc(w, r, s.offboardSpec())
}

// GET /api/boot-sequence/{runtime_key} — the folded 啟動程序 block for ONE runtime.
func (s *apiServer) HandleGetBootSequenceApiBootSequenceRuntimeKeyGet(w http.ResponseWriter, r *http.Request, runtimeKey string) {
	spec, ok := s.bootSequenceSpecFor(runtimeKey)
	if !ok {
		writeUnknownBootSequence(w, runtimeKey)
		return
	}
	dto, err := s.foldBootDocDTO(spec)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

// POST /api/boot-sequence/{runtime_key} — whole-document replace ({text}).
func (s *apiServer) HandleReplaceBootSequenceApiBootSequenceRuntimeKeyPost(w http.ResponseWriter, r *http.Request, runtimeKey string) {
	spec, ok := s.bootSequenceSpecFor(runtimeKey)
	if !ok {
		writeUnknownBootSequence(w, runtimeKey)
		return
	}
	var body BootDocumentReplaceDTO
	if !decodeJSONBodyStrict(w, r, &body, "text") {
		return
	}
	s.replaceBootDoc(w, r, spec, body.Text, body.AllowShrink != nil && *body.AllowShrink)
}

// POST /api/boot-sequence/{runtime_key}/reset — back to that runtime's shipped seed.
func (s *apiServer) HandleResetBootSequenceApiBootSequenceRuntimeKeyResetPost(w http.ResponseWriter, r *http.Request, runtimeKey string) {
	spec, ok := s.bootSequenceSpecFor(runtimeKey)
	if !ok {
		writeUnknownBootSequence(w, runtimeKey)
		return
	}
	s.resetBootDoc(w, r, spec)
}

// writeUnknownBootSequence NAMES the runtimes that exist. A bare "not found"
// would leave a caller that typed "Codex" or passed an empty string unable to
// tell a typo from a server that has no boot sequences at all.
func writeUnknownBootSequence(w http.ResponseWriter, runtimeKey string) {
	writeError(w, http.StatusNotFound,
		"no boot sequence for runtime '"+runtimeKey+"' — the runtimes with their own boot sequence are '"+
			bootSequenceKeyClaude+"' and '"+bootSequenceKeyCodex+"'")
}

// bootDocReadOnlyRefusal is the ONE sentence every write face answers for a
// read-only document, the way docCapRefusal and docWipeRefusal are the one text
// behind their gates. It says what the document IS rather than that the caller
// lacks a permission: no principal can edit it, so pointing at authz would send
// an owner looking for a role to grant.
func bootDocReadOnlyRefusal(spec bootDocSpec) string {
	return "the " + spec.DocName + " is a read-only document — it is shown so you can " +
		"see what agents are told, but no caller may edit it and there is no version " +
		"of it other than the shipped one; nothing was written"
}
