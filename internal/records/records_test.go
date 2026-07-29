package records

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCreateAtomicModesAndSymlink(t *testing.T) {
	root := t.TempDir()
	s := Store{Config: filepath.Join(root, "config"), State: filepath.Join(root, "state")}
	data := []byte(`{"version":1,"id":"agent","profile":{"display_name":"Agent","description":"","labels":[]},"inbound":{"name":"buzz","options":{"relay":"wss://buzz.4o4.one","identity_credential":"buzz-agent","respond_to":{"mode":"nobody","pubkeys":[]},"heartbeat_seconds":300}},"engine":{"name":"pi","credentials":{"mode":"inherit","overrides":{}},"options":{"project_trust":"never","telemetry":false,"update_checks":false}},"models":{"primary":"anthropic/model","fallbacks":[],"thinking":"low","max_attempts":1},"behavior":{"system_prompt":"SYSTEM.md","context_files":["AGENTS.md","CONSTRAINTS.md"],"skills":[],"extensions":[]},"tools":{"bundles":[],"allow":[],"deny":[],"credentials":{}},"workspace":{"root":"workspace","mounts":[]},"permissions":{"network":{"mode":"full"},"resources":{"memory_max_mb":64,"cpu_quota_percent":1,"tasks_max":1}},"runtime":{"driver":"systemd","start_on_boot":false,"restart":"no","workers":1}}`)
	if err := s.Create("agent", data); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"agent.json", "SYSTEM.md", "AGENTS.md", "CONSTRAINTS.md"} {
		fi, e := os.Stat(filepath.Join(s.Config, "agents", "agent", name))
		if e != nil || fi.Mode().Perm() != 0600 {
			t.Fatalf("%s mode=%v err=%v", name, fi.Mode().Perm(), e)
		}
	}
	if err := os.Remove(filepath.Join(s.Config, "agents", "agent", "agent.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/etc/passwd", filepath.Join(s.Config, "agents", "agent", "agent.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Read("agent"); err == nil {
		t.Fatal("followed symlink")
	}
}
