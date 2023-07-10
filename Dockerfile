# syntax=docker/dockerfile:1

FROM golang:1.19-alpine as builder

WORKDIR /app 

COPY go.mod go.sum ./

RUN apk add --update-cache git
RUN go mod download

COPY *.go ./
COPY ./tori ./tori

RUN CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="-w -s" .

FROM scratch

WORKDIR /app

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /app/telegram-tori-bot /usr/bin/

ENTRYPOINT ["telegram-tori-bot"]
