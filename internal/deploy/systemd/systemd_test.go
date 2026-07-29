package systemd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFixtureApplyIdempotentAndRollback(t *testing.T) {
	root := t.TempDir()
	receipt := filepath.Join(t.TempDir(), "receipt.json")
	arts := []Artifact{{Path: "etc/kothar/a", Mode: 0600, Category: "config", Content: []byte("one")}, {Path: "var/lib/kothar/a", Mode: 0600, Category: "state", Content: []byte("two")}}
	p := Build("a", []byte("effective"), nil, false, arts)
	if err := ApplyFixture(root, receipt, p, -1); err != nil {
		t.Fatal(err)
	}
	r, err := LoadReceipt(receipt)
	if err != nil {
		t.Fatal(err)
	}
	p2 := Build("a", []byte("effective"), r, false, arts)
	if len(p2.Categories) != 0 {
		t.Fatalf("reapply changed: %v", p2.Categories)
	}
	changed := append([]Artifact(nil), arts...)
	changed[1].Content = []byte("new prompt")
	if p3 := Build("a", []byte("effective"), r, false, changed); len(p3.Categories) != 1 || p3.Categories[0] != "state" {
		t.Fatalf("resource change missed: %v", p3.Categories)
	}
	changed = append([]Artifact(nil), arts...)
	changed[0].Mode = 0644
	if p3 := Build("a", []byte("effective"), r, false, changed); len(p3.Categories) != 1 || p3.Categories[0] != "config" {
		t.Fatalf("mode change missed: %v", p3.Categories)
	}
	receiptBefore, _ := os.ReadFile(receipt)
	bad := Build("a", []byte("changed"), r, false, []Artifact{{Path: "etc/kothar/a", Mode: 0600, Category: "config", Content: []byte("changed")}, {Path: "new", Mode: 0600, Category: "state", Content: []byte("new")}})
	if err = ApplyFixture(root, receipt, bad, 1); err == nil {
		t.Fatal("expected fault")
	}
	b, _ := os.ReadFile(filepath.Join(root, "etc/kothar/a"))
	if string(b) != "one" {
		t.Fatalf("rollback failed: %q", b)
	}
	if _, err = os.Stat(filepath.Join(root, "new")); !os.IsNotExist(err) {
		t.Fatal("created file survived rollback")
	}
	receiptAfter, _ := os.ReadFile(receipt)
	if string(receiptAfter) != string(receiptBefore) {
		t.Fatal("receipt changed after rollback")
	}
	fi, _ := os.Stat(filepath.Join(root, "etc/kothar/a"))
	if fi.Mode().Perm() != 0600 {
		t.Fatalf("mode rollback: %o", fi.Mode().Perm())
	}
}
func TestUnitEscape(t *testing.T) {
	if got := Unit("my-agent"); got != "kothar-agent@my\\x2dagent.service" {
		t.Fatal(got)
	}
}
