FROM node:24-alpine AS web-builder
WORKDIR /src
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml tsconfig*.json vite.config.ts ./
COPY web ./web
RUN corepack enable \
	&& pnpm install --frozen-lockfile \
	&& pnpm run build

FROM golang:1.26-alpine AS builder
ARG VERSION=dev

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web-builder /src/web/dist ./web/dist

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/gosshd-server ./cmd/gosshd-server

FROM alpine:3.22

RUN apk add --no-cache libcap \
	&& adduser -D -H -s /sbin/nologin gosshd
WORKDIR /app
COPY --from=builder /out/gosshd-server /usr/local/bin/gosshd-server
RUN setcap cap_net_bind_service=+ep /usr/local/bin/gosshd-server

EXPOSE 80 22
USER gosshd

ENTRYPOINT ["/usr/local/bin/gosshd-server"]
CMD ["--http-listen", ":80", "--ssh-listen", ":22", "--agent-path", "/app/agent"]
