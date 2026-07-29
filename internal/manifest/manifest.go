// Package manifest strictly decodes and validates agent-manifest v1.
package manifest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"regexp"
	"strings"
)

const MaxBytes = 1 << 20

var safeID = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
var modelName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:+-]{0,127}$`)
var namedRef = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)
var toolName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:-]{0,127}$`)
var labelName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)
var buzzPubkey = regexp.MustCompile(`^[0-9a-f]{64}$`)

type Manifest struct {
	Version     int         `json:"version"`
	ID          string      `json:"id"`
	Profile     Profile     `json:"profile"`
	Inbound     Inbound     `json:"inbound"`
	Engine      Engine      `json:"engine"`
	Models      Models      `json:"models"`
	Behavior    Behavior    `json:"behavior"`
	Tools       Tools       `json:"tools"`
	Workspace   Workspace   `json:"workspace"`
	Permissions Permissions `json:"permissions"`
	Runtime     Runtime     `json:"runtime"`
}
type Profile struct {
	DisplayName string   `json:"display_name"`
	Description string   `json:"description"`
	Labels      []string `json:"labels"`
}
type Engine struct {
	Name        string      `json:"name"`
	Credentials Credentials `json:"credentials"`
	Options     PiOptions   `json:"options"`
}
type Credentials struct {
	Mode      string            `json:"mode"`
	Overrides map[string]string `json:"overrides"`
}
type PiOptions struct {
	ProjectTrust string `json:"project_trust"`
	Telemetry    bool   `json:"telemetry"`
	UpdateChecks bool   `json:"update_checks"`
}
type Models struct {
	Primary     string   `json:"primary"`
	Fallbacks   []string `json:"fallbacks"`
	Thinking    string   `json:"thinking"`
	MaxAttempts int      `json:"max_attempts"`
}
type Behavior struct {
	SystemPrompt string   `json:"system_prompt"`
	ContextFiles []string `json:"context_files"`
	Skills       []string `json:"skills"`
	Extensions   []string `json:"extensions"`
}
type Tools struct {
	Bundles     []string          `json:"bundles"`
	Allow       []string          `json:"allow"`
	Deny        []string          `json:"deny"`
	Credentials map[string]string `json:"credentials"`
}
type Workspace struct {
	Root   string  `json:"root"`
	Mounts []Mount `json:"mounts"`
}
type Mount struct {
	Source string `json:"source"`
	Target string `json:"target"`
	Mode   string `json:"mode"`
}
type Permissions struct {
	Network   Network   `json:"network"`
	Resources Resources `json:"resources"`
}
type Network struct {
	Mode string `json:"mode"`
}
type Resources struct {
	MemoryMaxMB     int `json:"memory_max_mb"`
	CPUQuotaPercent int `json:"cpu_quota_percent"`
	TasksMax        int `json:"tasks_max"`
}
type Runtime struct {
	Driver      string `json:"driver"`
	StartOnBoot bool   `json:"start_on_boot"`
	Restart     string `json:"restart"`
	Workers     int    `json:"workers"`
}
type Inbound struct {
	Name    string      `json:"name"`
	Options BuzzOptions `json:"options"`
}
type BuzzOptions struct {
	Relay              string    `json:"relay"`
	IdentityCredential string    `json:"identity_credential"`
	RespondTo          RespondTo `json:"respond_to"`
	HeartbeatSeconds   int       `json:"heartbeat_seconds"`
}
type RespondTo struct {
	Mode    string   `json:"mode"`
	Pubkeys []string `json:"pubkeys"`
}

func Decode(r io.Reader) (*Manifest, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxBytes {
		return nil, fmt.Errorf("manifest exceeds %d bytes", MaxBytes)
	}
	if bytes.IndexByte(data, 0) >= 0 {
		return nil, fmt.Errorf("NUL is prohibited")
	}
	if err = checkJSON(data); err != nil {
		return nil, err
	}
	if err = requireManifestMembers(data); err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var m Manifest
	if err = dec.Decode(&m); err != nil {
		return nil, err
	}
	if err = ensureEOF(dec); err != nil {
		return nil, err
	}
	if err = m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

var manifestMembers = map[string]any{
	"version": nil, "id": nil,
	"profile":     map[string]any{"display_name": nil, "description": nil, "labels": nil},
	"inbound":     map[string]any{"name": nil, "options": map[string]any{"relay": nil, "identity_credential": nil, "respond_to": map[string]any{"mode": nil, "pubkeys": nil}, "heartbeat_seconds": nil}},
	"engine":      map[string]any{"name": nil, "credentials": map[string]any{"mode": nil, "overrides": nil}, "options": map[string]any{"project_trust": nil, "telemetry": nil, "update_checks": nil}},
	"models":      map[string]any{"primary": nil, "fallbacks": nil, "thinking": nil, "max_attempts": nil},
	"behavior":    map[string]any{"system_prompt": nil, "context_files": nil, "skills": nil, "extensions": nil},
	"tools":       map[string]any{"bundles": nil, "allow": nil, "deny": nil, "credentials": nil},
	"workspace":   map[string]any{"root": nil, "mounts": []any{map[string]any{"source": nil, "target": nil, "mode": nil}}},
	"permissions": map[string]any{"network": map[string]any{"mode": nil}, "resources": map[string]any{"memory_max_mb": nil, "cpu_quota_percent": nil, "tasks_max": nil}},
	"runtime":     map[string]any{"driver": nil, "start_on_boot": nil, "restart": nil, "workers": nil},
}

func requireManifestMembers(data []byte) error {
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	return requireMembers(value, manifestMembers, "manifest")
}

func requireMembers(value, spec any, at string) error {
	switch expected := spec.(type) {
	case map[string]any:
		object, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("%s must be an object", at)
		}
		for name, child := range expected {
			got, present := object[name]
			if !present {
				return fmt.Errorf("%s.%s is required", at, name)
			}
			if child != nil {
				if err := requireMembers(got, child, at+"."+name); err != nil {
					return err
				}
			}
		}
	case []any:
		array, ok := value.([]any)
		if !ok {
			return fmt.Errorf("%s must be an array", at)
		}
		for i, item := range array {
			if err := requireMembers(item, expected[0], fmt.Sprintf("%s[%d]", at, i)); err != nil {
				return err
			}
		}
	}
	return nil
}

