package main

import (
	"context"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"go.uber.org/zap"

	"github.com/jarsater/mcp-fabric/gateway/internal/api"
	"github.com/jarsater/mcp-fabric/gateway/internal/k8s"
	"github.com/jarsater/mcp-fabric/gateway/internal/mcp"
	"github.com/jarsater/mcp-fabric/gateway/internal/metrics"
	"github.com/jarsater/mcp-fabric/gateway/internal/routes"
	"github.com/jarsater/mcp-fabric/pkg/logging"
)

func main() {
	var (
		addr           string
		metricsAddr    string
		routesFile     string
		requestTimeout time.Duration
		mcpEnabled     bool
		mcpNamespace   string
	)

	flag.StringVar(&addr, "addr", ":8080", "HTTP listen address")
	flag.StringVar(&metricsAddr, "metrics-addr", ":9090", "Metrics listen address")
	flag.StringVar(&routesFile, "routes-file", "/etc/gateway/routes.json", "Path to routes configuration file")
	flag.DurationVar(&requestTimeout, "request-timeout", 5*time.Minute, "Request timeout for agent calls")
	flag.BoolVar(&mcpEnabled, "mcp-enabled", true, "Enable MCP protocol endpoints")
	flag.StringVar(&mcpNamespace, "mcp-namespace", "", "Namespace to watch for agents (empty = all namespaces)")
	flag.Parse()

	// Initialize logger
	logger := logging.NewLogger("gateway")
	defer func() { _ = logger.Sync() }()

	logger.Infof("Starting agent gateway on %s (mcp=%v, metrics=%s)", addr, mcpEnabled, metricsAddr)

	// Initialize route table
	table := routes.NewTable()

	// Load initial routes
	if err := table.LoadFromFile(routesFile); err != nil {
		logger.Warnf("Failed to load routes from %s: %v", routesFile, err)
	} else {
		logger.Infof("Loaded routes from %s", routesFile)
	}

	// Create handler
	handler := api.NewHandler(table, requestTimeout)
	handler.UpdateDefaults()

	// Setup file watcher for hot-reload
	go watchRoutesFile(logger, routesFile, table, handler)

	// Create HTTP mux
	mux := http.NewServeMux()

	// Register API routes
	mux.Handle("/v1/", handler)
	mux.Handle("/healthz", handler)

	// Setup MCP if enabled
	var mcpHandler *mcp.Handler
	if mcpEnabled {
		watcher, err := k8s.NewAgentWatcher(logger, mcpNamespace, nil)
		if err != nil {
			logger.Warnf("Failed to create agent watcher: %v (MCP disabled)", err)
		} else {
			mcpHandler = mcp.NewHandler(logger, watcher)

			// Notify MCP clients when agents change
			watcher, _ = k8s.NewAgentWatcher(logger, mcpNamespace, func() {
				if mcpHandler != nil {
					mcpHandler.NotifyToolsListChanged()
				}
			})

			// Start watcher
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			if err := watcher.Start(ctx); err != nil {
				logger.Warnf("Failed to start agent watcher: %v", err)
			} else {
				// Re-create handler with working watcher
				mcpHandler = mcp.NewHandler(logger, watcher)

				// Register MCP routes
				mux.HandleFunc("/mcp", mcpHandler.HandleHTTP)    // HTTP transport (recommended)
				mux.HandleFunc("/mcp/sse", mcpHandler.HandleSSE) // SSE transport (deprecated)
				mux.HandleFunc("/mcp/message", mcpHandler.HandleMessage)
				logger.Info("MCP endpoints enabled: /mcp (HTTP), /mcp/sse (SSE)")
			}
		}
	}

	// Create main server
	server := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: requestTimeout + 10*time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Create metrics server
	metricsMux := http.NewServeMux()
	metricsMux.Handle("/metrics", metrics.Handler())
	metricsMux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	metricsServer := &http.Server{
		Addr:         metricsAddr,
		Handler:      metricsMux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	// Start main server in goroutine
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatalf("Server error: %v", err)
		}
	}()

	// Start metrics server in goroutine
	go func() {
		if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Errorf("Metrics server error: %v", err)
		}
	}()

	logger.Infof("Agent gateway listening on %s (metrics on %s)", addr, metricsAddr)

	// Wait for shutdown signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Shutting down servers...")

	// Graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		logger.Errorf("Server shutdown error: %v", err)
	}

	if err := metricsServer.Shutdown(ctx); err != nil {
		logger.Errorf("Metrics server shutdown error: %v", err)
	}

	logger.Info("Servers stopped")
}

func watchRoutesFile(logger *zap.SugaredLogger, path string, table *routes.Table, handler *api.Handler) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		logger.Errorf("Failed to create file watcher: %v", err)
		return
	}
	defer func() { _ = watcher.Close() }()

	// Watch the directory containing the file
	dir := filepath.Dir(path)
	if err := watcher.Add(dir); err != nil {
		logger.Errorf("Failed to watch directory %s: %v", dir, err)
		return
	}

	logger.Infof("Watching %s for changes", path)

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}

			// Check if this is our file
			if filepath.Base(event.Name) != filepath.Base(path) {
				continue
			}

			// Reload on write or create
			if event.Op&(fsnotify.Write|fsnotify.Create) != 0 {
				logger.Info("Routes file changed, reloading...")

				// Small delay to ensure file is fully written
				time.Sleep(100 * time.Millisecond)

				if err := table.LoadFromFile(path); err != nil {
					logger.Errorf("Failed to reload routes: %v", err)
				} else {
					handler.UpdateDefaults()
					logger.Info("Routes reloaded successfully")
				}
			}

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			logger.Errorf("File watcher error: %v", err)
		}
	}
}
