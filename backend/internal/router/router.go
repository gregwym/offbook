package router

import (
	"context"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/gregwym/offbook/backend/internal/config"
	"github.com/gregwym/offbook/backend/internal/crypto"
	"github.com/gregwym/offbook/backend/internal/handler"
	"github.com/gregwym/offbook/backend/internal/repository"
	"github.com/gregwym/offbook/backend/internal/service"
	"github.com/gregwym/offbook/backend/internal/service/auth"
	"github.com/gregwym/offbook/backend/internal/service/household"
	plaidsvc "github.com/gregwym/offbook/backend/internal/service/plaid"
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
	plaidItemRepo := repository.NewPlaidItemRepository(gormDB)
	accountSvc := service.NewAccountService(accountRepo).WithPlaidItemRepo(plaidItemRepo)
	accountHandler := handler.NewAccountHandler(accountSvc)

	// PII flow: pii_repo is wired ONLY into pii_service, which is wired ONLY
	// into pii_handler. account_service / account_handler never see it.
	piiRepo := repository.NewPIIRepository(gormDB)
	piiSvc := service.NewPIIService(piiRepo, accountSvc)
	piiHandler := handler.NewPIIHandler(piiSvc)

	transactionRepo := repository.NewTransactionRepository(gormDB)
	categoryRepo := repository.NewCategoryRepository(gormDB)
	ruleRepo := repository.NewCategorizationRuleRepository(gormDB)
	transactionSvc := service.NewTransactionService(transactionRepo, accountRepo, categoryRepo).WithRuleRepo(ruleRepo)
	transactionHandler := handler.NewTransactionHandler(transactionSvc)

	categorySvc := service.NewCategoryService(categoryRepo)
	categoryHandler := handler.NewCategoryHandler(categorySvc)

	ruleSvc := service.NewCategorizationRuleService(ruleRepo, categoryRepo)
	ruleHandler := handler.NewCategorizationRuleHandler(ruleSvc)

	dashboardRepo := repository.NewDashboardRepository(gormDB)
	dashboardSvc := service.NewDashboardService(dashboardRepo)
	dashboardHandler := handler.NewDashboardHandler(dashboardSvc)

	householdRepo := repository.NewHouseholdRepository(gormDB)
	memberRepo := repository.NewHouseholdMemberRepository(gormDB)
	userRepo := repository.NewUserRepository(gormDB)
	householdSvc := household.NewService(
		householdRepo,
		memberRepo,
		repository.NewHouseholdInviteRepository(gormDB),
		repository.NewAccountShareRepository(gormDB),
		accountRepo,
		repository.NewInstanceConfigRepository(gormDB),
		userRepo,
		cfg.SessionSecret,
	)
	householdHandler := handler.NewHouseholdHandler(householdSvc)

	aggregator := household.NewAggregator(
		repository.NewHouseholdAggregatorRepository(gormDB),
		householdRepo,
	)
	aggregatorHandler := handler.NewHouseholdAggregatorHandler(aggregator, memberRepo)

	scopeSvc := service.NewScopeService(userRepo, memberRepo)
	scopeHandler := handler.NewScopeHandler(scopeSvc)

	// Plaid is optional — instances without PLAID_CLIENT_ID get an unconfigured
	// service that returns ErrNotConfigured for every call. Handler still
	// registers either way so the frontend gets a clear PLAID_NOT_CONFIGURED
	// error rather than a 404.
	plaidSyncErrRepo := repository.NewPlaidSyncErrorRepository(gormDB)
	plaidSvc := newPlaidService(cfg, gormDB, plaidItemRepo, accountRepo, transactionRepo, plaidSyncErrRepo, piiSvc).WithRuleRepo(ruleRepo)
	plaidHandler := handler.NewPlaidHandler(plaidSvc)

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
		categoryHandler.Register(secured)
		ruleHandler.Register(secured)
		dashboardHandler.Register(secured)
		householdHandler.Register(secured)
		aggregatorHandler.Register(secured)
		scopeHandler.Register(secured)
		plaidHandler.Register(secured)
	}

	return r
}

// newPlaidService returns a configured *plaidsvc.Service when the instance
// has PLAID_CLIENT_ID + PLAID_TOKEN_KEY, otherwise an unconfigured service
// whose methods return ErrNotConfigured. Panics on a configured-but-broken
// secret key — config.Load() already validated key length, so this should
// be unreachable in practice.
func newPlaidService(
	cfg config.Config,
	gormDB *gorm.DB,
	itemRepo repository.PlaidItemRepository,
	acctRepo repository.AccountRepository,
	txRepo repository.TransactionRepository,
	syncErrRepo repository.PlaidSyncErrorRepository,
	piiSvc *service.PIIService,
) *plaidsvc.Service {
	if !cfg.PlaidConfigured() {
		return plaidsvc.NewService(nil, nil, nil, nil, nil, nil, nil, nil, nil)
	}
	client, err := plaidsvc.NewSDKClient(plaidsvc.Config{
		ClientID: cfg.PlaidClientID,
		Secret:   cfg.PlaidSecret,
		Env:      cfg.PlaidEnv,
	})
	if err != nil {
		panic("plaid: " + err.Error())
	}
	box, err := crypto.NewSecretBox(cfg.PlaidTokenKey)
	if err != nil {
		panic("plaid: secretbox: " + err.Error())
	}
	// Load PFC → category map once at startup. A DB failure here is fatal
	// because the rest of the app expects plaid_category_map to exist; if
	// the migration hasn't run, the operator needs to know immediately.
	mapper, err := plaidsvc.NewCategoryMapper(context.Background(), repository.NewPlaidCategoryMapRepository(gormDB))
	if err != nil {
		panic("plaid: load category map: " + err.Error())
	}
	return plaidsvc.NewService(
		client,
		box,
		itemRepo,
		acctRepo,
		txRepo,
		syncErrRepo,
		piiSvc,
		mapper,
		gormDB,
	)
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
