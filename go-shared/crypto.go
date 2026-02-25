package goshared

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/scrypt"
)

const (
	algorithm  = "aes-256-gcm"
	ivLength   = 16
	saltLength = 16
)

const maxCacheSize = 1000

type scryptCacheKey struct {
	passwordHash [32]byte
	salt         string
}

type scryptCacheEntry struct {
	key     []byte
	addedAt time.Time
}

type scryptCache struct {
	mu    sync.RWMutex
	cache map[scryptCacheKey]scryptCacheEntry
}

var scryptCacheMem = &scryptCache{
	cache: make(map[scryptCacheKey]scryptCacheEntry),
}

func (c *scryptCache) get(key scryptCacheKey) ([]byte, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.cache[key]
	if !ok {
		return nil, false
	}
	return entry.key, true
}

func (c *scryptCache) set(key scryptCacheKey, val []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if len(c.cache) >= maxCacheSize {
		c.evict()
	}

	c.cache[key] = scryptCacheEntry{
		key:     val,
		addedAt: time.Now(),
	}
}

func (c *scryptCache) evict() {
	if len(c.cache) == 0 {
		return
	}

	type entry struct {
		key   scryptCacheKey
		added time.Time
	}

	entries := make([]entry, 0, len(c.cache))
	for k, v := range c.cache {
		entries = append(entries, entry{key: k, added: v.addedAt})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].added.Before(entries[j].added)
	})

	deleteCount := maxCacheSize / 4
	if deleteCount > len(entries) {
		deleteCount = len(entries)
	}

	for i := 0; i < deleteCount; i++ {
		delete(c.cache, entries[i].key)
	}
}

func deriveKeyWithCache(password string, salt []byte) ([]byte, error) {
	cacheKey := scryptCacheKey{
		passwordHash: sha256.Sum256([]byte(password)),
		salt:         hex.EncodeToString(salt),
	}

	if cached, ok := scryptCacheMem.get(cacheKey); ok {
		return cached, nil
	}

	key, err := scrypt.Key([]byte(password), salt, 16384, 8, 1, 32)
	if err != nil {
		return nil, fmt.Errorf("derive key: %w", err)
	}

	scryptCacheMem.set(cacheKey, key)
	return key, nil
}

func deriveKeyNoCache(password string, salt []byte) ([]byte, error) {
	key, err := scrypt.Key([]byte(password), salt, 16384, 8, 1, 32)
	if err != nil {
		return nil, fmt.Errorf("derive key: %w", err)
	}
	return key, nil
}

func Encrypt(plaintext string, password string) (string, error) {
	salt := make([]byte, saltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}

	key, err := deriveKeyNoCache(password, salt)
	if err != nil {
		return "", err
	}

	iv := make([]byte, ivLength)
	if _, err := rand.Read(iv); err != nil {
		return "", fmt.Errorf("generate iv: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("create cipher: %w", err)
	}

	gcm, err := cipher.NewGCMWithNonceSize(block, ivLength)
	if err != nil {
		return "", fmt.Errorf("create gcm: %w", err)
	}

	ciphertextWithTag := gcm.Seal(nil, iv, []byte(plaintext), nil)

	tagStart := len(ciphertextWithTag) - gcm.Overhead()
	ciphertext := ciphertextWithTag[:tagStart]
	tag := ciphertextWithTag[tagStart:]

	return fmt.Sprintf("%s:%s:%s:%s",
		hex.EncodeToString(salt),
		hex.EncodeToString(iv),
		hex.EncodeToString(tag),
		hex.EncodeToString(ciphertext),
	), nil
}

func Decrypt(ciphertext string, password string) (string, error) {
	parts := strings.Split(ciphertext, ":")
	if len(parts) != 4 {
		return "", fmt.Errorf("invalid ciphertext format: expected 4 parts, got %d", len(parts))
	}

	saltHex, ivHex, tagHex, encryptedHex := parts[0], parts[1], parts[2], parts[3]

	if saltHex == "" || ivHex == "" || tagHex == "" || encryptedHex == "" {
		return "", fmt.Errorf("invalid ciphertext format: empty part")
	}

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

	key, err := deriveKeyWithCache(password, salt)
	if err != nil {
		return "", err
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

func getEncryptionKey() (string, error) {
	key := os.Getenv("ENCRYPTION_KEY")
	if len(key) < 32 {
		return "", fmt.Errorf("ENCRYPTION_KEY must be at least 32 characters")
	}
	return key, nil
}

func EncryptWithKey(plaintext string) (string, error) {
	password, err := getEncryptionKey()
	if err != nil {
		return "", err
	}
	return Encrypt(plaintext, password)
}

func DecryptWithKey(ciphertext string) (string, error) {
	password, err := getEncryptionKey()
	if err != nil {
		return "", err
	}
	return Decrypt(ciphertext, password)
}
