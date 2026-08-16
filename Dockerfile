# MineGuard builds offline and ships as a single static binary.
#
# The builder stage compiles with the module cache disabled and the network
# turned off, so the image content is a function of the source tree alone. The
# final stage is scratch: the tool reads and writes plain files and needs no
# shell, no certificates and no user database.
FROM golang:1.22 AS builder

ENV GOTOOLCHAIN=local \
    CGO_ENABLED=0 \
    GOPROXY=off \
    GOFLAGS=-mod=mod

WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal

RUN go vet ./... \
 && go test ./... \
 && go build -trimpath -ldflags "-s -w" -o /out/mineguard ./cmd/mineguard

FROM scratch

COPY --from=builder /out/mineguard /mineguard
COPY examples /examples

ENTRYPOINT ["/mineguard"]
CMD ["--help"]
