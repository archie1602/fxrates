# syntax=docker/dockerfile:1.24.0

FROM golang:1.26.5-alpine3.24 AS build

ENV GOTOOLCHAIN=local
WORKDIR /src

COPY ["go.mod", "go.sum", "./"]
RUN go mod download

COPY ["cmd", "./cmd"]
COPY ["internal", "./internal"]

RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/fxrates \
    ./cmd/api

FROM alpine:3.24 AS runtime

RUN apk add --no-cache ca-certificates \
    && addgroup -S app \
    && adduser -S -G app app

COPY --from=build --chown=app:app /out/fxrates /usr/local/bin/fxrates

USER app
EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/fxrates"]
