// Package pi fixes the Pi process boundary and reviewed capabilities.
package pi

import "github.com/arthur404dev/kothar/internal/engine"

const Version = engine.PiVersion

func Capability() engine.Capability {
	capability, _ := engine.Lookup("pi")
	return capability
}
