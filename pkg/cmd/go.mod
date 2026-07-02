module github.com/flowgent-labs/flowgent/cmd

go 1.26.0

require (
	github.com/a2aproject/a2a-go v0.3.15
	github.com/flowgent-labs/flowgent/a2a v0.0.0
	github.com/flowgent-labs/flowgent/api v0.0.0
	// github.com/flowgent-labs/flowgent/cache v0.0.0  // UNUSED
	github.com/flowgent-labs/flowgent/common v0.0.0
	github.com/flowgent-labs/flowgent/config v0.0.0
	github.com/flowgent-labs/flowgent/controller v0.0.0
	github.com/flowgent-labs/flowgent/core v0.0.0
	github.com/flowgent-labs/flowgent/messager v0.0.0
	github.com/flowgent-labs/flowgent/model v0.0.0
	github.com/flowgent-labs/flowgent/notifier v0.0.0
	github.com/flowgent-labs/flowgent/sandbox v0.0.0
	github.com/flowgent-labs/flowgent/store v0.0.0
	github.com/flowgent-labs/flowgent/wallet v0.0.0
	github.com/spf13/cobra v1.10.1
)

require (
	github.com/mattn/go-runewidth v0.0.3 // indirect
	github.com/peterh/liner v1.2.2 // indirect
	golang.org/x/sys v0.42.0 // indirect
)

replace (
	github.com/flowgent-labs/flowgent/a2a => ../a2a
	github.com/flowgent-labs/flowgent/api => ../api
	github.com/flowgent-labs/flowgent/cache => ../cache
	github.com/flowgent-labs/flowgent/common => ../common
	github.com/flowgent-labs/flowgent/config => ../config
	github.com/flowgent-labs/flowgent/controller => ../controller
	github.com/flowgent-labs/flowgent/core => ../core
	github.com/flowgent-labs/flowgent/messager => ../messager
	github.com/flowgent-labs/flowgent/model => ../model
	github.com/flowgent-labs/flowgent/notifier => ../notifier
	github.com/flowgent-labs/flowgent/sandbox => ../sandbox
	github.com/flowgent-labs/flowgent/store => ../store
	github.com/flowgent-labs/flowgent/wallet => ../wallet
)
