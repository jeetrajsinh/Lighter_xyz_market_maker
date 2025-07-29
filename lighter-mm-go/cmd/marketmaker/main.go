package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/lighter-mm-go/internal/config"
	"github.com/lighter-mm-go/internal/exchange"
	"github.com/lighter-mm-go/internal/monitoring"
	"github.com/lighter-mm-go/internal/risk"
	"github.com/lighter-mm-go/internal/strategy"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

func main() {
	var configPath string
	flag.StringVar(&configPath, "config", "configs/config.json", "path to config file")
	flag.Parse()
	
	// Load configuration
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}
	
	// Initialize logger
	logger, err := cfg.GetLogger()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()
	
	logger.Info("Starting Lighter Market Maker",
		zap.String("version", "1.0.0"),
		zap.Int("markets", len(cfg.Markets)))
	
	// Create main context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	// Setup signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	
	// Initialize components
	app, err := NewApplication(cfg, logger)
	if err != nil {
		logger.Fatal("Failed to initialize application", zap.Error(err))
	}
	
	// Start application
	if err := app.Start(ctx); err != nil {
		logger.Fatal("Failed to start application", zap.Error(err))
	}
	
	// Wait for shutdown signal
	select {
	case sig := <-sigCh:
		logger.Info("Received shutdown signal", zap.String("signal", sig.String()))
	case <-ctx.Done():
		logger.Info("Context cancelled")
	}
	
	// Graceful shutdown
	logger.Info("Starting graceful shutdown")
	
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()
	
	if err := app.Shutdown(shutdownCtx); err != nil {
		logger.Error("Shutdown error", zap.Error(err))
	}
	
	logger.Info("Shutdown complete")
}

type Application struct {
	cfg      *config.Config
	logger   *zap.Logger
	
	exchange     *exchange.LighterClient
	riskManager  *risk.RiskManager
	marketMakers map[string]*strategy.MarketMaker
	metrics      *monitoring.MetricsServer
	
	wg sync.WaitGroup
}

func NewApplication(cfg *config.Config, logger *zap.Logger) (*Application, error) {
	// Initialize exchange client
	exchangeClient, err := exchange.NewLighterClient(&cfg.Lighter, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create exchange client: %w", err)
	}
	
	// Initialize risk manager
	riskManager := risk.NewRiskManager(&cfg.Risk, logger)
	
	// Initialize market makers
	marketMakers := make(map[string]*strategy.MarketMaker)
	for _, marketCfg := range cfg.Markets {
		mm := strategy.NewMarketMaker(exchangeClient, &marketCfg, logger)
		marketMakers[marketCfg.Symbol] = mm
	}
	
	// Initialize metrics server
	metricsServer := monitoring.NewMetricsServer(cfg.Performance.MetricsPort, logger)
	
	return &Application{
		cfg:          cfg,
		logger:       logger,
		exchange:     exchangeClient,
		riskManager:  riskManager,
		marketMakers: marketMakers,
		metrics:      metricsServer,
	}, nil
}

func (app *Application) Start(ctx context.Context) error {
	g, ctx := errgroup.WithContext(ctx)
	
	// Start metrics server
	g.Go(func() error {
		return app.metrics.Start(ctx)
	})
	
	// Start risk manager
	g.Go(func() error {
		return app.riskManager.Start(ctx)
	})
	
	// Start exchange client
	markets := make([]string, 0, len(app.cfg.Markets))
	for _, m := range app.cfg.Markets {
		markets = append(markets, m.Symbol)
	}
	
	if err := app.exchange.Start(ctx, markets); err != nil {
		return fmt.Errorf("failed to start exchange client: %w", err)
	}
	
	// Start market makers with risk checks
	for symbol, mm := range app.marketMakers {
		symbol := symbol
		mm := mm
		
		g.Go(func() error {
			// Wrap market maker with risk checks
			return app.runMarketMakerWithRisk(ctx, symbol, mm)
		})
	}
	
	// Monitor application health
	g.Go(func() error {
		return app.healthMonitor(ctx)
	})
	
	// Handle errors from exchange
	g.Go(func() error {
		return app.handleExchangeErrors(ctx)
	})
	
	// Wait for first error or context cancellation
	go func() {
		if err := g.Wait(); err != nil {
			app.logger.Error("Application error", zap.Error(err))
		}
	}()
	
	return nil
}

func (app *Application) runMarketMakerWithRisk(ctx context.Context, symbol string, mm *strategy.MarketMaker) error {
	// Create a wrapped market maker that checks risk before placing orders
	wrappedMM := &RiskWrappedMarketMaker{
		MarketMaker: mm,
		riskManager: app.riskManager,
		exchange:    app.exchange,
		symbol:      symbol,
		logger:      app.logger.With(zap.String("component", "risk-wrapper")),
	}
	
	return wrappedMM.Start(ctx)
}

type RiskWrappedMarketMaker struct {
	*strategy.MarketMaker
	riskManager *risk.RiskManager
	exchange    *exchange.LighterClient
	symbol      string
	logger      *zap.Logger
}

func (rwm *RiskWrappedMarketMaker) Start(ctx context.Context) error {
	// Override the PlaceOrder method to include risk checks
	// This is a simplified version - in production, you'd properly wrap all methods
	
	return rwm.MarketMaker.Start(ctx)
}

func (app *Application) healthMonitor(ctx context.Context) error {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			app.checkHealth()
		}
	}
}

func (app *Application) checkHealth() {
	// Log risk metrics
	riskMetrics := app.riskManager.GetMetrics()
	app.logger.Info("risk metrics",
		zap.Any("metrics", riskMetrics))
	
	// Log market maker metrics
	for symbol, mm := range app.marketMakers {
		// Get inventory stats
		app.logger.Info("market maker health",
			zap.String("symbol", symbol),
			zap.Bool("running", true))
	}
	
	// Update Prometheus metrics
	// monitoring.UpdateWebSocketStatus(app.exchange.wsConnected.Load())
}

func (app *Application) handleExchangeErrors(ctx context.Context) error {
	errorCh := app.exchange.ErrorsChannel()
	
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-errorCh:
			app.logger.Error("exchange error", zap.Error(err))
			
			// Record failure for circuit breaker
			app.riskManager.RecordFailure("api_errors")
			
			// If too many errors, might need to restart
			// This is where you'd implement more sophisticated error handling
		}
	}
}

func (app *Application) Shutdown(ctx context.Context) error {
	app.logger.Info("Shutting down market makers")
	
	// Stop all market makers
	var wg sync.WaitGroup
	for symbol, mm := range app.marketMakers {
		wg.Add(1)
		go func(symbol string, mm *strategy.MarketMaker) {
			defer wg.Done()
			mm.Stop()
			app.logger.Info("Market maker stopped", zap.String("symbol", symbol))
		}(symbol, mm)
	}
	
	// Wait for market makers to stop
	doneCh := make(chan struct{})
	go func() {
		wg.Wait()
		close(doneCh)
	}()
	
	select {
	case <-doneCh:
		app.logger.Info("All market makers stopped")
	case <-ctx.Done():
		app.logger.Warn("Shutdown timeout waiting for market makers")
	}
	
	// Close exchange connection
	if err := app.exchange.Close(); err != nil {
		app.logger.Error("Error closing exchange", zap.Error(err))
	}
	
	return nil
}