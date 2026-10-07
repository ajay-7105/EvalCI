package factory

import (
	"context"
	"fmt"

	"github.com/ajay-7105/EvalCI/internal/storage"
	"github.com/ajay-7105/EvalCI/internal/storage/sqlite"
)

// DriverSQLite is the supported driver name.
const DriverSQLite = "sqlite"

// Open opens the storage backend named by cfg.Driver. It is the only place that branches on
// the driver: callers hold the returned storage.Store and never see a backend type. An empty
// driver is invalid (callers skip storage when none is configured); any driver that is not
// available in this version, such as "postgres", returns storage.ErrBackendUnavailable.
func Open(ctx context.Context, cfg storage.Config) (storage.Store, error) {
	switch cfg.Driver {
	case DriverSQLite:
		return sqlite.Open(ctx, cfg.Path)
	case "":
		return nil, fmt.Errorf("storage driver is not set")
	}
	return nil, fmt.Errorf("%w: storage backend %q is not available in this version", storage.ErrBackendUnavailable, cfg.Driver)
}
