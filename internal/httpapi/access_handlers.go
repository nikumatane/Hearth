package httpapi

import (
	"errors"
	"net/http"
	"net/netip"
	"strings"
	"time"
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
	identity, _ := principalFromContext(r.Context())
	recordOperationAudit(s.operationAudits, operationAuditEntry{
		ID: newAuditID(), Event: operationEventMemberCreated,
		ActorCredentialID: identity.CredentialID, ActorRole: identity.Role,
		ActorIP: s.proxy.clientIP(r), TargetType: operationTargetMember,
		TargetID: member.ID, CurrentPermissions: member.Permissions,
		Success: true, CreatedAt: time.Now(),
	})
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
	passwordChanged := request.Password != nil
	member, permissionsChanged, err := s.access.updateMember(
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
	identity, _ := principalFromContext(r.Context())
	recordOperationAudit(s.operationAudits, operationAuditEntry{
		ID: newAuditID(), Event: operationEventMemberUpdated,
		ActorCredentialID: identity.CredentialID, ActorRole: identity.Role,
		ActorIP: s.proxy.clientIP(r), TargetType: operationTargetMember,
		TargetID: member.ID, PasswordChanged: passwordChanged,
		PermissionsChanged: permissionsChanged, CurrentPermissions: member.Permissions,
		Success: true, CreatedAt: time.Now(),
	})
	writeJSON(w, http.StatusOK, member)
}

func (s *server) deleteMember(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.access.deleteMember(id); err != nil {
		writeAccessError(w, err)
		return
	}
	s.sessions.deleteCredential(id)
	identity, _ := principalFromContext(r.Context())
	recordOperationAudit(s.operationAudits, operationAuditEntry{
		ID: newAuditID(), Event: operationEventMemberDeleted,
		ActorCredentialID: identity.CredentialID, ActorRole: identity.Role,
		ActorIP: s.proxy.clientIP(r), TargetType: operationTargetMember,
		TargetID: id, Success: true, CreatedAt: time.Now(),
	})
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) loginAudit(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"entries": s.access.auditEntries()})
}

func (s *server) configAudit(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"entries": s.configAudits.all()})
}

func (s *server) operationAudit(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"entries": s.operationAudits.all()})
}

func (s *server) listIPRules(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"rules": s.ipRules.all(time.Now())})
}

func (s *server) createIPRule(w http.ResponseWriter, r *http.Request) {
	var request struct {
		IP               string `json:"ip"`
		Kind             string `json:"kind"`
		Note             string `json:"note,omitempty"`
		ExpiresInHours   *int   `json:"expiresInHours,omitempty"`
		Permanent        bool   `json:"permanent,omitempty"`
		ConfirmCurrentIP bool   `json:"confirmCurrentIp,omitempty"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "IP 规则请求格式不正确")
		return
	}
	address, err := netip.ParseAddr(strings.TrimSpace(request.IP))
	if err != nil || !s.proxy.ruleAddressAllowed(address) {
		writeError(w, http.StatusBadRequest, "不能为无效、本机或可信代理地址创建规则")
		return
	}
	address = address.Unmap()
	if request.Kind != ipRuleAllow && request.Kind != ipRuleDeny {
		writeError(w, http.StatusBadRequest, "IP 规则类型不正确")
		return
	}
	sourceIP := s.proxy.clientIP(r)
	if request.Kind == ipRuleDeny && address.String() == sourceIP && !request.ConfirmCurrentIP {
		writeError(w, http.StatusConflict, "该操作会阻止当前 IP，请确认后重试")
		return
	}
	now := time.Now()
	var expiresAt *time.Time
	if !request.Permanent {
		hours := 24
		if request.Kind == ipRuleAllow {
			hours = 7 * 24
		}
		if request.ExpiresInHours != nil {
			hours = *request.ExpiresInHours
		}
		if hours < 1 || hours > 365*24 {
			writeError(w, http.StatusBadRequest, "有效期需要在 1 小时到 365 天之间")
			return
		}
		expiry := now.Add(time.Duration(hours) * time.Hour)
		expiresAt = &expiry
	}
	identity, _ := principalFromContext(r.Context())
	rule, err := s.ipRules.add(
		address.String(), request.Kind, request.Note, identity.CredentialID, expiresAt, now,
	)
	if err != nil {
		writeIPRuleError(w, err)
		return
	}
	recordOperationAudit(s.operationAudits, operationAuditEntry{
		ID: newAuditID(), Event: operationEventIPRuleAdded,
		ActorCredentialID: identity.CredentialID, ActorRole: identity.Role,
		ActorIP: sourceIP, TargetType: operationTargetIPRule,
		TargetID: rule.ID, TargetIP: rule.IP, RuleKind: rule.Kind,
		ExpiresAt: rule.ExpiresAt, Success: true, CreatedAt: now,
	})
	writeJSON(w, http.StatusCreated, rule)
}

func (s *server) deleteIPRule(w http.ResponseWriter, r *http.Request) {
	rule, err := s.ipRules.remove(r.PathValue("id"))
	if err != nil {
		writeIPRuleError(w, err)
		return
	}
	identity, _ := principalFromContext(r.Context())
	recordOperationAudit(s.operationAudits, operationAuditEntry{
		ID: newAuditID(), Event: operationEventIPRuleRemoved,
		ActorCredentialID: identity.CredentialID, ActorRole: identity.Role,
		ActorIP: s.proxy.clientIP(r), TargetType: operationTargetIPRule,
		TargetID: rule.ID, TargetIP: rule.IP, RuleKind: rule.Kind,
		Success: true, CreatedAt: time.Now(),
	})
	w.WriteHeader(http.StatusNoContent)
}

func writeIPRuleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errIPRuleNotFound):
		writeError(w, http.StatusNotFound, "IP 规则不存在")
	case errors.Is(err, errIPRuleLimit):
		writeError(w, http.StatusConflict, "IP 规则数量已达到上限")
	default:
		writeError(w, http.StatusBadRequest, err.Error())
	}
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
