package main

// api_scheduled_messages.go — T-f059 定期訊息: the owner-facing CRUD over a
// member's scheduled_message rows. The clock-driven twin of api_webhooks.go's
// management face, verb for verb; the firing itself lives in
// scheduled_message.go and the slot arithmetic in schedule_slot.go.
//
// 🔴 The recipient is resolved with resolveChatRecipient, NOT resolveMember.
// resolveMember excludes Kind == KindOutsource (api_helpers.go) — which is
// precisely why a webhook cannot be bound to an `ow-` worker today. A scheduled
// message IS a chat message, so it takes chat's recipient rule and staff and
// outsource workers alike can be scheduled to.

import (
	"net/http"
	"time"
)

// GET /api/members/{member_id}/scheduled-messages — the member's schedules,
// oldest→newest.
func (s *apiServer) HandleListScheduledMessagesApiMembersMemberIdScheduledMessagesGet(w http.ResponseWriter, r *http.Request, memberId string) {
	recipient, err := s.resolveChatRecipient(memberId)
	if err != nil {
		writeResolveError(w, err, "member", memberId)
		return
	}
	rows, err := s.dal.ListScheduledMessagesByMember(recipient)
	if err != nil {
		internalError(w, err)
		return
	}
	out := []scheduledMessageDTO{}
	for _, m := range rows {
		out = append(out, newScheduledMessageDTO(m))
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /api/members/{member_id}/scheduled-messages — create. body/cadence/
// hour/minute/timezone are required (spec): a schedule with no time of day is
// meaningless, and an omitted timezone would sooner or later be read as "the
// server's zone", which is the ambiguity this feature exists to remove.
func (s *apiServer) HandleCreateScheduledMessageApiMembersMemberIdScheduledMessagesPost(w http.ResponseWriter, r *http.Request, memberId string) {
	var body ScheduledMessageCreateDTO
	if !decodeJSONBodyRequired(w, r, &body, "body", "cadence", "hour", "minute", "timezone") {
		return
	}
	recipient, err := s.resolveChatRecipient(memberId)
	if err != nil {
		writeResolveError(w, err, "member", memberId)
		return
	}
	m := ScheduledMessage{
		ID:       "sch-" + newHexID(12),
		MemberID: recipient,
		Label:    strOrEmpty(body.Label),
		Body:     body.Body,
		Cadence:  string(body.Cadence),
		// Both day fields carry a default because the cadence is editable: a
		// daily schedule PATCHed to weekly later must have a defined day.
		DayOfWeek:  intOr(body.DayOfWeek, 0),
		DayOfMonth: intOr(body.DayOfMonth, 1),
		Hour:       body.Hour,
		Minute:     body.Minute,
		Timezone:   trimString(body.Timezone),
		Status:     ScheduledMessageStatusEnabled,
		CreatedTS:  nowSecs(),
	}
	if !s.validateScheduledMessage(w, m) {
		return
	}
	// The delivery cursor starts AT the slot current right now, so a schedule
	// created at 10:00 for daily 09:00 does not fire today. Seeding it is what
	// makes "a new schedule never fires immediately" structural rather than a
	// property of when the first tick happens to land.
	m.LastFiredSlot = currentSlotKey(m, time.Unix(int64(m.CreatedTS), 0))
	if err := s.dal.PutScheduledMessage(m); err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, newScheduledMessageDTO(m))
}

// PATCH /api/members/{member_id}/scheduled-messages/{schedule_id} — partial
// edit, including the enable/disable toggle. id and member_id are immutable.
func (s *apiServer) HandleUpdateScheduledMessageApiMembersMemberIdScheduledMessagesScheduleIdPatch(w http.ResponseWriter, r *http.Request, memberId, scheduleId string) {
	var body ScheduledMessageUpdateDTO
	if !decodeJSONBody(w, r, &body) {
		return
	}
	m, err := s.resolveScheduledMessage(memberId, scheduleId)
	if err != nil {
		writeResolveError(w, err, "scheduled message", scheduleId)
		return
	}
	// Whether the schedule was RE-AIMED, which is a different question from
	// whether it was edited: changing the label or the body leaves the slots
	// where they were, so the cursor must stay put too.
	//
	// 🔴 Compared by VALUE against the row as it stands, not by which fields the
	// caller happened to send. Re-aiming on mere presence means a caller that
	// PATCHes the whole form back — which is what every "save" button eventually
	// does — moves the cursor to now on every save even when nothing about the
	// timing changed. Land one of those in the window between a slot elapsing and
	// the next tick (up to a minute) and that delivery is swallowed permanently,
	// with no error, no log line and a card that looks entirely normal.
	reAimed := (body.Cadence != nil && string(*body.Cadence) != m.Cadence) ||
		(body.DayOfWeek != nil && *body.DayOfWeek != m.DayOfWeek) ||
		(body.DayOfMonth != nil && *body.DayOfMonth != m.DayOfMonth) ||
		(body.Hour != nil && *body.Hour != m.Hour) ||
		(body.Minute != nil && *body.Minute != m.Minute) ||
		(body.Timezone != nil && trimString(*body.Timezone) != m.Timezone)
	if body.Label != nil {
		m.Label = *body.Label
	}
	if body.Body != nil {
		m.Body = *body.Body
	}
	if body.Cadence != nil {
		m.Cadence = string(*body.Cadence)
	}
	if body.DayOfWeek != nil {
		m.DayOfWeek = *body.DayOfWeek
	}
	if body.DayOfMonth != nil {
		m.DayOfMonth = *body.DayOfMonth
	}
	if body.Hour != nil {
		m.Hour = *body.Hour
	}
	if body.Minute != nil {
		m.Minute = *body.Minute
	}
	if body.Timezone != nil {
		m.Timezone = trimString(*body.Timezone)
	}
	if body.Status != nil {
		if !ValidScheduledMessageStatus(string(*body.Status)) {
			writeError(w, http.StatusUnprocessableEntity,
				"status must be one of ['enabled' 'disabled']; got '"+string(*body.Status)+"'")
			return
		}
		m.Status = string(*body.Status)
	}
	if !s.validateScheduledMessage(w, *m) {
		return
	}
	// 🔴 The edit writes the owner's columns ONLY. It does not carry the cursor
	// back, because everything above this line is a read-modify-write over a
	// snapshot taken before the request was even parsed: a tick can deliver a
	// slot in that gap, and a whole-row re-put would roll its advance back and
	// make the next tick send the same slot again. The monotonic fire test cannot
	// save that — the cursor itself would have gone backwards. See
	// UpdateScheduledMessageSettings.
	if err := s.dal.UpdateScheduledMessageSettings(*m); err != nil {
		internalError(w, err)
		return
	}
	if reAimed {
		// Re-aiming moves the cursor to the slot current NOW, so the edit never
		// fires the slot it crossed: moving a daily schedule from 09:00 to 08:00
		// at noon must not deliver today's 08:00 retroactively. This is the one
		// write to the cursor an edit may make, and it is stated, not inherited.
		if err := s.dal.AimScheduledMessageCursor(m.ID, currentSlotKey(*m, time.Now())); err != nil {
			internalError(w, err)
			return
		}
	}
	// Re-read rather than serialise the snapshot: the cursor fields on the wire
	// must be the ones actually in the row, including an advance this request
	// deliberately did not overwrite.
	fresh, err := s.dal.GetScheduledMessage(m.ID)
	if err != nil || fresh == nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, newScheduledMessageDTO(*fresh))
}

