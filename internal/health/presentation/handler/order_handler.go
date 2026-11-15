package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"microservice/internal/health/application/usecase"
	"microservice/internal/health/domain/entity"
	"microservice/internal/health/domain/repository"
	"microservice/pkg/middleware"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"go.uber.org/zap"
)

type createOrderRequest struct {
	RUTCliente string                   `json:"rut_cliente" binding:"required"`
	Articulos  []createOrderItemRequest `json:"articulos" binding:"required,min=1,dive"`
}

type createOrderItemRequest struct {
	ID             string  `json:"id" binding:"required"`
	Cantidad       int     `json:"cantidad" binding:"required,gt=0"`
	PrecioUnitario float64 `json:"precio_unitario" binding:"required,gt=0"`
}

type createOrderResponse struct {
	OrderID string              `json:"order_id"`
	Estado  entity.EstadoPedido `json:"estado"`
}

type updateOrderItemRequest struct {
	Quantity  int     `json:"quantity" binding:"required,gt=0"`
	UnitPrice float64 `json:"unit_price" binding:"required,gt=0"`
}

type updateOrderItemResponse struct {
	ID        string  `json:"id"`
	ProductID string  `json:"product_id"`
	Quantity  int     `json:"quantity"`
	UnitPrice float64 `json:"unit_price"`
}

type findOrderByIDResponse struct {
	ID            string
	RUTCliente    string
	Estado        entity.EstadoPedido
	Detalles      []entity.DetallePedido
	FechaCreacion string
}

type OrderHandler struct {
	uc  *usecase.OrderUseCase
	log *zap.Logger
}

// Constructor del handler de pedidos.
func NewOrderHandler(uc *usecase.OrderUseCase, log *zap.Logger) *OrderHandler {
	return &OrderHandler{
		uc:  uc,
		log: log,
	}
}

func (h *OrderHandler) FindAll(c *gin.Context) {
	orders, err := h.uc.FindAll(c.Request.Context())
	if err != nil {
		h.log.Error("Error al consultar los pedidos",
			zap.String("error_type", fmt.Sprintf("%T", err)),
			zap.String("route", c.FullPath()),
			zap.String("request_id", middleware.RequestIDFromContext(c)),
		)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error al consultar los pedidos"})
		return
	}

	c.JSON(http.StatusOK, orders)
}

// *************************************************
func (h *OrderHandler) FindByID(c *gin.Context) {
	id := c.Param("id")
	if err := validatePathID(id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id es obligatorio"})
		return
	}

	order, err := h.uc.FindByID(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Pedido no encontrado"})
			return
		}

		h.log.Error("Error al consultar el pedido",
			zap.String("error_type", fmt.Sprintf("%T", err)),
			zap.String("route", c.FullPath()),
			zap.String("request_id", middleware.RequestIDFromContext(c)),
		)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error al consultar el pedido"})
		return
	}

	c.JSON(http.StatusOK, findOrderByIDResponse{
		ID:            order.ID,
		RUTCliente:    order.RUTCliente,
		Estado:        order.Estado,
		Detalles:      order.Detalles,
		FechaCreacion: order.FechaCreacion.Format("2006-01-02T15:04:05"),
	})
}

// *************************************************
func (h *OrderHandler) DeleteOrder(c *gin.Context) {
	id := c.Param("id")
	if err := validatePathID(id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id es obligatorio"})
		return
	}

	order, err := h.uc.Delete(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Pedido no encontrado"})
			return
		}

		h.log.Error("Error al eliminar el pedido",
			zap.String("error_type", fmt.Sprintf("%T", err)),
			zap.String("route", c.FullPath()),
			zap.String("request_id", middleware.RequestIDFromContext(c)),
		)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error al eliminar el pedido"})
		return
	}

	c.JSON(http.StatusOK, order)
}

