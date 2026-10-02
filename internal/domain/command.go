package domain

import (
	"strings"
	"time"
)

type CommandStatus string

const (
	CommandPending   CommandStatus = "pending"
	CommandApproved  CommandStatus = "approved"
	CommandRejected  CommandStatus = "rejected"
	CommandExecuting CommandStatus = "executing"
	CommandCompleted CommandStatus = "completed"
	CommandAborted   CommandStatus = "aborted"
)

type GateCommand struct {
	ID               string
	GateID           string
	TargetAperture   float64
	StartingAperture float64
	Reason           string
	RequestedBy      string
	Status           CommandStatus
	Interlock        *InterlockDecision
	InterlockCycle   InterlockCycle
	ExecutionToken   string
	ExecutedBy       string
	TerminalReason   string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	Version          int64
}

func NewGateCommand(
	commandID string,
	gateID string,
	targetAperture float64,
	startingAperture float64,
	maxAperture float64,
	reason string,
	requestedBy string,
	now time.Time,
) (GateCommand, error) {
	commandID = strings.TrimSpace(commandID)
	if commandID == "" {
		return GateCommand{}, Invalid("invalid_command_id", "command id is required")
	}

	gateID = strings.TrimSpace(gateID)
	if gateID == "" {
		return GateCommand{}, Invalid("invalid_gate_reference", "gate id is required")
	}

	reason = strings.TrimSpace(reason)
	if reason == "" {
		return GateCommand{}, Invalid("invalid_command_reason", "reason is required")
	}

	requestedBy = strings.TrimSpace(requestedBy)
	if requestedBy == "" {
		return GateCommand{}, Invalid("invalid_requested_by", "requested_by is required")
	}

	if startingAperture < 0 || startingAperture > maxAperture {
		return GateCommand{}, Invalid("invalid_starting_aperture", "starting aperture is outside the gate limit")
	}
	if targetAperture < 0 || targetAperture > maxAperture {
		return GateCommand{}, Invalid("invalid_target_aperture", "target aperture is outside the gate limit")
	}
	if targetAperture == startingAperture {
		return GateCommand{}, Invalid("no_aperture_change", "target aperture must differ from the current aperture")
	}

	return GateCommand{
		ID:               commandID,
		GateID:           gateID,
		TargetAperture:   targetAperture,
		StartingAperture: startingAperture,
		Reason:           reason,
		RequestedBy:      requestedBy,
		Status:           CommandPending,
		CreatedAt:        now.UTC(),
		UpdatedAt:        now.UTC(),
		Version:          1,
	}, nil
}

// Review applies one safety interlock decision to a pending command.
//
// The command only becomes approved once the full multi-person review cycle
// independently clears it; until then it stays pending and carries no token,
// so the gate must not be reserved either. Any blocked verdict rejects the
// adjustment outright: command status, gate reservation and execution
// credentials then describe one and the same terminal fact.
//
// Replaying a verifier's identical verdict is idempotent and reports changed
// == false: a retried request returns the already recorded fact instead of
// minting a new token or double counting the approval. A verifier
// contradicting their own recorded verdict is refused as a conflict. Approval
// and gate reservation stay coupled: the caller only reserves the gate when
// status transitions to approved.
func (c *GateCommand) Review(decision InterlockDecision, token string, now time.Time) (changed bool, err error) {
	if c.Status != CommandPending {
		// A reviewer retrying over an unstable link after the cycle closed
		// gets the already recorded fact back, including the same token; a
		// fresh opinion on a finished adjustment is refused instead so a
		// terminal adjustment can never become executable again.
		if prior, ok := c.InterlockCycle.VerdictByVerifier(decision.Verifier); ok && prior == decision.Verdict {
			return false, nil
		}
		return false, Conflict("command_not_pending", "only pending commands can be reviewed")
	}
	if c.InterlockCycle.Round == 0 {
		c.InterlockCycle = NewInterlockCycle(1)
	}

	cycleStatus := c.InterlockCycle.Record(decision)
	switch cycleStatus {
	case InterlockCycleDuplicate:
		// Replayed verdict: the recorded fact already stands. Nothing is
		// rewritten and no new token is issued.
		return false, nil
	case InterlockCycleConflict:
		return false, Conflict("interlock_verdict_conflict", "verifier already recorded a different verdict")
	}

	c.UpdatedAt = now.UTC()
	c.Version++

	decisionCopy := decision
	c.Interlock = &decisionCopy

	if !decision.IsClear() {
		// One blocked verdict terminates the adjustment. The gate has never
		// been reserved while review is still open, so nothing is left to
		// undo, and any token slot is cleared.
		c.ExecutionToken = ""
		c.Status = CommandRejected
		return true, nil
	}

	// Clear verdict but the cycle is still open: keep waiting for the
	// remaining independent reviewers. No approval, no token, no occupancy.
	if cycleStatus == InterlockCyclePending {
		return true, nil
	}

	token = strings.TrimSpace(token)
	if token == "" {
		return false, Invalid("missing_execution_token", "clear decisions require an execution token")
	}
	c.ExecutionToken = token
	c.Status = CommandApproved
	return true, nil
}

func (c *GateCommand) BeginExecution(token, operator string, now time.Time) error {
	if c.Status != CommandApproved {
		return Conflict("command_not_executable", "only approved commands can execute")
	}
	if strings.TrimSpace(operator) == "" {
		return Invalid("invalid_operator", "operator is required")
	}
	if strings.TrimSpace(token) == "" || token != c.ExecutionToken {
		return Conflict("invalid_execution_token", "execution token is invalid")
	}

	c.ExecutedBy = strings.TrimSpace(operator)
	c.Status = CommandExecuting
	c.UpdatedAt = now.UTC()
	c.Version++
	return nil
}

func (c *GateCommand) Complete(now time.Time) error {
	if c.Status != CommandExecuting {
		return Conflict("command_not_executing", "command is not executing")
	}

	c.ExecutionToken = ""
	c.Status = CommandCompleted
	c.UpdatedAt = now.UTC()
	c.Version++
	return nil
}

func (c *GateCommand) Abort(reason string, now time.Time) error {
	if c.Status != CommandExecuting {
		return Conflict("command_not_executing", "command is not executing")
	}

	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "gate_execution_failed"
	}
	c.ExecutionToken = ""
	c.TerminalReason = reason
	c.Status = CommandAborted
	c.UpdatedAt = now.UTC()
	c.Version++
	return nil
}
