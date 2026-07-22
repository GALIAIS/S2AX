package middleware

import (
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// CityVisualPackPublishGuard keeps the administrator-only visual publishing
// control plane closed unless the parent city simulation, the realtime pixel
// renderer, and the explicit publication switch are all enabled. A missing
// setting dependency fails closed rather than exposing a half-deployed asset
// workflow.
func CityVisualPackPublishGuard(settingService *service.SettingService) gin.HandlerFunc {
	return func(c *gin.Context) {
		if settingService == nil || !settingService.IsCityVisualPackPublishEnabled(c.Request.Context()) {
			AbortWithError(c, http.StatusNotFound, "CITY_VISUAL_PACK_PUBLISH_DISABLED", "City visual pack publishing is disabled")
			return
		}
		c.Next()
	}
}
