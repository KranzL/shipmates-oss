package testgen

import (
	"crypto/rand"
	"encoding/base32"
	"strings"
	"time"
)

var encoding = base32.NewEncoding("0123456789abcdefghjkmnpqrstvwxyz").WithPadding(base32.NoPadding)

func GenerateID() string {
	timestamp := time.Now().UnixMilli()

	randomBytes := make([]byte, 10)
	if _, err := rand.Read(randomBytes); err != nil {
		panic(err)
	}

	timestampBytes := make([]byte, 8)
	for i := 0; i < 8; i++ {
		timestampBytes[i] = byte(timestamp >> (56 - i*8))
	}

	combined := append(timestampBytes, randomBytes...)
	encoded := encoding.EncodeToString(combined)

	return strings.ToLower(encoded[:25])
}
