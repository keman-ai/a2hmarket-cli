package main

import (
	"fmt"
	"strings"

	"github.com/urfave/cli/v2"
)

const (
	worksSearchAPI  = "/findu-match/api/v1/inner/match/works_search"
	worksPublishAPI = "/findu-user/api/v1/user/works/change-requests"
	worksListAPI    = "/findu-user/api/v1/user/works/public"
	worksDeleteAPI  = "/findu-user/api/v1/user/works"
)

func worksCommand() *cli.Command {
	return &cli.Command{
		Name:  "works",
		Usage: "Search, publish, update, delete and list works posts",
		Subcommands: []*cli.Command{
			{
				Name:   "search",
				Usage:  "Search works by keyword",
				Action: worksSearchCmd,
				Flags: []cli.Flag{
					configDirFlag(),
					&cli.StringFlag{Name: "keyword", Aliases: []string{"k"}, Usage: "search keyword"},
					&cli.IntFlag{Name: "type", Usage: "2=demand 3=service"},
					&cli.StringFlag{Name: "city", Usage: "filter by city"},
					&cli.IntFlag{Name: "page", Value: 1, Usage: "page number (1-based)"},
					&cli.IntFlag{Name: "page-size", Value: 10, Usage: "page size"},
				},
			},
			{
				Name:   "publish",
				Usage:  "Publish a new works post",
				Action: worksPublishCmd,
				Flags: []cli.Flag{
					configDirFlag(),
					&cli.IntFlag{Name: "type", Usage: "2=demand 3=service", Required: true},
					&cli.StringFlag{Name: "title", Usage: "post title", Required: true},
					&cli.StringFlag{Name: "content", Usage: "post content", Required: true},
					&cli.StringFlag{Name: "expected-price", Usage: "expected price text (wrapped into extendInfo)"},
					&cli.StringFlag{Name: "service-method", Usage: "online|offline (wrapped into extendInfo)"},
					&cli.StringFlag{Name: "service-location", Usage: "location (wrapped into extendInfo)"},
					&cli.StringFlag{Name: "picture", Usage: "cover image URL"},
					&cli.BoolFlag{Name: "confirm-human-reviewed", Usage: "must be true to publish"},
				},
			},
			{
				Name:   "update",
				Usage:  "Update an existing works post (submit change request)",
				Action: worksUpdateCmd,
				Flags: []cli.Flag{
					configDirFlag(),
					&cli.StringFlag{Name: "works-id", Usage: "works ID to update", Required: true},
					&cli.IntFlag{Name: "type", Usage: "2=demand 3=service", Required: true},
					&cli.StringFlag{Name: "title", Usage: "post title", Required: true},
					&cli.StringFlag{Name: "content", Usage: "post content"},
					&cli.StringFlag{Name: "expected-price", Usage: "expected price text (wrapped into extendInfo)"},
					&cli.StringFlag{Name: "service-method", Usage: "online|offline (wrapped into extendInfo)"},
					&cli.StringFlag{Name: "service-location", Usage: "location (wrapped into extendInfo)"},
					&cli.StringFlag{Name: "picture", Usage: "cover image URL"},
					&cli.BoolFlag{Name: "confirm-human-reviewed", Usage: "must be true to update"},
				},
			},
			{
				Name:   "delete",
				Usage:  "Delete a works post",
				Action: worksDeleteCmd,
				Flags: []cli.Flag{
					configDirFlag(),
					&cli.StringFlag{Name: "works-id", Usage: "works ID to delete", Required: true},
					&cli.BoolFlag{Name: "confirm-human-reviewed", Usage: "must be true to delete"},
				},
			},
			{
				Name:   "list",
				Usage:  "List own works posts",
				Action: worksListCmd,
				Flags: []cli.Flag{
					configDirFlag(),
					&cli.IntFlag{Name: "type", Usage: "2=demand 3=service"},
					&cli.IntFlag{Name: "page", Value: 1, Usage: "page number"},
					&cli.IntFlag{Name: "page-size", Value: 20, Usage: "page size"},
				},
			},
		},
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// works search
// ─────────────────────────────────────────────────────────────────────────────

func worksSearchCmd(c *cli.Context) error {
	creds, err := loadCreds(expandHome(c.String("config-dir")))
	if err != nil {
		return err
	}
	client := buildAPIClient(creds)

	page := c.Int("page")
	if page < 1 {
		page = 1
	}
	// API pageNum is 0-indexed, CLI --page is 1-indexed
	body := map[string]interface{}{
		"serviceInfo": c.String("keyword"),
		"pageNum":     page - 1,
		"pageSize":    c.Int("page-size"),
	}
	if c.IsSet("type") {
		body["type"] = c.Int("type")
	}
	if city := c.String("city"); city != "" {
		body["city"] = city
	}

	var data interface{}
	if err := client.PostJSON(worksSearchAPI, body, &data); err != nil {
		return outputError("works.search", err)
	}
	return outputOK("works.search", data)
}

// ─────────────────────────────────────────────────────────────────────────────
// works publish
// ─────────────────────────────────────────────────────────────────────────────

func worksPublishCmd(c *cli.Context) error {
	if !c.Bool("confirm-human-reviewed") {
		return outputError("works.publish", fmt.Errorf(
			"必须显式声明 --confirm-human-reviewed 才能发布帖子。请确认帖子内容已经过人工审阅再发布"))
	}

	worksType := c.Int("type")
	if worksType != 2 && worksType != 3 {
		return outputError("works.publish", fmt.Errorf("--type 必须为 2（需求帖）或 3（服务帖）"))
	}

	title := strings.TrimSpace(c.String("title"))
	content := strings.TrimSpace(c.String("content"))
	if title == "" {
		return outputError("works.publish", fmt.Errorf("--title 不能为空"))
	}
	if content == "" {
		return outputError("works.publish", fmt.Errorf("--content 不能为空"))
	}
	if len([]rune(content)) > 2000 {
		return outputError("works.publish", fmt.Errorf("--content 最多 2000 字符"))
	}

	extendInfo := map[string]interface{}{"pois": []interface{}{}}
	if v := c.String("expected-price"); v != "" {
		extendInfo["expectedPrice"] = v
	}
	if v := c.String("service-method"); v != "" {
		extendInfo["serviceMethod"] = v
	}
	if v := c.String("service-location"); v != "" {
		extendInfo["serviceLocation"] = v
	}

	body := map[string]interface{}{
		"type":       worksType,
		"title":      title,
		"content":    content,
		"extendInfo": extendInfo,
	}
	if pic := c.String("picture"); pic != "" {
		body["pictures"] = []string{pic}
	}

	creds, err := loadCreds(expandHome(c.String("config-dir")))
	if err != nil {
		return err
	}
	client := buildAPIClient(creds)

	var data interface{}
	if err := client.PostJSON(worksPublishAPI, body, &data); err != nil {
		return outputError("works.publish", err)
	}
	return outputOK("works.publish", data)
}

// ─────────────────────────────────────────────────────────────────────────────
// works update
// ─────────────────────────────────────────────────────────────────────────────

func worksUpdateCmd(c *cli.Context) error {
	if !c.Bool("confirm-human-reviewed") {
		return outputError("works.update", fmt.Errorf(
			"必须显式声明 --confirm-human-reviewed 才能修改帖子。请确认帖子内容已经过人工审阅再提交"))
	}

	worksID := strings.TrimSpace(c.String("works-id"))
	if worksID == "" {
		return outputError("works.update", fmt.Errorf("--works-id 不能为空"))
	}

	worksType := c.Int("type")
	if worksType != 2 && worksType != 3 {
		return outputError("works.update", fmt.Errorf("--type 必须为 2（需求帖）或 3（服务帖）"))
	}

	title := strings.TrimSpace(c.String("title"))
	if title == "" {
		return outputError("works.update", fmt.Errorf("--title 不能为空"))
	}

	content := strings.TrimSpace(c.String("content"))
	if len([]rune(content)) > 2000 {
		return outputError("works.update", fmt.Errorf("--content 最多 2000 字符"))
	}

	extendInfo := map[string]interface{}{"pois": []interface{}{}}
	if v := c.String("expected-price"); v != "" {
		extendInfo["expectedPrice"] = v
	}
	if v := c.String("service-method"); v != "" {
		extendInfo["serviceMethod"] = v
	}
	if v := c.String("service-location"); v != "" {
		extendInfo["serviceLocation"] = v
	}

	body := map[string]interface{}{
		"worksId":    worksID,
		"type":       worksType,
		"title":      title,
		"extendInfo": extendInfo,
	}
	if content != "" {
		body["content"] = content
	}
	if pic := c.String("picture"); pic != "" {
		body["pictures"] = []string{pic}
	}

	creds, err := loadCreds(expandHome(c.String("config-dir")))
	if err != nil {
		return err
	}
	client := buildAPIClient(creds)

	var data interface{}
	if err := client.PostJSON(worksPublishAPI, body, &data); err != nil {
		return outputError("works.update", err)
	}
	return outputOK("works.update", data)
}

// ─────────────────────────────────────────────────────────────────────────────
// works delete
// ─────────────────────────────────────────────────────────────────────────────

func worksDeleteCmd(c *cli.Context) error {
	if !c.Bool("confirm-human-reviewed") {
		return outputError("works.delete", fmt.Errorf(
			"必须显式声明 --confirm-human-reviewed 才能删除帖子。删除后不可恢复，请确认"))
	}

	worksID := strings.TrimSpace(c.String("works-id"))
	if worksID == "" {
		return outputError("works.delete", fmt.Errorf("--works-id 不能为空"))
	}

	creds, err := loadCreds(expandHome(c.String("config-dir")))
	if err != nil {
		return err
	}
	client := buildAPIClient(creds)

	apiPath := worksDeleteAPI + "/" + worksID
	var data interface{}
	if err := client.DeleteJSON(apiPath, &data); err != nil {
		return outputError("works.delete", err)
	}
	return outputOK("works.delete", data)
}

// ─────────────────────────────────────────────────────────────────────────────
// works list
// ─────────────────────────────────────────────────────────────────────────────

func worksListCmd(c *cli.Context) error {
	creds, err := loadCreds(expandHome(c.String("config-dir")))
	if err != nil {
		return err
	}
	client := buildAPIClient(creds)

	page := c.Int("page")
	pageSize := c.Int("page-size")
	qs := fmt.Sprintf("?pageNum=%d&pageSize=%d", page, pageSize)
	if c.IsSet("type") {
		qs += fmt.Sprintf("&type=%d", c.Int("type"))
	}
	apiPath := worksListAPI + qs

	var data interface{}
	if err := client.GetJSON(apiPath, worksListAPI, &data); err != nil {
		return outputError("works.list", err)
	}
	return outputOK("works.list", data)
}