func ensureEOF(d *json.Decoder) error {
	var x any
	if err := d.Decode(&x); err != io.EOF {
		return fmt.Errorf("trailing JSON data")
	}
	return nil
}
func checkJSON(data []byte) error {
	d := json.NewDecoder(bytes.NewReader(data))
	var walk func() error
	walk = func() error {
		tok, err := d.Token()
		if err != nil {
			return err
		}
		switch v := tok.(type) {
		case json.Delim:
			if v == '{' {
				seen := map[string]bool{}
				for d.More() {
					k, _ := d.Token()
					key := k.(string)
					if seen[key] {
						return fmt.Errorf("duplicate key %q", key)
					}
					seen[key] = true
					if secretKey(key) {
						return fmt.Errorf("secret-bearing field %q is prohibited", key)
					}
					if err := walk(); err != nil {
						return err
					}
				}
				_, err = d.Token()
				return err
			}
			if v == '[' {
				for d.More() {
					if err := walk(); err != nil {
						return err
					}
				}
				_, err = d.Token()
				return err
			}
		case string:
			for _, r := range v {
				if r < 0x20 {
					return fmt.Errorf("control character is prohibited")
				}
			}
		}
		return nil
	}
	if err := walk(); err != nil {
		return err
	}
	if d.More() {
		return fmt.Errorf("trailing JSON data")
	}
	return nil
}
func secretKey(k string) bool {
	k = strings.ToLower(k)
	return k == "token" || k == "password" || k == "secret" || k == "api_key" || strings.HasSuffix(k, "_token") || strings.HasSuffix(k, "_password") || strings.HasSuffix(k, "_secret") || strings.HasSuffix(k, "_api_key")
}
func safePath(p string) bool {
	return p != "" && len(p) <= 255 && p == path.Clean(p) && !path.IsAbs(p) && p != "." && p != ".." && !strings.HasPrefix(p, "../") && !strings.Contains(p, "\\")
}
func uniqueBounded(values []string, max int, valid func(string) bool) bool {
	if values == nil || len(values) > max {
		return false
	}
	seen := map[string]bool{}
	for _, value := range values {
		if seen[value] || !valid(value) {
			return false
		}
		seen[value] = true
	}
	return true
}
func overlaps(a, b string) bool {
	return a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}
