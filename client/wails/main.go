package main

import (
	"embed"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	windowsOptions "github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:            app.windowTitle,
		Width:            app.windowWidth,
		Height:           app.windowHeight,
		MinWidth:         980,
		MinHeight:        640,
		DisableResize:    false,
		Fullscreen:       false,
		Frameless:        false,
		StartHidden:      false,
		BackgroundColour: &options.RGBA{R: 10, G: 16, B: 26, A: 255},
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		Windows: &windowsOptions.Options{
			WebviewUserDataPath: app.webviewDataPath(),
		},
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		Bind: []interface{}{
			app,
		},
	})
	if err != nil {
		println("GOSSHD client failed:", err.Error())
	}
}
