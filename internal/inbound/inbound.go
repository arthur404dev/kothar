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
	return Adapter{Name: name, Command: "/usr/local/libexec/kothar/buzz-acp", Version: BuzzRevision, Revision: BuzzRevision, PatchSHA256: "5df36384bb968a440b46c6da0e4846add0bc8efe9d46aa04d96b47a211b1b67f", BinarySHA256: "02079ca1e591dfb888cb1c98bc451fc2a279b6bb9f9010782107c59567989fa9"}, true
}
