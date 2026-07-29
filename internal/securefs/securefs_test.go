package securefs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRejectsSymlinkInEveryComponent(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	if err := os.Mkdir(real, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(real, "value"), []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(root, "alias")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadFile(root, filepath.Join("alias", "value"), 100); err == nil {
		t.Fatal("symlinked component accepted")
	}
	if err := os.Symlink(filepath.Join(real, "value"), filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ReadFile(root, "link", 100); err == nil {
		t.Fatal("symlinked file accepted")
	}
}
