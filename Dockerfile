# syntax=docker/dockerfile:1

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /bin/vanguard ./cmd/vanguard

FROM alpine:3.21
RUN adduser -D -g '' app
USER app
COPY --from=build /bin/vanguard /usr/local/bin/vanguard
ENTRYPOINT ["vanguard"]
