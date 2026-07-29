// Package framework defines normalized agent orchestration independent of transports and engines.
package framework

import (
	"context"

	"github.com/arthur404dev/kothar/internal/engine"
)

type Service struct{ Engine engine.Runner }

func (s Service) Run(ctx context.Context, request engine.Request, emit func(engine.Event) error) error {
	return s.Engine.Run(ctx, request, emit)
}
func (s Service) Cancel(sessionID string) error { return s.Engine.Cancel(sessionID) }
