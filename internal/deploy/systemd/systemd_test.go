package systemd

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/arthur404dev/kothar/internal/records"
)

func TestFixtureApplyIdempotentAndRollback(t *testing.T) {
	root := t.TempDir()
	receipt := filepath.Join(t.TempDir(), "receipt.json")
	arts := []Artifact{{Path: "etc/kothar/a", Mode: 0600, Category: "config", Content: []byte("one")}, {Path: "var/lib/kothar/a", Mode: 0600, Category: "state", Content: []byte("two")}}
	p := Build("a", []byte("effective"), nil, false, arts)
	if err := ApplyFixture(root, receipt, p, -1); err != nil {
		t.Fatal(err)
	}
	r, err := LoadReceipt(receipt, "a")
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

func TestReceiptIdentityAndOwnershipRollback(t *testing.T) {
	root := t.TempDir()
	receipt := filepath.Join(t.TempDir(), "receipt.json")
	initial := Receipt{AgentID: "a", Artifacts: []Artifact{}}
	if err := os.WriteFile(receipt, records.Marshal(initial), 0640); err != nil {
		t.Fatal(err)
	}
	wantUID, wantGID := os.Geteuid(), os.Getegid()
	if os.Geteuid() == 0 {
		wantUID, wantGID = 1, 1
		if err := os.Chown(receipt, wantUID, wantGID); err != nil {
			t.Fatalf("root chown fixture: %v", err)
		}
	} else if groups, err := os.Getgroups(); err == nil {
		for _, gid := range groups {
			if gid != wantGID && os.Chown(receipt, wantUID, gid) == nil {
				wantGID = gid
				break
			}
		}
	}
	plan := Build("a", []byte("changed"), &initial, false, []Artifact{{Path: "changed", Mode: 0600, Category: "config", Content: []byte("changed")}, {Path: "fail", Mode: 0600, Category: "config", Content: []byte("fail")}})
	if err := ApplyFixture(root, receipt, plan, 1); err == nil {
		t.Fatal("expected injected failure")
	}
	fi, err := os.Stat(receipt)
	if err != nil {
		t.Fatal(err)
	}
	st := fi.Sys().(*syscall.Stat_t)
	if int(st.Uid) != wantUID || int(st.Gid) != wantGID || fi.Mode().Perm() != 0640 {
		t.Fatalf("receipt metadata = %d:%d %o, want %d:%d 640", st.Uid, st.Gid, fi.Mode().Perm(), wantUID, wantGID)
	}
	if _, err = LoadReceipt(receipt, "b"); err == nil {
		t.Fatal("accepted receipt for another agent")
	}
}
func TestUnitEscape(t *testing.T) {
	if got := Unit("my-agent"); got != "kothar-agent@my\\x2dagent.service" {
		t.Fatal(got)
	}
}
