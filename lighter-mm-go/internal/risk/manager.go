package risk

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lighter-mm-go/internal/config"
	"github.com/lighter-mm-go/internal/exchange"
	"go.uber.org/zap"
)

type RiskManager struct {
	cfg      *config.RiskConfig
	logger   *zap.Logger
	
	// Position tracking
	positionsMu sync.RWMutex
	positions   map[string]*PositionRisk
	
	// PnL tracking
	dailyPnL    atomic.Value // *big.Float
	dailyVolume atomic.Value // *big.Float
	startTime   time.Time
	
	// Circuit breakers
	breakers    map[string]*CircuitBreaker
	breakersMu  sync.RWMutex
	
	// Metrics
	riskChecks   atomic.Uint64
	violations   atomic.Uint64
}

type PositionRisk struct {
	Market       string
	Position     *big.Float
	Notional     *big.Float
	MarkPrice    *big.Float
	UnrealizedPnL *big.Float
	LastUpdate   time.Time
}

type CircuitBreaker struct {
	Name         string
	Failures     atomic.Uint32
	LastFailure  atomic.Int64
	State        atomic.Uint32 // 0: closed, 1: open, 2: half-open
	OpenDuration time.Duration
	Threshold    uint32
}

const (
	CircuitClosed = iota
	CircuitOpen
	CircuitHalfOpen
)

func NewRiskManager(cfg *config.RiskConfig, logger *zap.Logger) *RiskManager {
	rm := &RiskManager{
		cfg:       cfg,
		logger:    logger,
		positions: make(map[string]*PositionRisk),
		breakers:  make(map[string]*CircuitBreaker),
		startTime: time.Now(),
	}
	
	rm.dailyPnL.Store(big.NewFloat(0))
	rm.dailyVolume.Store(big.NewFloat(0))
	
	// Initialize circuit breakers
	rm.initCircuitBreakers()
	
	return rm
}

func (rm *RiskManager) initCircuitBreakers() {
	// Order placement circuit breaker
	rm.breakers["order_placement"] = &CircuitBreaker{
		Name:         "order_placement",
		OpenDuration: 30 * time.Second,
		Threshold:    10,
	}
	
	// Loss limit circuit breaker
	rm.breakers["loss_limit"] = &CircuitBreaker{
		Name:         "loss_limit",
		OpenDuration: 5 * time.Minute,
		Threshold:    1,
	}
	
	// API error circuit breaker
	rm.breakers["api_errors"] = &CircuitBreaker{
		Name:         "api_errors",
		OpenDuration: 1 * time.Minute,
		Threshold:    5,
	}
}

func (rm *RiskManager) Start(ctx context.Context) error {
	// Position monitor
	go rm.positionMonitor(ctx)
	
	// Circuit breaker reset
	go rm.circuitBreakerManager(ctx)
	
	// Daily reset
	go rm.dailyReset(ctx)
	
	return nil
}

func (rm *RiskManager) CheckOrderRisk(market string, side string, price, size *big.Float) error {
	rm.riskChecks.Add(1)
	
	// Check circuit breakers
	if err := rm.checkCircuitBreakers(); err != nil {
		rm.violations.Add(1)
		return err
	}
	
	// Calculate order notional
	notional := new(big.Float).Mul(price, size)
	notionalUSD, _ := notional.Float64()
	
	// Check single order limit
	if notionalUSD > rm.cfg.MaxOrderValueUSD {
		rm.violations.Add(1)
		return fmt.Errorf("order value %.2f exceeds limit %.2f", 
			notionalUSD, rm.cfg.MaxOrderValueUSD)
	}
	
	// Check position limits
	if err := rm.checkPositionLimits(market, side, size, price); err != nil {
		rm.violations.Add(1)
		return err
	}
	
	// Check daily loss limit
	if err := rm.checkDailyLossLimit(); err != nil {
		rm.violations.Add(1)
		rm.tripCircuitBreaker("loss_limit")
		return err
	}
	
	return nil
}

func (rm *RiskManager) checkCircuitBreakers() error {
	rm.breakersMu.RLock()
	defer rm.breakersMu.RUnlock()
	
	for name, breaker := range rm.breakers {
		if breaker.IsOpen() {
			return fmt.Errorf("circuit breaker '%s' is open", name)
		}
	}
	
	return nil
}

