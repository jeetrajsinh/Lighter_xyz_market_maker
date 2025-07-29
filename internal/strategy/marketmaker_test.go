package strategy

import (
	"context"
	"math/big"
	"testing"
	"time"

	"github.com/lighter-mm-go/internal/config"
	"github.com/lighter-mm-go/internal/exchange"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"go.uber.org/zap"
)

type MockExchange struct {
	mock.Mock
}

func (m *MockExchange) GetOrderBook(market string) (*exchange.OrderBook, error) {
	args := m.Called(market)
	if ob := args.Get(0); ob != nil {
		return ob.(*exchange.OrderBook), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *MockExchange) PlaceOrder(ctx context.Context, market, side string, price, size *big.Float) (*exchange.Order, error) {
	args := m.Called(ctx, market, side, price, size)
	if order := args.Get(0); order != nil {
		return order.(*exchange.Order), args.Error(1)
	}
	return nil, args.Error(1)
}

func (m *MockExchange) CancelOrder(ctx context.Context, orderID string) error {
	args := m.Called(ctx, orderID)
	return args.Error(0)
}

func (m *MockExchange) GetActiveOrders(market string) []*exchange.Order {
	args := m.Called(market)
	if orders := args.Get(0); orders != nil {
		return orders.([]*exchange.Order)
	}
	return nil
}

func TestCalculateQuotes(t *testing.T) {
	logger := zap.NewNop()
	
	marketConfig := &config.MarketConfig{
		Symbol:              "ETH-USDC",
		BaseDecimals:        18,
		QuoteDecimals:       6,
		MinOrderSize:        "0.01",
		TickSize:            "0.01",
		MinSpreadBps:        10,
		MaxSpreadBps:        100,
		OrderLevels:         3,
		MaxPosition:         "10.0",
		OrderSizeMultiplier: 1.5,
		SkewFactor:          0.3,
	}
	
	mm := NewMarketMaker(nil, marketConfig, logger)
	
	state := &MarketState{
		BidPrice:       big.NewFloat(2000),
		AskPrice:       big.NewFloat(2002),
		MidPrice:       big.NewFloat(2001),
		Spread:         big.NewFloat(2),
		Volatility:     0.01,
		ImbalanceRatio: 0.1,
		UpdateTime:     time.Now(),
	}
	
	quotes := mm.calculateQuotes(state)
	
	// Should have 2 * OrderLevels quotes
	assert.Equal(t, 6, len(quotes))
	
	// Check bid quotes
	bidQuotes := quotes[:3]
	for i, quote := range bidQuotes {
		assert.Equal(t, "buy", quote.Side)
		assert.Equal(t, i, quote.Level)
		
		// Price should be below mid
		assert.True(t, quote.Price.Cmp(state.MidPrice) < 0)
		
		// Size should increase with level
		if i > 0 {
			assert.True(t, quote.Size.Cmp(bidQuotes[i-1].Size) > 0)
		}
	}
	
	// Check ask quotes  
	askQuotes := quotes[3:]
	for i, quote := range askQuotes {
		assert.Equal(t, "sell", quote.Side)
		assert.Equal(t, i, quote.Level)
		
		// Price should be above mid
		assert.True(t, quote.Price.Cmp(state.MidPrice) > 0)
		
		// Size should increase with level
		if i > 0 {
			assert.True(t, quote.Size.Cmp(askQuotes[i-1].Size) > 0)
		}
	}
}

func TestCalculateDynamicSpread(t *testing.T) {
	logger := zap.NewNop()
	
	marketConfig := &config.MarketConfig{
		Symbol:       "ETH-USDC",
		MinSpreadBps: 10,
		MaxSpreadBps: 100,
	}
	
	mm := NewMarketMaker(nil, marketConfig, logger)
	
	tests := []struct {
		name     string
		state    *MarketState
		minBps   float64
		maxBps   float64
	}{
		{
			name: "low volatility",
			state: &MarketState{
				Volatility:     0.001,
				ImbalanceRatio: 0,
			},
			minBps: 10,
			maxBps: 20,
		},
		{
			name: "high volatility",
			state: &MarketState{
				Volatility:     0.01,
				ImbalanceRatio: 0,
			},
			minBps: 100,
			maxBps: 120,
		},
		{
			name: "high imbalance",
			state: &MarketState{
				Volatility:     0.001,
				ImbalanceRatio: 0.5,
			},
			minBps: 20,
			maxBps: 40,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spread := mm.calculateDynamicSpread(tt.state)
			assert.GreaterOrEqual(t, spread, tt.minBps)
			assert.LessOrEqual(t, spread, tt.maxBps)
		})
	}
}

