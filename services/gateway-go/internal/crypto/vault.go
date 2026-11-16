package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"

	"golang.org/x/crypto/hkdf"
)

// Vault provides HKDF-derived, AES-256-GCM authenticated encryption for Coach BYOK LLM API keys.
// Keys are cryptographically isolated per (coachID, provider) pair using HKDF and authenticated using AAD.
type Vault struct {
	masterKey []byte
}

// NewVault initializes a Vault from a 64-character hex string (32 bytes for AES-256).
func NewVault(hexKey string) (*Vault, error) {
	cleaned := strings.TrimSpace(hexKey)
	if cleaned == "" {
		return nil, errors.New("master encryption key is required (64 hex chars); there is no default")
	}
	keyBytes, err := hex.DecodeString(cleaned)
	if err != nil {
		return nil, fmt.Errorf("invalid master encryption key hex: %w", err)
	}
	if len(keyBytes) != 32 {
		return nil, fmt.Errorf("master encryption key must be 32 bytes (64 hex chars), got %d bytes", len(keyBytes))
	}
	return &Vault{masterKey: keyBytes}, nil
}

// deriveSubkey expands the 32-byte master key into a coach-and-provider-isolated 32-byte AES key
// using HKDF (RFC 5869) with SHA-256.
//
// salt = []byte(coachID)
// info = []byte(provider)
func (v *Vault) deriveSubkey(coachID, provider string) ([]byte, error) {
	if coachID == "" || provider == "" {
		return nil, errors.New("coachID and provider cannot be empty for key derivation")
	}

	// 1. Initialize HKDF reader:
	reader := hkdf.New(sha256.New, v.masterKey, []byte(coachID), []byte(provider))
	// 2. Allocate 32 bytes:
	subkey := make([]byte, 32)
	// 3. Read full 32 bytes into subkey:
	if _, err := io.ReadFull(reader, subkey); err != nil {
		return nil, fmt.Errorf("hkdf subkey derivation failed: %w", err)
	}
	// 4. Return subkey, nil
	// =========================================================================

	return subkey, nil
}

// Encrypt encrypts a plaintext API key for a specific (coachID, provider) using an HKDF subkey
// and AES-256-GCM with Additional Authenticated Data (AAD = "coachID:provider").
func (v *Vault) Encrypt(coachID, provider, plaintext string) (ciphertext []byte, nonce []byte, err error) {
	if plaintext == "" {
		return nil, nil, errors.New("cannot encrypt empty API key")
	}

	// =========================================================================
	// TODO(engineer) 2.1b: AES-256-GCM Encryption with HKDF Subkey + AAD
	//
	// Instructions:
	// 1. Derive subkey: `subkey, err := v.deriveSubkey(coachID, provider)`
	// 2. Create AES cipher: `block, err := aes.NewCipher(subkey)`
	// 3. Create GCM: `gcm, err := cipher.NewGCM(block)`
	// 4. Generate random nonce:
	//    `nonce = make([]byte, gcm.NonceSize())`
	//    `io.ReadFull(rand.Reader, nonce)`
	// 5. Build AAD: `aad := []byte(coachID + ":" + provider)`
	// 6. Seal with AAD: `ciphertext = gcm.Seal(nil, nonce, []byte(plaintext), aad)`
	// =========================================================================

	subkey, err := v.deriveSubkey(coachID, provider)
	if err != nil {
		return nil, nil, err
	}

	block, err := aes.NewCipher(subkey)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create GCM mode: %w", err)
	}

	nonce = make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, fmt.Errorf("failed to generate random nonce: %w", err)
	}

	aad := []byte(coachID + ":" + provider)
	ciphertext = gcm.Seal(nil, nonce, []byte(plaintext), aad)
	return ciphertext, nonce, nil
}

// Decrypt decrypts an AES-256-GCM encrypted API key and verifies that the ciphertext
// was specifically minted for this (coachID, provider) pair via AAD authentication.
func (v *Vault) Decrypt(coachID, provider string, ciphertext []byte, nonce []byte) (string, error) {
	// =========================================================================
	// TODO(engineer) 2.1c: AES-256-GCM Decryption with HKDF Subkey + AAD Verification
	//
	// Instructions:
	// 1. Derive subkey: `subkey, err := v.deriveSubkey(coachID, provider)`
	// 2. Create AES cipher: `block, err := aes.NewCipher(subkey)`
	// 3. Create GCM: `gcm, err := cipher.NewGCM(block)`
	// 4. Verify nonce length: `if len(nonce) != gcm.NonceSize() { ... }`
	// 5. Build AAD: `aad := []byte(coachID + ":" + provider)`
	// 6. Open with AAD: `plaintext, err := gcm.Open(nil, nonce, ciphertext, aad)`
	// =========================================================================

	subkey, err := v.deriveSubkey(coachID, provider)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(subkey)
	if err != nil {
		return "", fmt.Errorf("failed to create AES cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM mode: %w", err)
	}
	if len(nonce) != gcm.NonceSize() {
		return "", fmt.Errorf("invalid nonce length: got %d, want %d", len(nonce), gcm.NonceSize())
	}

	aad := []byte(coachID + ":" + provider)
	plaintext, err := gcm.Open(nil, nonce, ciphertext, aad)
	if err != nil {
		return "", fmt.Errorf("ciphertext authentication or decryption failed: %w", err)
	}
	return string(plaintext), nil
}

// MaskKey generates a safe UI hint for an API key (e.g., "AIza...9f2B" or "sk-...8a1c").
func MaskKey(rawKey string) string {
	trimmed := strings.TrimSpace(rawKey)
	n := len(trimmed)
	if n <= 8 {
		return "••••"
	}
	prefix := trimmed[:4]
	suffix := trimmed[n-4:]
	return fmt.Sprintf("%s••••%s", prefix, suffix)
}
