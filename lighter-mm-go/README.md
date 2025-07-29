# High-Performance Go Market Maker for Lighter.xyz

A production-grade automated trading bot for Lighter.xyz that provides liquidity by continuously placing buy and sell orders. Built with Go programming language for maximum speed and reliability.

## Table of Contents
- [What is a Market Maker?](#what-is-a-market-maker)
- [Features](#features)
- [Prerequisites](#prerequisites)
- [Installation Guide](#installation-guide)
- [Configuration](#configuration)
- [Running the Bot](#running-the-bot)
- [Understanding the Dashboard](#understanding-the-dashboard)
- [Safety Features](#safety-features)
- [Troubleshooting](#troubleshooting)
- [Advanced Usage](#advanced-usage)

## What is a Market Maker?

A market maker is an automated trading bot that:
- Places buy orders slightly below the current market price
- Places sell orders slightly above the current market price
- Profits from the spread (difference) between buy and sell prices
- Provides liquidity to the market, making it easier for others to trade

**Important**: Market making involves financial risk. You can lose money if the market moves against your positions.

## Features

- **Automated Trading**: Places and manages orders 24/7 without manual intervention
- **High Performance**: Executes trades in milliseconds for competitive edge
- **Risk Protection**: Built-in safeguards to limit potential losses
- **Real-time Monitoring**: Track performance through web dashboard
- **Multi-Market Support**: Trade multiple pairs simultaneously
- **Crash Recovery**: Automatically resumes after connection issues

## Prerequisites

### For Non-Technical Users (Recommended)

You'll need:
1. A computer running Windows 10/11, macOS, or Linux
2. Docker Desktop installed (see installation guide below)
3. A Lighter.xyz account with API credentials
4. Some ETH and USDC in your Lighter account for trading

### System Requirements

- **Minimum**: 2 CPU cores, 4GB RAM, 10GB disk space
- **Recommended**: 4 CPU cores, 8GB RAM, 20GB disk space
- **Internet**: Stable connection with <100ms latency to Lighter servers

## Installation Guide

### Step 1: Install Docker Desktop

Docker makes it easy to run the market maker without dealing with complex setup.

**For Windows:**
1. Download Docker Desktop from: https://www.docker.com/products/docker-desktop/
2. Run the installer (Docker Desktop Installer.exe)
3. Follow the installation wizard
4. Restart your computer when prompted
5. Start Docker Desktop from the Start menu

**For macOS:**
1. Download Docker Desktop from: https://www.docker.com/products/docker-desktop/
2. Open the .dmg file
3. Drag Docker to your Applications folder
4. Start Docker from Applications
5. Allow the required permissions when prompted

**For Linux (Ubuntu/Debian):**
```bash
# Copy and paste these commands one by one:
curl -fsSL https://get.docker.com -o get-docker.sh
sudo sh get-docker.sh
sudo usermod -aG docker $USER
# Log out and back in after this
```

### Step 2: Download the Market Maker

1. Download the market maker code:
   - Click the green "Code" button on this page
   - Select "Download ZIP"
   - Extract the ZIP file to a folder on your computer

2. Open a terminal/command prompt:
   - **Windows**: Press `Win+R`, type `cmd`, press Enter
   - **macOS**: Press `Cmd+Space`, type `terminal`, press Enter
   - **Linux**: Press `Ctrl+Alt+T`

3. Navigate to the extracted folder:
   ```bash
   cd path/to/lighter-mm-go
   # Replace "path/to/lighter-mm-go" with the actual path
   ```

### Step 3: Get Your Lighter.xyz API Credentials

1. Log in to your Lighter.xyz account
2. Navigate to Settings → API Keys
3. Click "Create New API Key"
4. **Important**: Save your private key immediately - it won't be shown again!
5. Note down:
   - Your private key (starts with 0x...)
   - Account index (usually 0)
   - API key index (usually 2)

## Configuration

### Step 1: Create Your Configuration File

1. In the `configs` folder, make a copy of `config.json`:
   ```bash
   cp configs/config.json configs/my-config.json
   ```

2. Open `configs/my-config.json` in a text editor (Notepad, TextEdit, etc.)

3. Update these fields:

```json
{
  "lighter": {
    "baseURL": "https://mainnet.zklighter.elliot.ai",
    "wsURL": "wss://mainnet.zklighter.elliot.ai/ws",
    "apiKeyPrivateKey": "0xYOUR_PRIVATE_KEY_HERE",
    "accountIndex": 0,
    "apiKeyIndex": 2
  },
  "markets": [
    {
      "symbol": "ETH-USDC",
      "minOrderSize": "0.01",
      "maxPosition": "1.0"
    }
  ],
  "risk": {
    "maxTotalPositionUSD": 10000,
    "maxOrderValueUSD": 1000,
    "dailyLossLimitUSD": 100
  }
}
```

### Configuration Explained

**Lighter Settings:**
- `apiKeyPrivateKey`: Your private key from Lighter.xyz
- `accountIndex`: Usually 0 (your first account)
- `apiKeyIndex`: The index of your API key (check Lighter dashboard)

**Market Settings:**
- `symbol`: Trading pair (e.g., "ETH-USDC")
- `minOrderSize`: Smallest order size (e.g., "0.01" ETH)
- `maxPosition`: Maximum position size (e.g., "1.0" ETH)
- `minSpreadBps`: Minimum spread in basis points (10 = 0.1%)
- `maxSpreadBps`: Maximum spread in basis points (100 = 1%)
- `orderLevels`: Number of orders on each side (default: 3)

**Risk Settings:**
- `maxTotalPositionUSD`: Maximum total value of all positions
- `maxOrderValueUSD`: Maximum value of a single order
- `dailyLossLimitUSD`: Stop trading if daily loss exceeds this

### Example Configurations

**Conservative (Recommended for Beginners):**
```json
{
  "markets": [
    {
      "symbol": "ETH-USDC",
      "minOrderSize": "0.01",
      "maxPosition": "0.1",
      "minSpreadBps": 20,
      "maxSpreadBps": 200
    }
  ],
  "risk": {
    "maxTotalPositionUSD": 1000,
    "maxOrderValueUSD": 100,
    "dailyLossLimitUSD": 50
  }
}
```

**Moderate:**
```json
{
  "markets": [
    {
      "symbol": "ETH-USDC",
      "minOrderSize": "0.05",
      "maxPosition": "0.5",
      "minSpreadBps": 15,
      "maxSpreadBps": 150
    }
  ],
  "risk": {
    "maxTotalPositionUSD": 5000,
    "maxOrderValueUSD": 500,
    "dailyLossLimitUSD": 200
  }
}
```

## Running the Bot

### Option 1: Using Docker (Easiest)

1. Start the market maker:
   ```bash
   docker-compose up -d
   ```

2. Check if it's running:
   ```bash
   docker-compose ps
   ```
   You should see "lighter-mm" with status "Up"

3. View the logs:
   ```bash
   docker-compose logs -f marketmaker
   ```

4. Stop the market maker:
   ```bash
   docker-compose down
   ```

### Option 2: Direct Execution (Advanced)

If you have Go installed:
```bash
go run cmd/marketmaker/main.go -config configs/my-config.json
```

## Understanding the Dashboard

### Accessing the Dashboard

1. Open your web browser
2. Go to: http://localhost:3000
3. Login with:
   - Username: `admin`
   - Password: `admin`

### Key Metrics to Monitor

**Trading Activity:**
- `Orders Placed`: Total orders created
- `Orders Canceled`: Orders removed before filling
- `Fills Received`: Completed trades
- `Fill Rate`: Percentage of orders that get filled

**Position & Risk:**
- `Current Position`: Your current holdings (positive = long, negative = short)
- `Unrealized PnL`: Profit/loss on open positions
- `Daily PnL`: Today's profit/loss
- `Position Value`: USD value of your positions

**System Health:**
- `WebSocket Status`: Should be "Connected" (green)
- `Circuit Breakers`: Should be "Closed" (green)
- `Uptime`: How long the bot has been running

### Alert Conditions

Pay attention if you see:
- 🔴 **Circuit Breaker Open**: Trading has stopped due to errors
- 🔴 **Daily Loss Limit Hit**: Trading stopped due to losses
- 🟡 **High Position Skew**: Position is getting too one-sided
- 🟡 **WebSocket Disconnected**: Connection issues

## Safety Features

The bot includes multiple safety mechanisms:

1. **Position Limits**: Won't exceed configured maximum position
2. **Order Size Limits**: Prevents accidentally large orders
3. **Daily Loss Limit**: Stops trading after reaching loss threshold
4. **Circuit Breakers**: Automatic pause during system issues
5. **Gradual Start**: Begins with small orders to test connectivity

## Troubleshooting

### Common Issues and Solutions

**Bot won't start:**
- Check Docker is running: Docker icon should be in system tray
- Verify config file: Ensure all quotes and commas are correct
- Check API key: Make sure it's copied correctly with 0x prefix

**No orders being placed:**
- Check balance: Ensure you have funds in your Lighter account
- Verify market symbol: Must match exactly (e.g., "ETH-USDC")
- Review logs: `docker-compose logs marketmaker`

**Circuit breaker triggered:**
- Check your internet connection
- Verify API credentials are correct
- Ensure your Lighter account is active
- Wait 30 seconds - it will auto-reset

**High losses:**
- Reduce position limits in configuration
- Increase spread (minSpreadBps/maxSpreadBps)
- Check if market is unusually volatile
- Consider pausing during major news events

### Getting Help

1. **Check Logs First:**
   ```bash
   docker-compose logs --tail 100 marketmaker
   ```

2. **Export Detailed Logs:**
   ```bash
   docker-compose logs marketmaker > marketmaker-logs.txt
   ```

3. **Common Log Messages:**
   - `"websocket connected"` ✅ Good - connection established
   - `"failed to place order"` ⚠️ Check balance and limits
   - `"circuit breaker tripped"` 🛑 Auto-pause activated
   - `"risk check failed"` ⚠️ Order exceeded risk limits

## Monitoring Performance

### Daily Checklist

1. **Morning:**
   - Check overnight PnL
   - Verify all systems green in dashboard
   - Review any overnight errors in logs

2. **During Trading:**
   - Monitor position size stays within limits
   - Watch spread adjustments during volatility
   - Check fill rate (target: >30%)

3. **End of Day:**
   - Review total volume traded
   - Calculate profit/loss
   - Check for any system warnings

### Performance Metrics

**Good Performance Indicators:**
- Fill rate: 30-50%
- Spread captured: 50-80% of quoted spread
- Uptime: >95%
- Daily Sharpe ratio: >1.0

**Warning Signs:**
- Fill rate <20%: Spreads might be too tight
- Frequent circuit breaker trips: Connection issues
- One-sided inventory: Market trending strongly
- Daily losses >2%: Review strategy parameters

## Best Practices

### For Beginners

1. **Start Small:**
   - Use minimum position sizes initially
   - Set conservative loss limits
   - Monitor closely for first week

2. **Market Selection:**
   - Start with liquid pairs (ETH-USDC)
   - Avoid new or volatile tokens
   - One market at a time initially

3. **Risk Management:**
   - Never risk more than you can afford to lose
   - Set daily loss limits at 1-2% of capital
   - Keep some reserve funds uninvested

### Optimization Tips

1. **Spread Adjustment:**
   - Wider spreads = safer but less volume
   - Tighter spreads = more volume but more risk
   - Start wide, gradually tighten

2. **Position Management:**
   - Monitor inventory skew
   - Consider reducing size in trending markets
   - Rebalance if position becomes too one-sided

3. **Time Management:**
   - Run during liquid hours for better fills
   - Consider pausing during major events
   - Regular maintenance windows

## Advanced Usage

### Multiple Markets

To trade multiple pairs, add to the markets array:
```json
"markets": [
  {
    "symbol": "ETH-USDC",
    "minOrderSize": "0.01",
    "maxPosition": "0.5"
  },
  {
    "symbol": "BTC-USDC",
    "minOrderSize": "0.0005",
    "maxPosition": "0.02"
  }
]
```

### Custom Strategies

Adjust these parameters for different strategies:

**Tight Market Making:**
- `minSpreadBps`: 5-10
- `orderLevels`: 5-10
- `orderSizeMultiplier`: 1.2

**Wide Market Making:**
- `minSpreadBps`: 50-100
- `orderLevels`: 3-5
- `orderSizeMultiplier`: 2.0

**Inventory-Focused:**
- `skewFactor`: 0.5-0.7 (higher = more aggressive inventory management)
- `maxPosition`: Lower values

### Performance Tuning

For dedicated servers:
```json
"performance": {
  "updateIntervalMs": 50,
  "orderPoolSize": 2000,
  "websocketBufferSize": 20000
}
```

## Security Notes

1. **Protect Your API Key:**
   - Never share your private key
   - Don't commit config files to GitHub
   - Use environment variables in production

2. **Secure Your Server:**
   - Change default Grafana password
   - Use firewall to limit access
   - Regular security updates

3. **Monitor Access:**
   - Check API key usage on Lighter
   - Review access logs regularly
   - Rotate keys periodically

## Backup and Recovery

### Backup Your Configuration

1. Save your config file securely:
   ```bash
   cp configs/my-config.json ~/Documents/lighter-config-backup.json
   ```

2. Export your Grafana dashboards:
   - Go to Dashboard settings
   - Click "JSON Model"
   - Save to file

### Recovery Procedure

If the bot crashes:
1. Check the logs for errors
2. Verify your Lighter account status
3. Restart with: `docker-compose restart`
4. Monitor closely after restart

## Updates and Maintenance

### Updating the Bot

1. Stop the current version:
   ```bash
   docker-compose down
   ```

2. Backup your config:
   ```bash
   cp configs/my-config.json configs/my-config.backup.json
   ```

3. Pull latest version:
   ```bash
   git pull origin main
   ```

4. Rebuild and restart:
   ```bash
   docker-compose up -d --build
   ```

### Regular Maintenance

**Weekly:**
- Review performance metrics
- Adjust parameters based on market conditions
- Check for software updates
- Clean up old log files

**Monthly:**
- Full system backup
- Review and optimize strategy parameters
- Security audit (check access logs)
- Update documentation with lessons learned

## Getting Support

Before requesting help:
1. Check this README thoroughly
2. Review recent logs
3. Try restarting the bot
4. Check Lighter.xyz status page

When requesting help, provide:
- Your configuration (without private keys!)
- Recent log output
- Description of the issue
- Steps you've already tried

## Disclaimer

**Important**: This software is provided as-is for educational purposes. Cryptocurrency trading carries substantial risk of loss. Past performance does not guarantee future results. Only trade with funds you can afford to lose. The authors are not responsible for any financial losses incurred through the use of this software.