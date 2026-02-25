package goshared_test

import (
	"testing"

	goshared "github.com/KranzL/shipmates-oss/go-shared"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	password := "test-encryption-key-32-chars-min!"
	plaintext := "sk-ant-api03-secret-key-value"

	encrypted, err := goshared.Encrypt(plaintext, password)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	if encrypted == plaintext {
		t.Fatal("encrypted text should differ from plaintext")
	}

	decrypted, err := goshared.Decrypt(encrypted, password)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}

	if decrypted != plaintext {
		t.Fatalf("expected '%s', got '%s'", plaintext, decrypted)
	}
}

func TestDecryptWithWrongKey(t *testing.T) {
	password := "correct-key-that-is-long-enough!!"
	wrongPassword := "wrong-key-that-is-also-long-enuf!!"

	encrypted, err := goshared.Encrypt("secret data", password)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}

	_, err = goshared.Decrypt(encrypted, wrongPassword)
	if err == nil {
		t.Fatal("expected error when decrypting with wrong key, got nil")
	}
}

func TestEncryptDifferentCiphertexts(t *testing.T) {
	password := "test-encryption-key-32-chars-min!"
	plaintext := "same input text"

	enc1, _ := goshared.Encrypt(plaintext, password)
	enc2, _ := goshared.Encrypt(plaintext, password)

	if enc1 == enc2 {
		t.Fatal("encrypting same plaintext twice should produce different ciphertexts")
	}
}

func TestHashPassword(t *testing.T) {
	password := "SecurePassword123!"

	hash, err := goshared.HashPassword(password)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	if hash == password {
		t.Fatal("hash should differ from password")
	}
}
