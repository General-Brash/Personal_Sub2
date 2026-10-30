package admin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *SettingHandler) GetServerAutoRetryConfig(c *gin.Context) {
	cfg, err := h.settingService.LoadServerAutoRetryConfig(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, cfg)
}

func (h *SettingHandler) UpdateServerAutoRetryConfig(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
	raw, err := io.ReadAll(c.Request.Body)
	if err != nil {
		response.BadRequest(c, "Invalid server auto retry configuration body")
		return
	}
	var cfg service.ServerAutoRetryConfig
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&cfg); err != nil {
		response.BadRequest(c, "Invalid server auto retry configuration: "+err.Error())
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		response.BadRequest(c, "Configuration must contain one JSON object")
		return
	}
	// PUT is a complete replacement; omitted booleans must not silently become false.
	template, _ := json.Marshal(service.DefaultServerAutoRetryConfig())
	if err := requireServerAutoRetryFields(raw, template, ""); err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	saved, err := h.settingService.UpdateServerAutoRetryConfig(c.Request.Context(), cfg)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, saved)
}

func requireServerAutoRetryFields(raw, template []byte, prefix string) error {
	var actual, required map[string]json.RawMessage
	if err := json.Unmarshal(raw, &actual); err != nil || actual == nil {
		return fmt.Errorf("%s must be an object", strings.TrimSuffix(prefix, "."))
	}
	if err := json.Unmarshal(template, &required); err != nil {
		return err
	}
	for key, expected := range required {
		value, ok := actual[key]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("%s%s is required and cannot be null", prefix, key)
		}
		if len(expected) > 0 && expected[0] == '{' {
			if err := requireServerAutoRetryFields(value, expected, prefix+key+"."); err != nil {
				return err
			}
		}
	}
	return nil
}

func (h *SettingHandler) ListServerAutoRetryEvents(c *gin.Context) {
	page, pageSize := 1, 50
	for name, target := range map[string]*int{"page": &page, "page_size": &pageSize} {
		if raw, present := c.GetQuery(name); present {
			value, err := strconv.Atoi(raw)
			if err != nil || value <= 0 {
				response.BadRequest(c, name+" must be a positive integer")
				return
			}
			*target = value
		}
	}
	if pageSize > 200 {
		pageSize = 200
	}
	if page > int(^uint(0)>>1)/pageSize {
		response.BadRequest(c, "page is too large")
		return
	}
	result, err := h.opsService.ListServerAutoRetryEvents(c.Request.Context(), strings.TrimSpace(c.Query("request_id")), page, pageSize)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, result)
}
