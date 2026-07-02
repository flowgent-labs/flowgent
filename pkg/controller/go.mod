module github.com/flowgent-labs/flowgent/controller

go 1.26.0

require (
	github.com/flowgent-labs/flowgent/common v0.0.0
	github.com/flowgent-labs/flowgent/config v0.0.0
	github.com/flowgent-labs/flowgent/core v0.0.0
	github.com/flowgent-labs/flowgent/model v0.0.0
	k8s.io/api v0.34.2
	k8s.io/apimachinery v0.34.2
	k8s.io/client-go v0.34.2
)

replace (
	github.com/flowgent-labs/flowgent/common => ../common
	github.com/flowgent-labs/flowgent/config => ../config
	github.com/flowgent-labs/flowgent/core => ../core
	github.com/flowgent-labs/flowgent/model => ../model
)
