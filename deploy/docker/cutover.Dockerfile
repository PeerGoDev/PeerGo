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
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/legacy-torrents ./services/core/cmd/legacy-torrents && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/legacy-seedboxes ./services/core/cmd/legacy-seedboxes && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/legacy-user-state ./services/core/cmd/legacy-user-state && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/legacy-user-administration ./services/core/cmd/legacy-user-administration && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/legacy-personal-state ./services/core/cmd/legacy-personal-state && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/legacy-medals ./services/core/cmd/legacy-medals && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/legacy-media ./services/core/cmd/legacy-media && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/image-derivative-worker ./services/core/cmd/image-derivative-worker && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/core-control-projector ./services/core/cmd/projector && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/core-snapshot-publisher ./services/core/cmd/snapshot-builder && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/core-policy-worker ./services/core/cmd/promotion-worker && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/legacy-users ./services/privacy-vault/cmd/legacy-users && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/settlement-policy-timeline-append ./services/settlement/cmd/policy-timeline-append && \
    CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/settlement-control-api ./services/settlement/cmd/promotion-control-api

FROM alpine:3.22

RUN apk add --no-cache \
        bash \
        ca-certificates \
        curl \
        gzip \
        jq \
        postgresql17-client \
        tzdata \
        unzip \
        vips-tools && \
    mkdir -p \
        /cutover/input \
        /cutover/output \
        /usr/local/libexec/peergo \
        /usr/local/share/peergo \
        /var/lib/peergo/objects \
        /var/lib/peergo/tracker \
        /var/lib/peergo/image-derivative-tmp

COPY --from=build /out/ /usr/local/bin/
COPY scripts/migrate-ptyes.sh /usr/local/libexec/peergo/migrate-ptyes.sh
COPY scripts/rousi-cutover-container.sh /usr/local/bin/rousi-cutover-container
COPY examples/settlement/policy-snapshot.peergo-v1-normal.json /usr/local/share/peergo/policy-snapshot.peergo-v1-normal.json

RUN chmod 0555 \
        /usr/local/bin/rousi-cutover-container \
        /usr/local/libexec/peergo/migrate-ptyes.sh

USER root
WORKDIR /cutover
