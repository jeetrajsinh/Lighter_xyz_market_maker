package monitoring

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"runtime"

	"go.uber.org/zap"
)

var (
	// Order metrics
	ordersPlaced = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "marketmaker_orders_placed_total",
		Help: "Total number of orders placed",
	}, []string{"market", "side"})
	
	ordersCanceled = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "marketmaker_orders_canceled_total",
		Help: "Total number of orders canceled",
	}, []string{"market"})
	
	orderPlacementDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "marketmaker_order_placement_duration_seconds",
		Help:    "Order placement duration in seconds",
		Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1},
	}, []string{"market"})
	
	// Fill metrics
	fillsReceived = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "marketmaker_fills_received_total",
		Help: "Total number of fills received",
	}, []string{"market", "side"})
	
	fillVolume = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "marketmaker_fill_volume_total",
		Help: "Total fill volume in base currency",
	}, []string{"market", "side"})
	
	// Position metrics
	currentPosition = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "marketmaker_position",
		Help: "Current position in base currency",
	}, []string{"market"})
	
	positionNotional = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "marketmaker_position_notional_usd",
		Help: "Current position notional value in USD",
	}, []string{"market"})
	
	unrealizedPnL = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "marketmaker_unrealized_pnl_usd",
		Help: "Unrealized PnL in USD",
	}, []string{"market"})
	
	// Spread metrics
	currentSpread = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "marketmaker_spread_bps",
		Help: "Current spread in basis points",
	}, []string{"market"})
	
	inventorySkew = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "marketmaker_inventory_skew",
		Help: "Inventory skew (-1 to 1)",
	}, []string{"market"})
	
	// Risk metrics
	riskChecks = promauto.NewCounter(prometheus.CounterOpts{
		Name: "marketmaker_risk_checks_total",
		Help: "Total number of risk checks performed",
	})
	
	riskViolations = promauto.NewCounter(prometheus.CounterOpts{
		Name: "marketmaker_risk_violations_total",
		Help: "Total number of risk violations",
	})
	
	circuitBreakerState = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "marketmaker_circuit_breaker_state",
		Help: "Circuit breaker state (0=closed, 1=open, 2=half-open)",
	}, []string{"breaker"})
	
	// WebSocket metrics
	websocketConnected = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "marketmaker_websocket_connected",
		Help: "WebSocket connection status (1=connected, 0=disconnected)",
	})
	
	websocketMessages = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "marketmaker_websocket_messages_total",
		Help: "Total WebSocket messages received",
	}, []string{"type"})
	
	websocketReconnects = promauto.NewCounter(prometheus.CounterOpts{
		Name: "marketmaker_websocket_reconnects_total",
		Help: "Total WebSocket reconnection attempts",
	})
	
	// Performance metrics
	updateCycleTime = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "marketmaker_update_cycle_duration_seconds",
		Help:    "Market maker update cycle duration",
		Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5},
	})
	
	// System metrics
	goroutines = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "marketmaker_goroutines",
		Help: "Number of active goroutines",
	})
	
	memoryUsage = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "marketmaker_memory_bytes",
		Help: "Memory usage in bytes",
	}, []string{"type"})
)

type MetricsServer struct {
	server *http.Server
	logger *zap.Logger
}

func NewMetricsServer(port int, logger *zap.Logger) *MetricsServer {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.HandleFunc("/health", healthHandler)
	
	return &MetricsServer{
		server: &http.Server{
			Addr:         fmt.Sprintf(":%d", port),
			Handler:      mux,
			ReadTimeout:  10 * time.Second,
			WriteTimeout: 10 * time.Second,
		},
		logger: logger,
	}
}

func (ms *MetricsServer) Start(ctx context.Context) error {
	ms.logger.Info("starting metrics server", zap.String("addr", ms.server.Addr))
	
	go ms.collectSystemMetrics(ctx)
	
	go func() {
		if err := ms.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			ms.logger.Error("metrics server error", zap.Error(err))
		}
	}()
	
	<-ctx.Done()
	
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	return ms.server.Shutdown(shutdownCtx)
}

func (ms *MetricsServer) collectSystemMetrics(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			collectRuntimeMetrics()
		}
	}
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func collectRuntimeMetrics() {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	
	goroutines.Set(float64(runtime.NumGoroutine()))
	memoryUsage.WithLabelValues("alloc").Set(float64(m.Alloc))
	memoryUsage.WithLabelValues("sys").Set(float64(m.Sys))
	memoryUsage.WithLabelValues("heap").Set(float64(m.HeapAlloc))
}

// Helper functions for recording metrics

func RecordOrderPlaced(market, side string) {
	ordersPlaced.WithLabelValues(market, side).Inc()
}

func RecordOrderCanceled(market string) {
	ordersCanceled.WithLabelValues(market).Inc()
}

func RecordOrderPlacementTime(market string, duration time.Duration) {
	orderPlacementDuration.WithLabelValues(market).Observe(duration.Seconds())
}

func RecordFill(market, side string, volume float64) {
	fillsReceived.WithLabelValues(market, side).Inc()
	fillVolume.WithLabelValues(market, side).Add(volume)
}

func UpdatePosition(market string, position, notional, pnl float64) {
	currentPosition.WithLabelValues(market).Set(position)
	positionNotional.WithLabelValues(market).Set(notional)
	unrealizedPnL.WithLabelValues(market).Set(pnl)
}

func UpdateSpread(market string, spreadBps float64) {
	currentSpread.WithLabelValues(market).Set(spreadBps)
}

func UpdateInventorySkew(market string, skew float64) {
	inventorySkew.WithLabelValues(market).Set(skew)
}

func RecordRiskCheck() {
	riskChecks.Inc()
}

func RecordRiskViolation() {
	riskViolations.Inc()
}

func UpdateCircuitBreaker(name string, state uint32) {
	circuitBreakerState.WithLabelValues(name).Set(float64(state))
}

func UpdateWebSocketStatus(connected bool) {
	if connected {
		websocketConnected.Set(1)
	} else {
		websocketConnected.Set(0)
	}
}

func RecordWebSocketMessage(msgType string) {
	websocketMessages.WithLabelValues(msgType).Inc()
}

func RecordWebSocketReconnect() {
	websocketReconnects.Inc()
}

func RecordUpdateCycle(duration time.Duration) {
	updateCycleTime.Observe(duration.Seconds())
}