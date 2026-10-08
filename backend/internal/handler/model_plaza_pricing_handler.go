package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// ModelPlazaPricingHandler deliberately does not share the display-settings DTO/version.
type ModelPlazaPricingHandler struct {
	defaults *service.DefaultModelPricingService
	billing  *service.BillingService
}

func NewModelPlazaPricingHandler(defaults *service.DefaultModelPricingService, billing *service.BillingService) *ModelPlazaPricingHandler {
	return &ModelPlazaPricingHandler{defaults: defaults, billing: billing}
}

func (h *ModelPlazaPricingHandler) Get(c *gin.Context) {
	detail, err := h.billing.DefaultPricingDetail(c.Request.Context(), c.Query("model_id"))
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, detail)
}

func (h *ModelPlazaPricingHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 || page > 1_000_000 || len(c.Query("search")) > 256 {
		response.ErrorFrom(c, service.ErrDefaultPricingInvalid)
		return
	}
	result, err := h.defaults.List(c.Request.Context(), c.Query("search"), page, size)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}

type modelDefaultPricingUpdate struct {
	ModelID string                      `json:"model_id"`
	Version string                      `json:"version"`
	Patch   service.DefaultPricingPatch `json:"patch"`
}

type modelDefaultPricingReset struct {
	ModelID string   `json:"model_id"`
	Version string   `json:"version"`
	Fields  []string `json:"fields,omitempty"`
}

func bindDefaultPricingRequest(c *gin.Context, dst any) bool {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	dec := json.NewDecoder(c.Request.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		response.ErrorFrom(c, service.ErrDefaultPricingInvalid)
		return false
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		response.ErrorFrom(c, service.ErrDefaultPricingInvalid)
		return false
	}
	return true
}

func (h *ModelPlazaPricingHandler) Put(c *gin.Context) {
	middleware.SetAuditAction(c, "models.pricing.write")
	var body modelDefaultPricingUpdate
	if !bindDefaultPricingRequest(c, &body) {
		return
	}
	h.save(c, body.ModelID, body.Version, body.Patch, false)
}

func (h *ModelPlazaPricingHandler) Reset(c *gin.Context) {
	middleware.SetAuditAction(c, "models.pricing.reset")
	var body modelDefaultPricingReset
	if !bindDefaultPricingRequest(c, &body) {
		return
	}
	if len(body.Fields) == 0 {
		body.Fields = service.DefaultPricingEditableFields()
	}
	if len(body.Fields) > len(service.DefaultPricingEditableFields()) {
		response.ErrorFrom(c, service.ErrDefaultPricingInvalid)
		return
	}
	patch := service.DefaultPricingPatch{}
	for _, field := range body.Fields {
		if _, duplicate := patch[field]; duplicate {
			response.ErrorFrom(c, service.ErrDefaultPricingInvalid)
			return
		}
		patch[field] = json.RawMessage("null")
	}
	h.save(c, body.ModelID, body.Version, patch, true)
}

func (h *ModelPlazaPricingHandler) save(c *gin.Context, model, version string, patch service.DefaultPricingPatch, reset bool) {
	middleware.SetAuditExtra(c, map[string]any{"pricing_model": model, "old_pricing_revision": version, "result": "failed"})
	detail, change, err := h.billing.SaveDefaultPricing(c.Request.Context(), model, version, patch, reset)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	middleware.SetDefaultPricingAudit(c, *change)
	// The response is constructed from the committed candidate. A later read
	// failure must not turn a successful mutation into a misleading HTTP failure.
	response.Success(c, detail)
}
