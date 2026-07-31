package httpapi

import (
	"fmt"
	"sync"
	"time"
)

const (
	maxLoginFailureSources = 4096
	loginStateTTL          = time.Hour
	loginAuditInterval     = time.Minute
)

type loginGate struct {
	mu           sync.Mutex
	states       map[string]loginFailureState
	unknownSlots chan struct{}
	trustedSlots chan struct{}
}

type loginFailureState struct {
	failures          int
	lastFailureAt     time.Time
	nextAllowedAt     time.Time
	severity          string
	lastBlockedAudit  time.Time
	lastCapacityAudit time.Time
}

type loginDecision struct {
	Allowed      bool
	RetryAfter   time.Duration
	Reason       string
	Severity     string
	AttemptCount int
	ShouldAudit  bool
}

type loginAssessment struct {
	Failures    int
	Severity    string
	Reason      string
	AttackEvent bool
}

type loginAdmission struct {
	gate     *loginGate
	key      string
	trusted  bool
	slot     chan struct{}
	finished bool
}

func newLoginGate() *loginGate {
	return &loginGate{
		states:       make(map[string]loginFailureState),
		unknownSlots: make(chan struct{}, 2),
		trustedSlots: make(chan struct{}, 1),
	}
}

func (g *loginGate) begin(key string, trusted bool, now time.Time) (loginDecision, *loginAdmission) {
	g.mu.Lock()
	g.pruneLocked(now)
	state := g.states[key]
	if !state.nextAllowedAt.IsZero() && now.Before(state.nextAllowedAt) {
		shouldAudit := state.lastBlockedAudit.IsZero() || now.Sub(state.lastBlockedAudit) >= loginAuditInterval
		if shouldAudit {
			state.lastBlockedAudit = now
			g.states[key] = state
		}
		g.mu.Unlock()
		return loginDecision{
			RetryAfter: state.nextAllowedAt.Sub(now), Reason: "登录尝试过多",
			Severity: state.severity, AttemptCount: state.failures, ShouldAudit: shouldAudit,
		}, nil
	}
	g.mu.Unlock()

	slot := g.unknownSlots
	if trusted {
		slot = g.trustedSlots
	}
	select {
	case slot <- struct{}{}:
		return loginDecision{Allowed: true}, &loginAdmission{
			gate: g, key: key, trusted: trusted, slot: slot,
		}
	default:
		g.mu.Lock()
		state = g.states[key]
		shouldAudit := state.lastCapacityAudit.IsZero() || now.Sub(state.lastCapacityAudit) >= loginAuditInterval
		if shouldAudit {
			state.lastCapacityAudit = now
			g.states[key] = state
		}
		g.mu.Unlock()
		return loginDecision{
			RetryAfter: time.Second, Reason: "登录校验繁忙",
			Severity: "warning", AttemptCount: state.failures, ShouldAudit: shouldAudit,
		}, nil
	}
}

func (a *loginAdmission) cancel() {
	if a == nil || a.finished {
		return
	}
	a.finished = true
	<-a.slot
}

func (a *loginAdmission) finish(success bool, now time.Time) loginAssessment {
	if a == nil || a.finished {
		return loginAssessment{}
	}
	a.finished = true
	<-a.slot

	a.gate.mu.Lock()
	defer a.gate.mu.Unlock()
	if success {
		delete(a.gate.states, a.key)
		return loginAssessment{}
	}
	state := a.gate.states[a.key]
	if !state.lastFailureAt.IsZero() && now.Sub(state.lastFailureAt) >= loginStateTTL {
		state = loginFailureState{}
	}
	state.failures++
	state.lastFailureAt = now
	threshold, maximumDelay := 5, 5*time.Minute
	if a.trusted {
		threshold, maximumDelay = 10, 30*time.Second
	}
	delay := loginBackoff(state.failures, threshold, maximumDelay)
	if delay > 0 {
		state.nextAllowedAt = now.Add(delay)
	}
	severity := loginSeverity(state.failures, threshold)
	attackEvent := severity != "" && severity != state.severity
	state.severity = severity
	a.gate.states[a.key] = state

	reason := "密码不匹配"
	switch severity {
	case "warning":
		reason = "疑似自动化登录尝试"
	case "critical":
		reason = "疑似暴力破解"
	}
	return loginAssessment{
		Failures: state.failures, Severity: severity,
		Reason: reason, AttackEvent: attackEvent,
	}
}

func loginBackoff(failures, threshold int, maximum time.Duration) time.Duration {
	if failures < threshold {
		return 0
	}
	exponent := failures - threshold
	if exponent > 20 {
		return maximum
	}
	delay := time.Second * time.Duration(1<<exponent)
	if delay > maximum {
		return maximum
	}
	return delay
}

func loginSeverity(failures, threshold int) string {
	switch {
	case failures >= threshold+5:
		return "critical"
	case failures >= threshold:
		return "warning"
	default:
		return ""
	}
}

func (g *loginGate) pruneLocked(now time.Time) {
	for key, state := range g.states {
		if !state.lastFailureAt.IsZero() && now.Sub(state.lastFailureAt) >= loginStateTTL {
			delete(g.states, key)
		}
	}
	for len(g.states) >= maxLoginFailureSources {
		var oldestKey string
		var oldest time.Time
		for key, state := range g.states {
			candidate := state.lastFailureAt
			if candidate.IsZero() {
				candidate = state.lastCapacityAudit
			}
			if oldestKey == "" || candidate.Before(oldest) {
				oldestKey, oldest = key, candidate
			}
		}
		if oldestKey == "" {
			break
		}
		delete(g.states, oldestKey)
	}
}

func retryAfterHeader(duration time.Duration) string {
	seconds := int64(duration.Round(time.Second) / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	return fmt.Sprintf("%d", seconds)
}
