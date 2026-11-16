package crypto

import (
	"bytes"
	"testing"
)

func TestVaultEncryptDecrypt(t *testing.T) {
	vault, err := NewVault("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("failed to initialize vault: %v", err)
	}

	coachA := "coach-1111-aaaa"
	providerGemini := "gemini"
	rawKey := "AIzaSyD-CoachBuddySecretTestKey9876"

	ct1, nonce1, err := vault.Encrypt(coachA, providerGemini, rawKey)
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}
	ct2, nonce2, err := vault.Encrypt(coachA, providerGemini, rawKey)
	if err != nil {
		t.Fatalf("second Encrypt failed: %v", err)
	}

	// 1. Random nonces and distinct ciphertexts
	if bytes.Equal(nonce1, nonce2) {
		t.Fatal("expected unique random nonces across encryptions")
	}
	if bytes.Equal(ct1, ct2) {
		t.Fatal("expected distinct ciphertexts for identical plaintext due to random nonce")
	}

	// 2. Legitimate decryption
	decrypted, err := vault.Decrypt(coachA, providerGemini, ct1, nonce1)
	if err != nil {
		t.Fatalf("Decrypt failed: %v", err)
	}
	if decrypted != rawKey {
		t.Fatalf("expected %q, got %q", rawKey, decrypted)
	}

	// 3. Cross-coach isolation: Coach B MUST NOT decrypt Coach A's ciphertext
	coachB := "coach-2222-bbbb"
	if _, err := vault.Decrypt(coachB, providerGemini, ct1, nonce1); err == nil {
		t.Fatal("SECURITY VIOLATION: Coach B successfully decrypted Coach A's ciphertext!")
	}

	// 4. Cross-provider isolation: Gemini ciphertext MUST NOT decrypt with OpenAI provider
	providerOpenAI := "openai"
	if _, err := vault.Decrypt(coachA, providerOpenAI, ct1, nonce1); err == nil {
		t.Fatal("SECURITY VIOLATION: Decrypted Gemini key using OpenAI provider context!")
	}

	// 5. Tamper check: GCM MAC verification failure
	tampered := make([]byte, len(ct1))
	copy(tampered, ct1)
	tampered[0] ^= 0xFF
	if _, err := vault.Decrypt(coachA, providerGemini, tampered, nonce1); err == nil {
		t.Fatal("expected tampered ciphertext to fail GCM authentication")
	}
}

func TestMaskKey(t *testing.T) {
	masked := MaskKey("sk-proj-1234567890abcdef9x8Z")
	if masked != "sk-p••••9x8Z" {
		t.Fatalf("unexpected mask: %s", masked)
	}

	short := MaskKey("12345")
	if short != "••••" {
		t.Fatalf("expected short key to mask completely, got %s", short)
	}
}

func TestNewVault_RequiresKey(t *testing.T) {
	for _, k := range []string{"", "   "} {
		if _, err := NewVault(k); err == nil {
			t.Errorf("expected an error for empty master key %q", k)
		}
	}
}
