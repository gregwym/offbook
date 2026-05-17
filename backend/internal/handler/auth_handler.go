package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/gregwym/offbook/backend/internal/service/auth"
)

// AuthHandler exposes /setup/admin, /auth/*, and /me. These are the only
// routes mounted outside of RequireSession (except /me which is gated).
type AuthHandler struct {
	svc *auth.Service
}

func NewAuthHandler(s *auth.Service) *AuthHandler {
	return &AuthHandler{svc: s}
}

// RegisterPublic mounts the open endpoints. The /me endpoint is mounted
// separately via RegisterAuthenticated since it requires a valid session.
func (h *AuthHandler) RegisterPublic(g *gin.RouterGroup) {
	g.POST("/setup/admin", h.SetupAdmin)
	g.GET("/setup/status", h.SetupStatus)
	g.POST("/auth/signup", h.Signup)
	g.POST("/auth/signin", h.Signin)
	g.POST("/auth/signout", h.Signout)
}

func (h *AuthHandler) RegisterAuthenticated(g *gin.RouterGroup) {
	g.GET("/me", h.Me)
}

type setupAdminRequest struct {
	Email      string `json:"email"`
	Password   string `json:"password"`
	SignupMode string `json:"signup_mode"`
}

func (h *AuthHandler) SetupAdmin(c *gin.Context) {
	var req setupAdminRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_REQUEST"})
		return
	}
	res, err := h.svc.SetupAdmin(c.Request.Context(), auth.SetupAdminInput{
		Email:      req.Email,
		Password:   req.Password,
		SignupMode: req.SignupMode,
	})
	if err != nil {
		h.writeAuthError(c, err)
		return
	}
	h.setSessionCookie(c, res)
	c.JSON(http.StatusCreated, gin.H{"data": gin.H{
		"id":       res.User.ID,
		"email":    res.User.Email,
		"is_admin": res.User.IsAdmin,
	}})
}

// SetupStatus is unauthenticated and tells the frontend whether to render the
// first-boot wizard. Body: {"bootstrapped": bool, "signup_mode": string|null}.
func (h *AuthHandler) SetupStatus(c *gin.Context) {
	booted, err := h.svc.IsBootstrapped(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "code": "INTERNAL"})
		return
	}
	out := gin.H{"bootstrapped": booted, "signup_mode": nil}
	if booted {
		if cfg, err := h.svc.InstanceConfig(c.Request.Context()); err == nil {
			out["signup_mode"] = cfg.SignupMode
		}
	}
	c.JSON(http.StatusOK, gin.H{"data": out})
}

type credentialsRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *AuthHandler) Signup(c *gin.Context) {
	var req credentialsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_REQUEST"})
		return
	}
	res, err := h.svc.Signup(c.Request.Context(), auth.SignupInput{
		Email:    req.Email,
		Password: req.Password,
	})
	if err != nil {
		h.writeAuthError(c, err)
		return
	}
	h.setSessionCookie(c, res)
	c.JSON(http.StatusCreated, gin.H{"data": gin.H{
		"id":    res.User.ID,
		"email": res.User.Email,
	}})
}

func (h *AuthHandler) Signin(c *gin.Context) {
	var req credentialsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_REQUEST"})
		return
	}
	res, err := h.svc.Signin(c.Request.Context(), req.Email, req.Password)
	if err != nil {
		h.writeAuthError(c, err)
		return
	}
	h.setSessionCookie(c, res)
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"id":       res.User.ID,
		"email":    res.User.Email,
		"is_admin": res.User.IsAdmin,
	}})
}

func (h *AuthHandler) Signout(c *gin.Context) {
	cookie, err := c.Request.Cookie(auth.SessionCookieName)
	if err == nil && cookie.Value != "" {
		_ = h.svc.Signout(c.Request.Context(), cookie.Value)
	}
	h.clearSessionCookie(c)
	c.Status(http.StatusNoContent)
}

func (h *AuthHandler) Me(c *gin.Context) {
	uid := auth.MustUserID(c.Request.Context())
	c.JSON(http.StatusOK, gin.H{"data": gin.H{
		"id":      uid,
		"user_id": uid, // alias to keep frontend explicit
	}})
}

func (h *AuthHandler) setSessionCookie(c *gin.Context, res *auth.SigninResult) {
	maxAge := int(auth.SessionTTL.Seconds())
	// Secure is false in dev (HTTP). Toggle once we deploy behind TLS.
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(auth.SessionCookieName, res.Token, maxAge, "/", "", false, true)
}

func (h *AuthHandler) clearSessionCookie(c *gin.Context) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(auth.SessionCookieName, "", -1, "/", "", false, true)
}

func (h *AuthHandler) writeAuthError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, auth.ErrInstanceConfigured):
		c.JSON(http.StatusConflict, gin.H{"error": err.Error(), "code": "ALREADY_CONFIGURED"})
	case errors.Is(err, auth.ErrSignupClosed):
		c.JSON(http.StatusForbidden, gin.H{"error": err.Error(), "code": "SIGNUP_CLOSED"})
	case errors.Is(err, auth.ErrInvalidCredentials):
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error(), "code": "INVALID_CREDENTIALS"})
	case errors.Is(err, auth.ErrEmailRequired),
		errors.Is(err, auth.ErrInvalidEmail),
		errors.Is(err, auth.ErrPasswordTooShort),
		errors.Is(err, auth.ErrInvalidSignupMode):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_REQUEST"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "code": "INTERNAL"})
	}
}
