package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/gregwym/offbook/backend/internal/service"
	"github.com/gregwym/offbook/backend/internal/service/auth"
)

type CategoryHandler struct {
	svc *service.CategoryService
}

func NewCategoryHandler(s *service.CategoryService) *CategoryHandler {
	return &CategoryHandler{svc: s}
}

func (h *CategoryHandler) Register(g *gin.RouterGroup) {
	g.GET("/categories", h.List)
	g.POST("/categories", h.Create)
	g.PATCH("/categories/:id", h.Rename)
}

func (h *CategoryHandler) List(c *gin.Context) {
	uid := auth.MustUserID(c.Request.Context())
	cats, err := h.svc.List(c.Request.Context(), uid)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "code": "INTERNAL"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": cats, "total": int64(len(cats))})
}

// Create handles POST /categories (#573). Body: {"name", "parent_id"?}.
func (h *CategoryHandler) Create(c *gin.Context) {
	var req struct {
		Name     string `json:"name"`
		ParentID *int64 `json:"parent_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_REQUEST"})
		return
	}
	cat, err := h.svc.Create(c.Request.Context(), auth.MustUserID(c.Request.Context()), service.CreateCategoryInput{
		Name: req.Name, ParentID: req.ParentID,
	})
	if err != nil {
		writeCategoryError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": cat})
}

// Rename handles PATCH /categories/:id (#573). Body: {"name"}.
func (h *CategoryHandler) Rename(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id", "code": "INVALID_ID"})
		return
	}
	var req struct {
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_REQUEST"})
		return
	}
	cat, err := h.svc.Rename(c.Request.Context(), auth.MustUserID(c.Request.Context()), id, req.Name)
	if err != nil {
		writeCategoryError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": cat})
}

func writeCategoryError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, service.ErrInvalidCategoryName):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_NAME"})
	case errors.Is(err, service.ErrInvalidParent):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_PARENT"})
	case errors.Is(err, service.ErrCategoryNameTaken):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error(), "code": "CATEGORY_NAME_TAKEN"})
	case errors.Is(err, service.ErrCategoryReadOnly):
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error(), "code": "CATEGORY_READ_ONLY"})
	case errors.Is(err, service.ErrCategoryNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error(), "code": "CATEGORY_NOT_FOUND"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "code": "INTERNAL"})
	}
}
