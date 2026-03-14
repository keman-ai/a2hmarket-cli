package main

import (
	"github.com/keman-ai/a2hmarket-cli/internal/oss"
	"github.com/urfave/cli/v2"
)

func fileCommand() *cli.Command {
	return &cli.Command{
		Name:  "file",
		Usage: "Upload local files to OSS (24h temp storage, for A2A attachments)",
		Subcommands: []*cli.Command{
			{
				Name:   "upload",
				Usage:  "Upload a file and get a public URL",
				Action: fileUploadCmd,
				Flags: []cli.Flag{
					configDirFlag(),
					&cli.StringFlag{
						Name:     "file",
						Aliases:  []string{"f"},
						Usage:    "local file path",
						Required: true,
					},
				},
			},
		},
	}
}

func fileUploadCmd(c *cli.Context) error {
	configDir := expandHome(c.String("config-dir"))
	creds, err := loadCreds(configDir)
	if err != nil {
		return err
	}

	// nil allowedMIME means accept all supported types
	fileInfo, err := oss.Upload(creds, c.String("file"), "chatfile", nil)
	if err != nil {
		return outputError("file.upload", err)
	}

	return outputOK("file.upload", map[string]interface{}{
		"url":           fileInfo.URL,
		"object_key":    fileInfo.ObjectKey,
		"file_name":     fileInfo.FileName,
		"file_size":     fileInfo.FileSize,
		"mime_type":     fileInfo.MIMEType,
		"upload_subtype": fileInfo.UploadSubtype,
		"expires_at":    fileInfo.ExpiresAt,
		"expires_hours": fileInfo.ExpiresHours,
		"source":        fileInfo.Source,
		"_note":         "文件链接 24 小时后由 OSS 自动删除，请在有效期内使用",
	})
}
