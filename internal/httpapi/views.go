package httpapi

import (
	"time"

	"example.com/spillway-gate-coordinator/internal/contracts"
	"example.com/spillway-gate-coordinator/internal/domain"
)

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func gateView(gate domain.Gate) contracts.GateView {
	return contracts.GateView{
		ID:              gate.ID,
		Name:            gate.Name,
		Status:          string(gate.Status),
		Aperture:        gate.Aperture,
		MaxAperture:     gate.MaxAperture,
		ActiveCommandID: gate.ActiveCommandID,
		UpdatedAt:       formatTime(gate.UpdatedAt),
		Version:         gate.Version,
	}
}

func commandView(command domain.GateCommand, includeToken bool) contracts.CommandView {
	view := contracts.CommandView{
		ID:               command.ID,
		GateID:           command.GateID,
		TargetAperture:   command.TargetAperture,
		StartingAperture: command.StartingAperture,
		Reason:           command.Reason,
		RequestedBy:      command.RequestedBy,
		Status:           string(command.Status),
		ExecutedBy:       command.ExecutedBy,
		TerminalReason:   command.TerminalReason,
		CreatedAt:        formatTime(command.CreatedAt),
		UpdatedAt:        formatTime(command.UpdatedAt),
		Version:          command.Version,
	}

	if includeToken && command.Status == domain.CommandApproved {
		view.ExecutionToken = command.ExecutionToken
	}
	if command.Interlock != nil {
		view.Interlock = interlockDecisionView(*command.Interlock)
	}
	if command.InterlockCycle.Round > 0 {
		view.InterlockCycle = interlockCycleView(command)
	}
	return view
}

func interlockDecisionView(decision domain.InterlockDecision) *contracts.InterlockDecisionView {
	return &contracts.InterlockDecisionView{
		Verdict:    string(decision.Verdict),
		Verifier:   decision.Verifier,
		Note:       decision.Note,
		ReviewedAt: formatTime(decision.ReviewedAt),
	}
}

// interlockCycleView projects the review state so the control room can tell
// whether the adjustment is still under review, fully cleared or rejected,
// independent of any single reviewer's retried response.
func interlockCycleView(command domain.GateCommand) *contracts.InterlockCycleView {
	cycle := command.InterlockCycle
	// The review outcome is derived from the recorded decisions themselves,
	// not the command's later execution phase, so it stays explainable after
	// the command completes or aborts.
	status := string(domain.InterlockCyclePending)
	switch {
	case cycle.HasRejection():
		status = string(domain.InterlockCycleRejected)
	case cycle.IndependentApprovals() >= domain.RequiredInterlockApprovals:
		status = string(domain.InterlockCycleApproved)
	}

	decisions := make([]contracts.InterlockDecisionView, 0, len(cycle.Decisions))
	for _, decision := range cycle.Decisions {
		decisions = append(decisions, *interlockDecisionView(decision))
	}

	return &contracts.InterlockCycleView{
		Round:                cycle.Round,
		Status:               status,
		IndependentApprovals: cycle.IndependentApprovals(),
		RequiredApprovals:    domain.RequiredInterlockApprovals,
		Decisions:            decisions,
	}
}
