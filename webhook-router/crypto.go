package main

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"

	"golang.org/x/crypto/scrypt"
)

type scryptCacheKey struct {
	password string
	salt     string
}

var scryptCache sync.Map

func decryptAES256GCM(ciphertext string, password string) (string, error) {
	parts := strings.Split(ciphertext, ":")
	if len(parts) != 4 {
		return "", fmt.Errorf("invalid ciphertext format: expected 4 parts, got %d", len(parts))
	}

	saltHex, ivHex, tagHex, encryptedHex := parts[0], parts[1], parts[2], parts[3]

	salt, err := hex.DecodeString(saltHex)
	if err != nil {
		return "", fmt.Errorf("decode salt: %w", err)
	}

	iv, err := hex.DecodeString(ivHex)
	if err != nil {
		return "", fmt.Errorf("decode iv: %w", err)
	}

	tag, err := hex.DecodeString(tagHex)
	if err != nil {
		return "", fmt.Errorf("decode tag: %w", err)
	}

	encrypted, err := hex.DecodeString(encryptedHex)
	if err != nil {
		return "", fmt.Errorf("decode encrypted: %w", err)
	}

	cacheKey := scryptCacheKey{
		password: password,
		salt:     hex.EncodeToString(salt),
	}

	var key []byte
	if cached, ok := scryptCache.Load(cacheKey); ok {
		key = cached.([]byte)
	} else {
		derived, err := scrypt.Key([]byte(password), salt, 1<<14, 8, 1, 32)
		if err != nil {
			return "", fmt.Errorf("derive key: %w", err)
		}
		key = derived
		scryptCache.Store(cacheKey, key)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCMWithNonceSize(block, len(iv))
	if err != nil {
		return "", fmt.Errorf("create gcm: %w", err)
	}

	ciphertextWithTag := append(encrypted, tag...)
	plaintext, err := gcm.Open(nil, iv, ciphertextWithTag, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}

	return string(plaintext), nil
}
