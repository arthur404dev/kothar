package pi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/arthur404dev/kothar/internal/engine"
)

func TestPrepareAuthSelectivePreserveAndRefresh(t *testing.T) {
	root := t.TempDir()
	host := filepath.Join(root, "host.json")
	if err := os.WriteFile(host, []byte(`{"openai":{"type":"api_key","key":"selected"},"anthropic":{"type":"api_key","key":"unrelated"},"session":{"value":"private"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	a := engine.Agent{ID: "a", Models: engine.ModelPolicy{Primary: "openai/model"}, Credentials: engine.CredentialPolicy{Mode: "inherit", HostAuth: host}}
	dst := filepath.Join(root, "state", "pi", "auth.json")
	if err := Prepare(filepath.Join(root, "state"), a, "/fixed/buzz"); err != nil {
		t.Fatal(err)
	}
	assertAuthKeys(t, dst, "openai")
	if err := os.WriteFile(dst, []byte(`{"openai":{"type":"api_key","key":"refreshed"},"custom":{"value":"keep"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Prepare(filepath.Join(root, "state"), a, "/fixed/buzz"); err != nil {
		t.Fatal(err)
	}
	assertAuthKeys(t, dst, "openai", "custom")
	a.Credentials.Refresh = true
	if err := Prepare(filepath.Join(root, "state"), a, "/fixed/buzz"); err != nil {
		t.Fatal(err)
	}
	assertAuthKeys(t, dst, "openai")
	if fi, err := os.Stat(dst); err != nil || fi.Mode().Perm() != 0600 {
		t.Fatalf("mode: %v %v", fi, err)
	}
}

func assertAuthKeys(t *testing.T, path string, want ...string) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]json.RawMessage
	if json.Unmarshal(b, &got) != nil {
		t.Fatal("invalid auth")
	}
	if len(got) != len(want) {
		t.Fatalf("keys=%v", got)
	}
	for _, k := range want {
		if _, ok := got[k]; !ok {
			t.Fatalf("missing %s", k)
		}
	}
}

func TestCredentialModesFailClosed(t *testing.T) {
	for _, a := range []engine.Agent{
		{Models: engine.ModelPolicy{Primary: "openai/x"}, Credentials: engine.CredentialPolicy{Mode: "none"}},
		{Models: engine.ModelPolicy{Primary: "openai/x"}, Credentials: engine.CredentialPolicy{Mode: "custom", Overrides: map[string]string{}}},
	} {
		if err := Prepare(t.TempDir(), a, "/buzz"); err == nil {
			t.Fatal("unsafe mode accepted")
		}
	}
}
