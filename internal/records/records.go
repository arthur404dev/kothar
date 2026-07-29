// Package records owns local agent records and their atomic filesystem lifecycle.
package records

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"

	"github.com/arthur404dev/kothar/internal/manifest"
	"github.com/arthur404dev/kothar/internal/securefs"
)

var idPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
var ErrDeployed = errors.New("agent is deployed; remove it first")

type Store struct{ Config, State string }

func (s Store) Agents() string { return filepath.Join(s.Config, "agents") }
func (s Store) Dir(id string) (string, error) {
	if !idPattern.MatchString(id) {
		return "", fmt.Errorf("invalid agent id")
	}
	return filepath.Join(s.Agents(), id), nil
}
func (s Store) Path(id string) (string, error) {
	d, e := s.Dir(id)
	return filepath.Join(d, "agent.json"), e
}
func (s Store) Receipt(id string) string {
	return filepath.Join(s.State, "deployments", id, "receipt.json")
}

func (s Store) Create(id string, data []byte) error {
	d, err := s.Dir(id)
	if err != nil {
		return err
	}
	m, err := manifest.DecodeBytes(data)
	if err != nil {
		return err
	}
	if m.ID != id {
		return fmt.Errorf("manifest id %q does not match target agent %q", m.ID, id)
	}
	if err = securefs.EnsureDir(s.Agents(), 0700); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(s.Agents(), ".create-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if err = os.Chmod(tmp, 0700); err != nil {
		return err
	}
	for name, body := range map[string][]byte{"agent.json": data, "SYSTEM.md": {}, "AGENTS.md": {}, "CONSTRAINTS.md": {}} {
		if err = writeNew(filepath.Join(tmp, name), body, 0600); err != nil {
			return err
		}
	}
	return os.Rename(tmp, d)
}
func writeNew(path string, data []byte, mode fs.FileMode) error {
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if e != nil {
		return e
	}
	_, e = f.Write(data)
	if se := f.Sync(); e == nil {
		e = se
	}
	if ce := f.Close(); e == nil {
		e = ce
	}
	return e
}
func AtomicWrite(path string, data []byte, mode fs.FileMode) error {
	return securefs.AtomicWrite(path, data, mode)
}
func (s Store) Read(id string) ([]byte, error) {
	if _, e := s.Dir(id); e != nil {
		return nil, e
	}
	b, _, e := securefs.ReadFile(s.Config, filepath.Join("agents", id, "agent.json"), manifest.MaxBytes)
	return b, e
}
func (s Store) List() ([]string, error) {
	es, e := os.ReadDir(s.Agents())
	if errors.Is(e, os.ErrNotExist) {
		return []string{}, nil
	}
	if e != nil {
		return nil, e
	}
	out := []string{}
	for _, x := range es {
		if x.IsDir() && idPattern.MatchString(x.Name()) {
			if _, e = s.Read(x.Name()); e == nil {
				out = append(out, x.Name())
			}
		}
	}
	return out, nil
}
func (s Store) Delete(id string) error {
	if b, _, e := securefs.ReadFile(filepath.Dir(s.Receipt(id)), filepath.Base(s.Receipt(id)), manifest.MaxBytes); e == nil {
		var receipt struct {
			AgentID string `json:"agent_id"`
		}
		if e = json.Unmarshal(b, &receipt); e != nil {
			return e
		}
		if receipt.AgentID != id {
			return fmt.Errorf("receipt agent id %q does not match requested agent %q", receipt.AgentID, id)
		}
		return ErrDeployed
	} else if !errors.Is(e, os.ErrNotExist) {
		return e
	}
	d, e := s.Dir(id)
	if e != nil {
		return e
	}
	return os.RemoveAll(d)
}
func (s Store) Edit(id, editor string) error {
	p, e := s.Path(id)
	if e != nil {
		return e
	}
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		return fmt.Errorf("EDITOR is not set")
	}
	tmp, e := os.CreateTemp(filepath.Dir(p), ".editor-")
	if e != nil {
		return e
	}
	name := tmp.Name()
	defer os.Remove(name)
	old, e := s.Read(id)
	if e == nil {
		_, e = tmp.Write(old)
	}
	tmp.Close()
	if e != nil {
		return e
	}
	c := exec.Command(editor, name)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if e = c.Run(); e != nil {
		return e
	}
	data, e := os.ReadFile(name)
	if e != nil {
		return e
	}
	m, e := manifest.DecodeBytes(data)
	if e != nil {
		return e
	}
	if m.ID != id {
		return fmt.Errorf("id is immutable")
	}
	return AtomicWrite(p, data, 0600)
}
func Marshal(v any) []byte { b, _ := json.MarshalIndent(v, "", "  "); return append(b, '\n') }
