package webapi

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
)

// handleCustomBlocked 处理自定义拦截域名请求
func (s *Server) handleCustomBlocked(w http.ResponseWriter, r *http.Request) {
	customRulesFile := s.cfg.AdBlock.CustomRulesFile
	// Default validation if empty
	if customRulesFile == "" {
		customRulesFile = "./custom_rules.txt"
	}

	switch r.Method {
	case http.MethodGet:
		// 读取文件前加读锁
		s.customRulesMutex.RLock()
		defer s.customRulesMutex.RUnlock()

		// Read file content
		content, err := os.ReadFile(customRulesFile)
		if err != nil {
			if os.IsNotExist(err) {
				s.writeJSONSuccess(w, "Custom blocked domains", map[string]string{"content": ""})
				return
			}
			s.writeJSONError(w, "Failed to read custom rules file: "+err.Error(), http.StatusInternalServerError)
			return
		}
		s.writeJSONSuccess(w, "Custom blocked domains retrieved", map[string]string{"content": string(content)})

	case http.MethodPost:
		// 写入文件前加写锁
		s.customRulesMutex.Lock()
		defer s.customRulesMutex.Unlock()

		var payload struct {
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			s.writeJSONError(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		// Ensure directory exists
		dir := filepath.Dir(customRulesFile)
		if err := os.MkdirAll(dir, 0755); err != nil {
			s.writeJSONError(w, "Failed to create directory: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Write to file
		if err := os.WriteFile(customRulesFile, []byte(payload.Content), 0644); err != nil {
			s.writeJSONError(w, "Failed to write custom rules file: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Trigger AdBlock update
		go func() {
			if s.dnsServer.GetAdBlockManager() != nil {
				s.dnsServer.GetAdBlockManager().UpdateRules(true)
			}
		}()

		s.writeJSONSuccess(w, "Custom blocked domains saved and update triggered", nil)

	default:
		s.writeJSONError(w, "Invalid request method", http.StatusMethodNotAllowed)
	}
}

// handleCustomResponse 处理自定义响应规则请求
func (s *Server) handleCustomResponse(w http.ResponseWriter, r *http.Request) {
	customResponseFile := s.cfg.AdBlock.CustomResponseFile
	if customResponseFile == "" {
		customResponseFile = "./adblock_cache/custom_response_rules.txt"
	}

	switch r.Method {
	case http.MethodGet:
		// 读取文件前加读锁
		s.customResponseMutex.RLock()
		defer s.customResponseMutex.RUnlock()

		content, err := os.ReadFile(customResponseFile)
		if err != nil {
			if os.IsNotExist(err) {
				s.writeJSONSuccess(w, "Custom response rules", map[string]string{"content": ""})
				return
			}
			s.writeJSONError(w, "Failed to read custom response file: "+err.Error(), http.StatusInternalServerError)
			return
		}
		s.writeJSONSuccess(w, "Custom response rules retrieved", map[string]string{"content": string(content)})

	case http.MethodPost:
		// 写入文件前加写锁
		s.customResponseMutex.Lock()
		defer s.customResponseMutex.Unlock()

		var payload struct {
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			s.writeJSONError(w, "Invalid request body", http.StatusBadRequest)
			return
		}

		// Validate content BEFORE writing
		mgr := s.dnsServer.GetCustomResponseManager()
		if mgr == nil {
			// Should not happen if initialized correctly
			s.writeJSONError(w, "Custom response manager not initialized", http.StatusInternalServerError)
			return
		}

		if err := mgr.ValidateRules(payload.Content); err != nil {
			s.writeJSONError(w, "Validation failed: "+err.Error(), http.StatusBadRequest)
			return
		}

		// Ensure directory exists
		dir := filepath.Dir(customResponseFile)
		if err := os.MkdirAll(dir, 0755); err != nil {
			s.writeJSONError(w, "Failed to create directory: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Write to file
		if err := os.WriteFile(customResponseFile, []byte(payload.Content), 0644); err != nil {
			s.writeJSONError(w, "Failed to write custom response file: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Reload manager
		if err := mgr.Load(); err != nil {
			s.writeJSONError(w, "Saved but failed to reload rules: "+err.Error(), http.StatusInternalServerError)
			return
		}

		s.writeJSONSuccess(w, "Custom response rules saved and reloaded", nil)

	default:
		s.writeJSONError(w, "Invalid request method", http.StatusMethodNotAllowed)
	}
}
