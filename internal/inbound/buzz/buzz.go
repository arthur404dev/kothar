// Package buzz fixes the reviewed upstream buzz-acp deployment metadata.
package buzz

import "github.com/arthur404dev/kothar/internal/inbound"

const Revision = inbound.BuzzRevision

func Adapter() inbound.Adapter {
	adapter, _ := inbound.Lookup("buzz")
	return adapter
}
