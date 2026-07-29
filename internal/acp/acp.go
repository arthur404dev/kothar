// Package acp owns generic ACP transport lifecycle and stdout discipline.
package acp

import (
	"context"
	"io"
)

type Request struct{ SessionID, Prompt string }
type Event struct{ Kind, Text string }
type Handler interface {
	Run(context.Context, Request, func(Event) error) error
	Cancel(string) error
}
type Server struct {
	In      io.Reader
	Out     io.Writer
	Handler Handler
}
