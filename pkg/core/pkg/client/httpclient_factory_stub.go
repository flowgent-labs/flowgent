//go:build !x402

package client

import (
	"time"

	"github.com/flowgent-labs/flowgent/config/pkg/config"
	"github.com/flowgent-labs/flowgent/messager/pkg"
	model "github.com/flowgent-labs/flowgent/model/pkg"
)

// NewHttpClient creates a GenericHttpClient. x402 payment support is not
// compiled in this build (use -tags x402 to enable).
func NewHttpClient(_ *config.FlowgentConfig, _ messager.IMessager) (model.IFlowgentAPIClient, error) {
	return NewGenericHttpClient(30 * time.Second), nil
}
