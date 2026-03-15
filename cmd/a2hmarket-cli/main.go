package main

import (
	"os"
	"path/filepath"

	"github.com/keman-ai/a2hmarket-cli/internal/common"
	"github.com/keman-ai/a2hmarket-cli/internal/config"
	"github.com/urfave/cli/v2"
)

func main() {
	// 加载配置，确保 ~/.a2hmarket 目录存在，并初始化日志文件
	cfg, err := config.Load()
	if err == nil {
		logPath := filepath.Join(cfg.ConfigDir, "a2hmarket-cli.log")
		if initErr := common.InitLogger(logPath); initErr != nil {
			common.Warnf("无法初始化日志文件 %s: %v，将只输出到终端", logPath, initErr)
		}
	}

	app := &cli.App{
		Name:    "a2hmarket-cli",
		Usage:   "a2hmarket CLI — authentication, messaging and listener daemon",
		Version: "0.1.2",
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
			talentCommand(),
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
