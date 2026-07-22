package middleware

import (
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// CityRealtimePixelRendererGuard closes the experimental shared realtime
// renderer independently from the broader city-simulation domain. It is
// intentionally fail-closed: a missing setting store, disabled parent city
// feature, or disabled renderer hides the member-facing content plane rather
// than allowing a partially deployed visual API to leak through.
func CityRealtimePixelRendererGuard(settingService *service.SettingService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if settingService == nil || !settingService.IsCityPixelRendererEnabled(c.Request.Context()) {
			AbortWithError(c, http.StatusNotFound, "CITY_PIXEL_RENDERER_DISABLED", "City realtime pixel renderer is disabled")
			return
		}
		c.Next()
	}
}
