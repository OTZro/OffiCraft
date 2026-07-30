package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
)

var errDocumentHistoryCap = errors.New("restoring this version would violate the existing document size limit")

func historyKeyParts(kind, key string) (string, string, bool) {
	if kind != "lessons" {
		return key, "", key != ""
	}
	parts := strings.SplitN(key, "::", 2)
	return parts[0], func() string {
		if len(parts) == 2 {
			return parts[1]
		}
		return ""
	}(), len(parts) == 2 && parts[0] != "" && parts[1] != ""
}

func documentHistoryDTO(h DocumentHistory) (DocumentHistoryDTO, error) {
	content := map[string]string{}
	if err := json.Unmarshal([]byte(h.ContentJSON), &content); err != nil {
		return DocumentHistoryDTO{}, err
	}
	return DocumentHistoryDTO{Id: h.ID, Content: content, CreatedTs: h.CreatedTS, ActorId: h.ActorID}, nil
}

// Overlay documents must retain their persisted tombstone state, not only the
// folded text exposed to readers. A tombstone means "follow the seed"; writing
// that same text back as a live overlay would silently turn a default document
// into a customized one.
func historyTombstoned(content map[string]string) bool {
	value, _ := strconv.ParseBool(content["tombstoned"])
	return value
}

func userContextHistorySnapshot(current *UserContext) (string, error) {
	if current == nil {
		return "{}", nil
	}
	return historyJSON(map[string]string{
		"text": current.Text, "tombstoned": strconv.FormatBool(current.Tombstoned),
	})
}

func roleDefHistorySnapshot(current *RoleDef) (string, error) {
	if current == nil {
		return "{}", nil
	}
	return historyJSON(map[string]string{
		"name": current.Name, "definition_md": current.DefinitionMD,
		"tombstoned": strconv.FormatBool(current.Tombstoned),
	})
}

func lessonsHistorySnapshot(current *Lessons) (string, error) {
	if current == nil {
		return "{}", nil
	}
	return historyJSON(map[string]string{
		"text": current.Text, "tombstoned": strconv.FormatBool(current.Tombstoned),
	})
}

// The four readers below are what SaveWithDocumentHistory calls from inside the
// write transaction. They deliberately re-read the document rather than trust a
// value the handler folded earlier: the retained revision must be the state
// this write replaced, otherwise two writers racing on one document both retain
// the same ancestor and the revision written in between becomes unrecoverable.
func userContextSnapshotIn(q sqlQuerier) (string, error) {
	current, err := getUserContextOn(q)
	if err != nil {
		return "", err
	}
	return userContextHistorySnapshot(current)
}

func roleDefSnapshotIn(roleKey string) func(sqlQuerier) (string, error) {
	return func(q sqlQuerier) (string, error) {
		current, err := getRoleDefOn(q, roleKey)
		if err != nil {
			return "", err
		}
		return roleDefHistorySnapshot(current)
	}
}

func lessonsSnapshotIn(roleKey, taskType string) func(sqlQuerier) (string, error) {
	return func(q sqlQuerier) (string, error) {
		current, err := getLessonsOn(q, roleKey, taskType)
		if err != nil {
			return "", err
		}
		return lessonsHistorySnapshot(current)
	}
}

func taskManualSnapshotIn(typeKey string) func(sqlQuerier) (string, error) {
	return func(q sqlQuerier) (string, error) {
		current, err := getTaskManualOn(q, typeKey)
		if err != nil {
			return "", err
		}
		if current == nil {
			return "{}", nil
		}
		return taskManualHistorySnapshot(*current)
	}
}

func (s *apiServer) documentHistoryAllowed(w http.ResponseWriter, r *http.Request, kind, key string, write bool) bool {
	primary, _, valid := historyKeyParts(kind, key)
	if !valid {
		writeError(w, http.StatusBadRequest, "invalid document history key")
		return false
	}
	switch kind {
	case "global_context", "role_definition":
		if write && !principalAtLeast(s.principalOfRequest(r), principalAdminAgent) {
			writeError(w, http.StatusForbidden, "restoring this document requires admin capability")
			return false
		}
	case "lessons":
		if write && !s.lessonsWriteAuthz(w, r, primary) {
			return false
		}
	case "task_manual":
	default:
		writeError(w, http.StatusBadRequest, "unknown document history kind")
		return false
	}
	return true
}

