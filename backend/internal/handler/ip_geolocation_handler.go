package handler

import (
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

const maxIPGeolocationLookupItems = 100

type IPGeolocationHandler struct {
	service *service.IPGeolocationService
}

func NewIPGeolocationHandler(service *service.IPGeolocationService) *IPGeolocationHandler {
	return &IPGeolocationHandler{service: service}
}

type ipGeolocationLookupRequest struct {
	IPs []string `json:"ips"`
}

// Lookup resolves safe display-level geolocation for authenticated UI pages.
// GET logs already limit which client IPs a user can see; this endpoint never
// returns raw provider records or a provider error.
func (h *IPGeolocationHandler) Lookup(c *gin.Context) {
	var req ipGeolocationLookupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "invalid IP geolocation request")
		return
	}
	if len(req.IPs) == 0 || len(req.IPs) > maxIPGeolocationLookupItems {
		response.BadRequest(c, "ips must contain between 1 and 100 values")
		return
	}

	seen := make(map[string]struct{}, len(req.IPs))
	result := make([]service.IPGeolocationResult, 0, len(req.IPs))
	for _, rawIP := range req.IPs {
		ip := strings.TrimSpace(rawIP)
		if ip == "" {
			continue
		}
		if _, exists := seen[ip]; exists {
			continue
		}
		seen[ip] = struct{}{}
		if h.service == nil {
			result = append(result, service.IPGeolocationResult{IP: ip, Status: service.IPGeolocationStatusUnavailable})
			continue
		}
		result = append(result, h.service.Lookup(ip))
	}
	if len(result) == 0 {
		response.BadRequest(c, "ips must contain at least one non-empty value")
		return
	}
	response.Success(c, result)
}
