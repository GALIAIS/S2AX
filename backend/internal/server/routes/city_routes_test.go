package routes

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRegisterUserRoutesIncludesCitySimulationKernel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	noop := func(c *gin.Context) { c.Next() }
	handlers := &handler.Handlers{CityEconomy: handler.NewCityEconomyHandler(nil)}

	RegisterUserRoutes(
		router.Group("/api/v1"),
		handlers,
		servermiddleware.JWTAuthMiddleware(noop),
		servermiddleware.AuditLogMiddleware(noop),
		nil,
		nil,
	)

	wanted := map[string]bool{
		"GET /api/v1/city/spatial/rule-sets":                                                           false,
		"GET /api/v1/city/spatial/rule-sets/:rule_set_id":                                              false,
		"GET /api/v1/city/open-world/styles":                                                           false,
		"GET /api/v1/city/open-world/styles/:profile_id":                                               false,
		"GET /api/v1/city/worlds":                                                                      false,
		"POST /api/v1/city/worlds":                                                                     false,
		"GET /api/v1/city/worlds/:world_id":                                                            false,
		"GET /api/v1/city/worlds/:world_id/state":                                                      false,
		"GET /api/v1/city/worlds/:world_id/calendar":                                                   false,
		"GET /api/v1/city/worlds/:world_id/population":                                                 false,
		"GET /api/v1/city/worlds/:world_id/markets":                                                    false,
		"GET /api/v1/city/worlds/:world_id/services/catalog":                                           false,
		"GET /api/v1/city/worlds/:world_id/services/facilities":                                        false,
		"GET /api/v1/city/worlds/:world_id/services/demands":                                           false,
		"GET /api/v1/city/worlds/:world_id/services/connections":                                       false,
		"GET /api/v1/city/worlds/:world_id/services/settlements":                                       false,
		"GET /api/v1/city/worlds/:world_id/services/lifecycle/catalog":                                 false,
		"GET /api/v1/city/worlds/:world_id/services/lifecycle/facilities":                              false,
		"GET /api/v1/city/worlds/:world_id/services/lifecycle/operations":                              false,
		"GET /api/v1/city/worlds/:world_id/services/lifecycle/staffing":                                false,
		"GET /api/v1/city/worlds/:world_id/services/lifecycle/incidents":                               false,
		"GET /api/v1/city/worlds/:world_id/services/lifecycle/budget-movements":                        false,
		"GET /api/v1/city/worlds/:world_id/services/lifecycle/facts":                                   false,
		"GET /api/v1/city/worlds/:world_id/services/networks/catalog":                                  false,
		"GET /api/v1/city/worlds/:world_id/services/networks":                                          false,
		"GET /api/v1/city/worlds/:world_id/services/networks/nodes":                                    false,
		"GET /api/v1/city/worlds/:world_id/services/networks/edges":                                    false,
		"GET /api/v1/city/worlds/:world_id/services/networks/flows":                                    false,
		"GET /api/v1/city/worlds/:world_id/services/networks/facts":                                    false,
		"GET /api/v1/city/worlds/:world_id/services/networks/diagnostics":                              false,
		"GET /api/v1/city/worlds/:world_id/spatial/ruleset":                                            false,
		"GET /api/v1/city/worlds/:world_id/spatial/overmap":                                            false,
		"GET /api/v1/city/worlds/:world_id/open-world/generation":                                      false,
		"GET /api/v1/city/worlds/:world_id/open-world/verification":                                    false,
		"GET /api/v1/city/worlds/:world_id/open-world/map":                                             false,
		"GET /api/v1/city/worlds/:world_id/open-world/buildings/:building_code/portals":                false,
		"GET /api/v1/city/worlds/:world_id/open-world/buildings/:building_code/interiors/:floor_index": false,
		"GET /api/v1/city/worlds/:world_id/land":                                                       false,
		"GET /api/v1/city/worlds/:world_id/development":                                                false,
		"GET /api/v1/city/worlds/:world_id/spatial/chunks":                                             false,
		"GET /api/v1/city/worlds/:world_id/spatial/chunks/:chunk_x/:chunk_y/:z":                        false,
		"GET /api/v1/city/worlds/:world_id/spatial/changes":                                            false,
		"POST /api/v1/city/worlds/:world_id/navigation/path":                                           false,
		"GET /api/v1/city/worlds/:world_id/navigation/portals":                                         false,
		"GET /api/v1/city/worlds/:world_id/navigation/intents":                                         false,
		"GET /api/v1/city/worlds/:world_id/navigation/intents/:actor_code":                             false,
		"GET /api/v1/city/worlds/:world_id/navigation/reservations":                                    false,
		"POST /api/v1/city/worlds/:world_id/commands":                                                  false,
		"GET /api/v1/city/worlds/:world_id/commands/:command_id":                                       false,
		"POST /api/v1/city/worlds/:world_id/step":                                                      false,
		"GET /api/v1/city/worlds/:world_id/events":                                                     false,
		"GET /api/v1/city/worlds/:world_id/journals":                                                   false,
		"GET /api/v1/city/worlds/:world_id/journals/:tick/:sequence":                                   false,
		"GET /api/v1/city/worlds/:world_id/trial-balance":                                              false,
		"GET /api/v1/city/worlds/:world_id/resource-operations":                                        false,
		"GET /api/v1/city/worlds/:world_id/resource-operations/:tick/:sequence":                        false,
		"GET /api/v1/city/worlds/:world_id/market-settlements":                                         false,
		"GET /api/v1/city/worlds/:world_id/market-settlements/:tick/:sequence":                         false,
		"GET /api/v1/city/worlds/:world_id/population-movements":                                       false,
		"GET /api/v1/city/worlds/:world_id/population-movements/:tick/:sequence":                       false,
		"GET /api/v1/city/worlds/:world_id/population-migrations":                                      false,
		"GET /api/v1/city/worlds/:world_id/population-migrations/:tick/:sequence":                      false,
		"GET /api/v1/city/worlds/:world_id/household-movements":                                        false,
		"GET /api/v1/city/worlds/:world_id/household-movements/:tick/:sequence":                        false,
		"GET /api/v1/city/worlds/:world_id/snapshots":                                                  false,
		"GET /api/v1/city/worlds/:world_id/snapshots/:tick":                                            false,
		"GET /api/v1/city/worlds/:world_id/engine":                                                     false,
		"GET /api/v1/city/worlds/:world_id/upgrade-runs":                                               false,
		"POST /api/v1/city/worlds/:world_id/upgrade-runs":                                              false,
		"GET /api/v1/city/worlds/:world_id/upgrade-runs/:run_id":                                       false,
		"GET /api/v1/city/worlds/:world_id/replay-runs":                                                false,
		"POST /api/v1/city/worlds/:world_id/replay-runs":                                               false,
		"GET /api/v1/city/worlds/:world_id/replay-runs/:run_id":                                        false,
		"GET /api/v1/city/worlds/:world_id/recovery-runs":                                              false,
		"POST /api/v1/city/worlds/:world_id/recovery-runs":                                             false,
		"GET /api/v1/city/worlds/:world_id/recovery-runs/:run_id":                                      false,
	}
	for _, route := range router.Routes() {
		key := route.Method + " " + route.Path
		if _, exists := wanted[key]; exists {
			wanted[key] = true
		}
	}
	for route, found := range wanted {
		require.Truef(t, found, "route %s was not registered", route)
	}
}
