package main

import (
	"github.com/urfave/cli/v2"
)

const (
	talentSearchAPI = "/findu-match/api/v1/inner/match/findPersonByQuery"
)

func talentCommand() *cli.Command {
	return &cli.Command{
		Name:  "talent",
		Usage: "Search talent/influencer profiles",
		Subcommands: []*cli.Command{
			{
				Name:   "search",
				Usage:  "Search talent by service keyword and optional filters",
				Action: talentSearchCmd,
				Flags: []cli.Flag{
					configDirFlag(),
					&cli.StringFlag{Name: "service-info", Aliases: []string{"s"}, Usage: "service description keyword, e.g. '婚礼跟拍'"},
					&cli.IntFlag{Name: "price", Usage: "max price in CNY; services exceeding this are excluded"},
					&cli.StringFlag{Name: "province", Usage: "province, e.g. '浙江省'"},
					&cli.StringFlag{Name: "city", Usage: "city, e.g. '杭州市'"},
					&cli.StringFlag{Name: "district", Usage: "district, e.g. '西湖区'"},
					&cli.Float64Flag{Name: "longitude", Usage: "center longitude for geo-range search"},
					&cli.Float64Flag{Name: "latitude", Usage: "center latitude for geo-range search"},
					&cli.IntFlag{Name: "radius", Usage: "search radius in meters (requires --longitude and --latitude)"},
					&cli.IntFlag{Name: "page", Value: 1, Usage: "page number (1-based)"},
					&cli.IntFlag{Name: "page-size", Value: 10, Usage: "page size"},
				},
			},
		},
	}
}

func talentSearchCmd(c *cli.Context) error {
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
		"pageNum":  page - 1,
		"pageSize": c.Int("page-size"),
	}

	if v := c.String("service-info"); v != "" {
		body["serviceInfo"] = v
	}
	if c.IsSet("price") {
		body["price"] = c.Int("price")
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

	lon := c.Float64("longitude")
	lat := c.Float64("latitude")
	radius := c.Int("radius")
	if lon > 0 && lat > 0 && radius > 0 {
		body["longitude"] = lon
		body["latitude"] = lat
		body["radiusInMeter"] = radius
	}

	var data interface{}
	if err := client.PostJSON(talentSearchAPI, body, &data); err != nil {
		return outputError("talent.search", err)
	}
	return outputOK("talent.search", data)
}
