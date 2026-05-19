package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/gregwym/offbook/backend/internal/service"
	"github.com/gregwym/offbook/backend/internal/service/auth"
)

// UserSettingsHandler exposes the per-user AI settings surface.
type UserSettingsHandler struct {
	svc *service.UserSettingsService
}

func NewUserSettingsHandler(s *service.UserSettingsService) *UserSettingsHandler {
	return &UserSettingsHandler{svc: s}
}

func (h *UserSettingsHandler) Register(g *gin.RouterGroup) {
	g.GET("/me/settings", h.Get)
	g.PATCH("/me/settings", h.Update)
}

func (h *UserSettingsHandler) Get(c *gin.Context) {
	v, err := h.svc.Get(c.Request.Context(), auth.MustUserID(c.Request.Context()))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "code": "INTERNAL"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": v})
}

type updateUserSettingsRequest struct {
	ClaudeAPIKey      *string `json:"claude_api_key"`
	ClearClaudeAPIKey bool    `json:"clear_claude_api_key"`
	OllamaBaseURL     *string `json:"ollama_base_url"`
	ClearOllamaURL    bool    `json:"clear_ollama_url"`
	PreferredProvider *string `json:"preferred_provider"`
	PreferredModel    *string `json:"preferred_model"`
	ClearModel        bool    `json:"clear_preferred_model"`
}

func (h *UserSettingsHandler) Update(c *gin.Context) {
	var req updateUserSettingsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_REQUEST"})
		return
	}
	v, err := h.svc.Update(c.Request.Context(), auth.MustUserID(c.Request.Context()), service.UpdateUserSettingsInput{
		ClaudeAPIKey:      req.ClaudeAPIKey,
		ClearClaudeAPIKey: req.ClearClaudeAPIKey,
		OllamaBaseURL:     req.OllamaBaseURL,
		ClearOllamaURL:    req.ClearOllamaURL,
		PreferredProvider: req.PreferredProvider,
		PreferredModel:    req.PreferredModel,
		ClearModel:        req.ClearModel,
	})
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidProvider):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_PROVIDER"})
		default:
			// Empty-key-with-no-clear-flag and similar validation hits the
			// generic 400 path; the message is human-readable.
			if err.Error() == "claude_api_key must not be empty (use clear_claude_api_key to delete)" {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_REQUEST"})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "code": "INTERNAL"})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": v})
}
