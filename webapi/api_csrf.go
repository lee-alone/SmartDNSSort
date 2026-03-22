package webapi

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"sync"
	"time"

	"smartdnssort/logger"
)

// CSRFToken CSRF 令牌结构
type CSRFToken struct {
	Token     string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// CSRFManager CSRF 令牌管理器
type CSRFManager struct {
	tokens map[string]*CSRFToken
	mu     sync.RWMutex
	// 配置参数
	tokenLength    int
	tokenExpiry    time.Duration
	cleanupTicker  *time.Ticker
	cleanupStopped chan struct{}
}

// CSRFConfig CSRF 配置
type CSRFConfig struct {
	TokenLength int
	TokenExpiry time.Duration
}

// DefaultCSRFConfig 默认 CSRF 配置
var DefaultCSRFConfig = CSRFConfig{
	TokenLength: 32,
	TokenExpiry: 2 * time.Hour,
}

// NewCSRFManager 创建新的 CSRF 管理器
func NewCSRFManager(config CSRFConfig) *CSRFManager {
	m := &CSRFManager{
		tokens:         make(map[string]*CSRFToken),
		tokenLength:    config.TokenLength,
		tokenExpiry:    config.TokenExpiry,
		cleanupStopped: make(chan struct{}),
	}

	// 启动定期清理过期令牌的 goroutine
	m.cleanupTicker = time.NewTicker(30 * time.Minute)
	go m.cleanupLoop()

	return m
}

// GenerateToken 生成新的 CSRF 令牌
func (m *CSRFManager) GenerateToken() string {
	tokenBytes := make([]byte, m.tokenLength)
	if _, err := rand.Read(tokenBytes); err != nil {
		logger.Errorf("Failed to generate CSRF token: %v", err)
		// 使用时间戳作为后备方案
		return base64.URLEncoding.EncodeToString([]byte(time.Now().String()))
	}

	token := base64.URLEncoding.EncodeToString(tokenBytes)

	m.mu.Lock()
	defer m.mu.Unlock()

	m.tokens[token] = &CSRFToken{
		Token:     token,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(m.tokenExpiry),
	}

	logger.Debugf("Generated new CSRF token, expires at: %v", m.tokens[token].ExpiresAt)
	return token
}

// ValidateToken 验证 CSRF 令牌
func (m *CSRFManager) ValidateToken(token string) bool {
	if token == "" {
		return false
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	csrfToken, exists := m.tokens[token]
	if !exists {
		logger.Debugf("CSRF token not found: %s", token[:min(8, len(token))]+"...")
		return false
	}

	if time.Now().After(csrfToken.ExpiresAt) {
		logger.Debugf("CSRF token expired: %s", token[:min(8, len(token))]+"...")
		return false
	}

	return true
}

// RemoveToken 移除 CSRF 令牌（可选，用于一次性令牌）
func (m *CSRFManager) RemoveToken(token string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.tokens, token)
}

// cleanupLoop 定期清理过期令牌
func (m *CSRFManager) cleanupLoop() {
	for {
		select {
		case <-m.cleanupTicker.C:
			m.cleanupExpiredTokens()
		case <-m.cleanupStopped:
			return
		}
	}
}

// cleanupExpiredTokens 清理过期令牌
func (m *CSRFManager) cleanupExpiredTokens() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	expiredCount := 0
	for token, csrfToken := range m.tokens {
		if now.After(csrfToken.ExpiresAt) {
			delete(m.tokens, token)
			expiredCount++
		}
	}

	if expiredCount > 0 {
		logger.Debugf("Cleaned up %d expired CSRF tokens", expiredCount)
	}
}

// Stop 停止 CSRF 管理器
func (m *CSRFManager) Stop() {
	m.cleanupTicker.Stop()
	close(m.cleanupStopped)
}

// min 返回两个整数中的较小值
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// csrfMiddleware CSRF 保护中间件
// 用于保护非 GET 请求免受跨站请求伪造攻击
func (s *Server) csrfMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// GET、HEAD、OPTIONS 请求不需要 CSRF 保护
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}

		// 从请求头获取 CSRF 令牌
		token := r.Header.Get("X-CSRF-Token")
		if token == "" {
			// 也支持从表单字段获取
			token = r.FormValue("csrf_token")
		}

		// 验证令牌
		if !s.csrfManager.ValidateToken(token) {
			logger.Warnf("CSRF validation failed for %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)
			s.writeJSONError(w, "Invalid or missing CSRF token", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// handleCSRFToken 处理 CSRF 令牌请求
func (s *Server) handleCSRFToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeJSONError(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	token := s.csrfManager.GenerateToken()
	s.writeJSONSuccess(w, "CSRF token generated", map[string]string{
		"csrf_token": token,
		"expires_in": s.csrfManager.tokenExpiry.String(),
	})
}
