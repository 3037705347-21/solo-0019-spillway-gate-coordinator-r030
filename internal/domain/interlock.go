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
	InterlockCycleConflict  InterlockCycleStatus = "conflict"
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

// Record applies a review decision to the cycle.
//
// Decisions are keyed by verifier: replaying the same verifier's verdict is a
// retry (duplicate) and never adds weight, so unstable network retries cannot
// inflate the approval count. A verifier changing their recorded verdict is a
// conflict and is refused. Independent clear verdicts from distinct
// verifiers advance the cycle; RequiredInterlockApprovals of them complete it,
// and any single blocked verdict rejects the whole adjustment.
func (c *InterlockCycle) Record(decision InterlockDecision) InterlockCycleStatus {
	if c.Round < 1 {
		*c = NewInterlockCycle(1)
	}

	if prior, ok := c.VerdictByVerifier(decision.Verifier); ok {
		if prior == decision.Verdict {
			return InterlockCycleDuplicate
		}
		return InterlockCycleConflict
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

// VerdictByVerifier returns the verdict already recorded by a verifier.
func (c InterlockCycle) VerdictByVerifier(verifier string) (InterlockVerdict, bool) {
	for _, decision := range c.Decisions {
		if decision.Verifier == verifier {
			return decision.Verdict, true
		}
	}
	return "", false
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
