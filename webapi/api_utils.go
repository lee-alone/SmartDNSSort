package webapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"smartdnssort/config"
	"smartdnssort/logger"

	"gopkg.in/yaml.v3"
)

// writeJSONError 写入 JSON 错误响应
func (s *Server) writeJSONError(w http.ResponseWriter, message string, statusCode int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(APIResponse{
		Success: false,
		Message: message,
	})
}

// writeJSONSuccess 写入 JSON 成功响应
func (s *Server) writeJSONSuccess(w http.ResponseWriter, message string, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(APIResponse{
		Success: true,
		Message: message,
		Data:    data,
	})
}

// corsMiddleware CORS 中间件
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// findWebDirectory 查找 Web 目录
func (s *Server) findWebDirectory() string {
	possiblePaths := []string{}
	if exePath, err := os.Executable(); err == nil {
		execDir := filepath.Dir(exePath)
		possiblePaths = append(possiblePaths,
			filepath.Join(execDir, "web"),
			filepath.Join(execDir, "..", "web"),
		)
	}
	possiblePaths = append(possiblePaths, "./web", "web")
	possiblePaths = append(possiblePaths, "/var/lib/SmartDNSSort/web", "/usr/share/smartdnssort/web", "/etc/SmartDNSSort/web")

	for _, path := range possiblePaths {
		if info, err := os.Stat(path); err == nil && info.IsDir() {
			if _, err := os.Stat(filepath.Join(path, "index.html")); err == nil {
				return path
			}
		}
	}
	return ""
}

// deleteCacheFile 删除缓存文件
func (s *Server) deleteCacheFile(cacheFile string) error {
	if err := os.Remove(cacheFile); err != nil {
		if !os.IsNotExist(err) {
			// 文件存在但删除失败
			logger.Warnf("Warning: Failed to delete cache file %s: %v", cacheFile, err)
			return fmt.Errorf("Memory cache cleared, but failed to delete disk cache file: %v", err)
		}
		// 文件不存在,这是正常的
		logger.Infof("Disk cache file %s does not exist, skipping deletion.", cacheFile)
	} else {
		logger.Infof("Disk cache file %s deleted successfully.", cacheFile)
	}
	return nil
}

// writeConfigFile 写入配置文件
func (s *Server) writeConfigFile(yamlData []byte) error {
	return os.WriteFile(s.configPath, yamlData, 0644)
}

// addSourceToConfig 添加源到配置文件
func (s *Server) addSourceToConfig(url string) error {
	s.cfgMutex.Lock()
	defer s.cfgMutex.Unlock()

	cfg, err := config.LoadConfig(s.configPath)
	if err != nil {
		return err
	}

	// Check if already exists
	for _, u := range cfg.AdBlock.RuleURLs {
		if u == url {
			return nil // Already exists
		}
	}

	// Add to list
	cfg.AdBlock.RuleURLs = append(cfg.AdBlock.RuleURLs, url)

	// Save back to file
	yamlData, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	return s.writeConfigFile(yamlData)
}

// removeSourceFromConfig 从配置文件中移除源
func (s *Server) removeSourceFromConfig(url string) error {
	s.cfgMutex.Lock()
	defer s.cfgMutex.Unlock()

	cfg, err := config.LoadConfig(s.configPath)
	if err != nil {
		return err
	}

	// Filter out the URL
	var newURLs []string
	for _, u := range cfg.AdBlock.RuleURLs {
		if u != url {
			newURLs = append(newURLs, u)
		}
	}
	cfg.AdBlock.RuleURLs = newURLs

	// Save back to file
	yamlData, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	return s.writeConfigFile(yamlData)
}

// removeCustomRulesFromConfig 从配置文件中移除自定义规则
func (s *Server) removeCustomRulesFromConfig() error {
	s.cfgMutex.Lock()
	defer s.cfgMutex.Unlock()

	cfg, err := config.LoadConfig(s.configPath)
	if err != nil {
		return err
	}

	// Set CustomRulesFile to empty
	cfg.AdBlock.CustomRulesFile = ""

	// Save back to file
	yamlData, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}

	return s.writeConfigFile(yamlData)
}
