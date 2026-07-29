// Package systemd builds deterministic deployment plans and runs fixed systemd commands.
package systemd

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/arthur404dev/kothar/internal/manifest"
	"github.com/arthur404dev/kothar/internal/records"
	"github.com/arthur404dev/kothar/internal/securefs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var ErrRuntimeIncomplete = errors.New("runtime incomplete/not installed")

type Artifact struct {
	Path     string `json:"path"`
	Mode     uint32 `json:"mode"`
	UID      int    `json:"uid"`
	GID      int    `json:"gid"`
	Category string `json:"category"`
	Content  []byte `json:"-"`
	Hash     string `json:"hash"`
	Changed  bool   `json:"changed,omitempty"`
}
type Plan struct {
	AgentID            string     `json:"agent_id"`
	EffectiveHash      string     `json:"effective_hash"`
	Categories         []string   `json:"changed_categories"`
	RefreshCredentials bool       `json:"refresh_credentials"`
	Artifacts          []Artifact `json:"artifacts"`
	Removed            []Artifact `json:"removed,omitempty"`
}
type Receipt struct {
	AgentID            string     `json:"agent_id"`
	EffectiveHash      string     `json:"effective_hash"`
	Categories         []string   `json:"changed_categories"`
	RefreshCredentials bool       `json:"refresh_credentials"`
	AppliedAt          string     `json:"applied_at"`
	Artifacts          []Artifact `json:"artifacts"`
}
type Runner interface {
	Run(context.Context, string, ...string) ([]byte, error)
}
type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if name != "systemctl" && name != "journalctl" {
		return nil, fmt.Errorf("unsupported executable")
	}
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}
func Hash(data []byte) string { h := sha256.Sum256(data); return hex.EncodeToString(h[:]) }
func Build(id string, effective []byte, previous *Receipt, refresh bool, arts []Artifact) Plan {
	old := map[string]Artifact{}
	if previous != nil {
		for _, a := range previous.Artifacts {
			old[a.Path] = a
		}
	}
	set := map[string]bool{}
	for i := range arts {
		if arts[i].UID == 0 && arts[i].GID == 0 {
			arts[i].UID, arts[i].GID = os.Geteuid(), os.Getegid()
		}
		arts[i].Hash = Hash(arts[i].Content)
		was, ok := old[arts[i].Path]
		arts[i].Changed = !ok || was.Hash != arts[i].Hash || was.Mode != arts[i].Mode || was.UID != arts[i].UID || was.GID != arts[i].GID || was.Category != arts[i].Category
		if arts[i].Changed {
			set[arts[i].Category] = true
		}
		delete(old, arts[i].Path)
	}
	removed := make([]Artifact, 0, len(old))
	for _, artifact := range old {
		set[artifact.Category] = true
		removed = append(removed, artifact)
	}
	sort.Slice(removed, func(i, j int) bool { return removed[i].Path < removed[j].Path })
	sort.Slice(arts, func(i, j int) bool { return arts[i].Path < arts[j].Path })
	cats := make([]string, 0, len(set))
	for c := range set {
		cats = append(cats, c)
	}
	sort.Strings(cats)
	if refresh {
		cats = append(cats, "credentials")
	}
	return Plan{AgentID: id, EffectiveHash: Hash(effective), Categories: cats, RefreshCredentials: refresh, Artifacts: arts, Removed: removed}
}
func LoadReceipt(path string) (*Receipt, error) {
	b, _, e := securefs.ReadFile(filepath.Dir(path), filepath.Base(path), 1<<20)
	if e != nil {
		return nil, e
	}
	if e = manifest.ValidateJSON(b); e != nil {
		return nil, e
	}
	var r Receipt
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if e = d.Decode(&r); e != nil {
		return nil, e
	}
	var trailing any
	if e = d.Decode(&trailing); e == nil {
		return nil, fmt.Errorf("trailing JSON data")
	}
	if r.AgentID == "" || r.Artifacts == nil {
		return nil, fmt.Errorf("invalid receipt")
	}
	return &r, nil
}

