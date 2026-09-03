# syntax=docker/dockerfile:1
FROM golang:1.25-bookworm AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /out/demo-service-a ./test/kind/demo-service-a

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=builder /out/demo-service-a /demo-service-a
USER nonroot:nonroot
ENTRYPOINT ["/demo-service-a"]
