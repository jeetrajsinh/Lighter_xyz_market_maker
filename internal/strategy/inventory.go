package strategy

import (
	"math/big"
	"sync"
	"sync/atomic"

	"time"

	"github.com/lighter-mm-go/internal/config"
)

type InventoryManager struct {
	marketConfig *config.MarketConfig
	
	mu         sync.RWMutex
	position   *big.Float
	avgPrice   *big.Float
	totalBuys  *big.Float
	totalSells *big.Float
	
	volatility atomic.Value // float64
	
	// Performance optimization
	skewCache     atomic.Value // float64
	skewCacheTime atomic.Int64
}

func NewInventoryManager(marketConfig *config.MarketConfig) *InventoryManager {
	im := &InventoryManager{
		marketConfig: marketConfig,
		position:     big.NewFloat(0),
		avgPrice:     big.NewFloat(0),
		totalBuys:    big.NewFloat(0),
		totalSells:   big.NewFloat(0),
	}
	
	im.volatility.Store(0.0)
	im.skewCache.Store(0.0)
	
	return im
}

func (im *InventoryManager) UpdatePosition(side string, size, price *big.Float) {
	im.mu.Lock()
	defer im.mu.Unlock()
	
	if side == "buy" {
		// We bought, so our position increases
		newPosition := new(big.Float).Add(im.position, size)
		
		// Update average price
		if im.position.Sign() > 0 {
			// Weighted average
			totalValue := new(big.Float).Mul(im.position, im.avgPrice)
			newValue := new(big.Float).Mul(size, price)
			totalValue.Add(totalValue, newValue)
			
			im.avgPrice = new(big.Float).Quo(totalValue, newPosition)
		} else if im.position.Sign() < 0 && newPosition.Sign() > 0 {
			// Flipped from short to long
			im.avgPrice = price
		} else if im.position.Sign() == 0 {
			// New position
			im.avgPrice = price
		}
		
		im.position = newPosition
		im.totalBuys.Add(im.totalBuys, size)
		
	} else { // sell
		// We sold, so our position decreases
		newPosition := new(big.Float).Sub(im.position, size)
		
		// Update average price
		if im.position.Sign() < 0 {
			// Weighted average for short position
			totalValue := new(big.Float).Mul(im.position, im.avgPrice)
			totalValue.Abs(totalValue)
			newValue := new(big.Float).Mul(size, price)
			totalValue.Add(totalValue, newValue)
			
			absNewPosition := new(big.Float).Abs(newPosition)
			im.avgPrice = new(big.Float).Quo(totalValue, absNewPosition)
		} else if im.position.Sign() > 0 && newPosition.Sign() < 0 {
			// Flipped from long to short
			im.avgPrice = price
		} else if im.position.Sign() == 0 {
			// New short position
			im.avgPrice = price
		}
		
		im.position = newPosition
		im.totalSells.Add(im.totalSells, size)
	}
	
	// Invalidate skew cache
	im.skewCacheTime.Store(0)
}

func (im *InventoryManager) GetPosition() *big.Float {
	im.mu.RLock()
	defer im.mu.RUnlock()
	
	return new(big.Float).Set(im.position)
}

func (im *InventoryManager) GetSkew() float64 {
	// Check cache
	now := timeNow()
	if cacheTime := im.skewCacheTime.Load(); now-cacheTime < 100_000_000 { // 100ms
		return im.skewCache.Load().(float64)
	}
	
	im.mu.RLock()
	position := new(big.Float).Set(im.position)
	maxPosition, _ := new(big.Float).SetString(im.marketConfig.MaxPosition)
	im.mu.RUnlock()
	
	// Calculate skew: position / maxPosition
	// Positive skew means we're long, negative means short
	skew := new(big.Float).Quo(position, maxPosition)
	skewFloat, _ := skew.Float64()
	
	// Clamp between -1 and 1
	if skewFloat > 1 {
		skewFloat = 1
	} else if skewFloat < -1 {
		skewFloat = -1
	}
	
	// Update cache
	im.skewCache.Store(skewFloat)
	im.skewCacheTime.Store(now)
	
	return skewFloat
}

func (im *InventoryManager) GetPnL(currentPrice *big.Float) *big.Float {
	im.mu.RLock()
	defer im.mu.RUnlock()
	
	if im.position.Sign() == 0 {
		return big.NewFloat(0)
	}
	
	// PnL = position * (currentPrice - avgPrice)
	priceDiff := new(big.Float).Sub(currentPrice, im.avgPrice)
	pnl := new(big.Float).Mul(im.position, priceDiff)
	
	return pnl
}

func (im *InventoryManager) GetInventoryRisk() float64 {
	im.mu.RLock()
	position := new(big.Float).Set(im.position)
	avgPrice := new(big.Float).Set(im.avgPrice)
	im.mu.RUnlock()
	
	// Risk = |position| * price * volatility
	absPosition := new(big.Float).Abs(position)
	notional := new(big.Float).Mul(absPosition, avgPrice)
	notionalFloat, _ := notional.Float64()
	
	volatility := im.GetVolatility()
	
	return notionalFloat * volatility
}

func (im *InventoryManager) SetVolatility(vol float64) {
	im.volatility.Store(vol)
}

func (im *InventoryManager) GetVolatility() float64 {
	return im.volatility.Load().(float64)
}

func (im *InventoryManager) GetStats() map[string]interface{} {
	im.mu.RLock()
	defer im.mu.RUnlock()
	
	return map[string]interface{}{
		"position":    im.position.String(),
		"avgPrice":    im.avgPrice.String(),
		"totalBuys":   im.totalBuys.String(),
		"totalSells":  im.totalSells.String(),
		"skew":        im.GetSkew(),
		"volatility":  im.GetVolatility(),
		"risk":        im.GetInventoryRisk(),
	}
}

func (im *InventoryManager) IsWithinLimits() bool {
	im.mu.RLock()
	defer im.mu.RUnlock()
	
	maxPosition, _ := new(big.Float).SetString(im.marketConfig.MaxPosition)
	absPosition := new(big.Float).Abs(im.position)
	
	return absPosition.Cmp(maxPosition) <= 0
}

// Helper function for getting current time in nanoseconds
func timeNow() int64 {
	return time.Now().UnixNano()
}