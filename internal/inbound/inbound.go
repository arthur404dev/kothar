// Package inbound defines the static deployment metadata for inbound adapters.
package inbound

type Adapter struct {
	Name, Command, Version, Revision string
}

var adapters = map[string]Adapter{}

func Register(adapter Adapter)           { adapters[adapter.Name] = adapter }
func Lookup(name string) (Adapter, bool) { adapter, ok := adapters[name]; return adapter, ok }
