package store

import (
	"testing"

	"github.com/flowgent-labs/flowgent/common/pkg/utils"
	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

func TestColumnOrder(t *testing.T) {
	cols := utils.Columns[entities.FlowVersionInfo]()
	t.Logf("Columns: %s", cols)

	var afvi entities.FlowVersionInfo
	sCols, _ := utils.StructFields(&afvi)
	for i, c := range sCols {
		t.Logf("  [%d] %s", i, c)
	}
}
