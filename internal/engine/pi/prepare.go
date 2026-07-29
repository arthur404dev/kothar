package pi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/arthur404dev/kothar/internal/engine"
	"github.com/arthur404dev/kothar/internal/records"
	"github.com/arthur404dev/kothar/internal/securefs"
)

type preparedMetadata struct {
	SystemPrompt string            `json:"system_prompt_sha256"`
	Resources    map[string]string `json:"resources"`
	BuzzCLI      string            `json:"buzz_cli"`
}

// Prepare creates only the isolated Pi state selected by the validated agent record.
func Prepare(root string, a engine.Agent, buzzPath string) error {
	piDir := filepath.Join(root, "pi")
	if err := securefs.EnsureDir(piDir, 0700); err != nil {
		return err
	}
	if err := prepareAuth(filepath.Join(piDir, "auth.json"), a); err != nil {
		return err
	}
	resourceDir := filepath.Join(root, "resources")
	if err := securefs.EnsureDir(resourceDir, 0700); err != nil {
		return err
	}
	names := make([]string, 0, len(a.Resources))
	for name := range a.Resources {
		names = append(names, name)
	}
	sort.Strings(names)
	hashes := make(map[string]string, len(names))
	for _, name := range names {
		clean := filepath.Clean(filepath.FromSlash(name))
		if clean == "." || filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
			return fmt.Errorf("invalid runtime resource")
		}
		dst := filepath.Join(resourceDir, clean)
		if err := securefs.EnsureDir(filepath.Dir(dst), 0700); err != nil {
			return err
		}
		if err := records.AtomicWrite(dst, a.Resources[name], 0600); err != nil {
			return err
		}
		h := sha256.Sum256(a.Resources[name])
		hashes[name] = hex.EncodeToString(h[:])
	}
	if err := records.AtomicWrite(filepath.Join(piDir, "settings.json"), []byte("{\"updateChecks\":false}\n"), 0600); err != nil {
		return err
	}
	h := sha256.Sum256([]byte(a.SystemPrompt))
	meta, _ := json.Marshal(preparedMetadata{hex.EncodeToString(h[:]), hashes, buzzPath})
	return records.AtomicWrite(filepath.Join(root, "runtime.json"), append(meta, '\n'), 0600)
}

func providers(a engine.Agent) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, model := range append([]string{a.Models.Primary}, a.Models.Fallbacks...) {
		provider, _, ok := strings.Cut(model, "/")
		if !ok || provider == "" {
			return nil, fmt.Errorf("invalid model")
		}
		if !seen[provider] {
			seen[provider] = true
			out = append(out, provider)
		}
	}
	return out, nil
}

func readObject(path string) (map[string]json.RawMessage, error) {
	b, _, err := securefs.ReadFile(filepath.Dir(path), filepath.Base(path), 1<<20)
	if os.IsNotExist(err) {
		return map[string]json.RawMessage{}, nil
	}
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var v map[string]json.RawMessage
	if err = dec.Decode(&v); err != nil || v == nil {
		return nil, fmt.Errorf("invalid credential store")
	}
	return v, nil
}

func prepareAuth(dst string, a engine.Agent) error {
	selected, err := providers(a)
	if err != nil {
		return err
	}
	if _, err = os.Lstat(dst); err == nil && !a.Credentials.Refresh {
		return nil
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	current := map[string]json.RawMessage{}
	if a.Credentials.Refresh {
		current, err = readObject(dst)
		if err != nil {
			return err
		}
	}
	switch a.Credentials.Mode {
	case "": // Direct engine users may supply credentials by other isolated means.
	case "inherit":
		host, e := readObject(a.Credentials.HostAuth)
		if e != nil {
			return fmt.Errorf("provider credentials unavailable")
		}
		for _, p := range selected {
			if v, ok := host[p]; ok {
				current[p] = v
			} else {
				delete(current, p)
			}
		}
	case "custom":
		for _, p := range selected {
			ref := a.Credentials.Overrides[p]
			if ref == "" {
				return fmt.Errorf("provider credential binding missing")
			}
			b, _, e := securefs.ReadFile(a.Credentials.StoreRoot, ref, 1<<20)
			if e != nil {
				return fmt.Errorf("provider credential unavailable")
			}
			if !json.Valid(b) {
				return fmt.Errorf("invalid provider credential")
			}
			current[p] = append(json.RawMessage(nil), b...)
			for i := range b {
				b[i] = 0
			}
		}
	case "none":
		for _, p := range selected {
			if p != "ollama" {
				return fmt.Errorf("provider requires credentials")
			}
			delete(current, p)
		}
	default:
		return fmt.Errorf("invalid credential mode")
	}
	b, err := json.MarshalIndent(current, "", "  ")
	if err != nil {
		return err
	}
	defer func() {
		for i := range b {
			b[i] = 0
		}
	}()
	return records.AtomicWrite(dst, append(b, '\n'), 0600)
}
