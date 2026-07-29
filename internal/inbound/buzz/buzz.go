// Package buzz fixes the reviewed upstream buzz-acp deployment metadata.
package buzz

import "github.com/arthur404dev/kothar/internal/inbound"

const Revision = "7dfea2634f7e87f6a42f5fc1f22d9f77c648abfc"

func Adapter() inbound.Adapter {
	return inbound.Adapter{Name: "buzz", Command: "/usr/local/libexec/kothar/buzz-acp", Version: Revision, Revision: Revision}
}
