// @title Microservice API
// @version 1.0
// @description API de ejemplo basada en Clean Architecture
// @host localhost:3200
// @BasePath /api/v1
package main

import (
	"fmt"
	"microservice/cmd/app"
	"microservice/pkg/logger"

	"go.uber.org/zap"
)

func main() {
	app := app.NewApp()

	if err := app.Init(); err != nil {
		logger.L().Fatal("Error al iniciar",
			zap.String("error_type", fmt.Sprintf("%T", err)),
		)
	}

	if err := app.Run(); err != nil {
		logger.L().Fatal("Error al ejecutar aplicativo",
			zap.String("error_type", fmt.Sprintf("%T", err)),
		)
	}
}
