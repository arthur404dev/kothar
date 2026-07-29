// Package acp implements the generic ACP JSON-RPC stdin/stdout adapter.
package acp

import (
	"context"
	"io"
	"time"

	"github.com/arthur404dev/kothar/internal/framework"
)

const MaxLine = 10 << 20

type Server struct {
	In             io.Reader
	Out            io.Writer
	Err            io.Writer
	Service        *framework.Service
	RequestTimeout time.Duration
	TurnTimeout    time.Duration
}

func (s *Server) Serve(ctx context.Context) error { return s.serve(ctx) }
