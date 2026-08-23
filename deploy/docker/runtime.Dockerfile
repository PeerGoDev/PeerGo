# syntax=docker/dockerfile:1.7

FROM golang:1.26-alpine3.22 AS build

WORKDIR /src
COPY go.work go.work.sum ./
COPY contracts/go ./contracts/go
COPY libraries ./libraries
COPY services ./services
COPY tools/devtools ./tools/devtools
COPY tools/migrator ./tools/migrator
COPY tools/traffic-corpus ./tools/traffic-corpus

RUN mkdir -p /out && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/core-api ./services/core/cmd/api && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/core-admin ./services/core/cmd/admin && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/core-tracker-rate-policy ./services/core/cmd/tracker-rate-policy && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/core-audit-worker ./services/core/cmd/worker && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/core-control-projector ./services/core/cmd/projector && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/core-snapshot-publisher ./services/core/cmd/snapshot-builder && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/core-policy-worker ./services/core/cmd/promotion-worker && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/core-image-derivative-worker ./services/core/cmd/image-derivative-worker && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/core-seeding-reward-worker ./services/core/cmd/seeding-reward-worker && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/core-seeding-reward-compensation-preview ./services/core/cmd/seeding-reward-compensation-preview && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/core-seeding-reward-compensation-apply ./services/core/cmd/seeding-reward-compensation-apply && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/core-contribution-experience-worker ./services/core/cmd/contribution-experience-worker && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/core-progression-level-worker ./services/core/cmd/progression-level-worker && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/core-traffic-projector ./services/core/cmd/traffic-projector && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/core-storage-maintenance ./services/core/cmd/storage-maintenance && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/core-seeding-evidence-projector ./services/core/cmd/seeding-evidence-projector && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/core-hnr-projector ./services/core/cmd/hnr-projector && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/core-swarm-projector ./services/core/cmd/swarm-projector && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/core-traffic-consumer-init ./services/core/cmd/traffic-consumer-init && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/core-seeding-evidence-consumer-init ./services/core/cmd/seeding-evidence-consumer-init && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/core-hnr-consumer-init ./services/core/cmd/hnr-consumer-init && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/core-swarm-consumer-init ./services/core/cmd/swarm-consumer-init && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/peergo-preflight ./services/core/cmd/preflight && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/vault-api ./services/privacy-vault/cmd/api && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/email-relay ./services/email-relay/cmd/email-relay && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/audit-sink ./services/audit-sink/cmd/audit-sink && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/tracker ./services/tracker/cmd/tracker && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/tracker-stream-init ./services/tracker/cmd/jetstream-init && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/tracker-swarm-stream-init ./services/tracker/cmd/swarm-stream-init && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/tracker-snapshot-inspect ./services/tracker/cmd/snapshot-inspect && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/settlement-ingest ./services/settlement/cmd/settlement && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/settlement-consumer-init ./services/settlement/cmd/consumer-init && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/settlement-seeding-snapshot-projector ./services/settlement/cmd/seeding-snapshot-projector && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/settlement-seeding-snapshot-consumer-init ./services/settlement/cmd/seeding-snapshot-consumer-init && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/settlement-seeding-evidence-worker ./services/settlement/cmd/seeding-evidence-worker && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/settlement-policy-worker ./services/settlement/cmd/policy-worker && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/settlement-storage-maintenance ./services/settlement/cmd/storage-maintenance && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/settlement-control-api ./services/settlement/cmd/promotion-control-api && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/settlement-traffic-stream-init ./services/settlement/cmd/traffic-stream-init && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/settlement-traffic-dispatcher ./services/settlement/cmd/traffic-dispatcher && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/settlement-seeding-evidence-stream-init ./services/settlement/cmd/seeding-evidence-stream-init && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/settlement-seeding-evidence-dispatcher ./services/settlement/cmd/seeding-evidence-dispatcher && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/settlement-hnr-worker ./services/settlement/cmd/hnr-worker && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/settlement-hnr-work-reconcile ./services/settlement/cmd/hnr-work-reconcile && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/settlement-hnr-stream-init ./services/settlement/cmd/hnr-stream-init && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/settlement-hnr-dispatcher ./services/settlement/cmd/hnr-dispatcher && \
    GOWORK=off CGO_ENABLED=0 go -C tools/migrator build -trimpath -ldflags='-s -w' -o /out/goose github.com/pressly/goose/v3/cmd/goose

FROM alpine:3.22

RUN apk add --no-cache ca-certificates curl tzdata vips-tools && \
    addgroup -S -g 10001 peergo && \
    adduser -S -D -H -u 10001 -G peergo peergo && \
    mkdir -p /var/lib/peergo/objects /var/lib/peergo/tracker /var/lib/peergo/audit /var/lib/peergo/image-derivative-tmp && \
    chown -R peergo:peergo /var/lib/peergo

COPY --from=build /out/ /usr/local/bin/
COPY db/core/migrations /migrations/core
COPY db/vault/migrations /migrations/vault
COPY db/tracker/migrations /migrations/tracker

USER peergo:peergo
WORKDIR /var/lib/peergo
