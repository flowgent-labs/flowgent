module github.com/flowgent-labs/flowgent/a2a

go 1.26.0

require (
	github.com/a2aproject/a2a-go v0.3.15
	github.com/flowgent-labs/flowgent/common v0.0.0
	github.com/flowgent-labs/flowgent/config v0.0.0
	github.com/flowgent-labs/flowgent/core v0.0.0
	github.com/flowgent-labs/flowgent/model v0.0.0
)

require (
	github.com/fsnotify/fsnotify v1.8.0 // indirect
	github.com/go-viper/mapstructure/v2 v2.2.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/pelletier/go-toml/v2 v2.2.4 // indirect
	github.com/sagikazarmark/locafero v0.7.0 // indirect
	github.com/shopspring/decimal v1.4.0 // indirect
	github.com/sourcegraph/conc v0.3.0 // indirect
	github.com/spf13/afero v1.12.0 // indirect
	github.com/spf13/cast v1.7.1 // indirect
	github.com/spf13/pflag v1.0.9 // indirect
	github.com/spf13/viper v1.20.1 // indirect
	github.com/subosito/gotenv v1.6.0 // indirect
	github.com/x402-foundation/x402/go v0.0.0-20260529172747-45d81d46e5bd // indirect
	go.uber.org/atomic v1.9.0 // indirect
	go.uber.org/multierr v1.9.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
	golang.org/x/text v0.34.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace (
	github.com/flowgent-labs/flowgent/common => ../common
	github.com/flowgent-labs/flowgent/config => ../config
	github.com/flowgent-labs/flowgent/core => ../core
	github.com/flowgent-labs/flowgent/model => ../model
)
