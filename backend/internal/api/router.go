package api

import (
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/recover"
)

func SetupRouter(app *fiber.App, h *Handler) {

	app.Use(recover.New())
	app.Use(cors.New(cors.Config{
		AllowOrigins: "*",
		AllowHeaders: "Origin, Content-Type, Accept, Authorization, X-Requested-With",
		AllowMethods: "GET, POST, PUT, DELETE, OPTIONS",
	}))

	app.Get("/health", h.HealthCheck)

	v1 := app.Group("/api/v1")
	admin := v1.Group("/admin")
	admin.Post("/events/:id/prewarm", h.PrewarmEvent)
	admin.Get("/events/:id/metrics", h.GetAdminMetrics)
	admin.Post("/events/:id/reset", h.ResetEvent)
	admin.Post("/events/:id/reconcile", h.ReconcileEvent)
	admin.Post("/events/:id/users/:userId/unlock", h.UnlockUser)
	admin.Get("/circuit-breaker", h.GetCircuitBreakerStatus)

	tickets := v1.Group("/tickets")
	tickets.Post("/book", h.BookTicket)

	events := v1.Group("/events")
	events.Get("/:id", h.GetEvent)
	events.Get("/:id/stock", h.GetStock)

	orders := v1.Group("/orders")
	orders.Post("/:id/pay", h.PayOrder)
	orders.Post("/:id/cancel", h.CancelOrder)
}