func TestRoundToTick(t *testing.T) {
	logger := zap.NewNop()
	
	marketConfig := &config.MarketConfig{
		TickSize: "0.01",
	}
	
	mm := NewMarketMaker(nil, marketConfig, logger)
	
	tests := []struct {
		input    float64
		expected float64
	}{
		{2000.123, 2000.12},
		{2000.126, 2000.12},
		{2000.129, 2000.12},
		{2000.00, 2000.00},
		{1999.995, 1999.99},
	}
	
	for _, tt := range tests {
		result := mm.roundToTick(big.NewFloat(tt.input))
		resultFloat, _ := result.Float64()
		assert.InDelta(t, tt.expected, resultFloat, 0.001)
	}
}

func TestVolatilityCalculation(t *testing.T) {
	logger := zap.NewNop()
	
	mm := NewMarketMaker(nil, nil, logger)
	
	// Generate price series with known volatility
	prices := []float64{
		100, 101, 99, 102, 98, 103, 97, 104, 96, 105,
		100, 101, 99, 102, 98, 103, 97, 104, 96, 105,
	}
	
	vol := mm.calculateVolatility(prices)
	
	// Should be positive
	assert.Greater(t, vol, 0.0)
	
	// For this oscillating pattern, volatility should be significant
	assert.Greater(t, vol, 0.1)
}

func BenchmarkCalculateQuotes(b *testing.B) {
	logger := zap.NewNop()
	
	marketConfig := &config.MarketConfig{
		Symbol:              "ETH-USDC",
		MinOrderSize:        "0.01",
		TickSize:            "0.01",
		MinSpreadBps:        10,
		MaxSpreadBps:        100,
		OrderLevels:         5,
		MaxPosition:         "10.0",
		OrderSizeMultiplier: 1.5,
		SkewFactor:          0.3,
	}
	
	mm := NewMarketMaker(nil, marketConfig, logger)
	mm.inventory.SetVolatility(0.01)
	
	state := &MarketState{
		BidPrice:       big.NewFloat(2000),
		AskPrice:       big.NewFloat(2002),
		MidPrice:       big.NewFloat(2001),
		Spread:         big.NewFloat(2),
		Volatility:     0.01,
		ImbalanceRatio: 0.1,
		UpdateTime:     time.Now(),
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = mm.calculateQuotes(state)
	}
}

func BenchmarkUpdateOrders(b *testing.B) {
	logger := zap.NewNop()
	ctx := context.Background()
	
	marketConfig := &config.MarketConfig{
		Symbol:              "ETH-USDC",
		MinOrderSize:        "0.01",
		TickSize:            "0.01",
		OrderLevels:         5,
		OrderSizeMultiplier: 1.5,
	}
	
	mockExchange := new(MockExchange)
	mm := &MarketMaker{
		exchange:     mockExchange,
		marketConfig: marketConfig,
		logger:       logger,
		inventory:    NewInventoryManager(marketConfig),
	}
	
	// Mock active orders
	activeOrders := make([]*exchange.Order, 0)
	mockExchange.On("GetActiveOrders", "ETH-USDC").Return(activeOrders)
	
	// Mock order placement
	mockExchange.On("PlaceOrder", mock.Anything, mock.Anything, mock.Anything, 
		mock.Anything, mock.Anything).Return(&exchange.Order{ID: "123"}, nil)
	
	quotes := []Quote{
		{Side: "buy", Price: big.NewFloat(1999), Size: big.NewFloat(0.01), Level: 0},
		{Side: "buy", Price: big.NewFloat(1998), Size: big.NewFloat(0.015), Level: 1},
		{Side: "sell", Price: big.NewFloat(2001), Size: big.NewFloat(0.01), Level: 0},
		{Side: "sell", Price: big.NewFloat(2002), Size: big.NewFloat(0.015), Level: 1},
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = mm.updateOrders(ctx, quotes)
	}
}