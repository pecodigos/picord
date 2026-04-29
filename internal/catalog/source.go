package catalog

import "context"

type RefreshOptions struct {
	MaxPages int
	Offline  bool
}

type Source interface {
	Name() string
	Refresh(ctx context.Context, store *Store, opts RefreshOptions) error
}
