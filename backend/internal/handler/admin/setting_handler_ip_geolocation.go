package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// GetIPGeolocationSettings returns the independently managed IP attribution
// settings. These do not change the client-IP source used by ACLs or audit logs.
// GET /api/v1/admin/settings/ip-geolocation
func (h *SettingHandler) GetIPGeolocationSettings(c *gin.Context) {
	settings, err := h.settingService.GetIPGeolocationSettings(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, settings)
}

type UpdateIPGeolocationSettingsRequest struct {
	Provider                     service.IPGeolocationProvider `json:"provider"`
	IPv4XDBPath                  string                        `json:"ipv4_xdb_path"`
	IPv6XDBPath                  string                        `json:"ipv6_xdb_path"`
	CachePolicy                  string                        `json:"cache_policy"`
	Searchers                    int                           `json:"searchers"`
	CompatibilityFallbackEnabled bool                          `json:"compatibility_fallback_enabled"`
}

// UpdateIPGeolocationSettings saves and reloads the resolver immediately.
// PUT /api/v1/admin/settings/ip-geolocation
func (h *SettingHandler) UpdateIPGeolocationSettings(c *gin.Context) {
	var req UpdateIPGeolocationSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	settings, err := h.settingService.UpdateIPGeolocationSettings(c.Request.Context(), service.IPGeolocationSettings{
		Provider:                     req.Provider,
		IPv4XDBPath:                  req.IPv4XDBPath,
		IPv6XDBPath:                  req.IPv6XDBPath,
		CachePolicy:                  req.CachePolicy,
		Searchers:                    req.Searchers,
		CompatibilityFallbackEnabled: req.CompatibilityFallbackEnabled,
	})
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	response.Success(c, settings)
}
