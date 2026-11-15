package repository

import (
	"context"
	"errors"
	"microservice/internal/health/domain/entity"
)

var ErrIdempotencyKeyReuse = errors.New("idempotency key reused with a different request")

type IOrderRepository interface {
	Create(ctx context.Context, pedido entity.Pedido, detalles []entity.DetallePedido) (entity.Pedido, []entity.DetallePedido, error)
	CreateIdempotent(ctx context.Context, key, requestHash string, pedido entity.Pedido, detalles []entity.DetallePedido) (entity.Pedido, []entity.DetallePedido, error)
	UpdateOrderItem(ctx context.Context, orderID, itemID string, quantity int, unitPrice float64) (entity.DetallePedido, error)
	ArticuloExists(ctx context.Context, id string) (bool, error)
	FindAll(ctx context.Context) ([]entity.PedidoListado, error)
	FindByID(ctx context.Context, id string) (*entity.Pedido, error)
	Delete(ctx context.Context, id string) (*entity.Pedido, error)
}
