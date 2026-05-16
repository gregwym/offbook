package router

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/config"
	"github.com/gregwym/offbook/backend/internal/handler"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service"
)

func New(cfg config.Config, gormDB *gorm.DB) *gin.Engine {
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery(), corsMiddleware(cfg.FrontendURL))

	health := handler.NewHealthHandler(gormDB)

	accountRepo := repository.NewAccountRepository(gormDB)
	accountSvc := service.NewAccountService(accountRepo)
	accountHandler := handler.NewAccountHandler(accountSvc)

	v1 := r.Group("/api/v1")
	{
		v1.GET("/health", health.Get)
		accountHandler.Register(v1)
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
