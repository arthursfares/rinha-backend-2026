# syntax=docker/dockerfile:1

# build
FROM --platform=linux/amd64 golang:1.25-alpine AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN go run ./cmd/preprocess

ENV CGO_ENABLED=0 GOOS=linux GOARCH=amd64
RUN go build -tags=sonic -ldflags="-s -w" -o /out/fraud-api ./cmd/api

# runtime
FROM --platform=linux/amd64 gcr.io/distroless/static-debian12:nonroot
WORKDIR /app

COPY --from=builder /out/fraud-api /app/fraud-api
COPY --from=builder /src/references.bin /app/references.bin

EXPOSE 9999
USER nonroot:nonroot

ENTRYPOINT ["/app/fraud-api"]