// ApplyFixture is the reusable atomic installer. Callers must gate real application on runtime availability first.
func ApplyFixture(root, receiptPath string, p Plan, failAfter int) error {
	type backup struct {
		data     []byte
		mode     os.FileMode
		uid, gid int
	}
	backups := map[string]backup{}
	created := []string{}
	written := 0
	receipt, receiptMode, receiptExists := []byte(nil), os.FileMode(0600), false
	if b, fi, e := securefs.ReadFile(filepath.Dir(receiptPath), filepath.Base(receiptPath), 1<<20); e == nil {
		receipt, receiptMode, receiptExists = b, fi.Mode().Perm(), true
	}
	rollback := func() {
		for path, b := range backups {
			_ = records.AtomicWrite(path, b.data, b.mode)
			_ = os.Chown(path, b.uid, b.gid)
			_ = os.Chmod(path, b.mode)
		}
		for _, path := range created {
			_ = os.Remove(path)
		}
		if receiptExists {
			_ = records.AtomicWrite(receiptPath, receipt, receiptMode)
		} else {
			_ = os.Remove(receiptPath)
		}
	}
	for _, a := range p.Artifacts {
		if !a.Changed {
			continue
		}
		dst := filepath.Join(root, filepath.Clean("/"+a.Path))
		if !strings.HasPrefix(dst, filepath.Clean(root)+string(filepath.Separator)) {
			rollback()
			return fmt.Errorf("artifact escapes root")
		}
		if old, fi, e := securefs.ReadFile(filepath.Dir(dst), filepath.Base(dst), 1<<30); e == nil {
			st, ok := fi.Sys().(*syscall.Stat_t)
			if !ok {
				rollback()
				return fmt.Errorf("ownership unavailable")
			}
			backups[dst] = backup{old, fi.Mode().Perm(), int(st.Uid), int(st.Gid)}
		} else if errors.Is(e, os.ErrNotExist) {
			created = append(created, dst)
		} else {
			rollback()
			return e
		}
		if failAfter >= 0 && written == failAfter {
			rollback()
			return fmt.Errorf("injected apply failure")
		}
		if e := records.AtomicWrite(dst, a.Content, os.FileMode(a.Mode)); e != nil {
			rollback()
			return e
		}
		if e := os.Chown(dst, a.UID, a.GID); e != nil {
			rollback()
			return e
		}
		if e := os.Chmod(dst, os.FileMode(a.Mode)); e != nil {
			rollback()
			return e
		}
		written++
	}
	for _, a := range p.Removed {
		dst := filepath.Join(root, filepath.Clean("/"+a.Path))
		old, fi, e := securefs.ReadFile(filepath.Dir(dst), filepath.Base(dst), 1<<30)
		if errors.Is(e, os.ErrNotExist) {
			continue
		}
		if e != nil {
			rollback()
			return e
		}
		st, ok := fi.Sys().(*syscall.Stat_t)
		if !ok {
			rollback()
			return fmt.Errorf("ownership unavailable")
		}
		backups[dst] = backup{old, fi.Mode().Perm(), int(st.Uid), int(st.Gid)}
		if e = os.Remove(dst); e != nil {
			rollback()
			return e
		}
		written++
	}
	if written == 0 && !p.RefreshCredentials {
		return nil
	}
	stored := make([]Artifact, len(p.Artifacts))
	copy(stored, p.Artifacts)
	for i := range stored {
		stored[i].Changed = false
		stored[i].Content = nil
	}
	r := Receipt{AgentID: p.AgentID, EffectiveHash: p.EffectiveHash, Categories: p.Categories, RefreshCredentials: p.RefreshCredentials, AppliedAt: time.Now().UTC().Format(time.RFC3339), Artifacts: stored}
	if e := records.AtomicWrite(receiptPath, records.Marshal(r), 0600); e != nil {
		rollback()
		return e
	}
	return nil
}
func Unit(id string) string { return "kothar-agent@" + escape(id) + ".service" }
func escape(id string) string {
	var b strings.Builder
	for i, c := range []byte(id) {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '.' {
			b.WriteByte(c)
		} else if c == '-' && i > 0 {
			b.WriteString("\\x2d")
		} else {
			b.WriteString("\\x" + hex.EncodeToString([]byte{c}))
		}
	}
	return b.String()
}
func Service(ctx context.Context, r Runner, id, action string) ([]byte, error) {
	if action != "start" && action != "stop" && action != "restart" && action != "disable" && action != "status" {
		return nil, fmt.Errorf("unsupported service action")
	}
	args := []string{action}
	if action == "status" {
		args = []string{"show", Unit(id), "--no-page", "--property=ActiveState,SubState,LoadState"}
	} else {
		args = append(args, Unit(id))
	}
	return r.Run(ctx, "systemctl", args...)
}
func Logs(ctx context.Context, r Runner, id string, lines int, since string) ([]byte, error) {
	if lines < 1 || lines > 10000 {
		return nil, fmt.Errorf("lines out of bounds")
	}
	args := []string{"--unit", Unit(id), "--no-pager", "--lines", strconv.Itoa(lines), "--output", "short-iso"}
	if since != "" {
		if strings.ContainsAny(since, "\x00\r\n") {
			return nil, fmt.Errorf("invalid since")
		}
		args = append(args, "--since", since)
	}
	return r.Run(ctx, "journalctl", args...)
}
func EqualPlan(a, b Plan) bool { return bytes.Equal(records.Marshal(a), records.Marshal(b)) }