func (rm *RiskManager) checkPositionLimits(market, side string, size, price *big.Float) error {
	rm.positionsMu.Lock()
	defer rm.positionsMu.Unlock()
	
	posRisk, exists := rm.positions[market]
	if !exists {
		posRisk = &PositionRisk{
			Market:   market,
			Position: big.NewFloat(0),
			Notional: big.NewFloat(0),
		}
		rm.positions[market] = posRisk
	}
	
	// Calculate new position
	newPosition := new(big.Float).Set(posRisk.Position)
	if side == "buy" {
		newPosition.Add(newPosition, size)
	} else {
		newPosition.Sub(newPosition, size)
	}
	
	// Calculate new notional
	newNotional := new(big.Float).Mul(newPosition, price)
	newNotional.Abs(newNotional)
	newNotionalUSD, _ := newNotional.Float64()
	
	// Check total position limit
	totalNotional := rm.getTotalNotional()
	orderNotional := new(big.Float).Mul(size, price)
	totalNotional.Add(totalNotional, orderNotional)
	totalNotionalUSD, _ := totalNotional.Float64()
	
	if totalNotionalUSD > rm.cfg.MaxTotalPositionUSD {
		return fmt.Errorf("total position %.2f would exceed limit %.2f",
			totalNotionalUSD, rm.cfg.MaxTotalPositionUSD)
	}
	
	return nil
}

func (rm *RiskManager) checkDailyLossLimit() error {
	dailyPnL := rm.dailyPnL.Load().(*big.Float)
	pnlFloat, _ := dailyPnL.Float64()
	
	if pnlFloat < -rm.cfg.DailyLossLimitUSD {
		return fmt.Errorf("daily loss %.2f exceeds limit %.2f",
			-pnlFloat, rm.cfg.DailyLossLimitUSD)
	}
	
	return nil
}

func (rm *RiskManager) UpdatePosition(market string, position *exchange.Position) {
	rm.positionsMu.Lock()
	defer rm.positionsMu.Unlock()
	
	posRisk := &PositionRisk{
		Market:        market,
		Position:      position.Size,
		MarkPrice:     position.AvgPrice,
		UnrealizedPnL: position.PnL,
		LastUpdate:    time.Now(),
	}
	
	notional := new(big.Float).Mul(position.Size, position.AvgPrice)
	posRisk.Notional = notional.Abs(notional)
	
	rm.positions[market] = posRisk
}

func (rm *RiskManager) UpdateFill(fill *exchange.Fill) {
	// Update daily volume
	volume := rm.dailyVolume.Load().(*big.Float)
	fillNotional := new(big.Float).Mul(fill.Price, fill.Size)
	newVolume := new(big.Float).Add(volume, fillNotional)
	rm.dailyVolume.Store(newVolume)
	
	// Update daily PnL (simplified - should track realized PnL properly)
	// This is a placeholder - real implementation would track entry/exit prices
}

func (rm *RiskManager) getTotalNotional() *big.Float {
	rm.positionsMu.RLock()
	defer rm.positionsMu.RUnlock()
	
	total := big.NewFloat(0)
	for _, pos := range rm.positions {
		total.Add(total, pos.Notional)
	}
	
	return total
}

func (rm *RiskManager) positionMonitor(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(rm.cfg.PositionCheckInterval) * time.Millisecond)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			rm.checkPositionHealth()
		}
	}
}

func (rm *RiskManager) checkPositionHealth() {
	rm.positionsMu.RLock()
	defer rm.positionsMu.RUnlock()
	
	now := time.Now()
	for market, pos := range rm.positions {
		// Check for stale positions
		if now.Sub(pos.LastUpdate) > 30*time.Second {
			rm.logger.Warn("stale position data",
				zap.String("market", market),
				zap.Duration("age", now.Sub(pos.LastUpdate)))
		}
		
		// Check unrealized PnL
		if pnl, _ := pos.UnrealizedPnL.Float64(); pnl < -rm.cfg.DailyLossLimitUSD/10 {
			rm.logger.Warn("large unrealized loss",
				zap.String("market", market),
				zap.Float64("pnl", pnl))
		}
	}
}

func (rm *RiskManager) circuitBreakerManager(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			rm.updateCircuitBreakers()
		}
	}
}

