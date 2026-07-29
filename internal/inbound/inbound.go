// Package inbound defines the static deployment metadata for inbound adapters.
package inbound

type Adapter struct {
	Name, Command, Version, Revision string
}

const BuzzRevision = "7dfea2634f7e87f6a42f5fc1f22d9f77c648abfc"

func Lookup(name string) (Adapter, bool) {
	if name != "buzz" {
		return Adapter{}, false
	}
	return Adapter{Name: name, Command: "/usr/local/libexec/kothar/buzz-acp", Version: BuzzRevision, Revision: BuzzRevision}, true
}
