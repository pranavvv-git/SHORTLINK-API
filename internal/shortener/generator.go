package shortener

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

const alphabet = "23456789abcdefghijkmnopqrstuvwxyz"

type Generator struct{}

func (Generator) Generate(length int) (string, error) {
	if length <= 0 {
		return "", fmt.Errorf("length must be positive")
	}

	result := make([]byte, length)
	limit := big.NewInt(int64(len(alphabet)))
	for i := range result {
		index, err := rand.Int(rand.Reader, limit)
		if err != nil {
			return "", fmt.Errorf("generate random code: %w", err)
		}
		result[i] = alphabet[index.Int64()]
	}

	return string(result), nil
}
