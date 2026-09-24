package notifier

import (
	"context"

	"github.com/flowgent-labs/flowgent/model/pkg/entities"
)

// INotifierStore is the notification channel entity store interface.
type INotifierStore interface {
	Get(ctx context.Context, namespace, id string) (*entities.NotifyChannelInfo, error)
	List(ctx context.Context, namespace string, req entities.PageRequest) (*entities.Page[entities.NotifyChannelInfo], error)
	Save(ctx context.Context, entity *entities.NotifyChannelInfo) error
	Delete(ctx context.Context, namespace, id string) error
}
