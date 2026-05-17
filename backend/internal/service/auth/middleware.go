package auth

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
)

// RequireSession returns a gin middleware that 401s any request without a
// valid session cookie, and otherwise binds the authenticated user_id onto
// both the gin context and the request's context.Context.
func RequireSession(svc *Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		cookie, err := c.Request.Cookie(SessionCookieName)
		if err != nil || cookie.Value == "" {
			abort401(c, "missing session")
			return
		}
		u, err := svc.Authenticate(c.Request.Context(), cookie.Value)
		if err != nil {
			switch {
			case errors.Is(err, ErrSessionNotFound), errors.Is(err, ErrSessionExpired):
				abort401(c, err.Error())
			default:
				c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
					"error": err.Error(),
					"code":  "INTERNAL",
				})
			}
			return
		}
		c.Set("user_id", u.ID)
		c.Request = c.Request.WithContext(WithUser(c.Request.Context(), u.ID))
		c.Next()
	}
}

func abort401(c *gin.Context, msg string) {
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
		"error": msg,
		"code":  "UNAUTHENTICATED",
	})
}
