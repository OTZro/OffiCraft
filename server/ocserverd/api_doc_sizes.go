package main

// api_doc_sizes.go — the capped-document SIZE overview (peek_doc_sizes;
// GET /api/doc-sizes).
//
// WHY IT EXISTS: two of the five capped segments — a role's insight and its
// lessons — are reported by NO listing on this station at any price. The manual
// pair (sop_md / learnings, sizes and caps) is already on list_task_manuals'
// light view, and the role definition's size and cap already ride every
// list_roles row; so those three are cheap-but-scattered, while insight and
// lessons are simply unavailable in bulk. This route serves all five from one
// place so "which long-lived document is nearly full?" is a single call.
//
// WHAT IT DELIBERATELY DOES NOT CARRY: any document text. That is not a
// nice-to-have — it is the entire property. The response size is a function of
// how many roles and manuals exist and of nothing else, so a station whose docs
// grow does not make this call more expensive.

import (
	"net/http"
	"unicode/utf8"
)

// HandlePeekDocSizesApiDocSizesGet answers GET /api/doc-sizes.
//
// Each of the FIVE capped segments is reported against ITS OWN cap. They stopped
// sharing a number when T-ae38 split one cap into four and T-30f1 split the task
// manual's in two again; a listing that quoted one number for all five would be
// wrong the first time an owner raised any single one of them, and wrong
// silently — every row would still look plausible.
//
// The caps are read ONCE each for the whole listing, the same discipline
// HandleListTaskManualsApiTaskManualsGet follows: per-row reads could straddle a
// PATCH /api/settings and hand back one response quoting two different caps for
// the same segment.
//
// The SIZES come from the very same fold* helpers the per-document GETs use
// (foldRoleDefDTO / foldInsightDTO / foldLessonsDTO), so a size reported here
// cannot drift from what get_role / get_insight / get_lessons report for the
// same document. Only the cap field is replaced with the once-read value — the
// helpers read their own cap per call, which is right for a single-document read
// and wrong for a listing.
func (s *apiServer) HandlePeekDocSizesApiDocSizesGet(w http.ResponseWriter, r *http.Request) {
	dutyCap := s.dutyCap()
	insightCap := s.insightCap()
	learningCap := s.learningCap()
	sopCap := s.manualSopCap()
	learningsCap := s.manualLearningsCap()

	roleKeys, err := s.listRoleKeys()
	if err != nil {
		internalError(w, err)
		return
	}
	roles := []roleDocSizesDTO{}
	for _, roleKey := range roleKeys {
		duty, err := s.foldRoleDefDTO(roleKey)
		if err != nil {
			internalError(w, err)
			return
		}
		if duty == nil {
			// Same fail-quiet posture as GET /api/roles: a key that no longer
			// folds is simply not a role, so it is not a row here either.
			continue
		}
		insight, err := s.foldInsightDTO(roleKey)
		if err != nil {
			internalError(w, err)
			return
		}
		// ONLY the DEFAULT lessons bucket (seedLessonsTaskType) is sized — the
		// one the boot context injects and the one fillLessonsIdentityArgs folds
		// a blank task_type to. The write side puts NO constraint on the bucket
		// name (the default is a fill-in for an absent argument, not an
		// enumeration), so a write that names another bucket produces a document
		// that draws on this same lessons cap and never appears in this listing.
		// The wire descriptions say "default bucket" for exactly this reason;
		// covering every bucket needs a lessons enumeration the DAL does not
		// have (GetLessons is keyed (role_key, task_type)) plus a per-role array
		// on this response — a frozen-wire shape decision, not a defect fix.
		lessons, err := s.foldLessonsDTO(roleKey, seedLessonsTaskType)
		if err != nil {
			internalError(w, err)
			return
		}
		roles = append(roles, roleDocSizesDTO{
			RoleKey: roleKey,
			Duty:    docSizeDTO{SizeChars: duty.SizeChars, CapChars: dutyCap},
			Insight: docSizeDTO{SizeChars: insight.SizeChars, CapChars: insightCap},
			Lessons: docSizeDTO{SizeChars: lessons.SizeChars, CapChars: learningCap},
		})
	}

	manuals, err := s.dal.ListTaskManuals()
	if err != nil {
		internalError(w, err)
		return
	}
	taskManuals := []taskManualDocSizesDTO{}
	for _, m := range manuals {
		taskManuals = append(taskManuals, taskManualDocSizesDTO{
			TypeKey:   m.TypeKey,
			Sop:       docSizeDTO{SizeChars: utf8.RuneCountInString(m.SopMD), CapChars: sopCap},
			Learnings: docSizeDTO{SizeChars: utf8.RuneCountInString(m.Learnings), CapChars: learningsCap},
		})
	}

	writeJSON(w, http.StatusOK, docSizesDTO{Roles: roles, TaskManuals: taskManuals})
}
