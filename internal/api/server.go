package api

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"reflow/internal/util"
	"syscall"
	"time"

	"github.com/gorilla/mux"
)

const (
	defaultBindAddr = "0.0.0.0"
	defaultAPIPort  = "8585"
	shutdownTimeout = 10 * time.Second
)

// StartServer initializes and runs the Reflow internal API server.
func StartServer(basePath string, hostFlag string, portFlag string) error {
	bindAddr := defaultBindAddr
	if hostFlag != "" {
		if hostFlag == "localhost" {
			bindAddr = "127.0.0.1"
		} else if net.ParseIP(hostFlag) != nil {
			bindAddr = hostFlag
		} else {
			util.Log.Warnf("Invalid IP address or unsupported hostname ('%s') provided via --host flag. Defaulting API server to listen on '%s'.", hostFlag, defaultBindAddr)
		}
	}

	port := defaultAPIPort
	if portFlag != "" {
		port = portFlag
	}
	listenAddr := net.JoinHostPort(bindAddr, port)

	router := mux.NewRouter()
	RegisterRoutes(router, basePath)
	router.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "Reflow API Server running"})
	}).Methods(http.MethodGet)

	loggingHandler := loggingMiddleware(router)

	srv := &http.Server{
		Addr:         listenAddr,
		Handler:      loggingHandler,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	serverErrChan := make(chan error, 1)

	go func() {
		util.Log.Infof("Starting Reflow API server on http://%s", listenAddr)
		util.Log.Warn("API server is intended for local access by plugins only.")
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			util.Log.Errorf("API server ListenAndServe error: %v", err)
			serverErrChan <- fmt.Errorf("failed to start API server: %w", err)
		}
		close(serverErrChan)
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErrChan:
		return err
	case sig := <-quit:
		util.Log.Infof("Received signal %v. Shutting down API server...", sig)
	}

	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		util.Log.Errorf("API server forced to shutdown: %v", err)
		return fmt.Errorf("api server shutdown failed: %w", err)
	}

	util.Log.Info("API server stopped gracefully.")
	return nil
}
