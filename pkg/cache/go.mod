module github.com/flowgent-labs/flowgent/cache

go 1.26.0

require (
	// github.com/flowgent-labs/flowgent/common v0.0.0  // UNUSED
	// github.com/flowgent-labs/flowgent/model v0.0.0   // UNUSED
	github.com/redis/go-redis/v9 v9.17.0
)

replace (
	github.com/flowgent-labs/flowgent/common => ../common
	github.com/flowgent-labs/flowgent/model => ../model
)
