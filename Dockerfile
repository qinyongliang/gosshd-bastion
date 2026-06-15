FROM golang:1.26-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/gosshd-server ./cmd/gosshd-server
RUN set -eux; \
	build_agent() { \
		goos="$1"; \
		goarch="$2"; \
		goarm="${3:-}"; \
		path_arch="$goarch"; \
		suffix=""; \
		if [ -n "$goarm" ]; then path_arch="${goarch}v${goarm}"; fi; \
		if [ "$goos" = "windows" ]; then suffix=".exe"; fi; \
		mkdir -p "/out/agent/$goos/$path_arch"; \
		if [ -n "$goarm" ]; then \
			CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" GOARM="$goarm" go build -trimpath -ldflags="-s -w" -o "/out/agent/$goos/$path_arch/gosshd-agent$suffix" ./cmd/gosshd-agent; \
		else \
			CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -trimpath -ldflags="-s -w" -o "/out/agent/$goos/$path_arch/gosshd-agent$suffix" ./cmd/gosshd-agent; \
		fi; \
	}; \
	build_agent linux amd64; \
	build_agent linux arm64; \
	build_agent linux 386; \
	build_agent linux arm 6; \
	build_agent linux arm 7; \
	build_agent linux riscv64; \
	build_agent windows amd64; \
	build_agent windows arm64; \
	build_agent windows 386; \
	build_agent darwin amd64; \
	build_agent darwin arm64; \
	build_agent freebsd amd64; \
	build_agent freebsd arm64; \
	build_agent openbsd amd64; \
	build_agent openbsd arm64; \
	build_agent netbsd amd64; \
	build_agent netbsd arm64

FROM alpine:3.22

RUN apk add --no-cache libcap \
	&& adduser -D -H -s /sbin/nologin gosshd
WORKDIR /app
COPY --from=builder /out/gosshd-server /usr/local/bin/gosshd-server
COPY --from=builder /out/agent /app/agent
RUN setcap cap_net_bind_service=+ep /usr/local/bin/gosshd-server

EXPOSE 80 22
USER gosshd

ENTRYPOINT ["/usr/local/bin/gosshd-server"]
CMD ["--http-listen", ":80", "--ssh-listen", ":22", "--agent-path", "/app/agent"]
