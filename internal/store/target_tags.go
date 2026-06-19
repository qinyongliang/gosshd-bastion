package store

import (
	"crypto/rand"
	"errors"
	"hash/fnv"
	"math/big"
	"strings"
)

var targetTagColors = []string{"gray", "red", "orange", "yellow", "green", "blue", "purple"}

func normalizeTargetTagColor(color string) (string, error) {
	color = strings.ToLower(strings.TrimSpace(color))
	for _, allowed := range targetTagColors {
		if color == allowed {
			return color, nil
		}
	}
	return "", errors.New("target tag color is not allowed")
}

func randomTargetTagColor(name string) string {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(len(targetTagColors))))
	if err == nil {
		return targetTagColors[int(n.Int64())]
	}
	return fallbackTargetTagColor(name)
}

func fallbackTargetTagColor(name string) string {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(strings.TrimSpace(name)))
	return targetTagColors[int(hash.Sum32())%len(targetTagColors)]
}
