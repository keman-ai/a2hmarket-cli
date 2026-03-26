package main

// address.go — Shipping address management commands.

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/urfave/cli/v2"
)

const (
	addressAPI = "/findu-user/api/v1/user/shipping-address"
)

func addressCommand() *cli.Command {
	return &cli.Command{
		Name:  "address",
		Usage: "Manage shipping addresses (收货地址)",
		Subcommands: []*cli.Command{
			{
				Name:   "list",
				Usage:  "List all shipping addresses",
				Action: addressListCmd,
				Flags:  []cli.Flag{configDirFlag()},
			},
			{
				Name:   "get",
				Usage:  "Get a specific shipping address",
				Action: addressGetCmd,
				Flags: []cli.Flag{
					configDirFlag(),
					&cli.StringFlag{Name: "address-id", Usage: "address ID", Required: true},
				},
			},
			{
				Name:   "add",
				Usage:  "Add a new shipping address",
				Action: addressAddCmd,
				Flags: []cli.Flag{
					configDirFlag(),
					&cli.StringFlag{Name: "receiver-name", Usage: "receiver name (收件人)", Required: true},
					&cli.StringFlag{Name: "phone", Usage: "phone number", Required: true},
					&cli.StringFlag{Name: "province", Usage: "province (省)"},
					&cli.StringFlag{Name: "city", Usage: "city (市)"},
					&cli.StringFlag{Name: "district", Usage: "district (区)"},
					&cli.StringFlag{Name: "detail", Usage: "detail address (详细地址)", Required: true},
					&cli.StringFlag{Name: "postal-code", Usage: "postal code (邮编)"},
					&cli.StringFlag{Name: "label", Usage: "label (标签，如 家/公司)"},
					&cli.BoolFlag{Name: "default", Usage: "set as default address"},
				},
			},
			{
				Name:   "update",
				Usage:  "Update a shipping address",
				Action: addressUpdateCmd,
				Flags: []cli.Flag{
					configDirFlag(),
					&cli.StringFlag{Name: "address-id", Usage: "address ID", Required: true},
					&cli.StringFlag{Name: "receiver-name", Usage: "receiver name"},
					&cli.StringFlag{Name: "phone", Usage: "phone number"},
					&cli.StringFlag{Name: "province", Usage: "province"},
					&cli.StringFlag{Name: "city", Usage: "city"},
					&cli.StringFlag{Name: "district", Usage: "district"},
					&cli.StringFlag{Name: "detail", Usage: "detail address"},
					&cli.StringFlag{Name: "postal-code", Usage: "postal code"},
					&cli.StringFlag{Name: "label", Usage: "label"},
				},
			},
			{
				Name:   "delete",
				Usage:  "Delete a shipping address",
				Action: addressDeleteCmd,
				Flags: []cli.Flag{
					configDirFlag(),
					&cli.StringFlag{Name: "address-id", Usage: "address ID", Required: true},
				},
			},
			{
				Name:   "set-default",
				Usage:  "Set a shipping address as default",
				Action: addressSetDefaultCmd,
				Flags: []cli.Flag{
					configDirFlag(),
					&cli.StringFlag{Name: "address-id", Usage: "address ID", Required: true},
				},
			},
		},
	}
}

func addressListCmd(c *cli.Context) error {
	creds, err := loadCreds(expandHome(c.String("config-dir")))
	if err != nil {
		return err
	}
	client := buildAPIClient(creds)
	var raw json.RawMessage
	if err := client.GetJSON(addressAPI, "", &raw); err != nil {
		return outputError("address.list", err)
	}
	return outputRaw("address.list", raw)
}

func addressGetCmd(c *cli.Context) error {
	creds, err := loadCreds(expandHome(c.String("config-dir")))
	if err != nil {
		return err
	}
	client := buildAPIClient(creds)
	path := fmt.Sprintf("%s/%s", addressAPI, c.String("address-id"))
	var raw json.RawMessage
	if err := client.GetJSON(path, "", &raw); err != nil {
		return outputError("address.get", err)
	}
	return outputRaw("address.get", raw)
}

func addressAddCmd(c *cli.Context) error {
	creds, err := loadCreds(expandHome(c.String("config-dir")))
	if err != nil {
		return err
	}
	client := buildAPIClient(creds)

	body := map[string]interface{}{
		"receiverName":  c.String("receiver-name"),
		"phoneNumber":   c.String("phone"),
		"detailAddress": c.String("detail"),
		"isDefault":     c.Bool("default"),
	}
	if v := c.String("province"); v != "" {
		body["province"] = v
	}
	if v := c.String("city"); v != "" {
		body["city"] = v
	}
	if v := c.String("district"); v != "" {
		body["district"] = v
	}
	if v := c.String("postal-code"); v != "" {
		body["postalCode"] = v
	}
	if v := c.String("label"); v != "" {
		body["label"] = v
	}

	var raw json.RawMessage
	if err := client.PostJSON(addressAPI, body, &raw); err != nil {
		return outputError("address.add", err)
	}
	return outputRaw("address.add", raw)
}

func addressUpdateCmd(c *cli.Context) error {
	creds, err := loadCreds(expandHome(c.String("config-dir")))
	if err != nil {
		return err
	}
	client := buildAPIClient(creds)

	body := map[string]interface{}{}
	for _, pair := range []struct{ flag, key string }{
		{"receiver-name", "receiverName"},
		{"phone", "phoneNumber"},
		{"province", "province"},
		{"city", "city"},
		{"district", "district"},
		{"detail", "detailAddress"},
		{"postal-code", "postalCode"},
		{"label", "label"},
	} {
		if v := c.String(pair.flag); v != "" {
			body[pair.key] = v
		}
	}

	path := fmt.Sprintf("%s/%s", addressAPI, c.String("address-id"))
	var raw json.RawMessage
	if err := client.PutJSON(path, body, &raw); err != nil {
		return outputError("address.update", err)
	}
	return outputOK("address.update", map[string]interface{}{"updated": true})
}

func addressDeleteCmd(c *cli.Context) error {
	creds, err := loadCreds(expandHome(c.String("config-dir")))
	if err != nil {
		return err
	}
	client := buildAPIClient(creds)
	path := fmt.Sprintf("%s/%s", addressAPI, c.String("address-id"))
	if err := client.DeleteJSON(path, nil); err != nil {
		return outputError("address.delete", err)
	}
	return outputOK("address.delete", map[string]interface{}{"deleted": true})
}

func addressSetDefaultCmd(c *cli.Context) error {
	creds, err := loadCreds(expandHome(c.String("config-dir")))
	if err != nil {
		return err
	}
	client := buildAPIClient(creds)
	path := fmt.Sprintf("%s/%s/default", addressAPI, c.String("address-id"))
	var raw json.RawMessage
	if err := client.PutJSON(path, nil, &raw); err != nil {
		return outputError("address.set-default", err)
	}
	return outputOK("address.set-default", map[string]interface{}{"set_default": true})
}

// outputRaw prints a JSON success envelope with raw data.
func outputRaw(action string, raw json.RawMessage) error {
	var data interface{}
	if err := json.Unmarshal(raw, &data); err != nil {
		data = string(raw)
	}
	return outputOK(action, data)
}

// Ensure helpers are accessible
var _ = strings.TrimSpace
