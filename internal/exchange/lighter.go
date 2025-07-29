package exchange

import (
	"context"
	"fmt"
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"github.com/elliottech/lighter-go"
	"github.com/lighter-mm-go/internal/config"
	"go.uber.org/zap"
)

type Order struct {
	ID        string
	Market    string
	Side      string
	Price     *big.Float
	Size      *big.Float
	Timestamp time.Time
	Status    string
}

type OrderBook struct {
	Bids []PriceLevel
	Asks []PriceLevel
	Time time.Time
}

type PriceLevel struct {
	Price *big.Float
	Size  *big.Float
}

type Fill struct {
	OrderID   string
	Market    string
	Side      string
	Price     *big.Float
	Size      *big.Float
	Fee       *big.Float
	Timestamp time.Time
}

type Position struct {
	Market   string
	Size     *big.Float
	AvgPrice *big.Float
	PnL      *big.Float
}

type LighterClient struct {
	client       *lighter.Client
	logger       *zap.Logger
	cfg          *config.LighterConfig
	
	ordersMu     sync.RWMutex
	activeOrders map[string]*Order
	
	positionsMu  sync.RWMutex
	positions    map[string]*Position
	
	wsClient     *lighter.WebSocketClient
	wsConnected  atomic.Bool
	
	orderBookMu  sync.RWMutex
	orderBooks   map[string]*OrderBook
	
	fillsCh      chan *Fill
	ordersCh     chan *Order
	errorCh      chan error
	
	orderPool    *sync.Pool
	reconnectMu  sync.Mutex
}

func NewLighterClient(cfg *config.LighterConfig, logger *zap.Logger) (*LighterClient, error) {
	client, err := lighter.NewClient(
		cfg.BaseURL,
		cfg.APIKeyPrivateKey,
		cfg.AccountIndex,
		cfg.APIKeyIndex,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create lighter client: %w", err)
	}
	
	lc := &LighterClient{
		client:       client,
		logger:       logger,
		cfg:          cfg,
		activeOrders: make(map[string]*Order),
		positions:    make(map[string]*Position),
		orderBooks:   make(map[string]*OrderBook),
		fillsCh:      make(chan *Fill, 1000),
		ordersCh:     make(chan *Order, 1000),
		errorCh:      make(chan error, 100),
		orderPool: &sync.Pool{
			New: func() interface{} {
				return &Order{}
			},
		},
	}
	
	return lc, nil
}

func (lc *LighterClient) Start(ctx context.Context, markets []string) error {
	if err := lc.connectWebSocket(ctx, markets); err != nil {
		return fmt.Errorf("failed to connect websocket: %w", err)
	}
	
	go lc.websocketReconnector(ctx)
	go lc.positionTracker(ctx)
	
	return nil
}

func (lc *LighterClient) connectWebSocket(ctx context.Context, markets []string) error {
	lc.reconnectMu.Lock()
	defer lc.reconnectMu.Unlock()
	
	if lc.wsConnected.Load() {
		return nil
	}
	
	wsClient, err := lighter.NewWebSocketClient(lc.cfg.WsURL)
	if err != nil {
		return fmt.Errorf("failed to create websocket client: %w", err)
	}
	
	lc.wsClient = wsClient
	
	for _, market := range markets {
		if err := lc.wsClient.SubscribeOrderBook(market); err != nil {
			lc.logger.Error("failed to subscribe to orderbook", 
				zap.String("market", market), 
				zap.Error(err))
		}
		
		if err := lc.wsClient.SubscribeOrders(market); err != nil {
			lc.logger.Error("failed to subscribe to orders", 
				zap.String("market", market), 
				zap.Error(err))
		}
	}
	
	go lc.handleWebSocketMessages(ctx)
	
	lc.wsConnected.Store(true)
	lc.logger.Info("websocket connected", zap.Strings("markets", markets))
	
	return nil
}

func (lc *LighterClient) handleWebSocketMessages(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
			msg, err := lc.wsClient.ReadMessage()
			if err != nil {
				lc.wsConnected.Store(false)
				lc.errorCh <- fmt.Errorf("websocket error: %w", err)
				return
			}
			
			switch msg.Type {
			case "orderbook":
				lc.handleOrderBookUpdate(msg)
			case "order":
				lc.handleOrderUpdate(msg)
			case "fill":
				lc.handleFillUpdate(msg)
			}
		}
	}
}

func (lc *LighterClient) handleOrderBookUpdate(msg lighter.Message) {
	lc.orderBookMu.Lock()
	defer lc.orderBookMu.Unlock()
	
	// Parse and update orderbook
	// Implementation depends on lighter-go message format
}

func (lc *LighterClient) handleOrderUpdate(msg lighter.Message) {
	order := lc.orderPool.Get().(*Order)
	defer lc.orderPool.Put(order)
	
	// Parse order update
	// Update activeOrders map
	
	select {
	case lc.ordersCh <- order:
	default:
		lc.logger.Warn("orders channel full, dropping update")
	}
}

