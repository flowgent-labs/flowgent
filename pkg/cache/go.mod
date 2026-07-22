module github.com/flowgent-labs/flowgent/cache

go 1.26.0

// github.com/flowgent-labs/flowgent/common v0.0.0  // UNUSED
// github.com/flowgent-labs/flowgent/model v0.0.0   // UNUSED
require github.com/redis/go-redis/v9 v9.17.0

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
)

replace (
	github.com/flowgent-labs/flowgent/common => ../common
	github.com/flowgent-labs/flowgent/model => ../model
)
