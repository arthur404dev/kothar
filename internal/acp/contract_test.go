package acp

import (
	"bufio"
	"encoding/json"
	"os"
	"regexp"
	"strings"
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
		if len(scanner.Bytes()) == 0 {
			t.Fatalf("%s record %d is empty", name, len(messages)+1)
		}
		var message map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &message); err != nil {
			t.Fatalf("%s record %d is not JSON: %v", name, len(messages)+1, err)
		}
		messages = append(messages, message)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return messages
}

func indexOf(messages []map[string]any, typ string) int {
	for i, message := range messages {
		if message["type"] == typ {
			return i
		}
	}
	return -1
}

func TestBuzzACPReferenceLifecycle(t *testing.T) {
	messages := readFixture(t, "buzz-acp-lifecycle.ndjson")
	if messages[0]["method"] != "initialize" || messages[0]["id"] != messages[1]["id"] {
		t.Fatal("initialize request and response must correlate")
	}
	params := messages[0]["params"].(map[string]any)
	if params["protocolVersion"] != float64(2) {
		t.Fatalf("protocol version = %v", params["protocolVersion"])
	}
	capabilities := messages[1]["result"].(map[string]any)["agentCapabilities"].(map[string]any)
	if capabilities["loadSession"] != false {
		t.Fatal("unimplemented loadSession capability must not be advertised")
	}
	if messages[4]["method"] != "session/prompt" || messages[4]["id"] != messages[7]["id"] {
		t.Fatal("prompt request and terminal response must correlate")
	}
	if messages[5]["method"] != "session/update" || messages[6]["method"] != "session/update" {
		t.Fatal("stream updates must precede the prompt response")
	}
	if messages[7]["result"].(map[string]any)["stopReason"] != "end_turn" {
		t.Fatal("completed prompt must terminate as end_turn")
	}
	if _, hasID := messages[9]["id"]; hasID || messages[9]["method"] != "session/cancel" {
		t.Fatal("session/cancel must be a notification")
	}
}

func TestPiRPCPromptEvidence(t *testing.T) {
	messages := readFixture(t, "pi-rpc-lifecycle.ndjson")
	if messages[0]["command"] != "get_state" || messages[1]["command"] != "prompt" || messages[1]["success"] != true {
		t.Fatal("state and accepted prompt responses must be ordered and correlated")
	}
	var deltas []string
	for _, message := range messages {
		if event, ok := message["assistantMessageEvent"].(map[string]any); ok && event["type"] == "text_delta" {
			deltas = append(deltas, event["delta"].(string))
		}
	}
	if strings.Join(deltas, "|") != "K|OTH|AR|_OK" {
		t.Fatalf("real delta sequence = %q", deltas)
	}
	if end, settled := indexOf(messages, "agent_end"), indexOf(messages, "agent_settled"); end < 0 || settled != end+1 {
		t.Fatal("prompt evidence must end with ordered agent_end, agent_settled")
	}
}

func TestPiRPCActiveCancellationEvidence(t *testing.T) {
	messages := readFixture(t, "pi-rpc-active-cancel.ndjson")
	start, end, settled := indexOf(messages, "agent_start"), indexOf(messages, "agent_end"), indexOf(messages, "agent_settled")
	abort := -1
	for i, message := range messages {
		if message["id"] == "abort" && message["command"] == "abort" && message["success"] == true {
			abort = i
		}
	}
	if start < 0 || end <= start || settled <= end || abort <= settled {
		t.Fatal("active prompt must reach aborted agent_end/agent_settled before abort acknowledgement")
	}
	if reasons := messages[end]["stopReasons"].([]any); len(reasons) != 1 || reasons[0] != "aborted" {
		t.Fatalf("actual Pi cancellation boundary = %v", reasons)
	}
}

func TestPiRPCProbeMetadataAndRedaction(t *testing.T) {
	data, err := os.ReadFile("testdata/pi-rpc-probe.json")
	if err != nil {
		t.Fatal(err)
	}
	var evidence struct {
		PiVersion, PiCliSha256 string
		Command                []string
		Environment            map[string]string
		ActiveCancelInput      []map[string]any
		Prompt, ActiveCancel   struct {
			ExitStatus     int
			Stdout, Stderr string
			AbortElapsedMs *int
		}
	}
	if err := json.Unmarshal(data, &evidence); err != nil {
		t.Fatal(err)
	}
	if evidence.PiVersion != "0.82.1" || len(evidence.PiCliSha256) != 64 || strings.Join(evidence.Command, " ") == "" {
		t.Fatal("probe must pin Pi version, CLI hash, and exact sanitized command")
	}
	if evidence.Environment["HOME"] != "<ISOLATED_DIR>/home" || evidence.Environment["PI_CODING_AGENT_DIR"] != "<ISOLATED_DIR>/pi" {
		t.Fatal("probe must use isolated placeholder-recorded paths")
	}
	if evidence.Prompt.ExitStatus != 0 || evidence.Prompt.Stdout != "ndjson-only" || evidence.Prompt.Stderr != "empty" || evidence.ActiveCancel.ExitStatus != 0 || evidence.ActiveCancel.Stdout != "ndjson-only" || evidence.ActiveCancel.Stderr != "empty" {
		t.Fatal("probe exit and stdout/stderr classifications must be explicit")
	}
	if len(evidence.ActiveCancelInput) != 2 || evidence.ActiveCancelInput[0]["type"] != "prompt" || evidence.ActiveCancelInput[1]["type"] != "abort" || evidence.ActiveCancelInput[1]["after"] != "agent_start" {
		t.Fatal("sanitized input evidence must order active prompt then abort after agent_start")
	}
	if evidence.ActiveCancel.AbortElapsedMs == nil || *evidence.ActiveCancel.AbortElapsedMs < 0 || *evidence.ActiveCancel.AbortElapsedMs > 1000 {
		t.Fatalf("active abort elapsed milliseconds = %v", evidence.ActiveCancel.AbortElapsedMs)
	}

	secret := regexp.MustCompile(`(?i)(/home/|/users/|sk-[a-z0-9]|bearer[ :]|api[_-]?key|access[_-]?token|refresh[_-]?token|resp_[a-z0-9]|msg_[a-z0-9]|[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})`)
	for _, name := range []string{"pi-rpc-probe.json", "pi-rpc-lifecycle.ndjson", "pi-rpc-active-cancel.ndjson"} {
		fixture, err := os.ReadFile("testdata/" + name)
		if err != nil {
			t.Fatal(err)
		}
		if match := secret.Find(fixture); match != nil {
			t.Fatalf("%s contains secret-shaped or host-path evidence %q", name, match)
		}
	}
}
