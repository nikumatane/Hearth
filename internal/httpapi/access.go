package httpapi

import (
	"bufio"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	roleAdmin                  = "admin"
	roleMember                 = "member"
	permissionGameControl      = "game.control"
	permissionGameUpdate       = "game.update"
	permissionGameBackup       = "game.backup"
	permissionPalworldSettings = "palworld.settings"
	passwordIterations         = 120_000
	passwordLength             = 32
	maxMemberCredentials       = 20
	maxAuditEntries            = 500
)

var (
	errCredentialExists  = errors.New("credential password already exists")
	errMemberNotFound    = errors.New("member credential not found")
	errMemberLimit       = errors.New("member credential limit reached")
	errInvalidPermission = errors.New("invalid member permission")
	allPermissions       = []string{
		permissionGameControl,
		permissionGameUpdate,
		permissionGameBackup,
		permissionPalworldSettings,
	}
)

type principal struct {
	Role         string   `json:"role"`
	CredentialID string   `json:"credentialId"`
	Permissions  []string `json:"permissions"`
}

type passwordDigest struct {
	Salt       string `json:"salt"`
	Hash       string `json:"hash"`
	Iterations int    `json:"iterations"`
}

type memberCredential struct {
	ID          string         `json:"id"`
	Password    passwordDigest `json:"password"`
	Permissions []string       `json:"permissions"`
	CreatedAt   time.Time      `json:"createdAt"`
	UpdatedAt   time.Time      `json:"updatedAt"`
	LastUsedAt  *time.Time     `json:"lastUsedAt,omitempty"`
}

