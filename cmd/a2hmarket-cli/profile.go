package main

import (
	"github.com/keman-ai/a2hmarket-cli/internal/oss"
	"github.com/urfave/cli/v2"
)

const (
	profileAPIPath    = "/findu-user/api/v1/user/profile/public"
	changeRequestPath = "/findu-user/api/v1/user/profile/change-requests"
)

func profileCommand() *cli.Command {
	return &cli.Command{
		Name:  "profile",
		Usage: "View and update agent profile",
		Subcommands: []*cli.Command{
			{
				Name:   "get",
				Usage:  "Get current agent public profile",
				Action: profileGetCmd,
				Flags:  []cli.Flag{configDirFlag()},
			},
			{
				Name:   "upload-qrcode",
				Usage:  "Upload payment QR code image (jpg/png/webp)",
				Action: profileUploadQrcodeCmd,
				Flags: []cli.Flag{
					configDirFlag(),
					&cli.StringFlag{Name: "file", Aliases: []string{"f"}, Usage: "local image path", Required: true},
				},
			},
			{
				Name:   "delete-qrcode",
				Usage:  "Remove payment QR code from profile",
				Action: profileDeleteQrcodeCmd,
				Flags:  []cli.Flag{configDirFlag()},
			},
		},
	}
}

func profileGetCmd(c *cli.Context) error {
	configDir := expandHome(c.String("config-dir"))
	creds, err := loadCreds(configDir)
	if err != nil {
		return err
	}
	client := buildAPIClient(creds)

	var raw map[string]interface{}
	if err := client.GetJSON(profileAPIPath, "", &raw); err != nil {
		return outputError("profile.get", err)
	}

	data := map[string]interface{}{
		"nickname":         raw["nickname"],
		"avatarUrl":        raw["avatarUrl"],
		"bio":              raw["bio"],
		"abilities":        raw["abilities"],
		"realnameStatus":   raw["realnameStatus"],
		"paymentQrcodeUrl": raw["paymentQrcodeUrl"],
	}
	if raw["paymentQrcodeUrl"] == nil || raw["paymentQrcodeUrl"] == "" {
		data["_hint"] = "paymentQrcodeUrl 为空，可通过 `profile upload-qrcode --file <path>` 上传收款码"
	}
	return outputOK("profile.get", data)
}

func profileUploadQrcodeCmd(c *cli.Context) error {
	configDir := expandHome(c.String("config-dir"))
	creds, err := loadCreds(configDir)
	if err != nil {
		return err
	}

	fileInfo, err := oss.Upload(creds, c.String("file"), "profile", oss.ProfileQRCodeMIME)
	if err != nil {
		return outputError("profile.upload-qrcode", err)
	}

	client := buildAPIClient(creds)
	var changeData map[string]interface{}
	if err := client.PostJSON(changeRequestPath, map[string]interface{}{
		"key":   "paymentQrcodeUrl",
		"value": fileInfo.URL,
	}, &changeData); err != nil {
		return outputError("profile.upload-qrcode", err)
	}

	return outputOK("profile.upload-qrcode", map[string]interface{}{
		"paymentQrcodeUrl": fileInfo.URL,
		"objectKey":        fileInfo.ObjectKey,
		"changeRequestId":  changeData["changeRequestId"],
		"changeStatus":     changeData["status"],
	})
}

func profileDeleteQrcodeCmd(c *cli.Context) error {
	configDir := expandHome(c.String("config-dir"))
	creds, err := loadCreds(configDir)
	if err != nil {
		return err
	}
	client := buildAPIClient(creds)

	var changeData map[string]interface{}
	if err := client.PostJSON(changeRequestPath, map[string]interface{}{
		"key":   "paymentQrcodeUrl",
		"value": "",
	}, &changeData); err != nil {
		return outputError("profile.delete-qrcode", err)
	}

	return outputOK("profile.delete-qrcode", map[string]interface{}{
		"paymentQrcodeUrl": nil,
		"changeRequestId":  changeData["changeRequestId"],
		"changeStatus":     changeData["status"],
	})
}

// configDirFlag is a shared flag used by all P1 commands.
func configDirFlag() cli.Flag {
	return &cli.StringFlag{Name: "config-dir", Value: "~/.a2hmarket", Usage: "config directory"}
}
