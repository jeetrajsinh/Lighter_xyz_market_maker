package strategy

import (
	"context"
	"fmt"
	"math"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lighter-mm-go/internal/config"
	"github.com/lighter-mm-go/internal/exchange"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

type MarketMaker struct {
	exchange      *exchange.LighterClient
	marketConfig  *config.MarketConfig
	logger        *zap.Logger
	
	inventory     *InventoryManager
	
	// Performance optimization
	orderCache    sync.Map
	lastUpdate    atomic.Int64
	
	// Metrics
	ordersPlaced  atomic.Uint64
	ordersCanceled atomic.Uint64
	fillsReceived atomic.Uint64
	
	// Control
	running       atomic.Bool
	updateCh      chan struct{}
	stopCh        chan struct{}
}

type MarketState struct {
	BidPrice      *big.Float
	AskPrice      *big.Float
	MidPrice      *big.Float
	Spread        *big.Float
	Volatility    float64
	ImbalanceRatio float64
	UpdateTime    time.Time
}

func NewMarketMaker(
	exchange *exchange.LighterClient,
	marketConfig *config.MarketConfig,
	logger *zap.Logger,
) *MarketMaker {
	return &MarketMaker{
		exchange:     exchange,
		marketConfig: marketConfig,
		logger:       logger.With(zap.String("market", marketConfig.Symbol)),
		inventory:    NewInventoryManager(marketConfig),
		updateCh:     make(chan struct{}, 100),
		stopCh:       make(chan struct{}),
	}
}

func (mm *MarketMaker) Start(ctx context.Context) error {
	if !mm.running.CompareAndSwap(false, true) {
		return fmt.Errorf("market maker already running")
	}
	
	mm.logger.Info("starting market maker")
	
	g, ctx := errgroup.WithContext(ctx)
	
	// Order update handler
	g.Go(func() error {
		return mm.handleOrderUpdates(ctx)
	})
	
	// Fill handler
	g.Go(func() error {
		return mm.handleFills(ctx)
	})
	
	// Main market making loop
	g.Go(func() error {
		return mm.mainLoop(ctx)
	})
	
	// Volatility calculator
	g.Go(func() error {
		return mm.volatilityTracker(ctx)
	})
	
	return g.Wait()
}

func (mm *MarketMaker) mainLoop(ctx context.Context) error {
	ticker := time.NewTicker(time.Duration(mm.marketConfig.OrderLevels) * time.Millisecond)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			return mm.shutdown()
		case <-mm.stopCh:
			return mm.shutdown()
		case <-ticker.C:
			mm.updateQuotes(ctx)
		case <-mm.updateCh:
			mm.updateQuotes(ctx)
		}
	}
}

func (mm *MarketMaker) updateQuotes(ctx context.Context) {
	start := time.Now()
	
	// Get current market state
	state, err := mm.getMarketState()
	if err != nil {
		mm.logger.Error("failed to get market state", zap.Error(err))
		return
	}
	
	// Calculate optimal quotes
	quotes := mm.calculateQuotes(state)
	
	// Update orders concurrently
	if err := mm.updateOrders(ctx, quotes); err != nil {
		mm.logger.Error("failed to update orders", zap.Error(err))
		return
	}
	
	mm.lastUpdate.Store(time.Now().UnixNano())
	
	elapsed := time.Since(start)
	if elapsed > 10*time.Millisecond {
		mm.logger.Warn("slow quote update", zap.Duration("elapsed", elapsed))
	}
}

func (mm *MarketMaker) getMarketState() (*MarketState, error) {
	orderbook, err := mm.exchange.GetOrderBook(mm.marketConfig.Symbol)
	if err != nil {
		return nil, fmt.Errorf("failed to get orderbook: %w", err)
	}
	
	if len(orderbook.Bids) == 0 || len(orderbook.Asks) == 0 {
		return nil, fmt.Errorf("orderbook is empty")
	}
	
	bidPrice := orderbook.Bids[0].Price
	askPrice := orderbook.Asks[0].Price
	
	midPrice := new(big.Float).Add(bidPrice, askPrice)
	midPrice.Quo(midPrice, big.NewFloat(2))
	
	spread := new(big.Float).Sub(askPrice, bidPrice)
	
	// Calculate order book imbalance
	var bidVolume, askVolume float64
	for i := 0; i < min(5, len(orderbook.Bids)); i++ {
		vol, _ := orderbook.Bids[i].Size.Float64()
		bidVolume += vol
	}
	for i := 0; i < min(5, len(orderbook.Asks)); i++ {
		vol, _ := orderbook.Asks[i].Size.Float64()
		askVolume += vol
	}
	
	imbalanceRatio := 0.0
	if bidVolume+askVolume > 0 {
		imbalanceRatio = (bidVolume - askVolume) / (bidVolume + askVolume)
	}
	
	return &MarketState{
		BidPrice:       bidPrice,
		AskPrice:       askPrice,
		MidPrice:       midPrice,
		Spread:         spread,
		Volatility:     mm.inventory.GetVolatility(),
		ImbalanceRatio: imbalanceRatio,
		UpdateTime:     orderbook.Time,
	}, nil
}

