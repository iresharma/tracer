# syntax=docker/dockerfile:1
FROM golang:1.25-bookworm AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/httpgen ./test/devcluster/httpgen

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /out/httpgen /httpgen
USER nonroot:nonroot
ENTRYPOINT ["/httpgen"]
