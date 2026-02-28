module github.com/flowgent-labs/flowgent/notifier

go 1.26.0

require (
	github.com/flowgent-labs/flowgent/model v0.0.0
	github.com/google/uuid v1.6.0
)

require gopkg.in/yaml.v3 v3.0.1 // indirect

replace (
	github.com/flowgent-labs/flowgent/config => ../config
	github.com/flowgent-labs/flowgent/messaging => ../messaging
	github.com/flowgent-labs/flowgent/model => ../model
)
