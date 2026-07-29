package main

import (
	"encoding/json"
	"errors"
	"net/http"
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
		current, err := s.foldUserContextDTO()
		if err != nil {
			return err
		}
		snapshot, err := historyJSON(map[string]string{"text": current.Text})
		if err != nil {
			return err
		}
		return s.dal.SaveWithDocumentHistory(kind, key, snapshot, actor, func(ex sqlExecer) error {
			return putUserContextOn(ex, UserContext{Text: content["text"], Tombstoned: false})
		})
	case "role_definition":
		current, err := s.foldRoleDefDTO(key)
		if err != nil {
			return err
		}
		if current == nil {
			return errNotFound
		}
		snapshot, err := historyJSON(map[string]string{"name": current.Name, "definition_md": current.DefinitionMD})
		if err != nil {
			return err
		}
		return s.dal.SaveWithDocumentHistory(kind, key, snapshot, actor, func(ex sqlExecer) error {
			return putRoleDefOn(ex, RoleDef{RoleKey: key, Name: content["name"], DefinitionMD: content["definition_md"]})
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
		snapshot, err := historyJSON(map[string]string{"text": current.Text})
		if err != nil {
			return err
		}
		return s.dal.SaveWithDocumentHistory(kind, key, snapshot, actor, func(ex sqlExecer) error {
			return putLessonsOn(ex, Lessons{RoleKey: roleKey, TaskType: taskType, Text: content["text"]})
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
		snapshot, err := taskManualHistorySnapshot(*current)
		if err != nil {
			return err
		}
		next := *current
		next.Purpose, next.Fields, next.SopMD, next.Learnings = content["purpose"], content["fields"], content["sop_md"], content["learnings"]
		next.UpdatedTS = nowSecs()
		return s.dal.SaveWithDocumentHistory(kind, key, snapshot, actor, func(ex sqlExecer) error {
			return putTaskManualOn(ex, next)
		})
	}
	return errNotFound
}
