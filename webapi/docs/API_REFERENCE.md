# SmartDNSSort API Reference

## Overview

SmartDNSSort provides a RESTful API for managing DNS server configuration, monitoring statistics, and controlling various features like AdBlock and IP pool monitoring.

**Base URL:** `http://localhost:<webui-port>/api/`

**Default Port:** 8080 (configurable via `webui.listen_port`)

## Authentication

Currently, the API does not require authentication for local access. For production deployments, consider implementing authentication middleware.

## Security

### CSRF Protection

All non-GET requests require a valid CSRF token in the `X-CSRF-Token` header.

1. **Get CSRF Token:**
   ```
   GET /api/csrf-token
   ```
   Response:
   ```json
   {
     "success": true,
     "message": "CSRF token generated",
     "data": {
       "csrf_token": "base64-encoded-token",
       "expires_in": "2h0m0s"
     }
   }
   ```

2. **Use Token:**
   Include the token in subsequent POST/PUT/DELETE requests:
   ```
   X-CSRF-Token: <token>
   ```

### Content Security Policy

The API enforces the following CSP headers:
- `default-src 'self'`
- `script-src 'self' 'unsafe-inline'`
- `style-src 'self' 'unsafe-inline' https://fonts.googleapis.com`
- `font-src 'self' https://fonts.gstatic.com`
- `img-src 'self' data: blob:`
- `connect-src 'self'`

---

## API Endpoints

### Health Check

#### GET /health

Returns the service health status.

**Response:**
```json
{
  "success": true,
  "message": "Service is healthy",
  "data": {
    "status": "healthy"
  }
}
```

---

### Statistics

#### GET /api/stats

Retrieves DNS server statistics.

**Query Parameters:**
| Parameter | Type | Description |
|-----------|------|-------------|
| days | int | Time range in days (1, 7, or 30). Default: 7 |

**Response:**
```json
{
  "success": true,
  "message": "Statistics retrieved successfully",
  "data": {
    "total_queries": 12345,
    "blocked_queries": 100,
    "cached_queries": 5000,
    "average_latency_ms": 45,
    "top_domains": [...],
    "cache_memory_stats": {
      "max_memory_mb": 100,
      "current_entries": 5000,
      "memory_percent": 50.5
    },
    "network_online": true
  }
}
```

#### GET /api/upstream-stats

Retrieves upstream server statistics.

**Query Parameters:**
| Parameter | Type | Description |
|-----------|------|-------------|
| days | int | Time range in days (1, 7, or 30). Default: 7 |

**Response:**
```json
{
  "success": true,
  "message": "Upstream stats retrieved successfully",
  "data": {
    "servers": [
      {
        "address": "8.8.8.8:53",
        "queries": 1000,
        "errors": 5,
        "avg_latency_ms": 30
      }
    ]
  }
}
```

#### POST /api/stats/clear

Clears all statistics.

**CSRF Required:** Yes

**Response:**
```json
{
  "success": true,
  "message": "Statistics cleared successfully"
}
```

---

### Recent Queries

#### GET /api/recent-queries

Retrieves recent DNS queries.

**Query Parameters:**
| Parameter | Type | Description |
|-----------|------|-------------|
| days | int | Time range in days (1, 7, or 30). Default: 7 |

**Response:**
```json
{
  "success": true,
  "message": "Recent queries retrieved successfully",
  "data": ["example.com", "google.com", ...]
}
```

---

### Recent Blocked

#### GET /api/recent-blocked

Retrieves recently blocked domains.

**Query Parameters:**
| Parameter | Type | Description |
|-----------|------|-------------|
| days | int | Time range in days (1, 7, or 30). Default: 7 |

**Response:**
```json
{
  "success": true,
  "message": "Recently blocked domains retrieved successfully",
  "data": ["ads.example.com", "tracker.example.com", ...]
}
```

---

### Hot Domains

#### GET /api/hot-domains

Retrieves most frequently queried domains.

**Response:**
```json
{
  "success": true,
  "message": "Hot domains retrieved successfully",
  "data": [
    {"domain": "google.com", "count": 1000},
    {"domain": "facebook.com", "count": 500}
  ]
}
```

---

### Blocked Domains

#### GET /api/blocked-domains

Retrieves most frequently blocked domains.

**Query Parameters:**
| Parameter | Type | Description |
|-----------|------|-------------|
| days | int | Time range in days (1, 7, or 30). Default: 7 |