// *************************************************
func (h *OrderHandler) Create(c *gin.Context) {
	var request createOrderRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "rut_cliente y articulos son obligatorios; cada artículo requiere id, cantidad y precio_unitario mayores que 0"})
		return
	}

	idempotencyKey := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if len(idempotencyKey) > 255 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Idempotency-Key no puede superar 255 caracteres"})
		return
	}

	customerRUT, err := normalizeChileanRUT(request.RUTCliente)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "rut_cliente debe ser un RUT chileno válido"})
		return
	}

	detalles := make([]entity.DetallePedido, len(request.Articulos))
	for i, item := range request.Articulos {
		exists, err := h.uc.ArticuloExists(c.Request.Context(), item.ID)
		if err != nil {
			h.log.Error("Error al validar el artículo del pedido",
				zap.String("error_type", fmt.Sprintf("%T", err)),
				zap.String("article_id", item.ID),
				zap.String("route", c.FullPath()),
				zap.String("request_id", middleware.RequestIDFromContext(c)),
			)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error al validar los artículos"})
			return
		}
		if !exists {
			c.JSON(http.StatusBadRequest, gin.H{"error": "el artículo indicado no existe en el catálogo", "id": item.ID})
			return
		}

		detalles[i] = entity.DetallePedido{
			IDArticulo:     item.ID,
			Cantidad:       item.Cantidad,
			PrecioUnitario: item.PrecioUnitario,
		}
	}

	orderInput := entity.Pedido{
		RUTCliente: customerRUT,
		Estado:     entity.EstadoEnPreparacion,
	}
	var order entity.Pedido
	if idempotencyKey != "" {
		canonicalRequest, marshalErr := json.Marshal(struct {
			RUTCliente string                   `json:"rut_cliente"`
			Articulos  []createOrderItemRequest `json:"articulos"`
		}{RUTCliente: customerRUT, Articulos: request.Articulos})
		if marshalErr != nil {
			h.log.Error("Error al generar huella de idempotencia",
				zap.String("error_type", fmt.Sprintf("%T", marshalErr)),
				zap.String("route", c.FullPath()),
				zap.String("request_id", middleware.RequestIDFromContext(c)),
			)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Error al procesar la solicitud"})
			return
		}
		hash := sha256.Sum256(canonicalRequest)
		order, _, err = h.uc.CreateIdempotent(c.Request.Context(), idempotencyKey, hex.EncodeToString(hash[:]), orderInput, detalles)
	} else {
		order, _, err = h.uc.Create(c.Request.Context(), orderInput, detalles)
	}
	if err != nil {
		if errors.Is(err, repository.ErrIdempotencyKeyReuse) {
			c.JSON(http.StatusConflict, gin.H{"error": "Idempotency-Key ya fue utilizada con una solicitud diferente"})
			return
		}
		h.log.Error("Error al crear el pedido",
			zap.String("error_type", fmt.Sprintf("%T", err)),
			zap.String("route", c.FullPath()),
			zap.String("request_id", middleware.RequestIDFromContext(c)),
		)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error al crear el pedido"})
		return
	}

	c.JSON(http.StatusCreated, createOrderResponse{OrderID: order.ID, Estado: order.Estado})
}

func (h *OrderHandler) UpdateOrderItem(c *gin.Context) {
	orderID := c.Param("order_id")
	if err := validatePathID(orderID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "order_id es obligatorio"})
		return
	}

	itemID := c.Param("item_id")
	if err := validatePathID(itemID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "item_id es obligatorio"})
		return
	}

	var request updateOrderItemRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "quantity y unit_price son obligatorios y deben ser números mayores que 0"})
		return
	}

	item, err := h.uc.UpdateOrderItem(
		c.Request.Context(),
		orderID,
		itemID,
		request.Quantity,
		request.UnitPrice,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Ítem no encontrado en el pedido"})
			return
		}

		h.log.Error("Error al actualizar el ítem del pedido",
			zap.String("error_type", fmt.Sprintf("%T", err)),
			zap.String("route", c.FullPath()),
			zap.String("request_id", middleware.RequestIDFromContext(c)),
		)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Error al actualizar el ítem del pedido"})
		return
	}

	c.JSON(http.StatusOK, updateOrderItemResponse{
		ID:        item.ID,
		ProductID: item.IDArticulo,
		Quantity:  item.Cantidad,
		UnitPrice: item.PrecioUnitario,
	})
}

func validatePathID(value string) error {
	if strings.TrimSpace(value) == "" {
		return errors.New("el ID es obligatorio")
	}
	return nil
}