func (s *apiServer) HandleListDocumentHistoryApiDocumentHistoryKindKeyGet(w http.ResponseWriter, r *http.Request, kind string, key string) {
	if !s.documentHistoryAllowed(w, r, kind, key, false) {
		return
	}
	history, err := s.dal.ListDocumentHistory(kind, key)
	if err != nil {
		internalError(w, err)
		return
	}
	result := make([]DocumentHistoryDTO, 0, len(history))
	for _, h := range history {
		dto, err := documentHistoryDTO(h)
		if err != nil {
			internalError(w, err)
			return
		}
		result = append(result, dto)
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *apiServer) HandleRestoreDocumentHistoryApiDocumentHistoryKindKeyIdRestorePost(w http.ResponseWriter, r *http.Request, kind string, key string, id int64) {
	if !s.documentHistoryAllowed(w, r, kind, key, true) {
		return
	}
	history, err := s.dal.GetDocumentHistory(kind, key, id)
	if err != nil {
		internalError(w, err)
		return
	}
	if history == nil {
		writeError(w, http.StatusNotFound, "document history version not found")
		return
	}
	content := map[string]string{}
	if err := json.Unmarshal([]byte(history.ContentJSON), &content); err != nil {
		internalError(w, err)
		return
	}
	if err := s.restoreDocumentHistory(r, kind, key, content); err != nil {
		if errors.Is(err, errDocumentHistoryCap) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		internalError(w, err)
		return
	}
	s.publishDocumentHistoryRestore(r, kind, key)
	dto, err := documentHistoryDTO(*history)
	if err != nil {
		internalError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dto)
}

func (s *apiServer) publishDocumentHistoryRestore(r *http.Request, kind, key string) {
	switch kind {
	case "global_context":
		s.hub.Publish("global_context", "patch", "global_context", wireOwnerID, nil, audienceOwnerOnly(), requestTrigger(r))
	case "role_definition":
		s.hub.Publish("role", "patch", "role", wireOwnerID+"::"+key, nil, audienceOwnerOnly(), requestTrigger(r))
	case "lessons":
		s.hub.Publish("lessons", "patch", "lessons", wireOwnerID+"::"+key, nil, audienceOwnerOnly(), requestTrigger(r))
	case "task_manual":
		s.publishTaskManual(key, requestTrigger(r))
	}
}

func (s *apiServer) restoreDocumentHistory(r *http.Request, kind, key string, content map[string]string) error {
	actor := currentActor(r)
	switch kind {
	case "global_context":
		return s.dal.SaveWithDocumentHistory(kind, key, actor, userContextSnapshotIn, func(ex sqlExecer) error {
			return putUserContextOn(ex, UserContext{Text: content["text"], Tombstoned: historyTombstoned(content)})
		})
	case "role_definition":
		if folded, err := s.foldRoleDefDTO(key); err != nil {
			return err
		} else if folded == nil {
			return errNotFound
		}
		return s.dal.SaveWithDocumentHistory(kind, key, actor, roleDefSnapshotIn(key), func(ex sqlExecer) error {
			return putRoleDefOn(ex, RoleDef{RoleKey: key, Name: content["name"], DefinitionMD: content["definition_md"], Tombstoned: historyTombstoned(content)})
		})
	case "lessons":
		roleKey, taskType, _ := historyKeyParts(kind, key)
		current, err := s.foldLessonsDTO(roleKey, taskType)
		if err != nil {
			return err
		}
		if DocCapBlocked(current.Text, content["text"]) {
			return errDocumentHistoryCap
		}
		return s.dal.SaveWithDocumentHistory(kind, key, actor, lessonsSnapshotIn(roleKey, taskType), func(ex sqlExecer) error {
			return putLessonsOn(ex, Lessons{RoleKey: roleKey, TaskType: taskType, Text: content["text"], Tombstoned: historyTombstoned(content)})
		})
	case "task_manual":
		current, err := s.dal.GetTaskManual(key)
		if err != nil {
			return err
		}
		if current == nil {
			return errNotFound
		}
		if DocCapBlocked(current.Learnings, content["learnings"]) || DocCapBlocked(current.SopMD, content["sop_md"]) {
			return errDocumentHistoryCap
		}
		next := *current
		next.Purpose, next.Fields, next.SopMD, next.Learnings = content["purpose"], content["fields"], content["sop_md"], content["learnings"]
		next.UpdatedTS = nowSecs()
		return s.dal.SaveWithDocumentHistory(kind, key, actor, taskManualSnapshotIn(key), func(ex sqlExecer) error {
			return putTaskManualOn(ex, next)
		})
	}
	return errNotFound
}
