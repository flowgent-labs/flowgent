module github.com/flowgent-labs/flowgent/storage

go 1.26.0

require (
	authguard/adapters/golang v0.0.0
	github.com/flowgent-labs/flowgent/common v0.0.0
	github.com/flowgent-labs/flowgent/migration v0.0.0
	github.com/flowgent-labs/flowgent/model v0.0.0
	github.com/google/uuid v1.6.0
	github.com/jackc/pgx/v5 v5.7.6
	// go.opentelemetry.io/otel v1.38.0       // UNUSED
	// go.opentelemetry.io/otel/trace v1.38.0  // UNUSED
	modernc.org/sqlite v1.50.1
)

require (
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	github.com/shopspring/decimal v1.4.0 // indirect
	github.com/stretchr/testify v1.11.1 // indirect
	golang.org/x/crypto v0.48.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/sys v0.42.0 // indirect
	golang.org/x/text v0.34.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	modernc.org/libc v1.72.3 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
)

replace (
	authguard/adapters/golang => github.com/wl4g/authguard/src/adapters/golang v0.0.0-20260919033855-d0022086809a
	github.com/flowgent-labs/flowgent/common => ../common
	github.com/flowgent-labs/flowgent/migration => ../../migration
	github.com/flowgent-labs/flowgent/model => ../model
)
