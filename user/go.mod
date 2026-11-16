module avito-ai-buddy/user

go 1.27.0

require (
	avito-ai-buddy/pkg v0.0.0
	github.com/caarlos0/env/v10 v10.0.0
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.29.0
	github.com/jackc/pgx/v5 v5.13.0
	github.com/pressly/goose/v3 v3.27.0
	go.uber.org/zap v1.28.0
	golang.org/x/sync v0.22.0
	google.golang.org/grpc v1.80.0
	google.golang.org/protobuf v1.36.11
)

require (
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.3 // indirect
	github.com/mfridman/interpolate v0.0.2 // indirect
	github.com/sethvargo/go-retry v0.3.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/net v0.50.0 // indirect
	golang.org/x/sys v0.41.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260414002931-afd174a4e478 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260420184626-e10c466a9529 // indirect
)

replace avito-ai-buddy/pkg => ../pkg
