package risk

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/lighter-mm-go/internal/config"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestCheckOrderRisk(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.RiskConfig{
		MaxTotalPositionUSD: 10000,
		MaxOrderValueUSD:    1000,
		DailyLossLimitUSD:   500,
		PositionCheckInterval: 100,
	}
	
	rm := NewRiskManager(cfg, logger)
	
	tests := []struct {
		name      string
		market    string
		side      string
		price     float64
		size      float64
		expectErr bool
	}{
		{
			name:      "valid order",
			market:    "ETH-USDC",
			side:      "buy",
			price:     2000,
			size:      0.1,
			expectErr: false,
		},
		{
			name:      "order too large",
			market:    "ETH-USDC",
			side:      "buy",
			price:     2000,
			size:      1,
			expectErr: true,
		},
		{
			name:      "small order",
			market:    "ETH-USDC",
			side:      "sell",
			price:     2000,
			size:      0.01,
			expectErr: false,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := rm.CheckOrderRisk(
				tt.market,
				tt.side,
				big.NewFloat(tt.price),
				big.NewFloat(tt.size),
			)
			
			if tt.expectErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestCircuitBreaker(t *testing.T) {
	cb := &CircuitBreaker{
		Name:         "test",
		OpenDuration: 100 * time.Millisecond,
		Threshold:    3,
	}
	
	// Initially closed
	assert.False(t, cb.IsOpen())
	
	// Trip the breaker
	cb.Failures.Store(3)
	cb.State.Store(CircuitOpen)
	cb.LastFailure.Store(time.Now().UnixNano())
	
	// Should be open
	assert.True(t, cb.IsOpen())
	
	// Wait for duration
	time.Sleep(150 * time.Millisecond)
	
	// Manually transition to half-open (normally done by manager)
	cb.State.Store(CircuitHalfOpen)
	
	// Should not be open in half-open state
	assert.False(t, cb.IsOpen())
	
	// Reset
	cb.Reset()
	assert.Equal(t, uint32(0), cb.Failures.Load())
	assert.Equal(t, uint32(CircuitClosed), cb.State.Load())
}

func TestPositionLimits(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.RiskConfig{
		MaxTotalPositionUSD: 10000,
		MaxOrderValueUSD:    5000,
	}
	
	rm := NewRiskManager(cfg, logger)
	
	// Add existing position
	rm.positionsMu.Lock()
	rm.positions["ETH-USDC"] = &PositionRisk{
		Market:   "ETH-USDC",
		Position: big.NewFloat(2),
		Notional: big.NewFloat(4000),
	}
	rm.positionsMu.Unlock()
	
	// Try to add order that would exceed limit
	err := rm.checkPositionLimits(
		"ETH-USDC",
		"buy",
		big.NewFloat(4),
		big.NewFloat(2000),
	)
	
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "total position")
}

func TestDailyLossLimit(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.RiskConfig{
		DailyLossLimitUSD: 1000,
	}
	
	rm := NewRiskManager(cfg, logger)
	
	// Set daily PnL to loss
	rm.dailyPnL.Store(big.NewFloat(-500))
	
	// Should pass
	err := rm.checkDailyLossLimit()
	assert.NoError(t, err)
	
	// Set to exceed limit
	rm.dailyPnL.Store(big.NewFloat(-1500))
	
	// Should fail
	err = rm.checkDailyLossLimit()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "daily loss")
}

func TestGetMetrics(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.RiskConfig{
		MaxTotalPositionUSD: 10000,
		MaxOrderValueUSD:    1000,
		DailyLossLimitUSD:   500,
	}
	
	rm := NewRiskManager(cfg, logger)
	
	// Add some data
	rm.dailyPnL.Store(big.NewFloat(-100))
	rm.dailyVolume.Store(big.NewFloat(50000))
	rm.riskChecks.Store(100)
	rm.violations.Store(5)
	
	metrics := rm.GetMetrics()
	
	assert.Equal(t, -100.0, metrics["dailyPnL"])
	assert.Equal(t, 50000.0, metrics["dailyVolume"])
	assert.Equal(t, uint64(100), metrics["riskChecks"])
	assert.Equal(t, uint64(5), metrics["violations"])
	assert.Equal(t, 0, metrics["positionCount"])
	assert.Equal(t, 0, metrics["openBreakers"])
}

func TestConcurrentRiskChecks(t *testing.T) {
	logger := zap.NewNop()
	cfg := &config.RiskConfig{
		MaxTotalPositionUSD: 10000,
		MaxOrderValueUSD:    1000,
		DailyLossLimitUSD:   500,
	}
	
	rm := NewRiskManager(cfg, logger)
	ctx := context.Background()
	rm.Start(ctx)
	
	// Run concurrent risk checks
	done := make(chan bool, 10)
	
	for i := 0; i < 10; i++ {
		go func() {
			err := rm.CheckOrderRisk(
				"ETH-USDC",
				"buy",
				big.NewFloat(2000),
				big.NewFloat(0.1),
			)
			assert.NoError(t, err)
			done <- true
		}()
	}
	
	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}
	
	// Check metrics
	assert.Equal(t, uint64(10), rm.riskChecks.Load())
	assert.Equal(t, uint64(0), rm.violations.Load())
}

func BenchmarkCheckOrderRisk(b *testing.B) {
	logger := zap.NewNop()
	cfg := &config.RiskConfig{
		MaxTotalPositionUSD: 10000,
		MaxOrderValueUSD:    1000,
		DailyLossLimitUSD:   500,
	}
	
	rm := NewRiskManager(cfg, logger)
	
	price := big.NewFloat(2000)
	size := big.NewFloat(0.1)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = rm.CheckOrderRisk("ETH-USDC", "buy", price, size)
	}
}

func BenchmarkGetMetrics(b *testing.B) {
	logger := zap.NewNop()
	cfg := &config.RiskConfig{
		MaxTotalPositionUSD: 10000,
		MaxOrderValueUSD:    1000,
		DailyLossLimitUSD:   500,
	}
	
	rm := NewRiskManager(cfg, logger)
	
	// Add some positions
	for i := 0; i < 10; i++ {
		rm.positions[string(rune('A'+i))] = &PositionRisk{
			Market:   string(rune('A' + i)),
			Position: big.NewFloat(float64(i)),
			Notional: big.NewFloat(float64(i * 1000)),
		}
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = rm.GetMetrics()
	}
}