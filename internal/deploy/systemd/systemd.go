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
	"github.com/arthur404dev/kothar/internal/records"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

var ErrRuntimeIncomplete = errors.New("runtime incomplete/not installed")

type Artifact struct {
	Path     string `json:"path"`
	Mode     uint32 `json:"mode"`
	Category string `json:"category"`
	Content  []byte `json:"-"`
	Hash     string `json:"hash"`
}
type Plan struct {
	AgentID            string     `json:"agent_id"`
	EffectiveHash      string     `json:"effective_hash"`
	Categories         []string   `json:"changed_categories"`
	RefreshCredentials bool       `json:"refresh_credentials"`
	Artifacts          []Artifact `json:"artifacts"`
}
type Receipt struct {
	AgentID            string   `json:"agent_id"`
	EffectiveHash      string   `json:"effective_hash"`
	Categories         []string `json:"changed_categories"`
	RefreshCredentials bool     `json:"refresh_credentials"`
	AppliedAt          string   `json:"applied_at"`
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
	for i := range arts {
		arts[i].Hash = Hash(arts[i].Content)
	}
	sort.Slice(arts, func(i, j int) bool { return arts[i].Path < arts[j].Path })
	cats := []string{}
	if previous == nil || previous.EffectiveHash != Hash(effective) {
		set := map[string]bool{}
		for _, a := range arts {
			set[a.Category] = true
		}
		for c := range set {
			cats = append(cats, c)
		}
		sort.Strings(cats)
	}
	if refresh {
		cats = append(cats, "credentials")
	}
	return Plan{id, Hash(effective), cats, refresh, arts}
}
func LoadReceipt(path string) (*Receipt, error) {
	b, e := os.ReadFile(path)
	if e != nil {
		return nil, e
	}
	var r Receipt
	e = json.Unmarshal(b, &r)
	return &r, e
}

// ApplyFixture is the reusable atomic installer. Callers must gate real application on runtime availability first.
func ApplyFixture(root, receiptPath string, p Plan, failAfter int) error {
	backups := map[string][]byte{}
	created := []string{}
	written := 0
	rollback := func() {
		for path, b := range backups {
			_ = records.AtomicWrite(path, b, 0600)
		}
		for _, path := range created {
			_ = os.Remove(path)
		}
	}
	for _, a := range p.Artifacts {
		dst := filepath.Join(root, filepath.Clean("/"+a.Path))
		if !strings.HasPrefix(dst, filepath.Clean(root)+string(filepath.Separator)) {
			rollback()
			return fmt.Errorf("artifact escapes root")
		}
		if old, e := os.ReadFile(dst); e == nil {
			backups[dst] = old
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
		written++
	}
	r := Receipt{p.AgentID, p.EffectiveHash, p.Categories, p.RefreshCredentials, time.Now().UTC().Format(time.RFC3339)}
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
