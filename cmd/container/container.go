package container

import (
	"microservice/cmd/config"
	healthUseCase "microservice/internal/health/application/usecase"
	healthInfra "microservice/internal/health/infrastructure"
	"microservice/internal/health/presentation/handler"
	"microservice/pkg/database"
	"microservice/pkg/logger"

	"go.uber.org/zap"
)

type Container struct {
	DB            database.Database
	HealthHandler *handler.HealthHandler
	OrderHandler  *handler.OrderHandler
	Log           *zap.Logger
}

func NewContainer(cfg *config.Configuration, db database.Database, log *zap.Logger) (*Container, error) {

	repo := healthInfra.NewPostgresHealthRepository(db)
	healthUC := healthUseCase.NewHealthUseCase(repo)
	healthHdlr := handler.NewHealthHandler(healthUC)

	orderRepo, err := healthInfra.NewPostgresOrderRepository(db)
	if err != nil {
		return nil, err
	}
	orderUC := healthUseCase.NewOrderUseCase(orderRepo)
	orderHdlr := handler.NewOrderHandler(orderUC, log)

	logger.Success("Container de dependencias inicializado...")

	return &Container{
		DB:            db,
		HealthHandler: healthHdlr,
		OrderHandler:  orderHdlr,
		Log:           log,
	}, nil
}
