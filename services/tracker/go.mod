module github.com/peergo/peergo/services/tracker

go 1.26.0

require (
	github.com/nats-io/nats.go v1.52.0
	github.com/peergo/peergo/contracts/go v0.0.0
	github.com/peergo/peergo/libraries/natsauth v0.0.0
	github.com/prometheus/client_golang v1.24.1
)

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/klauspost/compress v1.19.1 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/nats-io/nkeys v0.4.15 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.70.1 // indirect
	github.com/prometheus/procfs v0.21.1 // indirect
	golang.org/x/crypto v0.49.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

replace github.com/peergo/peergo/contracts/go => ../../contracts/go

replace github.com/peergo/peergo/libraries/natsauth => ../../libraries/natsauth