type memberView struct {
	ID          string     `json:"id"`
	Permissions []string   `json:"permissions"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	LastUsedAt  *time.Time `json:"lastUsedAt,omitempty"`
}

type credentialDocument struct {
	Version int                `json:"version"`
	Members []memberCredential `json:"members"`
}

type auditEntry struct {
	ID           string    `json:"id"`
	IP           string    `json:"ip"`
	CredentialID string    `json:"credentialId"`
	Role         string    `json:"role,omitempty"`
	Success      bool      `json:"success"`
	Reason       string    `json:"reason,omitempty"`
	CreatedAt    time.Time `json:"createdAt"`
}

type accessStore struct {
	mu            sync.Mutex
	path          string
	auditPath     string
	adminPassword passwordDigest
	members       []memberCredential
	audits        []auditEntry
}

func newAccessStore(adminPassword, path, auditPath string) (*accessStore, error) {
	adminDigest, err := newPasswordDigest(adminPassword)
	if err != nil {
		return nil, fmt.Errorf("hash admin password: %w", err)
	}
	store := &accessStore{
		path: path, auditPath: auditPath, adminPassword: adminDigest,
		members: []memberCredential{}, audits: []auditEntry{},
	}
	if err := store.loadMembers(); err != nil {
		return nil, err
	}
	if err := store.loadAudits(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *accessStore) authenticate(password string) (principal, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if verifyPassword(password, s.adminPassword) {
		return principal{
			Role: roleAdmin, CredentialID: "ADMIN", Permissions: slices.Clone(allPermissions),
		}, true
	}
	for index := range s.members {
		if !verifyPassword(password, s.members[index].Password) {
			continue
		}
		now := time.Now()
		s.members[index].LastUsedAt = &now
		if err := s.persistMembersLocked(); err != nil {
			slog.Error("persist member last-used time", "error", err)
		}
		return principal{
			Role: roleMember, CredentialID: s.members[index].ID,
			Permissions: slices.Clone(s.members[index].Permissions),
		}, true
	}
	return principal{}, false
}

func (s *accessStore) memberViews() []memberView {
	s.mu.Lock()
	defer s.mu.Unlock()
	views := make([]memberView, 0, len(s.members))
	for _, member := range s.members {
		views = append(views, memberView{
			ID: member.ID, Permissions: slices.Clone(member.Permissions),
			CreatedAt: member.CreatedAt, UpdatedAt: member.UpdatedAt,
			LastUsedAt: member.LastUsedAt,
		})
	}
	slices.SortFunc(views, func(left, right memberView) int {
		return right.CreatedAt.Compare(left.CreatedAt)
	})
	return views
}

func (s *accessStore) createMember(password string, permissions []string) (memberView, error) {
	if err := validateMemberPassword(password); err != nil {
		return memberView{}, err
	}
	normalizedPermissions, err := normalizePermissions(permissions)
	if err != nil {
		return memberView{}, err
	}
	digest, err := newPasswordDigest(password)
	if err != nil {
		return memberView{}, err
	}
	id, err := newMemberID()
	if err != nil {
		return memberView{}, err
	}
	now := time.Now()
	member := memberCredential{
		ID: id, Password: digest, Permissions: normalizedPermissions,
		CreatedAt: now, UpdatedAt: now,
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.members) >= maxMemberCredentials {
		return memberView{}, errMemberLimit
	}
	if s.passwordExistsLocked(password, "") {
		return memberView{}, errCredentialExists
	}
	s.members = append(s.members, member)
	if err := s.persistMembersLocked(); err != nil {
		s.members = s.members[:len(s.members)-1]
		return memberView{}, err
	}
	return memberView{
		ID: id, Permissions: slices.Clone(normalizedPermissions),
		CreatedAt: now, UpdatedAt: now,
	}, nil
}

func (s *accessStore) updateMember(
	id string,
	password *string,
	permissions *[]string,
) (memberView, error) {
	if password == nil && permissions == nil {
		return memberView{}, errors.New("至少需要修改密码或权限")
	}
	var digest passwordDigest
	if password != nil {
		if err := validateMemberPassword(*password); err != nil {
			return memberView{}, err
		}
		var err error
		digest, err = newPasswordDigest(*password)
		if err != nil {
			return memberView{}, err
		}
	}
	var normalizedPermissions []string
	if permissions != nil {
		var err error
		normalizedPermissions, err = normalizePermissions(*permissions)
		if err != nil {
			return memberView{}, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	index := s.memberIndexLocked(id)
	if index < 0 {
		return memberView{}, errMemberNotFound
	}
	if password != nil && s.passwordExistsLocked(*password, id) {
		return memberView{}, errCredentialExists
	}
	previous := s.members[index]
	now := time.Now()
	if password != nil {
		s.members[index].Password = digest
	}
	if permissions != nil {
		s.members[index].Permissions = normalizedPermissions
	}
	s.members[index].UpdatedAt = now
	if err := s.persistMembersLocked(); err != nil {
		s.members[index] = previous
		return memberView{}, err
	}
	member := s.members[index]
	return memberView{
		ID: member.ID, Permissions: slices.Clone(member.Permissions),
		CreatedAt: member.CreatedAt, UpdatedAt: member.UpdatedAt,
		LastUsedAt: member.LastUsedAt,
	}, nil
}

func (s *accessStore) deleteMember(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	index := s.memberIndexLocked(id)
	if index < 0 {
		return errMemberNotFound
	}
	previous := slices.Clone(s.members)
	s.members = append(s.members[:index], s.members[index+1:]...)
	if err := s.persistMembersLocked(); err != nil {
		s.members = previous
		return err
	}
	return nil
}

func (s *accessStore) passwordExistsLocked(password, exceptID string) bool {
	if verifyPassword(password, s.adminPassword) {
		return true
	}
	for _, member := range s.members {
		if member.ID != exceptID && verifyPassword(password, member.Password) {
			return true
		}
	}
	return false
}

func (s *accessStore) memberIndexLocked(id string) int {
	for index, member := range s.members {
		if subtle.ConstantTimeCompare([]byte(member.ID), []byte(id)) == 1 {
			return index
		}
	}
	return -1
}

func (s *accessStore) recordAudit(entry auditEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.audits = append([]auditEntry{entry}, s.audits...)
	if len(s.audits) > maxAuditEntries {
		s.audits = s.audits[:maxAuditEntries]
	}
	if s.auditPath == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(s.auditPath), 0o700); err != nil {
		slog.Error("create audit directory", "error", err)
		return
	}
	file, err := os.OpenFile(s.auditPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		slog.Error("open login audit log", "error", err)
		return
	}
	defer file.Close()
	if err := json.NewEncoder(file).Encode(entry); err != nil {
		slog.Error("append login audit log", "error", err)
	}
}

func (s *accessStore) auditEntries() []auditEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.audits)
}

func (s *accessStore) loadMembers() error {
	if s.path == "" {
		return nil
	}
	data, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read member credentials: %w", err)
	}
	var document credentialDocument
	if err := json.Unmarshal(data, &document); err != nil {
		return fmt.Errorf("decode member credentials: %w", err)
	}
	if document.Version != 1 {
		return fmt.Errorf("unsupported member credential version %d", document.Version)
	}
	if len(document.Members) > maxMemberCredentials {
		return fmt.Errorf("member credential count exceeds %d", maxMemberCredentials)
	}
	for index := range document.Members {
		permissions, err := normalizePermissions(document.Members[index].Permissions)
		if err != nil {
			return fmt.Errorf("member %s permissions: %w", document.Members[index].ID, err)
		}
		document.Members[index].Permissions = permissions
	}
	s.members = document.Members
	return nil
}

func (s *accessStore) loadAudits() error {
	if s.auditPath == "" {
		return nil
	}
	file, err := os.Open(s.auditPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read login audit log: %w", err)
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	entries := make([]auditEntry, 0, maxAuditEntries)
	for scanner.Scan() {
		var entry auditEntry
		if json.Unmarshal(scanner.Bytes(), &entry) == nil {
			entries = append(entries, entry)
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan login audit log: %w", err)
	}
	if len(entries) > maxAuditEntries {
		entries = entries[len(entries)-maxAuditEntries:]
	}
	slices.Reverse(entries)
	s.audits = entries
	return nil
}

func (s *accessStore) persistMembersLocked() error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(credentialDocument{Version: 1, Members: s.members}, "", "  ")
	if err != nil {
		return err
	}
	temp := s.path + ".tmp"
	if err := os.WriteFile(temp, append(data, '\n'), 0o600); err != nil {
		return err
	}
	if err := replaceCredentialFile(temp, s.path); err != nil {
		_ = os.Remove(temp)
		return err
	}
	return nil
}

// replaceCredentialFile avoids relying on replacing an existing path with
// os.Rename, which is not supported consistently on Windows.
func replaceCredentialFile(temp, destination string) error {
	if _, err := os.Stat(destination); errors.Is(err, os.ErrNotExist) {
		return os.Rename(temp, destination)
	} else if err != nil {
		return err
	}

	backup := destination + ".bak"
	if err := os.Remove(backup); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Rename(destination, backup); err != nil {
		return err
	}
	if err := os.Rename(temp, destination); err != nil {
		if rollbackErr := os.Rename(backup, destination); rollbackErr != nil {
			return fmt.Errorf("replace credentials: %w (rollback failed: %v)", err, rollbackErr)
		}
		return err
	}
	if err := os.Remove(backup); err != nil && !errors.Is(err, os.ErrNotExist) {
		slog.Warn("remove credential backup", "path", backup, "error", err)
	}
	return nil
}

func newPasswordDigest(password string) (passwordDigest, error) {
	salt := make([]byte, 24)
	if _, err := rand.Read(salt); err != nil {
		return passwordDigest{}, err
	}
	hash := pbkdf2SHA256([]byte(password), salt, passwordIterations, passwordLength)
	return passwordDigest{
		Salt: base64.RawStdEncoding.EncodeToString(salt),
		Hash: base64.RawStdEncoding.EncodeToString(hash), Iterations: passwordIterations,
	}, nil
}

func verifyPassword(password string, digest passwordDigest) bool {
	salt, saltErr := base64.RawStdEncoding.DecodeString(digest.Salt)
	expected, hashErr := base64.RawStdEncoding.DecodeString(digest.Hash)
	if saltErr != nil || hashErr != nil || digest.Iterations < 1 || len(expected) == 0 {
		return false
	}
	actual := pbkdf2SHA256([]byte(password), salt, digest.Iterations, len(expected))
	return subtle.ConstantTimeCompare(actual, expected) == 1
}

func pbkdf2SHA256(password, salt []byte, iterations, keyLength int) []byte {
	result := make([]byte, 0, keyLength)
	var blockIndex uint32 = 1
	for len(result) < keyLength {
		mac := hmac.New(sha256.New, password)
		_, _ = mac.Write(salt)
		var counter [4]byte
		binary.BigEndian.PutUint32(counter[:], blockIndex)
		_, _ = mac.Write(counter[:])
		previous := mac.Sum(nil)
		block := slices.Clone(previous)
		for round := 1; round < iterations; round++ {
			mac = hmac.New(sha256.New, password)
			_, _ = mac.Write(previous)
			previous = mac.Sum(nil)
			for index := range block {
				block[index] ^= previous[index]
			}
		}
		result = append(result, block...)
		blockIndex++
	}
	return result[:keyLength]
}

func validateMemberPassword(password string) error {
	if len(password) < 10 {
		return errors.New("成员密码至少需要 10 个字符")
	}
	if len(password) > 256 {
		return errors.New("成员密码不能超过 256 个字符")
	}
	return nil
}

func normalizePermissions(permissions []string) ([]string, error) {
	requested := make(map[string]bool, len(permissions))
	for _, permission := range permissions {
		permission = strings.TrimSpace(permission)
		if permission == "" {
			continue
		}
		if !slices.Contains(allPermissions, permission) {
			return nil, fmt.Errorf("%w: %s", errInvalidPermission, permission)
		}
		requested[permission] = true
	}
	normalized := make([]string, 0, len(requested))
	for _, permission := range allPermissions {
		if requested[permission] {
			normalized = append(normalized, permission)
		}
	}
	return normalized, nil
}

func hasPermission(identity principal, permission string) bool {
	return identity.Role == roleAdmin || slices.Contains(identity.Permissions, permission)
}

func newMemberID() (string, error) {
	buffer := make([]byte, 5)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return "M-" + base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buffer), nil
}

func newAuditID() string {
	buffer := make([]byte, 9)
	if _, err := rand.Read(buffer); err == nil {
		return base64.RawURLEncoding.EncodeToString(buffer)
	}
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

func requestIP(r *http.Request) string {
	if forwarded := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-For"), ",")[0]); forwarded != "" {
		if ip := net.ParseIP(forwarded); ip != nil {
			return ip.String()
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}
