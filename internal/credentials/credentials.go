// Package credentials stores opaque named secrets without exposing their values.
package credentials

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"
)

var nameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

type Metadata struct {
	Name     string `json:"name"`
	Modified string `json:"modified"`
}
type Store struct{ Root string }

func (s Store) path(name string) (string, error) {
	if !nameRE.MatchString(name) {
		return "", fmt.Errorf("invalid credential name")
	}
	return filepath.Join(s.Root, name), nil
}
func (s Store) Set(name string, r io.Reader) error {
	p, e := s.path(name)
	if e != nil {
		return e
	}
	data, e := io.ReadAll(io.LimitReader(r, 1<<20+1))
	if e != nil {
		return e
	}
	if len(data) == 0 || len(data) > 1<<20 {
		return fmt.Errorf("credential must contain 1..1048576 bytes")
	}
	if e = os.MkdirAll(s.Root, 0700); e != nil {
		return e
	}
	f, e := os.CreateTemp(s.Root, ".credential-")
	if e != nil {
		return e
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	e = f.Chmod(0600)
	if e == nil {
		_, e = f.Write(data)
	}
	for i := range data {
		data[i] = 0
	}
	if e == nil {
		e = f.Sync()
	}
	if ce := f.Close(); e == nil {
		e = ce
	}
	if e != nil {
		return e
	}
	return os.Rename(tmp, p)
}
func (s Store) List() ([]Metadata, error) {
	es, e := os.ReadDir(s.Root)
	if errors.Is(e, os.ErrNotExist) {
		return []Metadata{}, nil
	}
	if e != nil {
		return nil, e
	}
	out := []Metadata{}
	for _, x := range es {
		if x.Type().IsRegular() && nameRE.MatchString(x.Name()) {
			fi, _ := x.Info()
			out = append(out, Metadata{x.Name(), fi.ModTime().UTC().Format(time.RFC3339)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
func (s Store) Remove(name string) error {
	p, e := s.path(name)
	if e != nil {
		return e
	}
	e = os.Remove(p)
	if errors.Is(e, os.ErrNotExist) {
		return nil
	}
	return e
}
