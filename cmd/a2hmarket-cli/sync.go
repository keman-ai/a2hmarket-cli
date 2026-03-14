package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/urfave/cli/v2"
)

const (
	profileAPIForSync = "/findu-user/api/v1/user/profile/public"
	worksSignPath     = "/findu-user/api/v1/user/works/public"
)

func syncCommand() *cli.Command {
	return &cli.Command{
		Name:   "sync",
		Usage:  "Sync agent profile and works to local cache (~/.a2hmarket/cache.json)",
		Action: syncCmd,
		Flags: []cli.Flag{
			configDirFlag(),
			&cli.StringFlag{
				Name:  "only",
				Usage: "only 'profile' or 'works'",
				Value: "",
			},
		},
	}
}

func syncCmd(c *cli.Context) error {
	configDir := expandHome(c.String("config-dir"))
	only := c.String("only")
	if only != "" && only != "profile" && only != "works" {
		return fmt.Errorf("--only must be 'profile' or 'works'")
	}

	creds, err := loadCreds(configDir)
	if err != nil {
		return err
	}
	client := buildAPIClient(creds)

	result := map[string]interface{}{
		"synced_at": time.Now().UTC().Format(time.RFC3339),
	}

	if only == "" || only == "profile" {
		var raw map[string]interface{}
		if err := client.GetJSON(profileAPIForSync, "", &raw); err != nil {
			return outputError("sync.profile", err)
		}
		result["profile"] = map[string]interface{}{
			"nickname":         raw["nickname"],
			"avatarUrl":        raw["avatarUrl"],
			"bio":              raw["bio"],
			"abilities":        raw["abilities"],
			"realnameStatus":   raw["realnameStatus"],
			"paymentQrcodeUrl": raw["paymentQrcodeUrl"],
		}
	}

	if only == "" || only == "works" {
		serviceWorks, err := syncWorks(client, 3)
		if err != nil {
			return outputError("sync.service_works", err)
		}
		demandWorks, err := syncWorks(client, 2)
		if err != nil {
			return outputError("sync.demand_works", err)
		}
		result["service_works"] = serviceWorks
		result["demand_works"] = demandWorks
	}

	// Write cache file
	cachePath := filepath.Join(configDir, "cache.json")
	cacheData, _ := json.MarshalIndent(result, "", "  ")
	if err := os.WriteFile(cachePath, append(cacheData, '\n'), 0644); err != nil {
		return fmt.Errorf("sync: write cache: %w", err)
	}

	out, _ := json.MarshalIndent(result, "", "  ")
	fmt.Println(string(out))
	return nil
}

func syncWorks(client interface {
	GetJSON(apiPath, signPath string, dest interface{}) error
}, worksType int) ([]map[string]interface{}, error) {
	apiPath := fmt.Sprintf("%s?type=%d&page=1&pageSize=50", worksSignPath, worksType)

	var raw map[string]interface{}
	if err := client.GetJSON(apiPath, worksSignPath, &raw); err != nil {
		return nil, err
	}

	var records []interface{}
	for _, key := range []string{"items", "records", "list"} {
		if v, ok := raw[key]; ok {
			if arr, ok := v.([]interface{}); ok {
				records = arr
				break
			}
		}
	}

	result := make([]map[string]interface{}, 0, len(records))
	for _, item := range records {
		r, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		result = append(result, map[string]interface{}{
			"worksId":    r["worksId"],
			"title":      strOrEmpty(r["title"]),
			"content":    strOrEmpty(r["content"]),
			"type":       r["type"],
			"status":     r["status"],
			"extendInfo": r["extendInfo"],
		})
	}
	return result, nil
}

func strOrEmpty(v interface{}) string {
	if v == nil {
		return ""
	}
	s, _ := v.(string)
	return s
}
