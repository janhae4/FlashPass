package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/recover"
)

func SetupRouter(app *fiber.App, h *Handler) {
	// Middleware
	app.Use(recover.New())
	app.Use(cors.New(cors.Config{
		AllowOrigins: "*",
		AllowHeaders: "Origin, Content-Type, Accept, Authorization, X-Requested-With",
		AllowMethods: "GET, POST, PUT, DELETE, OPTIONS",
	}))

	// Health check
	app.Get("/health", h.HealthCheck)

	// API v1 group
	v1 := app.Group("/api/v1")

	// Admin endpoints
	admin := v1.Group("/admin")
	admin.Post("/events/:id/prewarm", h.PrewarmEvent)

	// Ticket endpoints
	tickets := v1.Group("/tickets")
	tickets.Post("/book", h.BookTicket)

	// Event inventory endpoints
	events := v1.Group("/events")
	events.Get("/:id/stock", h.GetStock)

	// Order endpoints
	orders := v1.Group("/orders")
	orders.Post("/:id/pay", h.PayOrder)
}
