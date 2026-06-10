package main

import (
	"log"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

const singleInstanceID = "com.lwylink.client.windows"

func main() {
	diagnosticLogf("main start args=%q version=%s", os.Args, AppVersion)
	if handled, err := runUpdaterFromArgs(os.Args); handled {
		if err != nil {
			diagnosticLogf("updater failed: %v", err)
			log.Fatal(err)
		}
		diagnosticLogf("updater handled")
		return
	}

	app := NewApp()
	diagnosticLogf("wails run start")
	if err := wails.Run(&options.App{
		Title:     "LwyLink",
		Width:     1240,
		Height:    780,
		MinWidth:  1040,
		MinHeight: 680,
		Frameless: true,
		BackgroundColour: &options.RGBA{
			R: 15,
			G: 23,
			B: 24,
			A: 255,
		},
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		Windows: &windows.Options{
			DisableFramelessWindowDecorations: false,
		},
		OnStartup:     app.startup,
		OnShutdown:    app.shutdown,
		OnBeforeClose: app.beforeClose,
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: singleInstanceID,
			OnSecondInstanceLaunch: func(_ options.SecondInstanceData) {
				app.showMainWindow()
			},
		},
		Bind: []any{
			app,
		},
	}); err != nil {
		diagnosticLogf("wails run failed: %v", err)
		log.Fatal(err)
	}
	diagnosticLogf("main exit")
}
