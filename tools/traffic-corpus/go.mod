module github.com/peergo/peergo/tools/traffic-corpus

go 1.26.0

require (
	github.com/jackc/pgx/v5 v5.10.0
	github.com/peergo/peergo/contracts/go v0.0.0
	github.com/peergo/peergo/services/tracker v0.0.0
)

require (
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/klauspost/compress v1.19.1 // indirect
	github.com/nats-io/nats.go v1.52.0 // indirect
	github.com/nats-io/nkeys v0.4.15 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	github.com/peergo/peergo/libraries/natsauth v0.0.0 // indirect
	golang.org/x/crypto v0.49.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.35.0 // indirect
)

replace github.com/peergo/peergo/services/tracker => ../../services/tracker

replace github.com/peergo/peergo/contracts/go => ../../contracts/go

replace github.com/peergo/peergo/libraries/natsauth => ../../libraries/natsauth
