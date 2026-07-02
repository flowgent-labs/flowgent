module github.com/flowgent-labs/flowgent/a2a

go 1.26.0

require (
	github.com/a2aproject/a2a-go v0.3.15
	github.com/flowgent-labs/flowgent/common v0.0.0
	github.com/flowgent-labs/flowgent/config v0.0.0
	github.com/flowgent-labs/flowgent/core v0.0.0
	github.com/flowgent-labs/flowgent/model v0.0.0
)

replace (
	github.com/flowgent-labs/flowgent/common => ../common
	github.com/flowgent-labs/flowgent/config => ../config
	github.com/flowgent-labs/flowgent/core => ../core
	github.com/flowgent-labs/flowgent/model => ../model
)
