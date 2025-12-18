# WebAPI Structure Documentation

This document describes the organization of the WebAPI package after splitting the original `api.go` file.

## Overview

The original `api.go` file (~1100+ lines) has been split into 6 focused files, each handling a specific aspect of the Web API functionality.

## File Organization

### 1. `api.go` (~60 lines)
**Purpose**: Main API server initialization and core types

**Key Components**:
- `APIResponse` - Unified API response format
- `QueryResult` - DNS query result format
- `IPResult` - Individual IP result with RTT
- `Server` - Main Web API server struct
- `NewServer()` - Server constructor
- `Start()` - Start the Web API service
- `Stop()` - Stop the Web API service
- Route registration for all API endpoints

**Responsibilities**:
- Initialize HTTP server
- Register all API routes
- Manage server lifecycle
- Serve embedded web files

---

### 2. `api_handlers.go` (~200 lines)
**Purpose**: Basic API handlers for core DNS operations

**Key Handlers**:
- `handleQuery()` - DNS query endpoint
- `handleStats()` - Statistics retrieval
- `handleCacheMemoryStats()` - Cache memory statistics
- `handleHealth()` - Health check endpoint
- `handleClearCache()` - Clear DNS cache
- `handleClearStats()` - Clear statistics
- `handleRecentQueries()` - Get recent queries
- `handleHotDomains()` - Get hot domains
- `handleRestart()` - Service restart

**Responsibilities**:
- Handle basic DNS query operations
- Manage cache and statistics operations
- Provide health check functionality
- Support service restart

---

### 3. `api_config.go` (~150 lines)
**Purpose**: Configuration management API handlers

**Key Handlers**:
- `handleConfig()` - Main config endpoint (GET/POST)
- `handleGetConfig()` - Retrieve current configuration
- `handlePostConfig()` - Update configuration
- `validateConfig()` - Configuration validation

**Responsibilities**:
- Get current DNS server configuration
- Update configuration via API
- Validate configuration parameters
- Persist configuration to YAML file
- Apply configuration to running server

**Validation Includes**:
- Port ranges
- Upstream server settings
- Cache TTL parameters
- Ping settings
- Strategy validation

---

### 4. `api_adblock.go` (~360 lines)
**Purpose**: AdBlock filtering API handlers

**Key Handlers**:
- `handleAdBlockStatus()` - Get AdBlock status
- `handleAdBlockSources()` - Manage AdBlock sources (GET/POST/PUT/DELETE)
- `handleAdBlockUpdate()` - Trigger rule update
- `handleAdBlockToggle()` - Enable/disable AdBlock
- `handleAdBlockTest()` - Test domain against AdBlock rules
- `handleAdBlockBlockMode()` - Set block mode (nxdomain/refused/zero_ip)
- `handleAdBlockSettings()` - Get/update AdBlock settings

**Responsibilities**:
- Manage AdBlock enable/disable state
- Add/remove/enable/disable rule sources
- Trigger manual rule updates
- Test domains against AdBlock rules
- Configure block mode
- Manage AdBlock settings (update interval, cache settings, TTL)

---

### 5. `api_custom.go` (~100 lines)
**Purpose**: Custom rules API handlers

**Key Handlers**:
- `handleCustomBlocked()` - Manage custom blocked domains (GET/POST)
- `handleCustomResponse()` - Manage custom response rules (GET/POST)

**Responsibilities**:
- Read/write custom blocked domains file
- Read/write custom response rules file
- Validate custom response rules
- Trigger AdBlock updates after rule changes
- Ensure directories exist before writing files

---

### 6. `api_utils.go` (~150 lines)
**Purpose**: Utility functions for API operations

**Key Functions**:
- `writeJSONError()` - Write JSON error response
- `writeJSONSuccess()` - Write JSON success response
- `corsMiddleware()` - CORS middleware handler
- `findWebDirectory()` - Locate web directory
- `deleteCacheFile()` - Delete cache file
- `writeConfigFile()` - Write configuration to file
- `addSourceToConfig()` - Add source to config
- `removeSourceFromConfig()` - Remove source from config
- `removeCustomRulesFromConfig()` - Remove custom rules from config

**Responsibilities**:
- Provide common response formatting
- Handle CORS headers
- Locate web UI files
- Manage file operations
- Manage configuration file updates

---

## API Endpoints Summary

### Basic Operations
- `GET /api/query` - Query DNS cache
- `GET /api/stats` - Get statistics
- `POST /api/stats/clear` - Clear statistics
- `POST /api/cache/clear` - Clear cache
- `GET /api/cache/memory` - Get cache memory stats
- `GET /health` - Health check

### Configuration
- `GET /api/config` - Get current config
- `POST /api/config` - Update config

### AdBlock Management
- `GET /api/adblock/status` - Get AdBlock status
- `GET /api/adblock/sources` - List sources
- `POST /api/adblock/sources` - Add source
- `PUT /api/adblock/sources` - Enable/disable source
- `DELETE /api/adblock/sources` - Remove source
- `POST /api/adblock/update` - Update rules
- `POST /api/adblock/toggle` - Toggle AdBlock
- `POST /api/adblock/test` - Test domain
- `POST /api/adblock/blockmode` - Set block mode
- `GET/POST /api/adblock/settings` - Manage settings

### Custom Rules
- `GET/POST /api/custom/blocked` - Manage blocked domains
- `GET/POST /api/custom/response` - Manage response rules

### Utilities
- `GET /api/recent-queries` - Get recent queries
- `GET /api/hot-domains` - Get hot domains
- `POST /api/restart` - Restart service

---

## Design Principles

1. **Single Responsibility**: Each file handles a specific domain (handlers, config, adblock, custom rules, utilities)
2. **Clear Separation**: Related functionality is grouped together
3. **Reusable Utilities**: Common operations are centralized in `api_utils.go`
4. **Consistent Response Format**: All endpoints use `APIResponse` structure
5. **Error Handling**: Consistent error handling and logging across all handlers

---

## Dependencies

- `net/http` - HTTP server and handlers
- `encoding/json` - JSON encoding/decoding
- `gopkg.in/yaml.v3` - YAML configuration handling
- `smartdnssort/config` - Configuration management
- `smartdnssort/logger` - Logging
- `smartdnssort/cache` - DNS cache
- `smartdnssort/dnsserver` - DNS server core
- `github.com/miekg/dns` - DNS protocol handling

---

## Migration Notes

The split maintains 100% backward compatibility with the original API. All endpoints function identically to the original implementation. The split improves:

- **Maintainability**: Easier to locate and modify specific functionality
- **Testability**: Each file can be tested independently
- **Readability**: Smaller, focused files are easier to understand
- **Scalability**: New handlers can be added to appropriate files

