package middleware

import (
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// CitySimulationGuard keeps the optional city simulation closed until an
// administrator enables it in system settings. Platform administrators are
// additionally marked in the request context so they can manage every city,
// including worlds created before this policy existed.
func CitySimulationGuard(settingService *service.SettingService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if settingService == nil || !settingService.IsCitySimulationEnabled(c.Request.Context()) {
			AbortWithError(c, http.StatusNotFound, "CITY_SIMULATION_DISABLED", "City simulation is disabled")
			return
		}
		if role, ok := GetUserRoleFromContext(c); ok && role == service.RoleAdmin {
			c.Request = c.Request.WithContext(service.WithCitySystemAdministrator(c.Request.Context()))
		}
		c.Next()
	}
}
