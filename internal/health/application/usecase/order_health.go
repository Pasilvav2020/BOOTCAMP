package usecase

import (
	"context"
	"microservice/internal/health/domain/entity"
	"microservice/internal/health/domain/repository"
)

type OrderUseCase struct {
	repo repository.IOrderRepository
}

func NewOrderUseCase(repo repository.IOrderRepository) *OrderUseCase {
	return &OrderUseCase{repo: repo}
}

func (uc *OrderUseCase) FindAll(ctx context.Context) ([]entity.PedidoListado, error) {
	return uc.repo.FindAll(ctx)
}

func (uc *OrderUseCase) FindByID(ctx context.Context, id string) (*entity.Pedido, error) {
	return uc.repo.FindByID(ctx, id)
}
func (uc *OrderUseCase) Delete(ctx context.Context, id string) (*entity.Pedido, error) {
	return uc.repo.Delete(ctx, id)
}

func (uc *OrderUseCase) Create(ctx context.Context, pedido entity.Pedido, detalles []entity.DetallePedido) (entity.Pedido, []entity.DetallePedido, error) {
	return uc.repo.Create(ctx, pedido, detalles)
}

func (uc *OrderUseCase) CreateIdempotent(ctx context.Context, key, requestHash string, pedido entity.Pedido, detalles []entity.DetallePedido) (entity.Pedido, []entity.DetallePedido, error) {
	return uc.repo.CreateIdempotent(ctx, key, requestHash, pedido, detalles)
}

func (uc *OrderUseCase) UpdateOrderItem(ctx context.Context, orderID, itemID string, quantity int, unitPrice float64) (entity.DetallePedido, error) {
	return uc.repo.UpdateOrderItem(ctx, orderID, itemID, quantity, unitPrice)
}

func (uc *OrderUseCase) ArticuloExists(ctx context.Context, id string) (bool, error) {
	return uc.repo.ArticuloExists(ctx, id)
}
