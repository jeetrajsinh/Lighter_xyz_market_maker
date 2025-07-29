# High-Performance Go Market Maker for Lighter.xyz

A production-grade market making bot for Lighter.xyz built with Go for maximum performance and reliability.

## Features

- **High Performance**: Built in Go with concurrent order management
- **Low Latency**: Sub-millisecond order decisions with optimized execution
- **Risk Management**: Real-time position tracking, circuit breakers, and loss limits
- **Monitoring**: Prometheus metrics and Grafana dashboards
- **Resilient**: Automatic reconnection, graceful shutdown, and error recovery
- **Configurable**: Flexible market configuration with dynamic spread adjustment

## Architecture

```
lighter-mm-go/
├── cmd/marketmaker/      # Main application entry point
├── internal/
│   ├── config/          # Configuration management
│   ├── exchange/        # Lighter exchange client wrapper
│   ├── strategy/        # Market making strategy and inventory management
│   ├── risk/            # Risk management and circuit breakers
│   └── monitoring/      # Prometheus metrics
├── configs/             # Configuration files
└── Dockerfile          # Container configuration
```

## Performance Optimizations

- **Goroutines**: Concurrent order management for multiple markets
- **Object Pooling**: Reusable order objects to minimize allocations
- **Lock-free Operations**: Atomic operations for hot paths
- **Buffered Channels**: Non-blocking communication between components
- **Caching**: Local orderbook caching and skew calculations

## Quick Start

### Prerequisites

- Go 1.21 or higher
- Docker and Docker Compose (optional)
- Lighter.xyz API credentials

### Configuration

1. Copy the example configuration:
```bash
cp configs/config.json configs/config.local.json
```

2. Update with your credentials:
```json
{
  "lighter": {
    "apiKeyPrivateKey": "YOUR_PRIVATE_KEY",
    "accountIndex": 0,
    "apiKeyIndex": 2
  }
}
```

### Running Locally

```bash
# Install dependencies
go mod download

# Run the market maker
make run

# Or directly
go run cmd/marketmaker/main.go -config configs/config.local.json
```

### Running with Docker

```bash
# Build and run with Docker Compose
make docker-run

# View logs
make logs

# Stop
make docker-stop
```

## Testing

```bash
# Run all tests
make test

# Run with race detection
make race

# Run benchmarks
make bench

# Specific package tests
go test -v ./internal/strategy/...
```

## Monitoring

The market maker exposes Prometheus metrics on port 9090:

- Order placement/cancellation rates
- Fill rates and volumes
- Position and PnL tracking
- Spread and inventory skew
- WebSocket connection status
- Circuit breaker states

Access metrics at `http://localhost:9090/metrics`

With Docker Compose, Grafana is available at `http://localhost:3000` (admin/admin)

## Configuration Options

### Market Configuration
- `minOrderSize`: Minimum order size
- `tickSize`: Price tick size
- `minSpreadBps`/`maxSpreadBps`: Spread limits in basis points
- `orderLevels`: Number of price levels per side
- `maxPosition`: Maximum position size
- `orderSizeMultiplier`: Size increase per level
- `skewFactor`: Inventory skew adjustment factor

### Risk Configuration
- `maxTotalPositionUSD`: Maximum total position value
- `maxOrderValueUSD`: Maximum single order value
- `dailyLossLimitUSD`: Daily loss limit
- `positionCheckIntervalMs`: Position monitoring frequency

### Performance Configuration
- `updateIntervalMs`: Quote update frequency
- `orderPoolSize`: Order object pool size
- `websocketBufferSize`: WebSocket message buffer

## Risk Management

1. **Position Limits**: Enforced at order placement
2. **Loss Limits**: Daily loss tracking with circuit breaker
3. **Circuit Breakers**: Automatic trading halt on errors
4. **Order Validation**: Pre-trade risk checks
5. **Real-time Monitoring**: Continuous position and PnL tracking

## Development

### Code Structure

- `exchange/`: WebSocket handling, order management, position tracking
- `strategy/`: Quote calculation, inventory management, order placement logic
- `risk/`: Risk checks, circuit breakers, position limits
- `monitoring/`: Metrics collection and export

### Adding a New Market

1. Add market configuration to `configs/config.json`
2. Ensure proper decimals and tick size
3. Set appropriate position limits
4. Test with small sizes first

### Performance Profiling

```bash
# CPU profiling
make profile-cpu

# Memory profiling
make profile-mem
```

## Production Deployment

### Kubernetes

Example deployment manifest:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: lighter-marketmaker
spec:
  replicas: 1
  template:
    spec:
      containers:
      - name: marketmaker
        image: lighter-mm:latest
        resources:
          requests:
            memory: "1Gi"
            cpu: "1"
          limits:
            memory: "2Gi"
            cpu: "2"
```

### Best Practices

1. Start with conservative position limits
2. Monitor metrics closely during initial deployment
3. Use separate API keys for production
4. Enable structured logging for debugging
5. Set up alerts for circuit breaker trips

## Troubleshooting

### Common Issues

1. **WebSocket disconnections**: Check network stability and API limits
2. **Order rejections**: Verify balance and risk limits
3. **High latency**: Check CPU usage and network latency
4. **Memory growth**: Review goroutine leaks and channel buffers

### Debug Mode

Set log level to debug in configuration:
```json
{
  "logging": {
    "level": "debug"
  }
}
```

## License

This project is provided as-is for educational purposes. Use at your own risk in production environments.