FROM golang:1.22-bookworm AS builder

# Install build dependencies for CGo
RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential && rm -rf /var/lib/apt/lists/*

WORKDIR /src

# Download CozoDB static library for Linux x86_64
RUN mkdir -p libs && \
    curl -L -o libs/libcozo_c.a.gz \
    "https://github.com/cozodb/cozo/releases/download/v0.7.6/libcozo_c-0.7.6-x86_64-unknown-linux-gnu.a.gz" && \
    gunzip libs/libcozo_c.a.gz

COPY go.mod go.sum* ./
RUN go mod download 2>/dev/null || true
COPY . .
RUN go mod tidy
RUN CGO_LDFLAGS="-L/src/libs" go build -trimpath -ldflags="-s -w" -o /legacylens ./cmd/legacylens

FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates git libstdc++6 && \
    rm -rf /var/lib/apt/lists/*

RUN useradd -m -s /bin/bash app
USER app
WORKDIR /home/app

COPY --from=builder /legacylens /usr/local/bin/legacylens

ENV LEGACYLENS_BACKEND=sqlite \
    LEGACYLENS_SERVE=1 \
    PORT=8080

EXPOSE 8080

ENTRYPOINT ["legacylens"]
