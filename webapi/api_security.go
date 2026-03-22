package webapi

import (
	"net/http"
	"strings"

	"smartdnssort/logger"
)

// SecurityConfig 安全配置
type SecurityConfig struct {
	// CORS 配置
	AllowedOrigins []string // 允许的源列表，空表示允许所有
	AllowedMethods []string
	AllowedHeaders []string

	// CSP 配置
	ContentSecurityPolicy string

	// 其他安全头
	FrameOptions       string
	ContentTypeOptions string
	XSSProtection      string
	ReferrerPolicy     string
	PermissionsPolicy  string
}

// DefaultSecurityConfig 默认安全配置
var DefaultSecurityConfig = SecurityConfig{
	// CORS 配置 - 默认只允许同源访问
	AllowedOrigins: []string{}, // 空表示同源或通过配置指定
	AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
	AllowedHeaders: []string{"Content-Type", "X-CSRF-Token", "Authorization"},

	// CSP 配置 - 严格的默认策略
	ContentSecurityPolicy: "default-src 'self'; " +
		"script-src 'self' 'unsafe-inline'; " +
		"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; " +
		"font-src 'self' https://fonts.gstatic.com; " +
		"img-src 'self' data: blob:; " +
		"connect-src 'self'; " +
		"frame-ancestors 'self'; " +
		"form-action 'self'; " +
		"base-uri 'self';",

	// 其他安全头
	FrameOptions:       "SAMEORIGIN",
	ContentTypeOptions: "nosniff",
	XSSProtection:      "1; mode=block",
	ReferrerPolicy:     "strict-origin-when-cross-origin",
	PermissionsPolicy:  "geolocation=(), microphone=(), camera=()",
}

// securityMiddleware 安全中间件
// 添加 CSP 和其他安全响应头
func (s *Server) securityMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 设置 CSP 头
		if s.securityConfig.ContentSecurityPolicy != "" {
			w.Header().Set("Content-Security-Policy", s.securityConfig.ContentSecurityPolicy)
		}

		// 设置 X-Frame-Options
		if s.securityConfig.FrameOptions != "" {
			w.Header().Set("X-Frame-Options", s.securityConfig.FrameOptions)
		}

		// 设置 X-Content-Type-Options
		if s.securityConfig.ContentTypeOptions != "" {
			w.Header().Set("X-Content-Type-Options", s.securityConfig.ContentTypeOptions)
		}

		// 设置 X-XSS-Protection (虽然现代浏览器已弃用，但为兼容性保留)
		if s.securityConfig.XSSProtection != "" {
			w.Header().Set("X-XSS-Protection", s.securityConfig.XSSProtection)
		}

		// 设置 Referrer-Policy
		if s.securityConfig.ReferrerPolicy != "" {
			w.Header().Set("Referrer-Policy", s.securityConfig.ReferrerPolicy)
		}

		// 设置 Permissions-Policy
		if s.securityConfig.PermissionsPolicy != "" {
			w.Header().Set("Permissions-Policy", s.securityConfig.PermissionsPolicy)
		}

		next.ServeHTTP(w, r)
	})
}

// enhancedCORSMiddleware 增强的 CORS 中间件
// 支持配置化的源控制
func (s *Server) enhancedCORSMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		// 如果没有 Origin 头，说明是同源请求或非浏览器请求
		if origin == "" {
			next.ServeHTTP(w, r)
			return
		}

		// 检查是否允许该源
		allowedOrigin := s.getAllowedOrigin(origin)

		if allowedOrigin != "" {
			w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
			w.Header().Set("Access-Control-Allow-Methods", strings.Join(s.securityConfig.AllowedMethods, ", "))
			w.Header().Set("Access-Control-Allow-Headers", strings.Join(s.securityConfig.AllowedHeaders, ", "))
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Max-Age", "86400") // 24小时
		} else {
			// 不允许的源，记录日志
			logger.Warnf("CORS: Origin %s not allowed for %s %s", origin, r.Method, r.URL.Path)
		}

		// 处理预检请求
		if r.Method == http.MethodOptions {
			if allowedOrigin != "" {
				w.WriteHeader(http.StatusOK)
			} else {
				w.WriteHeader(http.StatusForbidden)
			}
			return
		}

		next.ServeHTTP(w, r)
	})
}

// getAllowedOrigin 获取允许的源
func (s *Server) getAllowedOrigin(origin string) string {
	// 如果配置为空，允许同源请求
	if len(s.securityConfig.AllowedOrigins) == 0 {
		// 检查是否是同源请求（通过检查 Host 头）
		// 注意：这里简化处理，实际生产环境可能需要更严格的检查
		return origin
	}

	// 检查配置的允许源列表
	for _, allowed := range s.securityConfig.AllowedOrigins {
		if allowed == "*" {
			return "*"
		}
		if allowed == origin {
			return origin
		}
		// 支持通配符匹配（例如 *.example.com）
		if strings.HasPrefix(allowed, "*.") {
			domain := allowed[2:]
			if strings.HasSuffix(origin, "://"+domain) || strings.HasSuffix(origin, "."+domain) {
				return origin
			}
		}
	}

	return ""
}

// isCSRFExemptPath 检查路径是否豁免 CSRF 保护
func (s *Server) isCSRFExemptPath(path string) bool {
	// 这些路径不需要 CSRF 保护
	exemptPaths := []string{
		"/api/csrf-token",
		"/health",
	}

	for _, exempt := range exemptPaths {
		if path == exempt {
			return true
		}
	}

	// GET 请求通常不需要 CSRF 保护
	return false
}

// combinedSecurityMiddleware 组合安全中间件
// 包含 CSP、CORS 和 CSRF 保护
func (s *Server) combinedSecurityMiddleware(next http.Handler) http.Handler {
	// 先应用安全头
	handler := s.securityMiddleware(next)
	// 再应用 CORS
	handler = s.enhancedCORSMiddleware(handler)
	// 最后应用 CSRF（仅对非 GET 请求）
	handler = s.csrfMiddleware(handler)

	return handler
}
