module github.com/flowgent-labs/flowgent/sandbox

go 1.26.0

require (
	github.com/flowgent-labs/flowgent/messaging v0.0.0
	github.com/flowgent-labs/flowgent/model v0.0.0
	golang.org/x/sys v0.42.0
)

replace (
	github.com/flowgent-labs/flowgent/messaging => ../messaging
	github.com/flowgent-labs/flowgent/model => ../model
)
