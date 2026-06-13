package health

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

type Pinger interface {
	Ping(context.Context) error
}

type Handler struct {
	mongo   Pinger
	redis   Pinger
	timeout time.Duration
}

type response struct {
	Status   string               `json:"status"`
	Services map[string]component `json:"services"`
}

type component struct {
	Status string `json:"status"`
}

func NewHandler(mongo, redis Pinger, timeout time.Duration) *Handler {
	return &Handler{
		mongo:   mongo,
		redis:   redis,
		timeout: timeout,
	}
}

func (h *Handler) Get(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), h.timeout)
	defer cancel()

	services := map[string]component{
		"application": {Status: "up"},
		"mongodb":     check(ctx, h.mongo),
		"redis":       check(ctx, h.redis),
	}

	status := "up"
	httpStatus := http.StatusOK
	if services["mongodb"].Status != "up" || services["redis"].Status != "up" {
		status = "degraded"
		httpStatus = http.StatusServiceUnavailable
	}

	c.JSON(httpStatus, response{
		Status:   status,
		Services: services,
	})
}

func check(ctx context.Context, pinger Pinger) component {
	if err := pinger.Ping(ctx); err != nil {
		return component{Status: "down"}
	}
	return component{Status: "up"}
}
