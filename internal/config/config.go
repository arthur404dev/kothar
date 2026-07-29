// Package config strictly loads non-secret global policy.
package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/arthur404dev/kothar/internal/manifest"
	"github.com/arthur404dev/kothar/internal/securefs"
)

type Config struct {
	AgentDefaults AgentDefaults `json:"agent_defaults"`
	HostPolicy    HostPolicy    `json:"host_policy"`
}
type AgentDefaults struct {
	Models      *manifest.Models      `json:"models,omitempty"`
	Permissions *manifest.Permissions `json:"permissions,omitempty"`
}
type HostPolicy struct {
	AllowedMountRoots []string `json:"allowed_mount_roots"`
}

func Empty() Config { return Config{HostPolicy: HostPolicy{AllowedMountRoots: []string{}}} }
func Load(root string) (Config, error) {
	b, _, err := securefs.ReadFile(root, "config.json", manifest.MaxBytes)
	if errors.Is(err, os.ErrNotExist) {
		return Empty(), nil
	}
	if err != nil {
		return Config{}, err
	}
	return DecodeBytes(b)
}
func DecodeBytes(b []byte) (Config, error) {
	var err error
	if bytes.IndexByte(b, 0) >= 0 {
		return Config{}, fmt.Errorf("NUL is prohibited")
	}
	if err = manifest.ValidateJSON(b); err != nil {
		return Config{}, err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var c Config
	if err = dec.Decode(&c); err != nil {
		return Config{}, err
	}
	var trailing any
	if err = dec.Decode(&trailing); err != io.EOF {
		return Config{}, fmt.Errorf("trailing JSON data")
	}
	if c.HostPolicy.AllowedMountRoots == nil {
		return Config{}, fmt.Errorf("host_policy.allowed_mount_roots is required")
	}
	if len(c.HostPolicy.AllowedMountRoots) > 64 {
		return Config{}, fmt.Errorf("too many mount roots")
	}
	seen := map[string]bool{}
	for _, p := range c.HostPolicy.AllowedMountRoots {
		if !filepath.IsAbs(p) || filepath.Clean(p) != p || len(p) > 4096 || seen[p] {
			return Config{}, fmt.Errorf("invalid allowed mount root")
		}
		seen[p] = true
	}
	if c.AgentDefaults.Models != nil {
		m := c.AgentDefaults.Models
		if m.Primary == "" || m.MaxAttempts < 1 || m.MaxAttempts > 10 || len(m.Fallbacks) > 8 {
			return Config{}, fmt.Errorf("invalid default models")
		}
	}
	if c.AgentDefaults.Permissions != nil {
		p := c.AgentDefaults.Permissions
		if p.Network.Mode != "full" && p.Network.Mode != "none" {
			return Config{}, fmt.Errorf("invalid default permissions")
		}
		if p.Resources.MemoryMaxMB < 64 || p.Resources.MemoryMaxMB > 1048576 || p.Resources.CPUQuotaPercent < 1 || p.Resources.CPUQuotaPercent > 10000 || p.Resources.TasksMax < 1 || p.Resources.TasksMax > 1048576 {
			return Config{}, fmt.Errorf("invalid default resources")
		}
	}
	return c, nil
}

// RestrictRoots returns intersections; command-line roots can only narrow policy.
func RestrictRoots(policy, flags []string) []string {
	if len(flags) == 0 {
		return policy
	}
	out := []string{}
	for _, a := range policy {
		for _, b := range flags {
			if beneath(a, b) {
				out = append(out, a)
				break
			}
			if beneath(b, a) {
				out = append(out, b)
				break
			}
		}
	}
	return out
}
func beneath(p, root string) bool {
	rel, err := filepath.Rel(root, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
