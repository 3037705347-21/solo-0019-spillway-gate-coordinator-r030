package main

import (
	"fmt"

	"net/http"
)

func runReviewSafetyInterlock(client *apiClient) error {
	if _, err := fixtureGate(client, "gate-south", "南侧泄洪闸", 5, 0.25); err != nil {
		return err
	}
	first, err := fixtureCommand(client, "gate-south", 1.5, "下游流量调整", "dispatcher-yang")
	if err != nil {
		return err
	}
	firstID, err := stringField(first, "id")
	if err != nil {
		return err
	}

	// First independent clear: review is still open, so the command must not
	// be executable yet and the gate must remain idle and unreserved.
	firstReview, err := fixtureClearReview(client, firstID, "safety-wu")
	if err != nil {
		return err
	}
	status, err := stringField(firstReview, "status")
	if err != nil {
		return err
	}
	if status != "pending" {
		return fmt.Errorf("first clear review status: expected pending, got %q", status)
	}
	if token, _ := stringField(firstReview, "execution_token"); token != "" {
		return fmt.Errorf("first clear review issued an execution token before review completed")
	}

	// Retrying the same verifier's clear over an unstable connection must be
	// idempotent: no second approval, no token, same recorded version.
	retried, err := fixtureClearReview(client, firstID, "safety-wu")
	if err != nil {
		return err
	}
	retryStatus, err := stringField(retried, "status")
	if err != nil {
		return err
	}
	if retryStatus != "pending" {
		return fmt.Errorf("retried clear review status: expected pending, got %q", retryStatus)
	}
	if v, ok := retried["version"].(float64); !ok || v != firstReview["version"].(float64) {
		return fmt.Errorf("retried clear review changed the recorded fact: versions %v -> %v", firstReview["version"], retried["version"])
	}
	if token, _ := stringField(retried, "execution_token"); token != "" {
		return fmt.Errorf("retried clear review issued a new execution token")
	}

	gateAfterOne, err := getGate(client, "gate-south")
	if err != nil {
		return err
	}
	gateStatus, err := stringField(gateAfterOne, "status")
	if err != nil {
		return err
	}
	activeCommandID := optionalStringField(gateAfterOne, "active_command_id")
	if gateStatus != "idle" || activeCommandID != "" {
		return fmt.Errorf(
			"gate must stay idle while review is open: status=%q active_command_id=%q",
			gateStatus,
			activeCommandID,
		)
	}

	// Second independent clear completes review: approval, token and gate
	// reservation appear together as one fact.
	approved, err := fixtureClearReview(client, firstID, "safety-zheng")
	if err != nil {
		return err
	}
	status, err = stringField(approved, "status")
	if err != nil {
		return err
	}
	if status != "approved" {
		return fmt.Errorf("second clear review status: expected approved, got %q", status)
	}
	token, err := stringField(approved, "execution_token")
	if err != nil {
		return err
	}
	if token == "" {
		return fmt.Errorf("completed clear review did not issue an execution token")
	}

	gateReserved, err := getGate(client, "gate-south")
	if err != nil {
		return err
	}
	reservedStatus, err := stringField(gateReserved, "status")
	if err != nil {
		return err
	}
	reservedCommand, err := stringField(gateReserved, "active_command_id")
	if err != nil {
		return err
	}
	if reservedStatus != "reserved" || reservedCommand != firstID {
		return fmt.Errorf(
			"gate reservation mismatch after approval: status=%q active_command_id=%q",
			reservedStatus,
			reservedCommand,
		)
	}

	// Replaying a clear after approval returns the same token and never
	// rotates it, so no stale credential can keep working.
	replayedApproved, err := fixtureClearReview(client, firstID, "safety-wu")
	if err != nil {
		return err
	}
	replayedToken, err := stringField(replayedApproved, "execution_token")
	if err != nil {
		return err
	}
	if replayedToken != token {
		return fmt.Errorf("replayed clear review rotated the execution token")
	}

	// A second command cannot steal the gate once the approved one owns it:
	// it needs its own completed review to even reach the reservation race.
	second, err := fixtureCommand(client, "gate-south", 2.5, "第二条冲突命令", "dispatcher-yang")
	if err != nil {
		return err
	}
	secondID, err := stringField(second, "id")
	if err != nil {
		return err
	}
	if _, err := fixtureClearReview(client, secondID, "safety-wu"); err != nil {
		return err
	}
	conflict, err := client.post("/commands/"+secondID+"/interlock", map[string]any{
		"verdict":  "clear",
		"verifier": "safety-zheng",
		"note":     "并发占用验证",
	})
	if err != nil {
		return err
	}
	if err := requireStatus(conflict, http.StatusConflict, "reject second gate reservation"); err != nil {
		return err
	}

	// The losing review must leave the command pending and tokenless rather
	// than half-approving it, and the gate must still belong to the winner.
	losing, err := getCommand(client, secondID)
	if err != nil {
		return err
	}
	losingStatus, err := stringField(losing, "status")
	if err != nil {
		return err
	}
	if losingStatus != "pending" {
		return fmt.Errorf("losing review status: expected pending, got %q", losingStatus)
	}
	if staleToken, _ := stringField(losing, "execution_token"); staleToken != "" {
		return fmt.Errorf("losing review leaked an execution token")
	}
	gateResult, err := getGate(client, "gate-south")
	if err != nil {
		return err
	}
	finalStatus, err := stringField(gateResult, "status")
	if err != nil {
		return err
	}
	finalCommand, err := stringField(gateResult, "active_command_id")
	if err != nil {
		return err
	}
	if finalStatus != "reserved" || finalCommand != firstID {
		return fmt.Errorf(
			"gate reservation mismatch: status=%q active_command_id=%q",
			finalStatus,
			finalCommand,
		)
	}

	// A blocked verdict while review is still open rejects the adjustment;
	// the gate stays idle and no token is ever issued.
	if _, err := fixtureGate(client, "gate-north-review", "北侧复核闸", 5, 0.25); err != nil {
		return err
	}
	disputed, err := fixtureCommand(client, "gate-north-review", 3.0, "被否定的调整", "dispatcher-yang")
	if err != nil {
		return err
	}
	disputedID, err := stringField(disputed, "id")
	if err != nil {
		return err
	}
	if _, err := fixtureClearReview(client, disputedID, "safety-wu"); err != nil {
		return err
	}
	gateBlocked, err := client.post("/commands/"+disputedID+"/interlock", map[string]any{
		"verdict":  "blocked",
		"verifier": "safety-he",
		"note":     "下游条件不满足",
	})
	if err != nil {
		return err
	}
	if err := requireStatus(gateBlocked, http.StatusOK, "record blocked verdict"); err != nil {
		return err
	}
	rejected, err := getCommand(client, disputedID)
	if err != nil {
		return err
	}
	rejectedStatus, err := stringField(rejected, "status")
	if err != nil {
		return err
	}
	if rejectedStatus != "rejected" {
		return fmt.Errorf("blocked review status: expected rejected, got %q", rejectedStatus)
	}
	if staleToken, _ := stringField(rejected, "execution_token"); staleToken != "" {
		return fmt.Errorf("rejected command still carries an execution token")
	}
	disputedGate, err := getGate(client, "gate-north-review")
	if err != nil {
		return err
	}
	disputedGateStatus, err := stringField(disputedGate, "status")
	if err != nil {
		return err
	}
	disputedGateCommand := optionalStringField(disputedGate, "active_command_id")
	if disputedGateStatus != "idle" || disputedGateCommand != "" {
		return fmt.Errorf(
			"blocked review must not occupy the gate: status=%q active_command_id=%q",
			disputedGateStatus,
			disputedGateCommand,
		)
	}

	// A rejected command can never be re-reviewed back into an executable
	// state.
	again, err := client.post("/commands/"+disputedID+"/interlock", map[string]any{
		"verdict":  "clear",
		"verifier": "safety-zheng",
		"note":     "试图复活已否定调整",
	})
	if err != nil {
		return err
	}
	if err := requireStatus(again, http.StatusConflict, "reject review of rejected command"); err != nil {
		return err
	}

	fmt.Println("review-safety-interlock: multi-reviewer approval, idempotent retries and rejection are consistent")
	return nil
}

func getGate(client *apiClient, id string) (map[string]any, error) {
	result, err := client.get("/gates/" + id)
	if err != nil {
		return nil, err
	}
	if err := requireStatus(result, http.StatusOK, "read gate"); err != nil {
		return nil, err
	}
	return objectField(result.Body, "gate")
}

func getCommand(client *apiClient, id string) (map[string]any, error) {
	result, err := client.get("/commands/" + id)
	if err != nil {
		return nil, err
	}
	if err := requireStatus(result, http.StatusOK, "read command"); err != nil {
		return nil, err
	}
	return objectField(result.Body, "command")
}

func optionalStringField(object map[string]any, name string) string {
	value, _ := object[name].(string)
	return value
}
