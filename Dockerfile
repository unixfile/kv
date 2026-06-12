FROM golang:1.26-alpine@sha256:7a3e50096189ad57c9f9f865e7e4aa8585ed1585248513dc5cda498e2f41812c AS builder

WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /kv ./cmd/kv

FROM scratch

COPY --from=builder /kv /kv
ENTRYPOINT ["/kv"]
