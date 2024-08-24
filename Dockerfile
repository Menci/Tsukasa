ARG ALPINE_VERSION=3.20
ARG GOLANG_VERSION=1.22.5

FROM docker.io/library/golang:${GOLANG_VERSION}-alpine AS builder
COPY . /build
RUN cd /build && go build -o tsukasa

FROM alpine:$ALPINE_VERSION
COPY --from=builder /build/tsukasa /usr/local/bin/tsukasa
ENTRYPOINT ["/usr/local/bin/tsukasa"]