// DELETE /api/members/{member_id}/scheduled-messages/{schedule_id} —
// permanent removal (status=disabled is the reversible one).
func (s *apiServer) HandleDeleteScheduledMessageApiMembersMemberIdScheduledMessagesScheduleIdDelete(w http.ResponseWriter, r *http.Request, memberId, scheduleId string) {
	m, err := s.resolveScheduledMessage(memberId, scheduleId)
	if err != nil {
		writeResolveError(w, err, "scheduled message", scheduleId)
		return
	}
	if err := s.dal.DeleteScheduledMessage(m.ID); err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, newScheduledMessageDTO(*m))
}

// resolveScheduledMessage returns the schedule addressed by (member,
// schedule_id), folding an absent member, an absent schedule, OR a schedule
// belonging to a DIFFERENT member onto errNotFound. That last case matters: the
// id alone would otherwise let one member's path edit another member's row.
func (s *apiServer) resolveScheduledMessage(memberID, scheduleID string) (*ScheduledMessage, error) {
	recipient, err := s.resolveChatRecipient(memberID)
	if err != nil {
		return nil, err
	}
	m, err := s.dal.GetScheduledMessage(scheduleID)
	if err != nil {
		return nil, err
	}
	if m == nil || m.MemberID != recipient {
		return nil, errNotFound
	}
	return m, nil
}

// validateScheduledMessage applies the domain invariants to a fully assembled
// row and writes the 422 face on the first failure. Applied identically on
// create and on PATCH — a partial edit assembles a WHOLE row before it is
// judged, so no combination of individually-legal edits can compose an illegal
// schedule.
func (s *apiServer) validateScheduledMessage(w http.ResponseWriter, m ScheduledMessage) bool {
	if !ValidScheduledMessageCadence(m.Cadence) {
		writeError(w, http.StatusUnprocessableEntity,
			"cadence must be one of ['daily' 'weekly' 'monthly']; got '"+m.Cadence+"'")
		return false
	}
	if err := ValidateScheduledMessageBody(m.Body); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return false
	}
	if err := ValidateScheduledMessageSlotFields(m.Hour, m.Minute, m.DayOfWeek, m.DayOfMonth); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return false
	}
	// 🔴 A timezone the tz database cannot resolve is refused HERE, at the write,
	// never softened into UTC downstream — a schedule that runs at the wrong
	// hour looks exactly like one that runs correctly.
	if err := ValidateScheduledMessageTimezone(m.Timezone); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return false
	}
	return true
}
