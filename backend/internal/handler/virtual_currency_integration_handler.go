package handler

import (
	"encoding/json"
	"io"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

type VirtualCurrencyIntegrationHandler struct {
	service *service.VirtualCurrencyIntegrationService
}

func NewVirtualCurrencyIntegrationHandler(integrationService *service.VirtualCurrencyIntegrationService) *VirtualCurrencyIntegrationHandler {
	return &VirtualCurrencyIntegrationHandler{service: integrationService}
}

// Execute accepts only signed POST requests. The exact raw body is included in
// the HMAC canonical payload, so JSON must be read before it is unmarshaled.
func (h *VirtualCurrencyIntegrationHandler) Execute(c *gin.Context) {
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, int64(serviceIntegrationBodyLimit())))
	if err != nil {
		response.BadRequest(c, "unable to read integration request")
		return
	}
	if len(body) > serviceIntegrationBodyLimit()-1 {
		response.BadRequest(c, "integration request body is too large")
		return
	}
	var mutation service.VirtualCurrencyIntegrationMutation
	if err := json.Unmarshal(body, &mutation); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	result, err := h.service.ExecuteSigned(c.Request.Context(), service.VirtualCurrencyIntegrationSignedRequest{
		Code:      c.GetHeader("X-Integration-Code"),
		Timestamp: c.GetHeader("X-Integration-Timestamp"),
		Nonce:     c.GetHeader("X-Integration-Nonce"),
		Signature: c.GetHeader("X-Integration-Signature"),
		Method:    c.Request.Method,
		Path:      c.Request.URL.Path,
		Body:      body,
		Mutation:  mutation,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, dto.VirtualCurrencyIntegrationMutationResultFromService(result))
}

func serviceIntegrationBodyLimit() int {
	// Keep this in the handler as a named helper so the request cap is visible
	// at the HTTP boundary without duplicating the service's security constant.
	return 1<<20 + 1
}
