package webapi

import (
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"smartdnssort/logger"
	"time"

	"gopkg.in/yaml.v3"
)

// SetupRequest 初始化设置请求
type SetupRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Port     int    `json:"port"`
}

// LoginRequest 登录请求
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// AuthStatusResponse 认证状态响应
type AuthStatusResponse struct {
	Initialized bool   `json:"initialized"`
	NeedSetup   bool   `json:"need_setup"`
	NeedLogin   bool   `json:"need_login"`
}

// handleSetup 处理初始化设置
// 仅在 Initialized == false 时允许调用
func (s *Server) handleSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeJSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 检查是否已经初始化
	s.cfgMutex.RLock()
	alreadyInitialized := s.cfg.WebUI.Initialized
	s.cfgMutex.RUnlock()

	if alreadyInitialized {
		s.writeJSONError(w, "Already initialized", http.StatusBadRequest)
		return
	}

	// 解析请求体
	var req SetupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeJSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// 验证输入
	if req.Username == "" || req.Password == "" {
		s.writeJSONError(w, "Username and password are required", http.StatusBadRequest)
		return
	}

	if len(req.Username) < 3 || len(req.Username) > 32 {
		s.writeJSONError(w, "Username must be 3-32 characters", http.StatusBadRequest)
		return
	}

	if len(req.Password) < 6 {
		s.writeJSONError(w, "Password must be at least 6 characters", http.StatusBadRequest)
		return
	}

	if req.Port < 1024 || req.Port > 65535 {
		s.writeJSONError(w, "Port must be between 1024 and 65535", http.StatusBadRequest)
		return
	}

	// 对密码进行哈希处理
	hashedPassword, err := HashPassword(req.Password)
	if err != nil {
		logger.Errorf("Failed to hash password: %v", err)
		s.writeJSONError(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// 更新配置
	s.cfgMutex.Lock()
	s.cfg.WebUI.Username = req.Username
	s.cfg.WebUI.Password = hashedPassword
	s.cfg.WebUI.Initialized = true
	if req.Port > 0 {
		s.cfg.WebUI.ListenPort = req.Port
	}

	// 保存到配置文件
	yamlData, err := yaml.Marshal(s.cfg)
	if err != nil {
		s.cfgMutex.Unlock()
		logger.Errorf("Failed to marshal config: %v", err)
		s.writeJSONError(w, "Failed to save configuration", http.StatusInternalServerError)
		return
	}

	if err := s.writeConfigFile(yamlData); err != nil {
		s.cfgMutex.Unlock()
		logger.Errorf("Failed to write config file: %v", err)
		s.writeJSONError(w, "Failed to save configuration", http.StatusInternalServerError)
		return
	}
	s.cfgMutex.Unlock()

	logger.Debug("WebUI initialized successfully")

	// 返回成功响应，告知前端即将重启
	s.writeJSONSuccess(w, "Setup completed successfully, service restarting...", map[string]interface{}{
		"port":     s.cfg.WebUI.ListenPort,
		"restarting": true,
	})

	// 异步重启服务以应用新配置（包括新端口）
	if s.restartFunc != nil {
		go func() {
			// 等待一小段时间确保响应已发送回客户端
			time.Sleep(1 * time.Second)
			logger.Debug("Triggering service restart after setup...")
			s.restartFunc()
		}()
	}
}

// handleLogin 处理登录请求
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeJSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 检查是否已初始化
	s.cfgMutex.RLock()
	initialized := s.cfg.WebUI.Initialized
	storedUsername := s.cfg.WebUI.Username
	storedPassword := s.cfg.WebUI.Password
	s.cfgMutex.RUnlock()

	if !initialized {
		s.writeJSONError(w, "Not initialized", http.StatusForbidden)
		return
	}

	// 解析请求体
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeJSONError(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// 验证用户名
	if subtle.ConstantTimeCompare([]byte(req.Username), []byte(storedUsername)) != 1 {
		s.writeJSONError(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	// 验证密码
	if !CheckPasswordHash(req.Password, storedPassword) {
		s.writeJSONError(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	// 登录成功，设置 Session Cookie
	sessionToken := generateSessionToken()
	
	// 存储会话
	s.sessionManager.SetSession(sessionToken, req.Username)

	// 设置 Cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    sessionToken,
		Path:     "/",
		HttpOnly: true,
		Secure:   false, // 在生产环境中应设为 true（使用 HTTPS 时）
		SameSite: http.SameSiteStrictMode,
		MaxAge:   86400, // 24 小时
	})

	logger.Debugf("User %s logged in successfully", req.Username)

	s.writeJSONSuccess(w, "Login successful", map[string]interface{}{
		"username": req.Username,
	})
}

// handleLogout 处理登出请求
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		s.writeJSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 获取当前会话
	cookie, err := r.Cookie("session")
	if err != nil {
		// 没有会话，直接返回成功
		s.writeJSONSuccess(w, "Logged out successfully", nil)
		return
	}

	// 删除会话
	s.sessionManager.DeleteSession(cookie.Value)

	// 清除 Cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   false,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   -1,
	})

	logger.Debug("User logged out")
	s.writeJSONSuccess(w, "Logged out successfully", nil)
}

// handleAuthStatus 处理认证状态查询
func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		s.writeJSONError(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	s.cfgMutex.RLock()
	initialized := s.cfg.WebUI.Initialized
	s.cfgMutex.RUnlock()

	resp := AuthStatusResponse{
		Initialized: initialized,
	}

	if !initialized {
		resp.NeedSetup = true
	} else {
		// 检查登录状态
		cookie, err := r.Cookie("session")
		if err != nil || !s.sessionManager.IsValidSession(cookie.Value) {
			resp.NeedLogin = true
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
