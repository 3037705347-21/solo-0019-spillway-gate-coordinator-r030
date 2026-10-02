package main

import (
	"fmt"
	"net/http"
)

func runExecuteGateCommand(client *apiClient) error {
	if _, err := fixtureGate(client, "gate-west", "西侧泄洪闸", 4, 0.25); err != nil {
		return err
	}
	command, err := fixtureCommand(client, "gate-west", 2.0, "平衡下游水位", "dispatcher-zhao")
	if err != nil {
		return err
	}
	commandID, err := stringField(command, "id")
	if err != nil {
		return err
	}

	// Only one independent clear: the command must still refuse execution.
	firstReview, err := fixtureClearReview(client, commandID, "safety-he")
	if err != nil {
		return err
	}
	pendingStatus, err := stringField(firstReview, "status")
	if err != nil {
		return err
	}
	if pendingStatus != "pending" {
		return fmt.Errorf("single clear review status: expected pending, got %q", pendingStatus)
	}
	premature, err := client.post("/commands/"+commandID+"/execute", map[string]any{
		"operator":       "operator-chen",
		"token":          "any-token",
		"simulate_fault": false,
	})
	if err != nil {
		return err
	}
	if err := requireStatus(premature, http.StatusConflict, "reject execution before review completes"); err != nil {
		return err
	}

	// Retrying the first reviewer must not complete the review either.
	if _, err := fixtureClearReview(client, commandID, "safety-he"); err != nil {
		return err
	}

	approved, err := fixtureClearReview(client, commandID, "safety-luo")
	if err != nil {
		return err
	}
	token, err := stringField(approved, "execution_token")
	if err != nil {
		return err
	}
	if token == "" {
		return fmt.Errorf("completed review did not issue an execution token")
	}

	// A stale token minted by an earlier transient response must not work;
	// only the token bound to the approved fact is accepted.
	stale, err := client.post("/commands/"+commandID+"/execute", map[string]any{
		"operator":       "operator-chen",
		"token":          "stale-token-from-retried-review",
		"simulate_fault": false,
	})
	if err != nil {
		return err
	}
	if err := requireStatus(stale, http.StatusConflict, "reject stale execution token"); err != nil {
		return err
	}

	executed, err := client.post("/commands/"+commandID+"/execute", map[string]any{
		"operator":       "operator-chen",
		"token":          token,
		"simulate_fault": false,
	})
	if err != nil {
		return err
	}
	if err := requireStatus(executed, http.StatusOK, "execute approved command"); err != nil {
		return err
	}
	executedCommand, err := objectField(executed.Body, "command")
	if err != nil {
		return err
	}
	completedStatus, err := stringField(executedCommand, "status")
	if err != nil {
		return err
	}
	if completedStatus != "completed" {
		return fmt.Errorf("execution status: expected completed, got %q", completedStatus)
	}

	executedGate, err := objectField(executed.Body, "gate")
	if err != nil {
		return err
	}
	gateStatus, err := stringField(executedGate, "status")
	if err != nil {
		return err
	}
	aperture, err := floatField(executedGate, "aperture")
	if err != nil {
		return err
	}
	if gateStatus != "idle" || aperture != 2.0 {
		return fmt.Errorf("settled gate mismatch: status=%q aperture=%v", gateStatus, aperture)
	}

	reused, err := client.post("/commands/"+commandID+"/execute", map[string]any{
		"operator":       "operator-chen",
		"token":          token,
		"simulate_fault": false,
	})
	if err != nil {
		return err
	}
	if err := requireStatus(reused, http.StatusConflict, "reject reused execution token"); err != nil {
		return err
	}

	// The simulated-fault path must keep its original failure semantics.
	if _, err := fixtureGate(client, "gate-east-fault", "东侧故障闸", 4, 0.25); err != nil {
		return err
	}
	faultCommand, err := fixtureCommand(client, "gate-east-fault", 3.0, "故障演练", "dispatcher-zhao")
	if err != nil {
		return err
	}
	faultID, err := stringField(faultCommand, "id")
	if err != nil {
		return err
	}
	if _, err := fixtureClearReview(client, faultID, "safety-he"); err != nil {
		return err
	}
	faultApproved, err := fixtureClearReview(client, faultID, "safety-luo")
	if err != nil {
		return err
	}
	faultToken, err := stringField(faultApproved, "execution_token")
	if err != nil {
		return err
	}
	faultResult, err := client.post("/commands/"+faultID+"/execute", map[string]any{
		"operator":       "operator-chen",
		"token":          faultToken,
		"simulate_fault": true,
	})
	if err != nil {
		return err
	}
	if err := requireStatus(faultResult, http.StatusOK, "execute fault simulation"); err != nil {
		return err
	}
	faultGate, err := objectField(faultResult.Body, "gate")
	if err != nil {
		return err
	}
	faultGateStatus, err := stringField(faultGate, "status")
	if err != nil {
		return err
	}
	if faultGateStatus != "faulted" {
		return fmt.Errorf("fault execution: expected faulted gate, got %q", faultGateStatus)
	}

	fmt.Println("execute-gate-command: completed command rejects stale/reused tokens; fault semantics preserved")
	return nil
}
