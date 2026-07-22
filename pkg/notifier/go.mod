module github.com/flowgent-labs/flowgent/notifier

go 1.26.0

require (
	github.com/flowgent-labs/flowgent/model v0.0.0
	github.com/google/uuid v1.6.0
)

require (
	github.com/kr/pretty v0.3.1 // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	github.com/shopspring/decimal v1.4.0 // indirect
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace (
	github.com/flowgent-labs/flowgent/config => ../config
	github.com/flowgent-labs/flowgent/messager => ../messager
	github.com/flowgent-labs/flowgent/model => ../model
)
