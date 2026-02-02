package main

import (
	"log"
	"os"

	"dns-dashboard/db"
	"dns-dashboard/handlers"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/template/html/v2"
)

func main() {
	clickhouseDSN := getEnv("CLICKHOUSE_DSN", "tcp://127.0.0.1:9000?database=dns")
	listenAddr := getEnv("LISTEN_ADDR", ":8080")

	if err := db.InitDB(clickhouseDSN); err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.CloseDB()

	engine := html.New("./views", ".html")
	app := fiber.New(fiber.Config{
		Views: engine,
	})

	app.Use(cors.New())

	// Routes
	app.Get("/", handlers.Dashboard)
	app.Get("/api/stats", handlers.ApiStats)
	app.Get("/api/query-types", handlers.ApiQueryTypes)
	app.Get("/api/response-codes", handlers.ApiResponseCodes)
	app.Get("/api/top-domains", handlers.ApiTopDomains)
	app.Get("/api/top-clients", handlers.ApiTopClients)
	app.Get("/api/recent-queries", handlers.ApiRecentQueries)
	app.Get("/api/timeline", handlers.ApiTimeline)
	app.Get("/logs", handlers.LogsPage)
	app.Get("/api/logs", handlers.ApiLogs)

	log.Printf("DNS Dashboard running on %s", listenAddr)
	log.Fatal(app.Listen(listenAddr))
}

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}
