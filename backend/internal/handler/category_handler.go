package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/gregwym/offbook/backend/internal/service"
)

type CategoryHandler struct {
	svc *service.CategoryService
}

func NewCategoryHandler(s *service.CategoryService) *CategoryHandler {
	return &CategoryHandler{svc: s}
}

func (h *CategoryHandler) Register(g *gin.RouterGroup) {
	g.GET("/categories", h.List)
}

func (h *CategoryHandler) List(c *gin.Context) {
	cats, err := h.svc.List(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "code": "INTERNAL"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": cats, "total": int64(len(cats))})
}
