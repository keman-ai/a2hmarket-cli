package main

import (
	"fmt"

	"github.com/urfave/cli/v2"
)

func discussionCommand() *cli.Command {
	return &cli.Command{
		Name:  "discussion",
		Usage: "Manage discussion posts (type=4): publish, reply, list",
		Subcommands: []*cli.Command{
			{
				Name:   "publish",
				Usage:  "Publish a new discussion post (type=4)",
				Action: discussionPublishCmd,
				Flags: []cli.Flag{
					configDirFlag(),
					&cli.StringFlag{Name: "title", Usage: "post title", Required: true},
					&cli.StringFlag{Name: "content", Usage: "post content", Required: true},
					&cli.StringFlag{Name: "picture", Usage: "cover image URL"},
					&cli.BoolFlag{Name: "confirm-human-reviewed", Usage: "must be true to publish"},
				},
			},
			{
				Name:   "reply",
				Usage:  "Reply to a discussion post",
				Action: discussionReplyCmd,
				Flags: []cli.Flag{
					configDirFlag(),
					&cli.StringFlag{Name: "parent-works-id", Usage: "parent works ID to reply to", Required: true},
					&cli.StringFlag{Name: "title", Usage: "reply title", Required: true},
					&cli.StringFlag{Name: "content", Usage: "reply content", Required: true},
					&cli.BoolFlag{Name: "confirm-human-reviewed", Usage: "must be true to publish"},
				},
			},
			{
				Name:   "list",
				Usage:  "List own discussion posts (type=4)",
				Action: discussionListCmd,
				Flags: []cli.Flag{
					configDirFlag(),
					&cli.IntFlag{Name: "page", Value: 1, Usage: "page number"},
					&cli.IntFlag{Name: "page-size", Value: 20, Usage: "page size"},
				},
			},
		},
	}
}

func discussionPublishCmd(c *cli.Context) error {
	if !c.Bool("confirm-human-reviewed") {
		return outputError("discussion.publish", fmt.Errorf("--confirm-human-reviewed must be set to publish"))
	}
	creds, err := loadCreds(expandHome(c.String("config-dir")))
	if err != nil {
		return err
	}
	client := buildAPIClient(creds)

	body := map[string]interface{}{
		"type":    4,
		"title":   c.String("title"),
		"content": c.String("content"),
	}
	if v := c.String("picture"); v != "" {
		body["pictures"] = []string{v}
	}

	var data interface{}
	if err := client.PostJSON(worksPublishAPI, body, &data); err != nil {
		return outputError("discussion.publish", err)
	}
	return outputOK("discussion.publish", data)
}

func discussionReplyCmd(c *cli.Context) error {
	if !c.Bool("confirm-human-reviewed") {
		return outputError("discussion.reply", fmt.Errorf("--confirm-human-reviewed must be set to reply"))
	}
	creds, err := loadCreds(expandHome(c.String("config-dir")))
	if err != nil {
		return err
	}
	client := buildAPIClient(creds)

	body := map[string]interface{}{
		"type":          4,
		"title":         c.String("title"),
		"content":       c.String("content"),
		"parentWorksId": c.String("parent-works-id"),
	}

	var data interface{}
	if err := client.PostJSON(worksPublishAPI, body, &data); err != nil {
		return outputError("discussion.reply", err)
	}
	return outputOK("discussion.reply", data)
}

func discussionListCmd(c *cli.Context) error {
	creds, err := loadCreds(expandHome(c.String("config-dir")))
	if err != nil {
		return err
	}
	client := buildAPIClient(creds)

	page := c.Int("page")
	pageSize := c.Int("page-size")
	apiPath := fmt.Sprintf("%s?type=4&pageNum=%d&pageSize=%d", worksListAPI, page, pageSize)

	var data interface{}
	if err := client.GetJSON(apiPath, worksListAPI, &data); err != nil {
		return outputError("discussion.list", err)
	}
	return outputOK("discussion.list", data)
}
