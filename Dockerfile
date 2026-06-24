FROM golang:1.24-bookworm AS builder

ARG TARGETARCH
ENV DEBIAN_FRONTEND=noninteractive

WORKDIR /src

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        clang \
        llvm \
        libbpf-dev \
        linux-libc-dev \
        make \
    && case "${TARGETARCH:-amd64}" in \
        amd64) ln -s /usr/include/x86_64-linux-gnu/asm /usr/include/asm ;; \
        arm64) ln -s /usr/include/aarch64-linux-gnu/asm /usr/include/asm ;; \
        arm) ln -s /usr/include/arm-linux-gnueabihf/asm /usr/include/asm ;; \
        *) echo "unsupported TARGETARCH=${TARGETARCH}" >&2; exit 1 ;; \
    esac \
    && rm -rf /var/lib/apt/lists/*

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN make build

FROM debian:bookworm-slim

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates \
        tzdata \
    && rm -rf /var/lib/apt/lists/*

COPY --from=builder /src/bin/ssh-login-alert /usr/local/bin/ssh-login-alert

ENTRYPOINT ["/usr/local/bin/ssh-login-alert"]