func (m Manifest) Validate() error {
	if m.Version != 1 {
		return fmt.Errorf("version must be 1")
	}
	if !safeID.MatchString(m.ID) {
		return fmt.Errorf("invalid id")
	}
	capability, ok := engines[m.Engine.Name]
	if !ok {
		return fmt.Errorf("unsupported engine")
	}
	if len(m.Profile.DisplayName) < 1 || len(m.Profile.DisplayName) > 100 || len(m.Profile.Description) > 500 || !uniqueBounded(m.Profile.Labels, 32, labelName.MatchString) {
		return fmt.Errorf("invalid profile")
	}
	if !oneOf(m.Engine.Credentials.Mode, "inherit", "custom", "none") {
		return fmt.Errorf("invalid credential mode")
	}
	if m.Engine.Credentials.Overrides == nil {
		return fmt.Errorf("credential overrides must be an object")
	}
	for k, v := range m.Engine.Credentials.Overrides {
		if !capability.Providers[k] || !namedRef.MatchString(v) {
			return fmt.Errorf("invalid credential override")
		}
	}
	if m.Engine.Options.ProjectTrust != "never" {
		return fmt.Errorf("project_trust must be never")
	}
	if !oneOf(m.Models.Thinking, "off", "low", "medium", "high") || !uniqueBounded(m.Models.Fallbacks, 8, func(string) bool { return true }) {
		return fmt.Errorf("invalid model policy")
	}
	providers := map[string]bool{}
	models := append([]string{m.Models.Primary}, m.Models.Fallbacks...)
	seenModels := map[string]bool{}
	for _, model := range models {
		provider, valid := modelProvider(model)
		if !valid || !capability.Providers[provider] || seenModels[model] {
			return fmt.Errorf("unsupported or duplicate model %q", model)
		}
		seenModels[model], providers[provider] = true, true
	}
	if len(m.Models.Fallbacks) > 0 && !capability.SafeFailover {
		return fmt.Errorf("engine does not support safe failover")
	}
	if m.Engine.Credentials.Mode == "custom" {
		for provider := range providers {
			if capability.Providers[provider] && !capability.Unauthenticated[provider] && m.Engine.Credentials.Overrides[provider] == "" {
				return fmt.Errorf("custom credentials missing provider %q", provider)
			}
		}
	}
	if m.Engine.Credentials.Mode == "none" {
		for provider := range providers {
			if !capability.Unauthenticated[provider] {
				return fmt.Errorf("provider %q requires credentials", provider)
			}
		}
	}
	if m.Models.MaxAttempts < 1 || m.Models.MaxAttempts > capability.MaxAttempts {
		return fmt.Errorf("max_attempts out of bounds")
	}
	if !oneOf(m.Permissions.Network.Mode, "full", "none") {
		return fmt.Errorf("invalid network mode")
	}
	if m.Runtime.Driver != "systemd" {
		return fmt.Errorf("unsupported runtime driver")
	}
	if !oneOf(m.Runtime.Restart, "no", "on-failure", "always") {
		return fmt.Errorf("invalid restart policy")
	}
	if m.Runtime.Workers < 1 || m.Runtime.Workers > 64 {
		return fmt.Errorf("workers out of bounds")
	}
	if m.Permissions.Resources.MemoryMaxMB < 64 || m.Permissions.Resources.MemoryMaxMB > 1048576 || m.Permissions.Resources.CPUQuotaPercent < 1 || m.Permissions.Resources.CPUQuotaPercent > 10000 || m.Permissions.Resources.TasksMax < 1 || m.Permissions.Resources.TasksMax > 1048576 {
		return fmt.Errorf("resources out of bounds")
	}
	if !uniqueBounded(m.Behavior.ContextFiles, 64, safePath) || !uniqueBounded(m.Behavior.Skills, 64, safePath) || !uniqueBounded(m.Behavior.Extensions, 64, safePath) || !safePath(m.Behavior.SystemPrompt) || !safePath(m.Workspace.Root) {
		return fmt.Errorf("invalid behavior or workspace path")
	}
	if !uniqueBounded(m.Tools.Bundles, 128, func(v string) bool { return capability.Bundles[v] }) || !uniqueBounded(m.Tools.Allow, 128, func(v string) bool { return capability.Tools[v] }) || !uniqueBounded(m.Tools.Deny, 128, func(v string) bool { return capability.Tools[v] }) || m.Tools.Credentials == nil {
		return fmt.Errorf("invalid tool declaration")
	}
	for tool, ref := range m.Tools.Credentials {
		if !capability.Tools[tool] || !toolName.MatchString(tool) || !namedRef.MatchString(ref) {
			return fmt.Errorf("invalid tool credential")
		}
	}
	if m.Workspace.Mounts == nil || len(m.Workspace.Mounts) > 32 {
		return fmt.Errorf("invalid mounts")
	}
	for i, mount := range m.Workspace.Mounts {
		if mount.Source == "" || len(mount.Source) > 4096 || !path.IsAbs(mount.Source) || path.Clean(mount.Source) != mount.Source || strings.Contains(mount.Source, "\\") || !safePath(mount.Target) || !oneOf(mount.Mode, "read_only", "read_write") {
			return fmt.Errorf("invalid mount %d", i)
		}
		for j := 0; j < i; j++ {
			if overlaps(mount.Target, m.Workspace.Mounts[j].Target) {
				return fmt.Errorf("overlapping mount targets")
			}
		}
	}
	if m.Inbound.Name != "buzz" {
		return fmt.Errorf("unsupported inbound adapter")
	}
	buzz := m.Inbound.Options
	if !strings.HasPrefix(buzz.Relay, "wss://") || buzz.HeartbeatSeconds < 30 || buzz.HeartbeatSeconds > 86400 {
		return fmt.Errorf("invalid Buzz transport policy")
	}
	if !namedRef.MatchString(buzz.IdentityCredential) {
		return fmt.Errorf("invalid Buzz credential reference")
	}
	if !oneOf(buzz.RespondTo.Mode, "nobody", "allowlist", "anyone") || !uniqueBounded(buzz.RespondTo.Pubkeys, 256, buzzPubkey.MatchString) {
		return fmt.Errorf("invalid respond_to policy")
	}
	if buzz.RespondTo.Mode == "allowlist" && len(buzz.RespondTo.Pubkeys) == 0 {
		return fmt.Errorf("allowlist requires pubkeys")
	}
	if buzz.RespondTo.Mode != "allowlist" && len(buzz.RespondTo.Pubkeys) != 0 {
		return fmt.Errorf("pubkeys require allowlist mode")
	}
	return nil
}
func oneOf(v string, values ...string) bool {
	for _, x := range values {
		if v == x {
			return true
		}
	}
	return false
}