func (lc *LighterClient) handleFillUpdate(msg lighter.Message) {
	// Parse fill and send to channel
}

func (lc *LighterClient) websocketReconnector(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !lc.wsConnected.Load() {
				lc.logger.Info("attempting websocket reconnection")
				markets := lc.getActiveMarkets()
				if err := lc.connectWebSocket(ctx, markets); err != nil {
					lc.logger.Error("reconnection failed", zap.Error(err))
				}
			}
		}
	}
}

func (lc *LighterClient) PlaceOrder(ctx context.Context, market, side string, price, size *big.Float) (*Order, error) {
	orderReq := &lighter.OrderRequest{
		Market: market,
		Side:   side,
		Price:  price.String(),
		Size:   size.String(),
		Type:   "limit",
	}
	
	resp, err := lc.client.PlaceOrder(ctx, orderReq)
	if err != nil {
		return nil, fmt.Errorf("failed to place order: %w", err)
	}
	
	order := &Order{
		ID:        resp.OrderID,
		Market:    market,
		Side:      side,
		Price:     price,
		Size:      size,
		Timestamp: time.Now(),
		Status:    "pending",
	}
	
	lc.ordersMu.Lock()
	lc.activeOrders[order.ID] = order
	lc.ordersMu.Unlock()
	
	return order, nil
}

func (lc *LighterClient) CancelOrder(ctx context.Context, orderID string) error {
	if err := lc.client.CancelOrder(ctx, orderID); err != nil {
		return fmt.Errorf("failed to cancel order: %w", err)
	}
	
	lc.ordersMu.Lock()
	delete(lc.activeOrders, orderID)
	lc.ordersMu.Unlock()
	
	return nil
}

func (lc *LighterClient) CancelAllOrders(ctx context.Context, market string) error {
	lc.ordersMu.RLock()
	orderIDs := make([]string, 0)
	for id, order := range lc.activeOrders {
		if order.Market == market {
			orderIDs = append(orderIDs, id)
		}
	}
	lc.ordersMu.RUnlock()
	
	var wg sync.WaitGroup
	errCh := make(chan error, len(orderIDs))
	
	for _, id := range orderIDs {
		wg.Add(1)
		go func(orderID string) {
			defer wg.Done()
			if err := lc.CancelOrder(ctx, orderID); err != nil {
				errCh <- err
			}
		}(id)
	}
	
	wg.Wait()
	close(errCh)
	
	for err := range errCh {
		if err != nil {
			return err
		}
	}
	
	return nil
}

func (lc *LighterClient) GetOrderBook(market string) (*OrderBook, error) {
	lc.orderBookMu.RLock()
	defer lc.orderBookMu.RUnlock()
	
	ob, exists := lc.orderBooks[market]
	if !exists {
		return nil, fmt.Errorf("orderbook for %s not found", market)
	}
	
	return ob, nil
}

func (lc *LighterClient) GetPosition(market string) (*Position, error) {
	lc.positionsMu.RLock()
	defer lc.positionsMu.RUnlock()
	
	pos, exists := lc.positions[market]
	if !exists {
		return &Position{
			Market: market,
			Size:   big.NewFloat(0),
			AvgPrice: big.NewFloat(0),
			PnL: big.NewFloat(0),
		}, nil
	}
	
	return pos, nil
}

func (lc *LighterClient) GetActiveOrders(market string) []*Order {
	lc.ordersMu.RLock()
	defer lc.ordersMu.RUnlock()
	
	orders := make([]*Order, 0)
	for _, order := range lc.activeOrders {
		if order.Market == market {
			orders = append(orders, order)
		}
	}
	
	return orders
}

func (lc *LighterClient) positionTracker(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			lc.updatePositions(ctx)
		}
	}
}

func (lc *LighterClient) updatePositions(ctx context.Context) {
	// Fetch positions from API and update local cache
}

func (lc *LighterClient) getActiveMarkets() []string {
	lc.orderBookMu.RLock()
	defer lc.orderBookMu.RUnlock()
	
	markets := make([]string, 0, len(lc.orderBooks))
	for market := range lc.orderBooks {
		markets = append(markets, market)
	}
	
	return markets
}

func (lc *LighterClient) FillsChannel() <-chan *Fill {
	return lc.fillsCh
}

func (lc *LighterClient) OrdersChannel() <-chan *Order {
	return lc.ordersCh
}

func (lc *LighterClient) ErrorsChannel() <-chan error {
	return lc.errorCh
}

func (lc *LighterClient) Close() error {
	if lc.wsClient != nil {
		lc.wsClient.Close()
	}
	
	close(lc.fillsCh)
	close(lc.ordersCh)
	close(lc.errorCh)
	
	return nil
}