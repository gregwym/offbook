package router

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/config"
	"github.com/gregwym/offbook/backend/internal/handler"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service"
	"github.com/gregwym/offbook/backend/internal/service/auth"
)

func New(cfg config.Config, gormDB *gorm.DB) *gin.Engine {
	r := gin.New()
	r.Use(gin.Logger(), gin.Recovery(), corsMiddleware(cfg.FrontendURL))

	health := handler.NewHealthHandler(gormDB)

	// Auth: gates every domain route. SESSION_SECRET is required from M2.5+.
	authSvc := auth.NewService(
		repository.NewUserRepository(gormDB),
		repository.NewSessionRepository(gormDB),
		repository.NewInstanceConfigRepository(gormDB),
		cfg.SessionSecret,
	)
	authHandler := handler.NewAuthHandler(authSvc)

	accountRepo := repository.NewAccountRepository(gormDB)
	accountSvc := service.NewAccountService(accountRepo)
	accountHandler := handler.NewAccountHandler(accountSvc)

	// PII flow: pii_repo is wired ONLY into pii_service, which is wired ONLY
	// into pii_handler. account_service / account_handler never see it.
	piiRepo := repository.NewPIIRepository(gormDB)
	piiSvc := service.NewPIIService(piiRepo, accountSvc)
	piiHandler := handler.NewPIIHandler(piiSvc)

	transactionRepo := repository.NewTransactionRepository(gormDB)
	categoryRepo := repository.NewCategoryRepository(gormDB)
	transactionSvc := service.NewTransactionService(transactionRepo, accountRepo, categoryRepo)
	transactionHandler := handler.NewTransactionHandler(transactionSvc)

	dashboardRepo := repository.NewDashboardRepository(gormDB)
	dashboardSvc := service.NewDashboardService(dashboardRepo)
	dashboardHandler := handler.NewDashboardHandler(dashboardSvc)

	v1 := r.Group("/api/v1")
	{
		// Open routes — no session required.
		v1.GET("/health", health.Get)
		authHandler.RegisterPublic(v1)

		// Authenticated routes — session middleware gates everything below.
		secured := v1.Group("")
		secured.Use(auth.RequireSession(authSvc))
		authHandler.RegisterAuthenticated(secured)
		accountHandler.Register(secured)
		piiHandler.RegisterAccountRoutes(secured)
		transactionHandler.Register(secured)
		dashboardHandler.Register(secured)
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
