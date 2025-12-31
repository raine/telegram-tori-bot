# syntax=docker/dockerfile:1

FROM golang:1.25-alpine3.21 as builder

WORKDIR /app

COPY go.mod go.sum ./

RUN apk add --update-cache git
RUN go mod download

COPY *.go ./
COPY ./internal ./internal

ARG VERSION=dev
ARG BUILD_TIME=unknown

RUN CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="-w -s -X github.com/raine/telegram-tori-bot/internal/bot.Version=${VERSION} -X 'github.com/raine/telegram-tori-bot/internal/bot.BuildTime=${BUILD_TIME}'" .

FROM scratch

WORKDIR /app

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /app/telegram-tori-bot /usr/bin/

ENTRYPOINT ["telegram-tori-bot"]
