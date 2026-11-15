package app

import (
	"microservice/pkg/logger"
)

func (a *App) Shutdown() {

	// Cerramos conexiones globales
	// Cierre DB, Colas, recursos , etc...

	logger.General("Cerrando conexiones y recursos...")

	if a.db != nil {
		a.db.Close()
	}

	logger.General("Recursos cerrados correctamente")
}