func (rm *RiskManager) updateCircuitBreakers() {
	rm.breakersMu.Lock()
	defer rm.breakersMu.Unlock()
	
	now := time.Now()
	
	for _, breaker := range rm.breakers {
		state := breaker.State.Load()
		
		switch state {
		case CircuitOpen:
			lastFailure := time.Unix(0, breaker.LastFailure.Load())
			if now.Sub(lastFailure) > breaker.OpenDuration {
				// Move to half-open
				breaker.State.Store(CircuitHalfOpen)
				rm.logger.Info("circuit breaker moved to half-open",
					zap.String("breaker", breaker.Name))
			}
			
		case CircuitHalfOpen:
			// Reset failures counter to allow retry
			breaker.Failures.Store(0)
		}
	}
}

func (rm *RiskManager) tripCircuitBreaker(name string) {
	rm.breakersMu.Lock()
	defer rm.breakersMu.Unlock()
	
	breaker, exists := rm.breakers[name]
	if !exists {
		return
	}
	
	failures := breaker.Failures.Add(1)
	breaker.LastFailure.Store(time.Now().UnixNano())
	
	if failures >= breaker.Threshold {
		breaker.State.Store(CircuitOpen)
		rm.logger.Error("circuit breaker tripped",
			zap.String("breaker", name),
			zap.Uint32("failures", failures))
	}
}

func (rm *RiskManager) RecordSuccess(operation string) {
	rm.breakersMu.Lock()
	defer rm.breakersMu.Unlock()
	
	if breaker, exists := rm.breakers[operation]; exists {
		if breaker.State.Load() == CircuitHalfOpen {
			// Success in half-open state, close the circuit
			breaker.State.Store(CircuitClosed)
			breaker.Failures.Store(0)
			rm.logger.Info("circuit breaker closed",
				zap.String("breaker", operation))
		}
	}
}

func (rm *RiskManager) RecordFailure(operation string) {
	rm.tripCircuitBreaker(operation)
}

func (rm *RiskManager) dailyReset(ctx context.Context) {
	for {
		// Calculate time until next UTC midnight
		now := time.Now().UTC()
		tomorrow := now.AddDate(0, 0, 1)
		midnight := time.Date(tomorrow.Year(), tomorrow.Month(), tomorrow.Day(), 
			0, 0, 0, 0, time.UTC)
		
		sleepDuration := midnight.Sub(now)
		
		select {
		case <-ctx.Done():
			return
		case <-time.After(sleepDuration):
			rm.resetDailyMetrics()
		}
	}
}

func (rm *RiskManager) resetDailyMetrics() {
	rm.dailyPnL.Store(big.NewFloat(0))
	rm.dailyVolume.Store(big.NewFloat(0))
	rm.startTime = time.Now()
	
	rm.logger.Info("daily risk metrics reset")
}

func (rm *RiskManager) GetMetrics() map[string]interface{} {
	dailyPnL := rm.dailyPnL.Load().(*big.Float)
	dailyVolume := rm.dailyVolume.Load().(*big.Float)
	
	pnlFloat, _ := dailyPnL.Float64()
	volumeFloat, _ := dailyVolume.Float64()
	
	rm.positionsMu.RLock()
	positionCount := len(rm.positions)
	totalNotional, _ := rm.getTotalNotional().Float64()
	rm.positionsMu.RUnlock()
	
	rm.breakersMu.RLock()
	openBreakers := 0
	for _, breaker := range rm.breakers {
		if breaker.IsOpen() {
			openBreakers++
		}
	}
	rm.breakersMu.RUnlock()
	
	return map[string]interface{}{
		"dailyPnL":        pnlFloat,
		"dailyVolume":     volumeFloat,
		"totalNotional":   totalNotional,
		"positionCount":   positionCount,
		"riskChecks":      rm.riskChecks.Load(),
		"violations":      rm.violations.Load(),
		"openBreakers":    openBreakers,
		"uptimeHours":     time.Since(rm.startTime).Hours(),
	}
}

func (cb *CircuitBreaker) IsOpen() bool {
	return cb.State.Load() == CircuitOpen
}

func (cb *CircuitBreaker) Reset() {
	cb.State.Store(CircuitClosed)
	cb.Failures.Store(0)
}