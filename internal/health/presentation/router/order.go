package router

import (
	"microservice/internal/health/presentation/handler"

	"github.com/gin-gonic/gin"
)

func RegisterOrderRoutes(api *gin.RouterGroup, h *handler.OrderHandler) {
	api.GET("/v1/orders", h.FindAll)
	api.GET("/v1/orders/:id", h.FindByID)
	api.POST("/v1/orders", h.Create)
	api.PUT("/v1/orders/:order_id/items/:item_id", h.UpdateOrderItem)
	api.DELETE("/v1/orders/:id", h.DeleteOrder)
}
