module github.com/flowgent-labs/flowgent/sandbox

go 1.26.0

require (
	github.com/flowgent-labs/flowgent/messager v0.0.0
	github.com/flowgent-labs/flowgent/model v0.0.0
	golang.org/x/sys v0.42.0
)

require (
	github.com/eclipse/paho.mqtt.golang v1.5.1 // indirect
	github.com/gorilla/websocket v1.5.4-0.20250319132907-e064f32e3674 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	github.com/shopspring/decimal v1.4.0 // indirect
	go.opentelemetry.io/otel v1.38.0 // indirect
	go.opentelemetry.io/otel/metric v1.38.0 // indirect
	golang.org/x/net v0.50.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace (
	github.com/flowgent-labs/flowgent/messager => ../messager
	github.com/flowgent-labs/flowgent/model => ../model
)
