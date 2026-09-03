# syntax=docker/dockerfile:1
FROM golang:1.25-bookworm AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/demo-service-b ./test/kind/demo-service-b

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /out/demo-service-b /demo-service-b
USER nonroot:nonroot
ENTRYPOINT ["/demo-service-b"]
