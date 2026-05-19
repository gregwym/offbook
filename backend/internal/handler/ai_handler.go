package handler

import (
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/gregwym/offbook/backend/internal/service/ai"
	"github.com/gregwym/offbook/backend/internal/service/auth"
)

// AIHandler exposes the AI advisor surface. SendMessage holds open an SSE
// stream — every other handler returns the standard `{"data": ...}`
// envelope.
type AIHandler struct {
	svc *ai.Service
}

func NewAIHandler(s *ai.Service) *AIHandler {
	return &AIHandler{svc: s}
}

func (h *AIHandler) Register(g *gin.RouterGroup) {
	g.POST("/ai/threads", h.CreateThread)
	g.GET("/ai/threads", h.ListThreads)
	g.GET("/ai/threads/:id/messages", h.ListMessages)
	g.POST("/ai/threads/:id/messages", h.SendMessage)
}

type createThreadRequest struct {
	Title *string `json:"title"`
}

func (h *AIHandler) CreateThread(c *gin.Context) {
	var req createThreadRequest
	// Body is optional — a brand-new thread with no title is valid.
	_ = c.ShouldBindJSON(&req)
	t, err := h.svc.CreateThread(c.Request.Context(), auth.MustUserID(c.Request.Context()), req.Title)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "code": "INTERNAL"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"data": t})
}

func (h *AIHandler) ListThreads(c *gin.Context) {
	threads, err := h.svc.ListThreads(c.Request.Context(), auth.MustUserID(c.Request.Context()))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "code": "INTERNAL"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": threads, "total": int64(len(threads))})
}

func (h *AIHandler) ListMessages(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	msgs, err := h.svc.ListMessages(c.Request.Context(), auth.MustUserID(c.Request.Context()), id)
	if err != nil {
		h.writeServiceError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"data": msgs, "total": int64(len(msgs))})
}

type sendMessageRequest struct {
	Content string `json:"content"`
}

// SendMessage holds an SSE stream open. Event format is:
//
//	event: delta
//	data: {"text": "Hello"}
//
//	event: done
//	data: {"finish_reason":"end_turn","input_tokens":17,"output_tokens":9,"message_id":42}
//
// Errors during the stream go on `event: error`. The connection closes
// after a terminal `done` or `error`.
func (h *AIHandler) SendMessage(c *gin.Context) {
	id, ok := parseID(c)
	if !ok {
		return
	}
	var req sendMessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "INVALID_REQUEST"})
		return
	}

	events, err := h.svc.SendMessage(c.Request.Context(), auth.MustUserID(c.Request.Context()), id, req.Content)
	if err != nil {
		h.writeServiceError(c, err)
		return
	}

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no") // disable nginx-style buffering
	c.Writer.WriteHeader(http.StatusOK)

	c.Stream(func(w io.Writer) bool {
		ev, ok := <-events
		if !ok {
			return false
		}
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, string(ev.Data))
		return ev.Type != ai.SSEDone && ev.Type != ai.SSEError
	})
}

func (h *AIHandler) writeServiceError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ai.ErrThreadNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error(), "code": "AI_THREAD_NOT_FOUND"})
	case errors.Is(err, ai.ErrEmptyMessage):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error(), "code": "EMPTY_MESSAGE"})
	case errors.Is(err, ai.ErrNoProvider):
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": err.Error(), "code": "NO_AI_PROVIDER"})
	default:
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error(), "code": "INTERNAL"})
	}
}
