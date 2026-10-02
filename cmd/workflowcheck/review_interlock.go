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

	incomplete, err := fixtureClearReview(client, firstID, "safety-wu")
	if err != nil {
		return err
	}
	status, err := stringField(incomplete, "status")
	if err != nil {
		return err
	}
	if status != "pending" {
		return fmt.Errorf("first clear review status: expected pending, got %q", status)
	}
	if _, exists := incomplete["execution_token"]; exists {
		return fmt.Errorf("incomplete review must not issue an execution token")
	}
	if err := requireGateState(client, "gate-south", "idle", ""); err != nil {
		return err
	}

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
	if err := requireGateState(client, "gate-south", "idle", ""); err != nil {
		return err
	}

	approved, err := fixtureClearReview(client, firstID, "safety-he")
	if err != nil {
		return err
	}
	status, err = stringField(approved, "status")
	if err != nil {
		return err
	}
	if status != "approved" {
		return fmt.Errorf("second independent clear review status: expected approved, got %q", status)
	}
	token, err := stringField(approved, "execution_token")
	if err != nil {
		return err
	}
	if token == "" {
		return fmt.Errorf("completed review did not issue an execution token")
	}
	if err := requireGateState(client, "gate-south", "reserved", firstID); err != nil {
		return err
	}

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
		"verifier": "safety-he",
		"note":     "并发占用验证",
	})
	if err != nil {
		return err
	}
	if err := requireStatus(conflict, http.StatusConflict, "reject second gate reservation"); err != nil {
		return err
	}
	if err := requireGateState(client, "gate-south", "reserved", firstID); err != nil {
		return err
	}

	blocked, err := client.post("/commands/"+firstID+"/interlock", map[string]any{
		"verdict":  "blocked",
		"verifier": "safety-li",
		"note":     "下游水位越限",
	})
	if err != nil {
		return err
	}
	if err := requireStatus(blocked, http.StatusOK, "record blocked disagreement"); err != nil {
		return err
	}
	blockedCommand, err := objectField(blocked.Body, "command")
	if err != nil {
		return err
	}
	blockedStatus, err := stringField(blockedCommand, "status")
	if err != nil {
		return err
	}
	if blockedStatus != "rejected" {
		return fmt.Errorf("blocked review status: expected rejected, got %q", blockedStatus)
	}
	if _, exists := blockedCommand["execution_token"]; exists {
		return fmt.Errorf("rejected command must not retain an execution token")
	}
	if err := requireGateState(client, "gate-south", "idle", ""); err != nil {
		return err
	}

	revoked, err := client.post("/commands/"+firstID+"/execute", map[string]any{
		"operator":       "operator-chen",
		"token":          token,
		"simulate_fault": false,
	})
	if err != nil {
		return err
	}
	if err := requireStatus(revoked, http.StatusConflict, "reject revoked execution token"); err != nil {
		return err
	}

	reopened, err := client.post("/commands/"+firstID+"/interlock", map[string]any{
		"verdict":  "clear",
		"verifier": "safety-li",
		"note":     "尝试恢复被否决的命令",
	})
	if err != nil {
		return err
	}
	if err := requireStatus(reopened, http.StatusConflict, "reject review of rejected command"); err != nil {
		return err
	}

	blockedSecond, err := client.post("/commands/"+secondID+"/interlock", map[string]any{
		"verdict":  "blocked",
		"verifier": "safety-li",
		"note":     "维持否决",
	})
	if err != nil {
		return err
	}
	if err := requireStatus(blockedSecond, http.StatusOK, "block pending command"); err != nil {
		return err
	}
	secondCommand, err := objectField(blockedSecond.Body, "command")
	if err != nil {
		return err
	}
	secondStatus, err := stringField(secondCommand, "status")
	if err != nil {
		return err
	}
	if secondStatus != "rejected" {
		return fmt.Errorf("blocked pending command status: expected rejected, got %q", secondStatus)
	}
	if err := requireGateState(client, "gate-south", "idle", ""); err != nil {
		return err
	}

	fmt.Println("review-safety-interlock: multi-review approval, retry, disagreement, and conflict stay consistent")
	return nil
}

func requireGateState(client *apiClient, gateID, status, activeCommandID string) error {
	result, err := client.get("/gates/" + gateID)
	if err != nil {
		return err
	}
	if err := requireStatus(result, http.StatusOK, "read gate"); err != nil {
		return err
	}
	gate, err := objectField(result.Body, "gate")
	if err != nil {
		return err
	}
	gateStatus, err := stringField(gate, "status")
	if err != nil {
		return err
	}
	if gateStatus != status {
		return fmt.Errorf("gate %s status: expected %q, got %q", gateID, status, gateStatus)
	}
	if activeCommandID == "" {
		if value, exists := gate["active_command_id"]; exists {
			return fmt.Errorf("gate %s should not reference an active command, got %v", gateID, value)
		}
		return nil
	}
	value, err := stringField(gate, "active_command_id")
	if err != nil {
		return err
	}
	if value != activeCommandID {
		return fmt.Errorf("gate %s active command: expected %q, got %q", gateID, activeCommandID, value)
	}
	return nil
}
