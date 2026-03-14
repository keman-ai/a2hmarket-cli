package main

import (
	"os"

	"github.com/keman-ai/a2hmarket-cli/internal/common"
	"github.com/urfave/cli/v2"
)

func main() {
	app := &cli.App{
		Name:    "a2hmarket-cli",
		Usage:   "a2hmarket CLI — authentication, messaging and listener daemon",
		Version: "0.3.0",
		Commands: []*cli.Command{
			genAuthCodeCommand(),
			getAuthCommand(),
			sendCommand(),
			listenCommand(),
			listenerCommand(),
			inboxCommand(),
			profileCommand(),
			syncCommand(),
			worksCommand(),
			orderCommand(),
			fileCommand(),
			statusCommand(),
			apiCallCommand(),
		},
	}

	if err := app.Run(os.Args); err != nil {
		common.Error(err)
		os.Exit(1)
	}
}
