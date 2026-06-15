FROM golang:1.26-alpine AS builder
ARG VERSION=dev

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

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
