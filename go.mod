module github.com/corygyarmathy/typist

go 1.26.4

// Dependencies will be added as each phase is implemented
// Expected core set after phase 4:
//   - github.com/oapi-codegen/runtime
//   - github.com/golang-jwt/jwt/v5
//   - github.com/prometheus/client_golang

require (
	github.com/google/uuid v1.6.0
	github.com/jackc/pgx/v5 v5.10.0
	github.com/oapi-codegen/runtime v1.5.0
	github.com/pressly/goose/v3 v3.27.2
)

require (
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/mfridman/interpolate v0.0.2 // indirect
	github.com/sethvargo/go-retry v0.3.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/sync v0.21.0 // indirect
	golang.org/x/sys v0.46.0 // indirect
	golang.org/x/text v0.38.0 // indirect
)
