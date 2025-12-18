# DNS Server Structure Documentation

This document describes the organization of the DNS Server package after splitting the original `server.go` file.

## Overview

The original `server.go` file (~600+ lines) has been split into 5 focused files, each handling a specific aspect of the DNS server functionality.

## File Organization

### 1. `server.go` (~110 lines)
**Purpose**: Core server struct definition and accessor methods

**Key Components**:
- `Server` struct - Main DNS server structure with all dependencies
- `GetCustomResponseManager()` - Get custom response manager
- `GetStats()` - Get statistics
- `ClearStats()` - Clear statistics
- `RecordRecentQuery()` - Record recent query
- `GetRecentQueries()` - Get recent queries
- `GetCache()` - Get cache instance
- `GetConfig()` - Get current configuration
- `GetAdBlockManager()` - Get AdBlock manager
- `SetAdBlockEnabled()` - Enable/disable AdBlock

**Responsibilities**:
- Define Server struct with all components
- Provide accessor methods for server state
- Manage recent queries circular buffer
- Provide thread-safe access to configuration and managers

---

### 2. `server_init.go` (~110 lines)
**Purpose**: Server initialization and component setup

**Key Functions**:
- `NewServer()` - Create and initialize new DNS server
- `convertHealthCheckConfig()` - Convert health check configuration

**Initialization Steps**:
1. Create sort queue for async IP sorting
2. Create refresh queue for cache refresh
3. Initialize bootstrap resolver
4. Initialize upstream servers
5. Create cache instance
6. Load persistent cache from disk
7. Initialize AdBlock manager
8. Initialize custom response manager
9. Setup prefetcher
10. Configure callbacks and sort functions

**Responsibilities**:
- Initialize all server components
- Load persistent cache
- Setup AdBlock and custom response managers
- Configure callbacks and sort functions
- Handle initialization errors gracefully

---

### 3. `server_lifecycle.go` (~60 lines)
**Purpose**: Server startup and shutdown operations

**Key Functions**:
- `Start()` - Start DNS server (UDP/TCP)
- `Shutdown()` - Gracefully shutdown server

**Start Process**:
1. Register DNS handler
2. Create UDP server
3. Create TCP server (if enabled)
4. Start cache cleanup routine
5. Start cache persistence routine
6. Start prefetcher
7. Listen and serve

**Shutdown Process**:
1. Shutdown UDP server
2. Shutdown TCP server
3. Stop sort queue
4. Stop prefetcher
5. Stop refresh queue

**Responsibilities**:
- Manage server lifecycle
- Handle UDP and TCP listeners
- Start background routines
- Graceful shutdown

---

### 4. `server_config.go` (~100 lines)
**Purpose**: Configuration management and hot-reload

**Key Functions**:
- `ApplyConfig()` - Apply new configuration (hot-reload)

**Configuration Changes Handled**:
- Upstream servers and strategy
- Ping settings
- Sort queue workers
- Refresh queue workers
- Prefetch settings

**Hot-Reload Process**:
1. Create new components outside lock
2. Compare old and new configurations
3. Create new components only if changed
4. Acquire lock and swap components
5. Stop old components
6. Update configuration reference

**Responsibilities**:
- Support hot-reload of configuration
- Minimize lock contention
- Gracefully replace components
- Maintain service availability during reload

---

### 5. `server_callbacks.go` (~40 lines)
**Purpose**: Upstream manager callbacks

**Key Functions**:
- `setupUpstreamCallback()` - Setup cache update callback

**Callback Responsibilities**:
- Update raw cache with new IPs from upstream
- Detect when more IPs are collected
- Trigger re-sorting when IP count increases
- Log cache update events

**Responsibilities**:
- Handle upstream cache updates
- Manage cache invalidation
- Trigger async sorting when needed

---

## Server Struct Fields

```go
type Server struct {
    mu                 sync.RWMutex              // Protects config and components
    cfg                *config.Config            // Current configuration
    stats              *stats.Stats              // Statistics collector
    cache              *cache.Cache              // DNS cache
    upstream           *upstream.Manager         // Upstream DNS manager
    pinger             *ping.Pinger              // IP pinger for sorting
    sortQueue          *cache.SortQueue          // Async sort queue
    prefetcher         *prefetch.Prefetcher      // DNS prefetcher
    refreshQueue       *RefreshQueue             // Async refresh queue
    recentQueries      [20]string                // Circular buffer for recent queries
    recentQueriesIndex int                       // Current index in circular buffer
    recentQueriesMu    sync.Mutex                // Protects recent queries
    udpServer          *dns.Server               // UDP DNS server
    tcpServer          *dns.Server               // TCP DNS server
    adblockManager     *adblock.AdBlockManager   // AdBlock filtering
    customRespManager  *CustomResponseManager    // Custom response rules
    requestGroup       singleflight.Group        // Merge concurrent requests
}
```

---

## Component Lifecycle

### Initialization Order
1. Sort Queue → Refresh Queue → Bootstrap Resolver
2. Upstream Servers → Upstream Manager
3. Cache (load from disk)
4. AdBlock Manager (start in background)
5. Custom Response Manager
6. Prefetcher
7. Setup callbacks and sort functions

### Shutdown Order
1. UDP Server
2. TCP Server
3. Sort Queue
4. Prefetcher
5. Refresh Queue

---

## Thread Safety

- **RWMutex (mu)**: Protects configuration and component references during hot-reload
- **Mutex (recentQueriesMu)**: Protects recent queries circular buffer
- **Singleflight**: Merges concurrent requests for the same domain

---

## Dependencies

- `smartdnssort/config` - Configuration management
- `smartdnssort/cache` - DNS cache
- `smartdnssort/upstream` - Upstream DNS servers
- `smartdnssort/ping` - IP latency measurement
- `smartdnssort/prefetch` - DNS prefetching
- `smartdnssort/stats` - Statistics collection
- `smartdnssort/adblock` - AdBlock filtering
- `smartdnssort/logger` - Logging
- `github.com/miekg/dns` - DNS protocol
- `golang.org/x/sync/singleflight` - Request deduplication

---

## Design Principles

1. **Single Responsibility**: Each file handles a specific aspect (init, lifecycle, config, callbacks)
2. **Minimal Lock Contention**: Hot-reload creates components outside lock
3. **Graceful Degradation**: Initialization failures don't crash the server
4. **Thread Safety**: Proper synchronization for concurrent access
5. **Component Isolation**: Each component can be replaced independently

---

## Migration Notes

The split maintains 100% backward compatibility. All existing code that uses the Server struct continues to work without changes. The split improves:

- **Maintainability**: Easier to locate and modify specific functionality
- **Testability**: Each file can be tested independently
- **Readability**: Smaller, focused files are easier to understand
- **Scalability**: New functionality can be added to appropriate files

