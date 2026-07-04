module github.com/flowgent-labs/flowgent/tests/e2e

go 1.26.0

require (
	github.com/flowgent-labs/flowgent/api v0.0.0
	github.com/flowgent-labs/flowgent/config v0.0.0
	github.com/flowgent-labs/flowgent/core v0.0.0
	github.com/flowgent-labs/flowgent/model v0.0.0
	github.com/go-ldap/ldap/v3 v3.4.10
)

require (
	github.com/Azure/go-ntlmssp v0.0.0-20221128193559-754e69321358 // indirect
	github.com/fsnotify/fsnotify v1.8.0 // indirect
	github.com/go-asn1-ber/asn1-ber v1.5.7 // indirect
	github.com/go-viper/mapstructure/v2 v2.2.1 // indirect
	github.com/golang-jwt/jwt/v5 v5.3.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/pelletier/go-toml/v2 v2.2.3 // indirect
	github.com/sagikazarmark/locafero v0.7.0 // indirect
	github.com/shopspring/decimal v1.4.0 // indirect
	github.com/sourcegraph/conc v0.3.0 // indirect
	github.com/spf13/afero v1.12.0 // indirect
	github.com/spf13/cast v1.7.1 // indirect
	github.com/spf13/pflag v1.0.6 // indirect
	github.com/spf13/viper v1.20.1 // indirect
	github.com/subosito/gotenv v1.6.0 // indirect
	golang.org/x/crypto v0.41.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
	golang.org/x/text v0.32.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace (
	github.com/flowgent-labs/flowgent/api => ../../pkg/api
	github.com/flowgent-labs/flowgent/config => ../../pkg/config
	github.com/flowgent-labs/flowgent/core => ../../pkg/core
	github.com/flowgent-labs/flowgent/model => ../../pkg/model
)
