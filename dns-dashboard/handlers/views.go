package handlers

import "github.com/gofiber/fiber/v2"

func Dashboard(c *fiber.Ctx) error {
	return c.Render("dashboard", fiber.Map{
		"Title": "DNS Analytics Dashboard",
	})
}

func LogsPage(c *fiber.Ctx) error {
	return c.Render("logs", fiber.Map{
		"Title": "Query Logs",
	})
}
