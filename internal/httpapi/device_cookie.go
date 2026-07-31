package httpapi

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	deviceCookieName       = "hearth_known_device"
	deviceCookieKeySize    = 32
	deviceCookieTTL        = 30 * 24 * time.Hour
	deviceCookieRefreshAge = 23 * 24 * time.Hour
	maxDeviceTokenBytes    = 1024
)

type deviceCookieManager struct {
	key []byte
}

type deviceCookiePayload struct {
	Version   int    `json:"v"`
	DeviceID  string `json:"deviceId"`
	IssuedAt  int64  `json:"issuedAt"`
	ExpiresAt int64  `json:"expiresAt"`
}

type knownDevice struct {
	ID        string
	IssuedAt  time.Time
	ExpiresAt time.Time
}

func newDeviceCookieManager(path string) (*deviceCookieManager, error) {
	key, err := loadOrCreateDeviceKey(path)
	if err != nil {
		return nil, err
	}
	return &deviceCookieManager{key: key}, nil
}

func loadOrCreateDeviceKey(path string) ([]byte, error) {
	if path == "" {
		key := make([]byte, deviceCookieKeySize)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
		return key, nil
	}
	key, err := os.ReadFile(path)
	if err == nil {
		if len(key) != deviceCookieKeySize {
			return nil, fmt.Errorf("device cookie key must contain exactly %d bytes", deviceCookieKeySize)
		}
		return key, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("read device cookie key: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create device cookie key directory: %w", err)
	}
	key = make([]byte, deviceCookieKeySize)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return loadOrCreateDeviceKey(path)
	}
	if err != nil {
		return nil, fmt.Errorf("create device cookie key: %w", err)
	}
	ok := false
	defer func() {
		_ = file.Close()
		if !ok {
			_ = os.Remove(path)
		}
	}()
	if _, err := file.Write(key); err != nil {
		return nil, fmt.Errorf("write device cookie key: %w", err)
	}
	if err := file.Sync(); err != nil {
		return nil, fmt.Errorf("sync device cookie key: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("close device cookie key: %w", err)
	}
	ok = true
	return key, nil
}

func (m *deviceCookieManager) read(r *http.Request, now time.Time) (knownDevice, bool) {
	cookie, err := r.Cookie(deviceCookieName)
	if err != nil || len(cookie.Value) > maxDeviceTokenBytes {
		return knownDevice{}, false
	}
	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 2 {
		return knownDevice{}, false
	}
	payloadBytes, payloadErr := base64.RawURLEncoding.DecodeString(parts[0])
	signature, signatureErr := base64.RawURLEncoding.DecodeString(parts[1])
	if payloadErr != nil || signatureErr != nil {
		return knownDevice{}, false
	}
	expected := m.sign(parts[0])
	if len(signature) != len(expected) || subtle.ConstantTimeCompare(signature, expected) != 1 {
		return knownDevice{}, false
	}
	var payload deviceCookiePayload
	decoder := json.NewDecoder(strings.NewReader(string(payloadBytes)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil || payload.Version != 1 ||
		!validDeviceID(payload.DeviceID) {
		return knownDevice{}, false
	}
	issuedAt := time.Unix(payload.IssuedAt, 0)
	expiresAt := time.Unix(payload.ExpiresAt, 0)
	if expiresAt.Before(now) || issuedAt.After(now.Add(5*time.Minute)) ||
		expiresAt.Sub(issuedAt) > deviceCookieTTL+time.Minute {
		return knownDevice{}, false
	}
	return knownDevice{ID: payload.DeviceID, IssuedAt: issuedAt, ExpiresAt: expiresAt}, true
}

func (m *deviceCookieManager) newCookie(now time.Time, secure bool) (*http.Cookie, knownDevice, error) {
	random := make([]byte, 18)
	if _, err := rand.Read(random); err != nil {
		return nil, knownDevice{}, err
	}
	device := knownDevice{
		ID:        base64.RawURLEncoding.EncodeToString(random),
		IssuedAt:  now,
		ExpiresAt: now.Add(deviceCookieTTL),
	}
	cookie, err := m.cookieForDevice(device, secure)
	return cookie, device, err
}

func (m *deviceCookieManager) refreshCookie(device knownDevice, now time.Time, secure bool) (*http.Cookie, error) {
	device.IssuedAt = now
	device.ExpiresAt = now.Add(deviceCookieTTL)
	return m.cookieForDevice(device, secure)
}

func (m *deviceCookieManager) cookieForDevice(device knownDevice, secure bool) (*http.Cookie, error) {
	payload := deviceCookiePayload{
		Version: 1, DeviceID: device.ID,
		IssuedAt: device.IssuedAt.Unix(), ExpiresAt: device.ExpiresAt.Unix(),
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	body := base64.RawURLEncoding.EncodeToString(encoded)
	signature := base64.RawURLEncoding.EncodeToString(m.sign(body))
	return &http.Cookie{
		Name: deviceCookieName, Value: body + "." + signature,
		Path: "/api/v1/session", HttpOnly: true, Secure: secure,
		SameSite: http.SameSiteStrictMode, MaxAge: int(deviceCookieTTL / time.Second),
		Expires: device.ExpiresAt,
	}, nil
}

func (m *deviceCookieManager) sign(value string) []byte {
	mac := hmac.New(sha256.New, m.key)
	_, _ = mac.Write([]byte("hearth-device-cookie-v1\x00"))
	_, _ = mac.Write([]byte(value))
	return mac.Sum(nil)
}

func validDeviceID(value string) bool {
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	return err == nil && len(decoded) == 18
}
