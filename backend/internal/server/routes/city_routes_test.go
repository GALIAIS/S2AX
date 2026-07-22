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
		"GET /api/v1/city/worlds/:world_id/clock":                                                      false,
		"GET /api/v1/city/worlds/:world_id/timeline":                                                   false,
		"GET /api/v1/city/worlds/:world_id/realtime/projection":                                        false,
		"GET /api/v1/city/worlds/:world_id/realtime/visual-manifest":                                   false,
		"GET /api/v1/city/worlds/:world_id/realtime/patches":                                           false,
		"GET /api/v1/city/worlds/:world_id/realtime/pixel-chunks/:chunk_x/:chunk_y/:z":                 false,
		"GET /api/v1/city/worlds/:world_id/realtime/character":                                         false,
		"GET /api/v1/city/worlds/:world_id/realtime/character/events":                                  false,
		"GET /api/v1/city/worlds/:world_id/realtime/character/relations":                               false,
		"GET /api/v1/city/worlds/:world_id/realtime/character/case-reviews":                            false,
		"POST /api/v1/city/worlds/:world_id/realtime/character":                                        false,
		"POST /api/v1/city/worlds/:world_id/realtime/character/agent":                                  false,
		"POST /api/v1/city/worlds/:world_id/realtime/character/move":                                   false,
		"POST /api/v1/city/worlds/:world_id/realtime/character/portals":                                false,
		"POST /api/v1/city/worlds/:world_id/realtime/character/activities":                             false,
		"POST /api/v1/city/worlds/:world_id/realtime/character/roles":                                  false,
		"GET /api/v1/city/worlds/:world_id/realtime/events":                                            false,
		"GET /api/v1/admin/city/clock-health":                                                          false,
		"POST /api/v1/admin/city/worlds/:world_id/pause":                                               false,
		"POST /api/v1/admin/city/worlds/:world_id/resume":                                              false,
		"GET /api/v1/admin/city/visual-packs":                                                          false,
		"GET /api/v1/admin/city/visual-release-policies":                                               false,
		"GET /api/v1/admin/city/visual-packs/:pack_id/:pack_version/generation-jobs":                   false,
		"GET /api/v1/admin/city/visual-packs/:pack_id/:pack_version/review-events":                     false,
		"GET /api/v1/admin/city/visual-packs/:pack_id/:pack_version":                                   false,
		"POST /api/v1/admin/city/visual-packs":                                                         false,
		"PATCH /api/v1/admin/city/visual-packs/:pack_id/:pack_version":                                 false,
		"POST /api/v1/admin/city/visual-packs/:pack_id/:pack_version/generation-jobs":                  false,
		"PATCH /api/v1/admin/city/visual-packs/:pack_id/:pack_version/generation-jobs/:job_id/review":  false,
		"POST /api/v1/admin/city/visual-packs/:pack_id/:pack_version/publish":                          false,
		"POST /api/v1/admin/city/visual-packs/:pack_id/:pack_version/retire":                           false,
		"PUT /api/v1/admin/city/visual-release-policies/:spatial_profile_id":                           false,
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
		"GET /api/v1/city/worlds/:world_id/open-world/mobility/od":                                     false,
		"GET /api/v1/city/worlds/:world_id/open-world/commutes":                                        false,
		"GET /api/v1/city/worlds/:world_id/open-world/commute-sources":                                 false,
		"GET /api/v1/city/worlds/:world_id/open-world/commute-lifecycle":                               false,
		"GET /api/v1/city/worlds/:world_id/open-world/supply-chain":                                    false,
		"GET /api/v1/city/worlds/:world_id/open-world/enterprise-freight":                              false,
		"GET /api/v1/city/worlds/:world_id/open-world/enterprise-freight/receipts":                     false,
		"GET /api/v1/city/worlds/:world_id/open-world/freight-batches":                                 false,
		"GET /api/v1/city/worlds/:world_id/open-world/spatial-network":                                 false,
		"GET /api/v1/city/worlds/:world_id/open-world/infrastructure":                                  false,
		"GET /api/v1/city/worlds/:world_id/open-world/effective-capacity":                              false,
		"GET /api/v1/city/worlds/:world_id/open-world/freight-settlements":                             false,
		"GET /api/v1/city/worlds/:world_id/open-world/carrier-recovery":                                false,
		"GET /api/v1/city/worlds/:world_id/open-world/carrier-commerce":                                false,
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
