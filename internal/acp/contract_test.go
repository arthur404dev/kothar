package acp

import (
	"bufio"
	"encoding/json"
	"os"
	"testing"
)

func readFixture(t *testing.T, name string) []map[string]any {
	t.Helper()
	file, err := os.Open("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	var messages []map[string]any
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var message map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &message); err != nil {
			t.Fatalf("stdout record %d is not JSON: %v", len(messages)+1, err)
		}
		messages = append(messages, message)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return messages
}

func TestBuzzACPReferenceLifecycle(t *testing.T) {
	messages := readFixture(t, "buzz-acp-lifecycle.ndjson")
	if got := messages[0]["method"]; got != "initialize" {
		t.Fatalf("first method = %v", got)
	}
	params := messages[0]["params"].(map[string]any)
	if got := params["protocolVersion"]; got != float64(2) {
		t.Fatalf("protocol version = %v", got)
	}
	if got := messages[4]["method"]; got != "session/prompt" {
		t.Fatalf("prompt method = %v", got)
	}
	result := messages[7]["result"].(map[string]any)
	if got := result["stopReason"]; got != "end_turn" {
		t.Fatalf("stop reason = %v", got)
	}
	if _, hasID := messages[9]["id"]; hasID || messages[9]["method"] != "session/cancel" {
		t.Fatal("session/cancel must be a notification")
	}
	cancelled := messages[10]["result"].(map[string]any)
	if cancelled["stopReason"] != "cancelled" {
		t.Fatal("cancelled prompt must terminate as cancelled")
	}
}

func TestPiRPCReferenceLifecycle(t *testing.T) {
	messages := readFixture(t, "pi-rpc-lifecycle.ndjson")
	if messages[0]["type"] != "get_state" || messages[2]["type"] != "prompt" {
		t.Fatal("Pi state must precede prompt")
	}
	if messages[3]["success"] != true {
		t.Fatal("Pi did not accept prompt")
	}
	if messages[7]["type"] != "agent_settled" {
		t.Fatal("Pi completion boundary must be agent_settled")
	}
	if messages[9]["success"] != true {
		t.Fatal("Pi did not acknowledge abort")
	}
}
