package manifest

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func rootPath(parts ...string) string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(append([]string{filepath.Dir(file), "..", ".."}, parts...)...)
}

func example(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(rootPath("examples", "agents", "atlas", "agent.json"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func schema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	s, err := jsonschema.NewCompiler().Compile("file://" + rootPath("schema", "agent-v1.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func schemaAccepts(t *testing.T, s *jsonschema.Schema, data []byte) bool {
	t.Helper()
	var value any
	if json.Unmarshal(data, &value) != nil {
		return false
	}
	return s.Validate(value) == nil
}

func TestExampleDecoderAndSchema(t *testing.T) {
	data := example(t)
	m, err := Decode(bytes.NewReader(data))
	if err != nil || m.ID != "atlas" || !schemaAccepts(t, schema(t), data) {
		t.Fatalf("approved example rejected: %v %#v", err, m)
	}
}

func TestStrictDecoderFixtures(t *testing.T) {
	good := string(example(t))
	fixtures := map[string]string{
		"unknown":     strings.Replace(good, `"version": 1`, `"unknown": 0, "version": 1`, 1),
		"duplicate":   strings.Replace(good, `"version": 1`, `"version": 1, "version": 1`, 1),
		"trailing":    good + "{}",
		"secret":      strings.Replace(good, `"version": 1`, `"token": "literal", "version": 1`, 1),
		"unsafe-path": strings.Replace(good, `"SYSTEM.md"`, `"../SYSTEM.md"`, 1),
		"control":     strings.Replace(good, `"Atlas"`, `"Atlas\u0001"`, 1),
	}
	for name, data := range fixtures {
		t.Run(name, func(t *testing.T) {
			if _, err := Decode(strings.NewReader(data)); err == nil {
				t.Fatal("invalid fixture accepted")
			}
		})
	}
}

func TestRequiredMembersIncludingZeroValues(t *testing.T) {
	var value map[string]any
	if err := json.Unmarshal(example(t), &value); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		object map[string]any
		member string
	}{
		{value, "version"},
		{value["engine"].(map[string]any)["options"].(map[string]any), "telemetry"},
		{value["runtime"].(map[string]any), "start_on_boot"},
		{value["inbound"].(map[string]any)["options"].(map[string]any), "heartbeat_seconds"},
	}
	for _, tc := range cases {
		old := tc.object[tc.member]
		delete(tc.object, tc.member)
		data, _ := json.Marshal(value)
		if _, err := Decode(bytes.NewReader(data)); err == nil {
			t.Fatalf("missing %s accepted", tc.member)
		}
		tc.object[tc.member] = old
	}
}

func TestSchemaAndStrictPipelineAgreement(t *testing.T) {
	good := string(example(t))
	fixtures := map[string]string{
		"missing-profile-member": strings.Replace(good, `"description": "Research and engineering assistant",`, "", 1),
		"duplicate-label":        strings.Replace(good, `"buzz", "research"`, `"buzz", "buzz"`, 1),
		"too-many-fallbacks":     strings.Replace(good, `["openai/gpt-5.4"]`, `["openai/a","openai/b","openai/c","openai/d","openai/e","openai/f","openai/g","openai/h","openai/i"]`, 1),
		"unsupported-provider":   strings.Replace(good, `anthropic/claude-sonnet-4-6`, `other/model`, 1),
		"unsupported-bundle":     strings.Replace(good, `"buzz", "workspace", "git"`, `"magic"`, 1),
		"invalid-pubkey":         strings.Replace(good, `"pubkeys": []`, `"pubkeys": ["bad"]`, 1),
		"unsafe-mount":           strings.Replace(good, `"mounts": []`, `"mounts": [{"source":"relative","target":"data","mode":"read_only"}]`, 1),
	}
	s := schema(t)
	for name, data := range fixtures {
		t.Run(name, func(t *testing.T) {
			_, decodeErr := Decode(strings.NewReader(data))
			if decodeErr == nil || schemaAccepts(t, s, []byte(data)) {
				t.Fatalf("agreement failure: decoder=%v schemaAccepted=%v", decodeErr, schemaAccepts(t, s, []byte(data)))
			}
		})
	}
}

func TestSemanticValidationBoundary(t *testing.T) {
	good := string(example(t))
	mounts := `"mounts": [{"source":"/srv/a","target":"data","mode":"read_only"},{"source":"/srv/b","target":"data/cache","mode":"read_only"}]`
	fixtures := map[string]string{
		"custom-provider-coverage": strings.Replace(good, `"mode": "inherit"`, `"mode": "custom"`, 1),
		"none-local-models":        strings.Replace(good, `"mode": "inherit"`, `"mode": "none"`, 1),
		"buzz-mode-pubkeys":        strings.Replace(good, `"mode": "nobody"`, `"mode": "allowlist"`, 1),
		"mount-target-overlap":     strings.Replace(good, `"mounts": []`, mounts, 1),
	}
	s := schema(t)
	for name, data := range fixtures {
		t.Run(name, func(t *testing.T) {
			if !schemaAccepts(t, s, []byte(data)) {
				t.Fatal("structurally valid fixture rejected by schema")
			}
			if _, err := Decode(strings.NewReader(data)); err == nil {
				t.Fatal("cross-field semantic violation accepted by Go validator")
			}
		})
	}
}

func TestCompiledEngineExecutablePinsCannotBeOverridden(t *testing.T) {
	want := map[string]Executable{
		"pi": {Identity: "pi", Command: "/usr/local/libexec/kothar/pi", Version: "0.82.1"},
	}
	if !reflect.DeepEqual(engines["pi"].Executables, want) {
		t.Fatalf("compiled executable metadata changed: %#v", engines["pi"].Executables)
	}
	override := strings.Replace(string(example(t)), `"name": "pi"`, `"name": "pi", "command": "/tmp/pi", "version": "latest"`, 1)
	if _, err := Decode(strings.NewReader(override)); err == nil {
		t.Fatal("manifest executable override accepted")
	}
}

func TestEngineCredentialModes(t *testing.T) {
	good := string(example(t))
	custom := strings.Replace(good, `"mode": "inherit"`, `"mode": "custom"`, 1)
	custom = strings.Replace(custom, `"overrides": {}`, `"overrides": {"anthropic":"anthropic-atlas","openai":"openai-atlas"}`, 1)
	if _, err := Decode(strings.NewReader(custom)); err != nil {
		t.Fatalf("complete custom credentials rejected: %v", err)
	}
	for name, data := range map[string]string{
		"custom-missing":     strings.Replace(good, `"mode": "inherit"`, `"mode": "custom"`, 1),
		"none-authenticated": strings.Replace(good, `"mode": "inherit"`, `"mode": "none"`, 1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Decode(strings.NewReader(data)); err == nil {
				t.Fatal("invalid credential policy accepted")
			}
		})
	}
}
