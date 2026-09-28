// Package opamp integrates the OpAMP server into the Arveld controller.
package opamp

import (
	"context"
	"fmt"
	"log/slog"

	clienttypes "github.com/open-telemetry/opamp-go/client/types"
)

type slogLogger struct {
	logger *slog.Logger
}

var _ clienttypes.Logger = (*slogLogger)(nil)

func (adapter *slogLogger) Debugf(
	ctx context.Context,
	format string,
	args ...any,
) {
	adapter.logger.DebugContext(ctx, fmt.Sprintf(format, args...))
}

func (adapter *slogLogger) Errorf(
	ctx context.Context,
	format string,
	args ...any,
) {
	adapter.logger.ErrorContext(ctx, fmt.Sprintf(format, args...))
}
