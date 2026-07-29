package config

import "testing"

func TestStrictConfig(t *testing.T) {
	good := []byte(`{"agent_defaults":{},"host_policy":{"allowed_mount_roots":[]}}`)
	if _, err := DecodeBytes(good); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		`{"agent_defaults":{},"host_policy":{"allowed_mount_roots":[]},"host_policy":{"allowed_mount_roots":[]}}`,
		`{"agent_defaults":{},"host_policy":{"allowed_mount_roots":[]},"token":"x"}`,
		`{"agent_defaults":{},"host_policy":{"allowed_mount_roots":[]}} {}`,
	} {
		if _, err := DecodeBytes([]byte(bad)); err == nil {
			t.Fatalf("accepted %s", bad)
		}
	}
}

func TestRestrictionsCannotWidenPolicy(t *testing.T) {
	got := RestrictRoots([]string{"/srv"}, []string{"/tmp"})
	if len(got) != 0 {
		t.Fatal(got)
	}
	got = RestrictRoots([]string{"/srv"}, []string{"/srv/agents"})
	if len(got) != 1 || got[0] != "/srv/agents" {
		t.Fatal(got)
	}
}
