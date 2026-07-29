package credentials

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestProtectedMetadataOnly(t *testing.T) {
	s := Store{Root: filepath.Join(t.TempDir(), "credentials")}
	secret := []byte("do-not-print")
	if err := s.Set("provider", bytes.NewReader(secret)); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(s.Root, "provider"))
	if err != nil || fi.Mode().Perm() != 0600 {
		t.Fatalf("mode=%v err=%v", fi.Mode().Perm(), err)
	}
	items, err := s.List()
	if err != nil || len(items) != 1 || items[0].Name != "provider" {
		t.Fatalf("%v %v", items, err)
	}
}
