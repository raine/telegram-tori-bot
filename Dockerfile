# syntax=docker/dockerfile:1

FROM golang:1.25-alpine3.21 as builder

WORKDIR /app

COPY go.mod go.sum ./

RUN apk add --update-cache git
RUN go mod download

COPY *.go ./
COPY ./tori ./tori
COPY ./llm ./llm
COPY ./storage ./storage

ARG VERSION=dev
ARG BUILD_TIME=unknown

RUN CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="-w -s -X main.Version=${VERSION} -X 'main.BuildTime=${BUILD_TIME}'" .

FROM scratch

WORKDIR /app

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /app/telegram-tori-bot /usr/bin/

ENTRYPOINT ["telegram-tori-bot"]
