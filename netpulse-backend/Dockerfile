FROM golang:1.25-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/netpulse-api ./cmd/api

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata
COPY --from=builder /out/netpulse-api /usr/local/bin/netpulse-api
EXPOSE 8080
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/netpulse-api"]
