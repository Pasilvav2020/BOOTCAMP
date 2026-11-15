package app

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"microservice/cmd/config"
	"microservice/pkg/database"
	"microservice/pkg/logger"
)

type shutdownTestDatabase struct {
	closed atomic.Bool
}

func (d *shutdownTestDatabase) Ping(context.Context) error { return nil }
func (d *shutdownTestDatabase) Close()                     { d.closed.Store(true) }
func (d *shutdownTestDatabase) Native() interface{}        { return nil }

var _ database.Database = (*shutdownTestDatabase)(nil)

func TestShutdownServerDrainsActiveRequests(t *testing.T) {
	logger.Init("dev")
	started := make(chan struct{})
	release := make(chan struct{})
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		w.WriteHeader(http.StatusOK)
	})}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		_ = server.Serve(listener)
	}()

	response := make(chan error, 1)
	go func() {
		res, err := http.Get("http://" + listener.Addr().String())
		if err != nil {
			response <- err
			return
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK {
			response <- errors.New("request did not complete successfully")
			return
		}
		response <- nil
	}()
	<-started

	db := &shutdownTestDatabase{}
	app := &App{config: &config.Configuration{ShutDownTimeOut: 2}, db: db}
	shutdownDone := make(chan error, 1)
	go func() {
		shutdownDone <- app.shutdownServer(server)
	}()

	time.Sleep(20 * time.Millisecond)
	close(release)

	if err := <-shutdownDone; err != nil {
		t.Fatalf("shutdownServer() error = %v", err)
	}
	if err := <-response; err != nil {
		t.Fatalf("active request failed during graceful shutdown: %v", err)
	}
	if !db.closed.Load() {
		t.Fatal("database was not closed after requests drained")
	}
}

func TestShutdownServerForcesCloseAfterTimeoutAndClosesResources(t *testing.T) {
	logger.Init("dev")
	started := make(chan struct{})
	server := &http.Server{Handler: http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	})}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		_ = server.Serve(listener)
	}()
	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		res, err := http.Get("http://" + listener.Addr().String())
		if err == nil {
			res.Body.Close()
		}
	}()
	<-started

	db := &shutdownTestDatabase{}
	app := &App{config: &config.Configuration{ShutDownTimeOut: 1}, db: db}
	err = app.shutdownServer(server)
	if err == nil {
		t.Fatal("shutdownServer() error = nil, want timeout")
	}
	if !db.closed.Load() {
		t.Fatal("database was not closed after shutdown timeout")
	}
	select {
	case <-requestDone:
	case <-time.After(time.Second):
		t.Fatal("active request was not interrupted after timeout")
	}
}
