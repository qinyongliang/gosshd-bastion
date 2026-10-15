// gosshd-desktop-mcp is a standalone local MCP host for the independent module.
// The remote Bastion uses exactly the same module through an adapter.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"log"
	"net"
	"os"
	"os/signal"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/qinyongliang/gosshd-bastion/internal/desktop"
	desktopmcp "github.com/qinyongliang/gosshd-bastion/internal/desktop/mcptools"
)

var version = "dev"

func main() {
	allowRead := flag.Bool("allow-read", false, "explicitly allow reading this user desktop or unlocked Android shell/paired accessibility screen")
	allowControl := flag.Bool("allow-control", false, "explicitly allow controlling this user desktop or unlocked Android shell/paired accessibility screen (also requires allow-read)")
	helper := flag.String("desktop-helper", "", "absolute native helper executable path (macOS requires a path unless an adjacent app bundle exists; leave empty on Android)")
	backend := flag.String("android-backend", "auto", "Android backend: auto, shell or accessibility")
	tokenFile := flag.String("android-bridge-token-file", "", "private Android companion pairing token file")
	flag.Parse()
	if *allowControl && !*allowRead {
		log.Fatal("allow-control requires allow-read")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	service := desktop.NewService(desktop.Adapter{
		Authorize: func(_ context.Context, actor, target string, control bool) error {
			if actor != "local" || target != "local" || !*allowRead || (control && !*allowControl) {
				return errors.New("local desktop permission not granted")
			}
			return nil
		},
		Dial: func(_ context.Context, _ string) (io.ReadWriteCloser, error) {
			client, helperStream := net.Pipe()
			go func() {
				_ = desktop.Serve(ctx, helperStream, desktop.Config{Enabled: true, HelperPath: *helper, AndroidBackend: *backend, AndroidBridgeTokenFile: *tokenFile})
			}()
			return client, nil
		},
		Audit: func(_ context.Context, event desktop.Event) error { return json.NewEncoder(os.Stderr).Encode(event) },
	})
	defer service.Close()
	server := mcp.NewServer(&mcp.Implementation{Name: "gosshd-desktop", Version: version}, &mcp.ServerOptions{Instructions: "Use desktop_session_open with target_id local. " + desktopmcp.OperatorInstructions})
	desktopmcp.RegisterMCP(server, service, "local")
	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil && ctx.Err() == nil {
		log.Fatal(err)
	}
}