type Quote struct {
	Side  string
	Price *big.Float
	Size  *big.Float
	Level int
}

func (mm *MarketMaker) calculateQuotes(state *MarketState) []Quote {
	quotes := make([]Quote, 0, mm.marketConfig.OrderLevels*2)
	
	// Get inventory skew
	inventorySkew := mm.inventory.GetSkew()
	
	// Calculate base spread
	baseSpreadBps := mm.calculateDynamicSpread(state)
	
	// Adjust spread based on inventory
	bidSpreadBps := baseSpreadBps * (1 - inventorySkew*mm.marketConfig.SkewFactor)
	askSpreadBps := baseSpreadBps * (1 + inventorySkew*mm.marketConfig.SkewFactor)
	
	// Generate bid quotes
	for i := 0; i < mm.marketConfig.OrderLevels; i++ {
		spreadMultiplier := 1.0 + float64(i)*0.5
		bidSpread := bidSpreadBps * spreadMultiplier / 10000.0
		
		bidPrice := new(big.Float).Set(state.MidPrice)
		adjustment := new(big.Float).Mul(state.MidPrice, big.NewFloat(bidSpread))
		bidPrice.Sub(bidPrice, adjustment)
		
		// Round to tick size
		bidPrice = mm.roundToTick(bidPrice)
		
		// Calculate size with exponential decay
		baseSize, _ := new(big.Float).SetString(mm.marketConfig.MinOrderSize)
		sizeMultiplier := math.Pow(mm.marketConfig.OrderSizeMultiplier, float64(i))
		size := new(big.Float).Mul(baseSize, big.NewFloat(sizeMultiplier))
		
		quotes = append(quotes, Quote{
			Side:  "buy",
			Price: bidPrice,
			Size:  size,
			Level: i,
		})
	}
	
	// Generate ask quotes
	for i := 0; i < mm.marketConfig.OrderLevels; i++ {
		spreadMultiplier := 1.0 + float64(i)*0.5
		askSpread := askSpreadBps * spreadMultiplier / 10000.0
		
		askPrice := new(big.Float).Set(state.MidPrice)
		adjustment := new(big.Float).Mul(state.MidPrice, big.NewFloat(askSpread))
		askPrice.Add(askPrice, adjustment)
		
		// Round to tick size
		askPrice = mm.roundToTick(askPrice)
		
		// Calculate size with exponential decay
		baseSize, _ := new(big.Float).SetString(mm.marketConfig.MinOrderSize)
		sizeMultiplier := math.Pow(mm.marketConfig.OrderSizeMultiplier, float64(i))
		size := new(big.Float).Mul(baseSize, big.NewFloat(sizeMultiplier))
		
		quotes = append(quotes, Quote{
			Side:  "sell",
			Price: askPrice,
			Size:  size,
			Level: i,
		})
	}
	
	return quotes
}

func (mm *MarketMaker) calculateDynamicSpread(state *MarketState) float64 {
	baseSpread := float64(mm.marketConfig.MinSpreadBps)
	
	// Adjust for volatility
	volAdjustment := state.Volatility * 10000 // Convert to bps
	
	// Adjust for order book imbalance
	imbalanceAdjustment := math.Abs(state.ImbalanceRatio) * 20.0
	
	dynamicSpread := baseSpread + volAdjustment + imbalanceAdjustment
	
	// Cap at max spread
	if dynamicSpread > float64(mm.marketConfig.MaxSpreadBps) {
		dynamicSpread = float64(mm.marketConfig.MaxSpreadBps)
	}
	
	return dynamicSpread
}

func (mm *MarketMaker) updateOrders(ctx context.Context, quotes []Quote) error {
	// Get current orders
	activeOrders := mm.exchange.GetActiveOrders(mm.marketConfig.Symbol)
	
	// Build order map for quick lookup
	orderMap := make(map[string]*exchange.Order)
	for _, order := range activeOrders {
		key := fmt.Sprintf("%s-%d", order.Side, mm.getOrderLevel(order))
		orderMap[key] = order
	}
	
	// Prepare updates
	var toCancel []string
	var toPlace []Quote
	
	for _, quote := range quotes {
		key := fmt.Sprintf("%s-%d", quote.Side, quote.Level)
		existingOrder, exists := orderMap[key]
		
		if !exists {
			// No order at this level, place new
			toPlace = append(toPlace, quote)
		} else {
			// Check if order needs update
			priceDiff := new(big.Float).Sub(existingOrder.Price, quote.Price)
			priceDiff.Abs(priceDiff)
			
			threshold := new(big.Float).Mul(quote.Price, big.NewFloat(0.0001)) // 0.01% threshold
			
			if priceDiff.Cmp(threshold) > 0 || existingOrder.Size.Cmp(quote.Size) != 0 {
				toCancel = append(toCancel, existingOrder.ID)
				toPlace = append(toPlace, quote)
			}
			
			delete(orderMap, key)
		}
	}
	
	// Cancel orders not in quotes
	for _, order := range orderMap {
		toCancel = append(toCancel, order.ID)
	}
	
	// Execute updates concurrently
	g, ctx := errgroup.WithContext(ctx)
	
	// Cancel orders
	for _, orderID := range toCancel {
		orderID := orderID
		g.Go(func() error {
			if err := mm.exchange.CancelOrder(ctx, orderID); err != nil {
				mm.logger.Warn("failed to cancel order", 
					zap.String("orderID", orderID), 
					zap.Error(err))
			}
			mm.ordersCanceled.Add(1)
			return nil
		})
	}
	
	// Wait for cancellations
	if err := g.Wait(); err != nil {
		return err
	}
	
	// Place new orders
	g, ctx = errgroup.WithContext(ctx)
	
	for _, quote := range toPlace {
		quote := quote
		g.Go(func() error {
			_, err := mm.exchange.PlaceOrder(ctx, 
				mm.marketConfig.Symbol, 
				quote.Side, 
				quote.Price, 
				quote.Size)
			if err != nil {
				mm.logger.Warn("failed to place order",
					zap.String("side", quote.Side),
					zap.String("price", quote.Price.String()),
					zap.Error(err))
			} else {
				mm.ordersPlaced.Add(1)
			}
			return nil
		})
	}
	
	return g.Wait()
}

