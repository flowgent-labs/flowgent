module github.com/flowgent-labs/flowgent/core

go 1.26.0

require (
	github.com/flowgent-labs/flowgent/api v0.0.0
	github.com/flowgent-labs/flowgent/cache v0.0.0
	github.com/flowgent-labs/flowgent/common v0.0.0
	github.com/flowgent-labs/flowgent/config v0.0.0
	github.com/flowgent-labs/flowgent/messaging v0.0.0
	github.com/flowgent-labs/flowgent/model v0.0.0
	github.com/flowgent-labs/flowgent/notifier v0.0.0
	github.com/flowgent-labs/flowgent/sandbox v0.0.0
	github.com/flowgent-labs/flowgent/store v0.0.0
	github.com/flowgent-labs/flowgent/wallet v0.0.0
	github.com/google/uuid v1.6.0
	github.com/jackc/pgx/v5 v5.7.6
	github.com/redis/go-redis/v9 v9.17.0
	github.com/robfig/cron/v3 v3.0.1
	github.com/spf13/viper v1.20.1
	go.opentelemetry.io/otel v1.38.0
	go.opentelemetry.io/otel/sdk v1.38.0
	go.opentelemetry.io/otel/trace v1.38.0
	golang.org/x/time v0.9.0
	gopkg.in/yaml.v3 v3.0.1
	k8s.io/api v0.34.2
	k8s.io/apimachinery v0.34.2
	k8s.io/client-go v0.34.2
)

replace (
	github.com/flowgent-labs/flowgent/api => ../api
	github.com/flowgent-labs/flowgent/cache => ../cache
	github.com/flowgent-labs/flowgent/common => ../common
	github.com/flowgent-labs/flowgent/config => ../config
	github.com/flowgent-labs/flowgent/messaging => ../messaging
	github.com/flowgent-labs/flowgent/model => ../model
	github.com/flowgent-labs/flowgent/notifier => ../notifier
	github.com/flowgent-labs/flowgent/sandbox => ../sandbox
	github.com/flowgent-labs/flowgent/store => ../store
	github.com/flowgent-labs/flowgent/wallet => ../wallet
)
