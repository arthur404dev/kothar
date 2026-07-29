// Package inbound defines the static deployment metadata for inbound adapters.
package inbound

type Adapter struct {
	Name, Command, Version, Revision string
	PatchSHA256, BinarySHA256        string
}

const BuzzRevision = "7dfea2634f7e87f6a42f5fc1f22d9f77c648abfc"

func Lookup(name string) (Adapter, bool) {
	if name != "buzz" {
		return Adapter{}, false
	}
	return Adapter{Name: name, Command: "/usr/local/libexec/kothar/buzz-acp", Version: BuzzRevision, Revision: BuzzRevision, PatchSHA256: "9bea56aa02e8e89a3c5c35f42c415a8b4297528fd59c82424f6c1402316b5dbf", BinarySHA256: "fd7bbfbd7fa0fcb1d68623f9ba013d67bd8dde744fd15201652ffd0a34167f0a"}, true
}
