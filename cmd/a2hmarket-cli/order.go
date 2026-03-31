package main

import (
	"fmt"
	"strings"

	"github.com/urfave/cli/v2"
)

func orderCommand() *cli.Command {
	orderIDFlag := &cli.StringFlag{Name: "order-id", Usage: "order ID", Required: true}
	return &cli.Command{
		Name:  "order",
		Usage: "Manage orders (create, confirm, reject, cancel, list...)",
		Subcommands: []*cli.Command{
			{
				Name:   "create",
				Usage:  "Create a new order (seller side)",
				Action: orderCreateCmd,
				Flags: []cli.Flag{
					configDirFlag(),
					&cli.StringFlag{Name: "customer-id", Usage: "buyer agent ID", Required: true},
					&cli.StringFlag{Name: "title", Usage: "order title", Required: true},
					&cli.StringFlag{Name: "content", Usage: "order description", Required: true},
					&cli.IntFlag{Name: "price-cent", Usage: "price in cents (e.g. 10000 = 100 yuan, 15000 = 150 USD)", Required: true},
					&cli.StringFlag{Name: "currency", Usage: "currency: CNY or USD", Required: true},
					&cli.StringFlag{Name: "product-id", Usage: "works ID (demand post ID when order-type=2, service post ID when order-type=3)", Required: true},
					&cli.IntFlag{Name: "order-type", Usage: "order type: 2=seller takes buyer's demand task, 3=buyer purchases seller's existing service", Required: true},
				},
			},
			{
				Name:   "confirm",
				Usage:  "Confirm order (buyer side)",
				Action: orderSimpleCmd("confirm"),
				Flags:  []cli.Flag{configDirFlag(), orderIDFlag},
			},
			{
				Name:   "reject",
				Usage:  "Reject order (buyer side)",
				Action: orderSimpleCmd("reject"),
				Flags:  []cli.Flag{configDirFlag(), orderIDFlag},
			},
			{
				Name:   "cancel",
				Usage:  "Cancel order (seller side)",
				Action: orderSimpleCmd("cancel"),
				Flags:  []cli.Flag{configDirFlag(), orderIDFlag},
			},
			{
				Name:   "confirm-received",
				Usage:  "Confirm payment received (seller side)",
				Action: orderSimpleCmd("confirm-received"),
				Flags:  []cli.Flag{configDirFlag(), orderIDFlag},
			},
			{
				Name:   "confirm-service-completed",
				Usage:  "Confirm service completed (buyer side)",
				Action: orderSimpleCmd("confirm-service-completed"),
				Flags:  []cli.Flag{configDirFlag(), orderIDFlag},
			},
			{
				Name:   "get",
				Usage:  "Get order detail",
				Action: orderGetCmd,
				Flags:  []cli.Flag{configDirFlag(), orderIDFlag},
			},
			{
				Name:   "list-sales",
				Usage:  "List orders where you are the seller",
				Action: orderListCmd("/findu-trade/api/v1/orders/sales-orders", "order.list-sales"),
				Flags: []cli.Flag{
					configDirFlag(),
					&cli.StringFlag{Name: "status", Usage: "PENDING_CONFIRM|CONFIRMED|PAID|COMPLETED|REJECTED|CANCELLED"},
					&cli.IntFlag{Name: "page", Value: 1},
					&cli.IntFlag{Name: "page-size", Value: 20},
				},
			},
			{
				Name:   "list-purchase",
				Usage:  "List orders where you are the buyer",
				Action: orderListCmd("/findu-trade/api/v1/orders/purchase-orders", "order.list-purchase"),
				Flags: []cli.Flag{
					configDirFlag(),
					&cli.StringFlag{Name: "status", Usage: "PENDING_CONFIRM|CONFIRMED|PAID|COMPLETED|REJECTED|CANCELLED"},
					&cli.IntFlag{Name: "page", Value: 1},
					&cli.IntFlag{Name: "page-size", Value: 20},
				},
			},
		},
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// order create
// ─────────────────────────────────────────────────────────────────────────────

func orderCreateCmd(c *cli.Context) error {
	creds, err := loadCreds(expandHome(c.String("config-dir")))
	if err != nil {
		return err
	}

	title := strings.TrimSpace(c.String("title"))
	if len([]rune(title)) > 100 {
		return outputError("order.create", fmt.Errorf("--title 最多 100 字符"))
	}
	priceCent := c.Int("price-cent")
	if priceCent <= 0 {
		return outputError("order.create", fmt.Errorf("--price-cent 必须为正整数，单位为分（如 10000 = 100元）"))
	}

	orderType := c.Int("order-type")
	if orderType != 2 && orderType != 3 {
		return outputError("order.create", fmt.Errorf("--order-type 必须为 2（接买家需求任务）或 3（采购卖家现成服务）"))
	}

	currency := strings.ToUpper(strings.TrimSpace(c.String("currency")))
	if currency != "CNY" && currency != "USD" {
		return outputError("order.create", fmt.Errorf("--currency 必须为 CNY 或 USD"))
	}

	body := map[string]interface{}{
		"providerId": creds.AgentID,
		"customerId": c.String("customer-id"),
		"title":      title,
		"content":    c.String("content"),
		"price":      priceCent,
		"currency":   currency,
		"productId":  c.String("product-id"),
		"orderType":  orderType,
	}

	client := buildAPIClient(creds)
	var data interface{}
	if err := client.PostJSON("/findu-trade/api/v1/orders/create", body, &data); err != nil {
		return outputError("order.create", err)
	}
	return outputOK("order.create", data)
}

// ─────────────────────────────────────────────────────────────────────────────
// order simple actions (confirm / reject / cancel / confirm-received / confirm-service-completed)
// ─────────────────────────────────────────────────────────────────────────────

// orderSimpleCmd returns a CLI action for state-transition endpoints that only need an order ID.
func orderSimpleCmd(sub string) cli.ActionFunc {
	action := "order." + sub
	return func(c *cli.Context) error {
		creds, err := loadCreds(expandHome(c.String("config-dir")))
		if err != nil {
			return err
		}
		orderID := strings.TrimSpace(c.String("order-id"))
		if orderID == "" {
			return outputError(action, fmt.Errorf("--order-id 不能为空"))
		}

		apiPath := fmt.Sprintf("/findu-trade/api/v1/orders/%s/%s", orderID, sub)
		client := buildAPIClient(creds)
		var data interface{}
		if err := client.PostJSON(apiPath, map[string]interface{}{}, &data); err != nil {
			return outputError(action, err)
		}
		return outputOK(action, data)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// order get
// ─────────────────────────────────────────────────────────────────────────────

func orderGetCmd(c *cli.Context) error {
	creds, err := loadCreds(expandHome(c.String("config-dir")))
	if err != nil {
		return err
	}
	orderID := strings.TrimSpace(c.String("order-id"))
	if orderID == "" {
		return outputError("order.get", fmt.Errorf("--order-id 不能为空"))
	}

	client := buildAPIClient(creds)
	var raw map[string]interface{}
	apiPath := fmt.Sprintf("/findu-trade/api/v1/orders/%s/detail", orderID)
	if err := client.GetJSON(apiPath, "", &raw); err != nil {
		return outputError("order.get", err)
	}

	data := map[string]interface{}{
		"orderId":     raw["orderId"],
		"providerId":  raw["providerId"],
		"customerId":  raw["customerId"],
		"title":       raw["title"],
		"content":     raw["content"],
		"price":       raw["price"],
		"productId":   raw["productId"],
		"status":      raw["status"],
		"currentType": raw["currentType"],
		"profile":     raw["profile"],
		"createdAt":   raw["createdAt"],
		"updatedAt":   raw["updatedAt"],
	}
	return outputOK("order.get", data)
}

// ─────────────────────────────────────────────────────────────────────────────
// order list-sales / list-purchase
// ─────────────────────────────────────────────────────────────────────────────

func orderListCmd(apiBase, action string) cli.ActionFunc {
	return func(c *cli.Context) error {
		creds, err := loadCreds(expandHome(c.String("config-dir")))
		if err != nil {
			return err
		}

		page := c.Int("page")
		pageSize := c.Int("page-size")
		qs := fmt.Sprintf("?page=%d&pageSize=%d", page, pageSize)
		if status := c.String("status"); status != "" {
			qs += "&status=" + status
		}
		apiPath := apiBase + qs

		client := buildAPIClient(creds)
		var raw map[string]interface{}
		if err := client.GetJSON(apiPath, apiBase, &raw); err != nil {
			return outputError(action, err)
		}

		var items []map[string]interface{}
		if rawList, ok := raw["list"].([]interface{}); ok {
			for _, item := range rawList {
				r, ok := item.(map[string]interface{})
				if !ok {
					continue
				}
				items = append(items, formatOrderItem(r))
			}
		}

		return outputOK(action, map[string]interface{}{
			"total":    raw["total"],
			"page":     raw["page"],
			"pageSize": raw["pageSize"],
			"items":    items,
		})
	}
}

func formatOrderItem(r map[string]interface{}) map[string]interface{} {
	var profile interface{}
	if p, ok := r["profile"].(map[string]interface{}); ok {
		profile = map[string]interface{}{
			"nickname":  p["nickname"],
			"userId":    p["userId"],
			"avatarUrl": p["avatarUrl"],
		}
	}
	return map[string]interface{}{
		"orderId":     r["orderId"],
		"title":       r["title"],
		"price":       r["price"],
		"status":      r["status"],
		"providerId":  r["providerId"],
		"customerId":  r["customerId"],
		"currentType": r["currentType"],
		"profile":     profile,
		"gmtCreate":   r["gmtCreate"],
	}
}
