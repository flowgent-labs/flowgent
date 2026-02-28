package notification

import "context"

// Sender defines the interface for a notification channel backend.
type Sender interface {
	Type() string
	Send(ctx context.Context, recipient string, title string, body string) error
	Validate() error
}
