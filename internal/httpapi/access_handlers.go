package httpapi

import (
	"errors"
	"net/http"
)

func (s *server) members(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"members": s.access.memberViews()})
}

func (s *server) createMember(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Password    string   `json:"password"`
		Permissions []string `json:"permissions"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "成员密码请求格式不正确")
		return
	}
	member, err := s.access.createMember(request.Password, request.Permissions)
	request.Password = ""
	if err != nil {
		writeAccessError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, member)
}

func (s *server) updateMember(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Password    *string   `json:"password,omitempty"`
		Permissions *[]string `json:"permissions,omitempty"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "成员密码请求格式不正确")
		return
	}
	member, err := s.access.updateMember(
		r.PathValue("id"), request.Password, request.Permissions,
	)
	if request.Password != nil {
		*request.Password = ""
	}
	if err != nil {
		writeAccessError(w, err)
		return
	}
	s.sessions.deleteCredential(member.ID)
	writeJSON(w, http.StatusOK, member)
}

func (s *server) deleteMember(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.access.deleteMember(id); err != nil {
		writeAccessError(w, err)
		return
	}
	s.sessions.deleteCredential(id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) loginAudit(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"entries": s.access.auditEntries()})
}

func writeAccessError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errMemberNotFound):
		writeError(w, http.StatusNotFound, "成员密码不存在")
	case errors.Is(err, errCredentialExists):
		writeError(w, http.StatusConflict, "该密码已被管理员或其他成员使用")
	case errors.Is(err, errMemberLimit):
		writeError(w, http.StatusConflict, "成员密码数量已达到上限")
	case errors.Is(err, errInvalidPermission):
		writeError(w, http.StatusBadRequest, "成员权限不正确")
	default:
		writeError(w, http.StatusBadRequest, err.Error())
	}
}
