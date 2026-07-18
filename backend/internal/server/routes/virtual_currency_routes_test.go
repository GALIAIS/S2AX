package routes

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/gin-gonic/gin"
)

func TestRegisterVirtualCurrencyRoutes(t *testing.T) {
	router := gin.New()
	handlers := &handler.Handlers{Admin: &handler.AdminHandlers{
		VirtualCurrency: adminhandler.NewVirtualCurrencyHandler(nil),
	}}

	registerVirtualCurrencyRoutes(router.Group("/api/v1/admin"), handlers)

	wanted := map[string]bool{
		"POST /api/v1/admin/currencies/:currency/adjustments":          false,
		"POST /api/v1/admin/currencies/:currency/enable-for-all-users": false,
		"GET /api/v1/admin/currencies/:currency/users/:user_id/ledger": false,
	}
	for _, route := range router.Routes() {
		key := route.Method + " " + route.Path
		if _, ok := wanted[key]; ok {
			wanted[key] = true
		}
	}
	for route, found := range wanted {
		if !found {
			t.Fatalf("route %s was not registered", route)
		}
	}
}