**Response:**
```json
{
  "success": true,
  "message": "Blocked domains retrieved successfully",
  "data": [
    {"domain": "ads.example.com", "count": 100},
    {"domain": "tracker.example.com", "count": 50}
  ]
}
```

---

### Configuration

#### GET /api/config

Retrieves current DNS server configuration.

**Response:**
```json
{
  "dns": {
    "listen_port": 53,
    "protocol": "udp"
  },
  "upstream": {
    "servers": ["8.8.8.8:53", "1.1.1.1:53"],
    "strategy": "racing",
    "timeout_ms": 2000
  },
  "cache": {
    "max_memory_mb": 100,
    "min_ttl_seconds": 60,
    "max_ttl_seconds": 86400
  },
  "ping": {
    "count": 3,
    "timeout_ms": 1000,
    "strategy": "min"
  },
  "adblock": {
    "enable": true,
    "block_mode": "zero_ip"
  },
  "webui": {
    "enabled": true,
    "listen_port": 8080
  }
}
```

#### POST /api/config

Updates DNS server configuration.

**CSRF Required:** Yes

**Request Body:** Configuration JSON object

**Response:**
```json
{
  "success": true,
  "message": "Configuration updated successfully"
}
```

**Error Response:**
```json
{
  "success": false,
  "message": "Validation failed: invalid DNS listen port"
}
```

#### POST /api/config/reset

Resets configuration to defaults.

**CSRF Required:** Yes

**Response:**
```json
{
  "success": true,
  "message": "Configuration reset to defaults"
}
```

#### GET /api/config/export

Exports current configuration as JSON file.

**Response:** JSON file download with `Content-Disposition: attachment`

---

### Cache Management

#### POST /api/cache/clear

Clears DNS cache.

**CSRF Required:** Yes

**Response:**
```json
{
  "success": true,
  "message": "Cache cleared successfully"
}
```

#### GET /api/cache/memory

Retrieves cache memory statistics.

**Response:**
```json
{
  "success": true,
  "message": "Cache memory stats retrieved successfully",
  "data": {
    "max_memory_mb": 100,
    "max_entries": 100000,
    "current_entries": 5000,
    "current_memory_mb": 50,
    "memory_percent": 50.0,
    "expired_entries": 100,
    "expired_percent": 2.0,
    "protected_entries": 50,
    "evictions_per_min": 0.5
  }
}
```

---

### Service Control

#### POST /api/restart

Restarts the DNS service.

**CSRF Required:** Yes

**Response:**
```json
{
  "success": true,
  "message": "Service restart initiated"
}
```

---

### AdBlock Management

#### GET /api/adblock/status

Retrieves AdBlock status.

**Response:**
```json
{
  "success": true,
  "message": "AdBlock status retrieved",
  "data": {
    "enabled": true,
    "block_mode": "zero_ip",
    "total_rules": 50000,
    "last_update": "2024-01-01T00:00:00Z"
  }
}
```

#### POST /api/adblock/toggle

Toggles AdBlock on/off.

**CSRF Required:** Yes

**Request Body:**
```json
{
  "enabled": true
}
```

**Response:**
```json
{
  "success": true,
  "message": "AdBlock toggled successfully"
}
```

#### POST /api/adblock/update

Updates AdBlock rules from sources.

**CSRF Required:** Yes

**Response:**
```json
{
  "success": true,
  "message": "AdBlock rules updated",
  "data": {
    "updated_sources": 3,
    "total_rules": 55000
  }
}
```

#### GET /api/adblock/sources

Retrieves AdBlock source list.

**Response:**
```json
{
  "success": true,
  "message": "AdBlock sources retrieved",
  "data": {
    "sources": [
      {
        "url": "https://example.com/blocklist.txt",
        "enabled": true,
        "last_update": "2024-01-01T00:00:00Z",
        "rule_count": 20000
      }
    ]
  }
}
```

#### POST /api/adblock/test

Tests if a domain would be blocked.

**Request Body:**
```json
{
  "domain": "ads.example.com"
}
```

**Response:**
```json
{
  "success": true,
  "message": "Test completed",
  "data": {
    "domain": "ads.example.com",
    "blocked": true,
    "rule": "||ads.example.com^"
  }
}
```

---

### IP Pool Monitoring

#### GET /api/ip-pool/status

Retrieves IP pool status.