func (mm *MarketMaker) handleOrderUpdates(ctx context.Context) error {
	ordersCh := mm.exchange.OrdersChannel()
	
	for {
		select {
		case <-ctx.Done():
			return nil
		case order := <-ordersCh:
			if order.Market != mm.marketConfig.Symbol {
				continue
			}
			
			// Trigger quote update on order status change
			if order.Status == "filled" || order.Status == "canceled" {
				select {
				case mm.updateCh <- struct{}{}:
				default:
				}
			}
		}
	}
}

func (mm *MarketMaker) handleFills(ctx context.Context) error {
	fillsCh := mm.exchange.FillsChannel()
	
	for {
		select {
		case <-ctx.Done():
			return nil
		case fill := <-fillsCh:
			if fill.Market != mm.marketConfig.Symbol {
				continue
			}
			
			mm.fillsReceived.Add(1)
			
			// Update inventory
			mm.inventory.UpdatePosition(fill.Side, fill.Size, fill.Price)
			
			// Log fill
			mm.logger.Info("fill received",
				zap.String("side", fill.Side),
				zap.String("price", fill.Price.String()),
				zap.String("size", fill.Size.String()))
			
			// Trigger quote update
			select {
			case mm.updateCh <- struct{}{}:
			default:
			}
		}
	}
}

func (mm *MarketMaker) volatilityTracker(ctx context.Context) error {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	
	priceHistory := make([]float64, 0, 60)
	
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			state, err := mm.getMarketState()
			if err != nil {
				continue
			}
			
			midPrice, _ := state.MidPrice.Float64()
			priceHistory = append(priceHistory, midPrice)
			
			if len(priceHistory) > 60 {
				priceHistory = priceHistory[1:]
			}
			
			if len(priceHistory) >= 20 {
				volatility := mm.calculateVolatility(priceHistory)
				mm.inventory.SetVolatility(volatility)
			}
		}
	}
}

func (mm *MarketMaker) calculateVolatility(prices []float64) float64 {
	if len(prices) < 2 {
		return 0
	}
	
	// Calculate returns
	returns := make([]float64, len(prices)-1)
	for i := 1; i < len(prices); i++ {
		returns[i-1] = math.Log(prices[i] / prices[i-1])
	}
	
	// Calculate standard deviation
	var sum, sumSq float64
	for _, r := range returns {
		sum += r
		sumSq += r * r
	}
	
	n := float64(len(returns))
	mean := sum / n
	variance := (sumSq / n) - (mean * mean)
	
	// Annualized volatility (assuming 1-second intervals)
	return math.Sqrt(variance * 31536000) // seconds in a year
}

func (mm *MarketMaker) roundToTick(price *big.Float) *big.Float {
	tickSize, _ := new(big.Float).SetString(mm.marketConfig.TickSize)
	
	// price / tickSize
	ticks := new(big.Float).Quo(price, tickSize)
	
	// Round to nearest integer
	ticksInt, _ := ticks.Int64()
	
	// ticksInt * tickSize
	return new(big.Float).Mul(big.NewFloat(float64(ticksInt)), tickSize)
}

func (mm *MarketMaker) getOrderLevel(order *exchange.Order) int {
	// Implementation to determine order level based on price distance
	return 0
}

func (mm *MarketMaker) shutdown() error {
	mm.running.Store(false)
	
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	// Cancel all orders
	if err := mm.exchange.CancelAllOrders(ctx, mm.marketConfig.Symbol); err != nil {
		mm.logger.Error("failed to cancel orders during shutdown", zap.Error(err))
	}
	
	mm.logger.Info("market maker stopped",
		zap.Uint64("ordersPlaced", mm.ordersPlaced.Load()),
		zap.Uint64("ordersCanceled", mm.ordersCanceled.Load()),
		zap.Uint64("fillsReceived", mm.fillsReceived.Load()))
	
	return nil
}

func (mm *MarketMaker) Stop() {
	close(mm.stopCh)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}