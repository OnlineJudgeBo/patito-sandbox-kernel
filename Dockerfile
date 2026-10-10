FROM golang:1.24 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY src ./src
RUN CGO_ENABLED=0 go build -trimpath -o /out/patito-judge ./src/cmd/judge

FROM ubuntu:24.04 AS isolate
RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
        build-essential git ca-certificates pkg-config libcap-dev libsystemd-dev \
    && rm -rf /var/lib/apt/lists/*
RUN git clone --depth 1 --branch v2.0 https://github.com/ioi/isolate.git /isolate \
    && make -C /isolate isolate \
    && make -C /isolate install

# Node runs the JavaScript submissions and Dolos, the similarity tool.
FROM node:22-bookworm AS node
RUN npm install -g @dodona/dolos && dolos --version

FROM ubuntu:24.04
# Toolchains of the supported languages: C, C++, Java, Python 3, Go, JavaScript and PSeInt.
RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
        gcc g++ libc6-dev openjdk-17-jdk-headless python3 golang-go libcap2 dos2unix \
    && rm -rf /var/lib/apt/lists/*
COPY --from=node /usr/local/bin /usr/local/bin
COPY --from=node /usr/local/lib/node_modules /usr/local/lib/node_modules
COPY --from=isolate /usr/local/bin/isolate /usr/local/bin/isolate
COPY --from=isolate /usr/local/etc/isolate /usr/local/etc/isolate
# Without systemd there is no cgroup keeper: the entrypoint creates this group itself.
RUN chmod 4755 /usr/local/bin/isolate \
    && sed -i 's|^cg_root = .*|cg_root = /sys/fs/cgroup/isolate|' /usr/local/etc/isolate
COPY --from=build /out/patito-judge /usr/local/bin/patito-judge
COPY --chmod=755 pseint/pseint pseint/psexport /usr/bin/
COPY docker/entrypoint.sh /usr/local/bin/entrypoint.sh

ENV JUDGE_HOME=/home/judge
ENV CONF=/home/judge/etc/judge.json
ENV LANG=C.UTF-8
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
