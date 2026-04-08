package webapi

import (
	"golang.org/x/crypto/bcrypt"
)

// HashPassword 使用 bcrypt 算法对密码进行哈希
// 成本参数设为 14，在安全性和性能之间取得平衡
func HashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), 14)
	return string(bytes), err
}

// CheckPasswordHash 验证密码与哈希是否匹配
func CheckPasswordHash(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}
