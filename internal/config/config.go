package config

import (
	"fmt"
	"math/big"
	"sync"

	"github.com/spf13/viper"
	"go.uber.org/zap"
)

type Config struct {
	Lighter     LighterConfig     `json:"lighter"`
	Markets     []MarketConfig    `json:"markets"`
	Risk        RiskConfig        `json:"risk"`
	Performance PerformanceConfig `json:"performance"`
	Logging     LoggingConfig     `json:"logging"`
}

type LighterConfig struct {
	BaseURL          string `json:"baseURL"`
	WsURL            string `json:"wsURL"`
	APIKeyPrivateKey string `json:"apiKeyPrivateKey"`
	AccountIndex     int    `json:"accountIndex"`
	APIKeyIndex      int    `json:"apiKeyIndex"`
}

type MarketConfig struct {
	Symbol              string  `json:"symbol"`
	BaseDecimals        int     `json:"baseDecimals"`
	QuoteDecimals       int     `json:"quoteDecimals"`
	MinOrderSize        string  `json:"minOrderSize"`
	TickSize            string  `json:"tickSize"`
	MinSpreadBps        int     `json:"minSpreadBps"`
	MaxSpreadBps        int     `json:"maxSpreadBps"`
	OrderLevels         int     `json:"orderLevels"`
	MaxPosition         string  `json:"maxPosition"`
	OrderSizeMultiplier float64 `json:"orderSizeMultiplier"`
	SkewFactor          float64 `json:"skewFactor"`
}

type RiskConfig struct {
	MaxTotalPositionUSD   float64 `json:"maxTotalPositionUSD"`
	MaxOrderValueUSD      float64 `json:"maxOrderValueUSD"`
	DailyLossLimitUSD     float64 `json:"dailyLossLimitUSD"`
	PositionCheckInterval int     `json:"positionCheckIntervalMs"`
}

type PerformanceConfig struct {
	MetricsPort         int `json:"metricsPort"`
	UpdateIntervalMs    int `json:"updateIntervalMs"`
	OrderPoolSize       int `json:"orderPoolSize"`
	WebsocketBufferSize int `json:"websocketBufferSize"`
}

type LoggingConfig struct {
	Level            string `json:"level"`
	OutputPath       string `json:"outputPath"`
	ErrorOutputPath  string `json:"errorOutputPath"`
}

var (
	cfg  *Config
	once sync.Once
)

func Load(configPath string) (*Config, error) {
	var err error
	once.Do(func() {
		viper.SetConfigFile(configPath)
		viper.SetConfigType("json")

		if err = viper.ReadInConfig(); err != nil {
			err = fmt.Errorf("failed to read config: %w", err)
			return
		}

		cfg = &Config{}
		if err = viper.Unmarshal(cfg); err != nil {
			err = fmt.Errorf("failed to unmarshal config: %w", err)
			return
		}

		if err = cfg.Validate(); err != nil {
			err = fmt.Errorf("invalid config: %w", err)
			return
		}
	})

	return cfg, err
}

func Get() *Config {
	if cfg == nil {
		panic("config not loaded")
	}
	return cfg
}

func (c *Config) Validate() error {
	if c.Lighter.BaseURL == "" {
		return fmt.Errorf("lighter.baseURL is required")
	}
	if c.Lighter.APIKeyPrivateKey == "" || c.Lighter.APIKeyPrivateKey == "0x..." {
		return fmt.Errorf("lighter.apiKeyPrivateKey must be set")
	}
	if len(c.Markets) == 0 {
		return fmt.Errorf("at least one market must be configured")
	}

	for i, market := range c.Markets {
		if err := validateMarket(market); err != nil {
			return fmt.Errorf("market[%d]: %w", i, err)
		}
	}

	if c.Risk.MaxTotalPositionUSD <= 0 {
		return fmt.Errorf("risk.maxTotalPositionUSD must be positive")
	}
	if c.Risk.MaxOrderValueUSD <= 0 {
		return fmt.Errorf("risk.maxOrderValueUSD must be positive")
	}
	if c.Performance.UpdateIntervalMs <= 0 {
		return fmt.Errorf("performance.updateIntervalMs must be positive")
	}

	return nil
}

func validateMarket(m MarketConfig) error {
	if m.Symbol == "" {
		return fmt.Errorf("symbol is required")
	}
	if m.BaseDecimals <= 0 || m.QuoteDecimals <= 0 {
		return fmt.Errorf("decimals must be positive")
	}
	
	if _, ok := new(big.Float).SetString(m.MinOrderSize); !ok {
		return fmt.Errorf("invalid minOrderSize: %s", m.MinOrderSize)
	}
	if _, ok := new(big.Float).SetString(m.TickSize); !ok {
		return fmt.Errorf("invalid tickSize: %s", m.TickSize)
	}
	if _, ok := new(big.Float).SetString(m.MaxPosition); !ok {
		return fmt.Errorf("invalid maxPosition: %s", m.MaxPosition)
	}
	
	if m.MinSpreadBps <= 0 || m.MaxSpreadBps <= m.MinSpreadBps {
		return fmt.Errorf("invalid spread configuration")
	}
	if m.OrderLevels <= 0 {
		return fmt.Errorf("orderLevels must be positive")
	}
	if m.OrderSizeMultiplier <= 0 {
		return fmt.Errorf("orderSizeMultiplier must be positive")
	}
	if m.SkewFactor < 0 || m.SkewFactor > 1 {
		return fmt.Errorf("skewFactor must be between 0 and 1")
	}

	return nil
}

func (c *Config) GetMarket(symbol string) (*MarketConfig, error) {
	for i := range c.Markets {
		if c.Markets[i].Symbol == symbol {
			return &c.Markets[i], nil
		}
	}
	return nil, fmt.Errorf("market %s not found", symbol)
}

func (c *Config) GetLogger() (*zap.Logger, error) {
	cfg := zap.NewProductionConfig()
	
	switch c.Logging.Level {
	case "debug":
		cfg.Level = zap.NewAtomicLevelAt(zap.DebugLevel)
	case "info":
		cfg.Level = zap.NewAtomicLevelAt(zap.InfoLevel)
	case "warn":
		cfg.Level = zap.NewAtomicLevelAt(zap.WarnLevel)
	case "error":
		cfg.Level = zap.NewAtomicLevelAt(zap.ErrorLevel)
	}
	
	cfg.OutputPaths = []string{c.Logging.OutputPath}
	cfg.ErrorOutputPaths = []string{c.Logging.ErrorOutputPath}
	
	return cfg.Build()
}