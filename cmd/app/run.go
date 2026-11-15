package app

import (
	"context"
	"errors"
	"fmt"
	"microservice/pkg/logger"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"go.uber.org/zap"
)

func (a *App) Run() error {
	defer logger.L().Sync()

	srv := &http.Server{
		Addr:    ":" + strconv.Itoa(a.config.Port),
		Handler: a.router,
	}

	serveErr := make(chan error, 1)
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(quit)

	go func() {
		logger.L().Info("Microservice iniciado correctamente",
			zap.Int("port", a.config.Port),
		)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
	}()

	select {
	case sig := <-quit:
		logger.L().Info("Señal de apagado recibida",
			zap.String("signal", sig.String()),
		)
	case err := <-serveErr:
		logger.L().Error("El servidor HTTP terminó inesperadamente",
			zap.String("error_type", fmt.Sprintf("%T", err)),
			zap.Int("port", a.config.Port),
		)
		if shutdownErr := a.shutdownServer(srv); shutdownErr != nil {
			return errors.Join(err, shutdownErr)
		}
		return err
	}

	if err := a.shutdownServer(srv); err != nil {
		return err
	}

	logger.L().Info("Servidor terminado correctamente")
	return nil
}

func (a *App) shutdownServer(srv *http.Server) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(a.config.ShutDownTimeOut)*time.Second)
	defer cancel()

	err := srv.Shutdown(ctx)
	if err != nil {
		logger.L().Error("Se agotó el límite para drenar solicitudes; cerrando conexiones activas",
			zap.String("error_type", fmt.Sprintf("%T", err)),
			zap.Int("timeout_seconds", a.config.ShutDownTimeOut),
		)
		if closeErr := srv.Close(); closeErr != nil {
			err = errors.Join(err, closeErr)
		}
	}

	a.Shutdown()
	return err
}
