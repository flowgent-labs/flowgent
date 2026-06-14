package store

import (
	"github.com/flowgent-labs/flowgent/config/pkg/config"
)

// InitStore creates the Store implementation based on config.
func InitStore(cfg *config.FlowgentConfig) IStore {
	return NewStoreManager(cfg)
}