**Response:**
```json
{
  "success": true,
  "message": "IP pool status retrieved successfully",
  "data": {
    "total_ips": 100,
    "total_ref_count": 500,
    "total_heat": 10000,
    "last_updated": "2024-01-01T00:00:00Z",
    "monitor_stats": {
      "enabled": true,
      "check_interval_ms": 5000
    }
  }
}
```

#### GET /api/ip-pool/top

Retrieves top IPs by reference count.

**Query Parameters:**
| Parameter | Type | Description |
|-----------|------|-------------|
| limit | int | Number of results. Default: 10 |

**Response:**
```json
{
  "success": true,
  "message": "IP pool data retrieved successfully",
  "data": [
    {
      "ip": "1.2.3.4",
      "rep_domain": "example.com",
      "ref_count": 50,
      "access_heat": 1000,
      "rtt": 30,
      "last_access": "2024-01-01T00:00:00Z"
    }
  ]
}
```

#### POST /api/ip-pool/toggle

Toggles IP pool monitoring.

**CSRF Required:** Yes

**Request Body:**
```json
{
  "enabled": true
}
```

---

### Custom Rules

#### GET /api/custom/blocked

Retrieves custom blocked domains.

**Response:**
```json
{
  "success": true,
  "message": "Custom blocked domains retrieved",
  "data": ["custom-blocked.example.com", ...]
}
```

#### POST /api/custom/blocked

Adds a custom blocked domain.

**CSRF Required:** Yes

**Request Body:**
```json
{
  "domain": "custom-blocked.example.com"
}
```

#### DELETE /api/custom/blocked

Removes a custom blocked domain.

**CSRF Required:** Yes

**Request Body:**
```json
{
  "domain": "custom-blocked.example.com"
}
```

#### GET /api/custom/response

Retrieves custom response rules.

**Response:**
```json
{
  "success": true,
  "message": "Custom response rules retrieved",
  "data": [
    {
      "domain": "custom.example.com",
      "response": "192.168.1.1",
      "response_type": "ip"
    }
  ]
}
```

---

### Recursor Management

#### GET /api/recursor/status

Retrieves recursor status.

**Response:**
```json
{
  "success": true,
  "message": "Recursor status retrieved",
  "data": {
    "enabled": true,
    "running": true,
    "version": "1.0.0"
  }
}
```

#### GET /api/recursor/install-status

Retrieves recursor installation status.

**Response:**
```json
{
  "success": true,
  "message": "Install status retrieved",
  "data": {
    "installed": true,
    "version": "1.0.0",
    "path": "/usr/sbin/unbound"
  }
}
```

#### GET /api/recursor/system-info

Retrieves system information for recursor installation.

**Response:**
```json
{
  "success": true,
  "message": "System info retrieved",
  "data": {
    "os": "linux",
    "arch": "amd64",
    "has_systemd": true
  }
}
```

---

### Unbound Configuration

#### GET /api/unbound/config

Retrieves Unbound configuration.

**Response:**
```json
{
  "success": true,
  "message": "Unbound config retrieved",
  "data": {
    "content": "# Unbound configuration..."
  }
}
```

#### POST /api/unbound/config

Updates Unbound configuration.

**CSRF Required:** Yes

**Request Body:**
```json
{
  "content": "# Updated Unbound configuration..."
}
```

---

## Error Handling

All API endpoints follow a consistent error response format:

```json
{
  "success": false,
  "message": "Error description"
}
```

### Common HTTP Status Codes

| Status Code | Description |
|-------------|-------------|
| 200 | Success |
| 400 | Bad Request - Invalid parameters |
| 403 | Forbidden - Invalid or missing CSRF token |
| 404 | Not Found |
| 405 | Method Not Allowed |
| 413 | Request Entity Too Large |
| 500 | Internal Server Error |

---

## Rate Limiting

Currently, no rate limiting is implemented. For production use, consider adding rate limiting middleware.

---

## CORS Policy

The API supports CORS with the following default policy:
- **Allowed Origins:** Same origin (configurable)
- **Allowed Methods:** GET, POST, PUT, DELETE, OPTIONS
- **Allowed Headers:** Content-Type, X-CSRF-Token, Authorization
- **Max Age:** 86400 seconds

---

## WebSocket Support

WebSocket support is not currently implemented. Real-time updates are achieved through polling.

---

## Versioning

The API is versioned through the URL path. Current version is v1 (implicit in `/api/`).

---

## Changelog

### v1.0.0
- Initial API release
- CSRF protection
- CSP headers
- Unified error handling
- Configuration validation
- AdBlock management
- IP pool monitoring
- Custom rules support
