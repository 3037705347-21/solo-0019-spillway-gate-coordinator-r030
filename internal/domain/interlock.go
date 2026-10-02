package domain

import (
	"strings"
	"time"
)

type InterlockVerdict string

const (
	InterlockClear   InterlockVerdict = "clear"
	InterlockBlocked InterlockVerdict = "blocked"
)

type InterlockDecision struct {
	Verdict    InterlockVerdict
	Verifier   string
	Note       string
	ReviewedAt time.Time
}

const RequiredInterlockApprovals = 2

type InterlockCycleStatus string

const (
	InterlockCyclePending   InterlockCycleStatus = "pending"
	InterlockCycleApproved  InterlockCycleStatus = "approved"
	InterlockCycleRejected  InterlockCycleStatus = "rejected"
	InterlockCycleDuplicate InterlockCycleStatus = "duplicate"
)

type InterlockCycle struct {
	Round       int
	Decisions   []InterlockDecision
	ApprovedBy  []string
	RejectedBy  []string
	LastUpdated time.Time
}

func NewInterlockCycle(round int) InterlockCycle {
	if round < 1 {
		round = 1
	}
	return InterlockCycle{
		Round:      round,
		Decisions:  make([]InterlockDecision, 0, RequiredInterlockApprovals),
		ApprovedBy: make([]string, 0, RequiredInterlockApprovals),
	}
}

func (c *InterlockCycle) Record(decision InterlockDecision) InterlockCycleStatus {
	if c.Round < 1 {
		*c = NewInterlockCycle(1)
	}

	for _, recorded := range c.Decisions {
		if recorded.Verifier == decision.Verifier && recorded.Verdict == decision.Verdict {
			return InterlockCycleDuplicate
		}
	}

	decisionCopy := decision
	c.Decisions = append(c.Decisions, decisionCopy)
	c.LastUpdated = decision.ReviewedAt.UTC()

	if !decision.IsClear() {
		c.RejectedBy = append(c.RejectedBy, decision.Verifier)
		return InterlockCycleRejected
	}

	c.ApprovedBy = append(c.ApprovedBy, decision.Verifier)
	if c.IndependentApprovals() >= RequiredInterlockApprovals {
		return InterlockCycleApproved
	}
	return InterlockCyclePending
}

func (c InterlockCycle) Clone() InterlockCycle {
	cloned := c
	cloned.Decisions = append([]InterlockDecision(nil), c.Decisions...)
	cloned.ApprovedBy = append([]string(nil), c.ApprovedBy...)
	cloned.RejectedBy = append([]string(nil), c.RejectedBy...)
	return cloned
}

func (c InterlockCycle) IndependentApprovals() int {
	seen := make(map[string]struct{}, len(c.ApprovedBy))
	for _, verifier := range c.ApprovedBy {
		seen[verifier] = struct{}{}
	}
	return len(seen)
}

func (c InterlockCycle) HasRejection() bool {
	for _, decision := range c.Decisions {
		if !decision.IsClear() {
			return true
		}
	}
	return false
}

func NewInterlockDecision(verdict, verifier, note string, now time.Time) (InterlockDecision, error) {
	normalized := InterlockVerdict(strings.ToLower(strings.TrimSpace(verdict)))
	if normalized != InterlockClear && normalized != InterlockBlocked {
		return InterlockDecision{}, Invalid("invalid_interlock_verdict", "verdict must be clear or blocked")
	}

	verifier = strings.TrimSpace(verifier)
	if verifier == "" {
		return InterlockDecision{}, Invalid("invalid_interlock_verifier", "verifier is required")
	}

	note = strings.TrimSpace(note)
	if normalized == InterlockBlocked && note == "" {
		return InterlockDecision{}, Invalid("invalid_interlock_note", "blocked decisions require a note")
	}

	return InterlockDecision{
		Verdict:    normalized,
		Verifier:   verifier,
		Note:       note,
		ReviewedAt: now.UTC(),
	}, nil
}

func (d InterlockDecision) IsClear() bool {
	return d.Verdict == InterlockClear
}
