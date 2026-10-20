package auth

import (
	"errors"
	"golang.org/x/crypto/bcrypt"
)

func Hash(password string) (string, error) {
	if len(password) < 8 || len(password) > 72 {
		return "", errors.New("密码必须为 8 至 72 字节")
	}
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(b), err
}
func Verify(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}
