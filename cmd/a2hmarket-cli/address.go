package main

import (
	"fmt"
	"strings"

	"github.com/urfave/cli/v2"
)

const (
	addressAPIPath = "/findu-user/api/v1/user/shipping-address"
)

func addressCommand() *cli.Command {
	return &cli.Command{
		Name:  "address",
		Usage: "Manage shipping addresses (list, create, delete, set-default)",
		Subcommands: []*cli.Command{
			{
				Name:   "list",
				Usage:  "List all shipping addresses",
				Action: addressListCmd,
				Flags:  []cli.Flag{configDirFlag()},
			},
			{
				Name:   "create",
				Usage:  "Create a new shipping address",
				Action: addressCreateCmd,
				Flags: []cli.Flag{
					configDirFlag(),
					&cli.StringFlag{Name: "receiver-name", Usage: "receiver name", Required: true},
					&cli.StringFlag{Name: "phone", Usage: "phone number", Required: true},
					&cli.StringFlag{Name: "province", Usage: "province (e.g. 广东省)", Required: true},
					&cli.StringFlag{Name: "city", Usage: "city (e.g. 深圳市)", Required: true},
					&cli.StringFlag{Name: "district", Usage: "district (e.g. 南山区)", Required: true},
					&cli.StringFlag{Name: "detail", Usage: "detailed street address", Required: true},
					&cli.StringFlag{Name: "postal-code", Usage: "postal code (optional)"},
					&cli.StringFlag{Name: "label", Usage: "label (e.g. 家, 公司)"},
				},
			},
			{
				Name:   "delete",
				Usage:  "Delete a shipping address",
				Action: addressDeleteCmd,
				Flags: []cli.Flag{
					configDirFlag(),
					&cli.StringFlag{Name: "address-id", Usage: "address ID to delete", Required: true},
				},
			},
			{
				Name:   "set-default",
				Usage:  "Set a shipping address as default",
				Action: addressSetDefaultCmd,
				Flags: []cli.Flag{
					configDirFlag(),
					&cli.StringFlag{Name: "address-id", Usage: "address ID to set as default", Required: true},
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

	var data interface{}
	if err := client.GetJSON(addressAPIPath, addressAPIPath, &data); err != nil {
		return outputError("address.list", err)
	}
	return outputOK("address.list", data)
}

func addressCreateCmd(c *cli.Context) error {
	creds, err := loadCreds(expandHome(c.String("config-dir")))
	if err != nil {
		return err
	}
	client := buildAPIClient(creds)

	body := map[string]interface{}{
		"receiverName":  c.String("receiver-name"),
		"phoneNumber":   c.String("phone"),
		"province":      c.String("province"),
		"city":          c.String("city"),
		"district":      c.String("district"),
		"detailAddress": c.String("detail"),
	}
	if v := c.String("postal-code"); v != "" {
		body["postalCode"] = v
	}
	if v := c.String("label"); v != "" {
		body["label"] = v
	}

	var data interface{}
	if err := client.PostJSON(addressAPIPath, body, &data); err != nil {
		return outputError("address.create", err)
	}
	return outputOK("address.create", data)
}

func addressDeleteCmd(c *cli.Context) error {
	creds, err := loadCreds(expandHome(c.String("config-dir")))
	if err != nil {
		return err
	}
	client := buildAPIClient(creds)

	addressID := strings.TrimSpace(c.String("address-id"))
	apiPath := fmt.Sprintf("%s/%s", addressAPIPath, addressID)

	var data interface{}
	if err := client.DeleteJSON(apiPath, &data); err != nil {
		return outputError("address.delete", err)
	}
	return outputOK("address.delete", data)
}

func addressSetDefaultCmd(c *cli.Context) error {
	creds, err := loadCreds(expandHome(c.String("config-dir")))
	if err != nil {
		return err
	}
	client := buildAPIClient(creds)

	addressID := strings.TrimSpace(c.String("address-id"))
	apiPath := fmt.Sprintf("%s/%s/default", addressAPIPath, addressID)

	var data interface{}
	if err := client.PutJSON(apiPath, nil, &data); err != nil {
		return outputError("address.set-default", err)
	}
	return outputOK("address.set-default", data)
}
