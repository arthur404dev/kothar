package internal_test

import (
	"encoding/json"
	"io"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

type listedPackage struct {
	ImportPath string
	Imports    []string
}

func TestPackageBoundaries(t *testing.T) {
	cmd := exec.Command("go", "list", "-json", "./internal/...")
	_, file, _, _ := runtime.Caller(0)
	cmd.Dir = filepath.Dir(filepath.Dir(file))
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(out)
	for {
		var pkg listedPackage
		if err := dec.Decode(&pkg); err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		for _, imported := range pkg.Imports {
			framework := strings.HasSuffix(pkg.ImportPath, "/internal/framework")
			disallowed := framework && (strings.Contains(imported, "/internal/acp") || strings.Contains(imported, "/internal/engine/pi") || strings.Contains(imported, "/internal/inbound/buzz") || strings.Contains(imported, "cobra") || strings.Contains(imported, "/deploy/systemd"))
			disallowed = disallowed || strings.HasSuffix(pkg.ImportPath, "/internal/acp") && (strings.Contains(imported, "/internal/engine/pi") || strings.Contains(imported, "/internal/inbound/buzz"))
			disallowed = disallowed || strings.HasSuffix(pkg.ImportPath, "/internal/engine") && (strings.Contains(imported, "/internal/acp") || strings.Contains(imported, "/internal/inbound/buzz"))
			disallowed = disallowed || strings.HasSuffix(pkg.ImportPath, "/internal/engine/pi") && (strings.Contains(imported, "/internal/acp") || strings.Contains(imported, "/internal/inbound/buzz"))
			if disallowed {
				t.Fatalf("forbidden import: %s -> %s", pkg.ImportPath, imported)
			}
		}
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
}
