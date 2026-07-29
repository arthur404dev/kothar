// Package engine defines engine-independent execution and static capability contracts.
package engine

import "context"

type Request struct{ SessionID, Prompt string }
type Event struct{ Kind, Text string }
type Runner interface {
	Run(context.Context, Request, func(Event) error) error
	Cancel(string) error
}

type Capability struct {
	Name, Command, Version    string
	Providers, Tools, Bundles []string
}

var engines = map[string]Capability{}

func Register(capability Capability)        { engines[capability.Name] = capability }
func Lookup(name string) (Capability, bool) { capability, ok := engines[name]; return capability, ok }
