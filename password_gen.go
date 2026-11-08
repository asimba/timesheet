package main

import (
	"crypto/rand"
	"math/big"
)

// generatePassword генерирует случайный пароль заданной длины.
// Использует криптографически стойкий генератор случайных чисел.
func generatePassword(length int) (string, error) {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	// Ограничиваем длину разумными пределами
	if length < 4 {
		length = 4
	}
	if length > 100 {
		length = 100
	}

	result := make([]byte, length)
	for i := range result {
		num, err := rand.Int(rand.Reader, big.NewInt(int64(len(charset))))
		if err != nil {
			return "", err
		}
		result[i] = charset[num.Int64()]
	}
	return string(result), nil
}