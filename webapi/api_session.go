package webapi

import (
	"crypto/rand"
	"encoding/hex"
	"smartdnssort/logger"
	"sync"
	"time"
)

// SessionManager 会话管理器
type SessionManager struct {
	sessions map[string]*Session
	mutex    sync.RWMutex
	maxAge   time.Duration
}

// Session 会话信息
type Session struct {
	Username  string
	CreatedAt time.Time
	ExpiresAt time.Time
}

// NewSessionManager 创建新的会话管理器
func NewSessionManager(maxAge time.Duration) *SessionManager {
	sm := &SessionManager{
		sessions: make(map[string]*Session),
		maxAge:   maxAge,
	}
	
	// 启动定期清理过期会话的 goroutine
	go sm.cleanupExpiredSessions()
	
	return sm
}

// generateSessionToken 生成安全的会话令牌
func generateSessionToken() string {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		// 加密安全随机数生成器失败时，必须 panic 而非回退到可预测的值
		// 使用可预测的回退会严重削弱会话令牌的安全性
		logger.Fatalf("CRITICAL: Failed to generate cryptographically secure session token: %v", err)
		// 永远不会到达这里
	}
	return hex.EncodeToString(bytes)
}

// SetSession 设置会话
func (sm *SessionManager) SetSession(token string, username string) {
	now := time.Now()
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	
	sm.sessions[token] = &Session{
		Username:  username,
		CreatedAt: now,
		ExpiresAt: now.Add(sm.maxAge),
	}
}

// IsValidSession 检查会话是否有效
func (sm *SessionManager) IsValidSession(token string) bool {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	
	session, exists := sm.sessions[token]
	if !exists {
		return false
	}
	
	// 检查是否过期
	if time.Now().After(session.ExpiresAt) {
		return false
	}
	
	return true
}

// GetSessionUsername 获取会话用户名
func (sm *SessionManager) GetSessionUsername(token string) string {
	sm.mutex.RLock()
	defer sm.mutex.RUnlock()
	
	session, exists := sm.sessions[token]
	if !exists {
		return ""
	}
	
	// 检查是否过期
	if time.Now().After(session.ExpiresAt) {
		return ""
	}
	
	return session.Username
}

// DeleteSession 删除会话
func (sm *SessionManager) DeleteSession(token string) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()
	
	delete(sm.sessions, token)
}

// cleanupExpiredSessions 清理过期的会话
func (sm *SessionManager) cleanupExpiredSessions() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	
	for range ticker.C {
		sm.mutex.Lock()
		now := time.Now()
		for token, session := range sm.sessions {
			if now.After(session.ExpiresAt) {
				delete(sm.sessions, token)
			}
		}
		sm.mutex.Unlock()
	}
}

// Stop 停止会话管理器
func (sm *SessionManager) Stop() {
	// cleanup goroutine 会通过 ticker 自动结束
}
