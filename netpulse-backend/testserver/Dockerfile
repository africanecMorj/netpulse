FROM golang:1.25-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/netpulse-agent ./cmd/agent \
 && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/testserver-control ./cmd/testserver-control

FROM ubuntu:24.04
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates procps stress-ng curl \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --system --uid 10001 --create-home netpulse
COPY --from=builder /out/netpulse-agent /usr/local/bin/netpulse-agent
COPY --from=builder /out/testserver-control /usr/local/bin/testserver-control
USER netpulse
EXPOSE 8090
HEALTHCHECK --interval=5s --timeout=3s --retries=10 CMD curl -fsS http://127.0.0.1:8090/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/testserver-control"]
