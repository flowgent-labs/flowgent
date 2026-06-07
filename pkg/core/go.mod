module github.com/flowgent-labs/flowgent/core

go 1.26.0

require (
	github.com/flowgent-labs/flowgent/api v0.0.0
	github.com/flowgent-labs/flowgent/cache v0.0.0
	github.com/flowgent-labs/flowgent/common v0.0.0
	github.com/flowgent-labs/flowgent/config v0.0.0
	github.com/flowgent-labs/flowgent/messager v0.0.0
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

require (
	github.com/anthropics/anthropic-sdk-go v1.46.0 // indirect
	github.com/bahlo/generic-list-go v0.2.0 // indirect
	github.com/buger/jsonparser v1.1.2 // indirect
	github.com/google/jsonschema-go v0.4.2 // indirect
	github.com/invopop/jsonschema v0.13.0 // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/mark3labs/mcp-go v0.54.1 // indirect
	github.com/openai/openai-go v1.12.0 // indirect
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.2 // indirect
	github.com/spf13/cast v1.7.1 // indirect
	github.com/standard-webhooks/standard-webhooks/libraries v0.0.1 // indirect
	github.com/tidwall/gjson v1.18.0 // indirect
	github.com/tidwall/match v1.1.1 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	github.com/tidwall/sjson v1.2.5 // indirect
	github.com/wk8/go-ordered-map/v2 v2.1.8 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/text v0.32.0 // indirect
)

replace (
	github.com/flowgent-labs/flowgent/api => ../api
	github.com/flowgent-labs/flowgent/cache => ../cache
	github.com/flowgent-labs/flowgent/common => ../common
	github.com/flowgent-labs/flowgent/config => ../config
	github.com/flowgent-labs/flowgent/messager => ../messager
	github.com/flowgent-labs/flowgent/model => ../model
	github.com/flowgent-labs/flowgent/notifier => ../notifier
	github.com/flowgent-labs/flowgent/sandbox => ../sandbox
	github.com/flowgent-labs/flowgent/store => ../store
	github.com/flowgent-labs/flowgent/wallet => ../wallet
)
