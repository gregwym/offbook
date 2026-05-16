package router

import (
	"github.com/gin-gonic/gin"

	"github.com/gregwym/offbook/backend/internal/config"
	"github.com/gregwym/offbook/backend/internal/handler"
)

func New(cfg config.Config) *gin.Engine {
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery(), corsMiddleware(cfg.FrontendURL))

	v1 := r.Group("/api/v1")
	{
		v1.GET("/health", handler.Health)
	}

	return r
}

func corsMiddleware(origin string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", origin)
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type, Authorization")
		c.Header("Access-Control-Allow-Credentials", "true")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	}
}
