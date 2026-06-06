package notifier

import (
	"context"

	"github.com/flowgent-labs/flowgent/common/src/utils"
	"github.com/flowgent-labs/flowgent/model/src"
)

// INotifierStore is the notification channel entity store interface.
type INotifierStore interface {
	Get(ctx context.Context, id string) (*model.NotifierChannel, error)
	Select(ctx context.Context, page, pageSize int) (*utils.Page[model.NotifierChannel], error)
	Save(ctx context.Context, entity *model.NotifierChannel) error
	Delete(ctx context.Context, id string) error
}
