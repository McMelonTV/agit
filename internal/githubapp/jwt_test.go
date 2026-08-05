package githubapp

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestGenerateJWT(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_700_000_000, 0).UTC()
	token, err := GenerateJWT("12345", key, now)
	if err != nil {
		t.Fatal(err)
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("got %d JWT parts, want 3", len(parts))
	}

	claimsJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	var claims map[string]any
	if err := json.Unmarshal(claimsJSON, &claims); err != nil {
		t.Fatal(err)
	}
	if got := claims["iss"]; got != "12345" {
		t.Fatalf("iss = %v, want 12345", got)
	}
	if got := int64(claims["iat"].(float64)); got != now.Add(-time.Minute).Unix() {
		t.Fatalf("iat = %d", got)
	}
	if got := int64(claims["exp"].(float64)); got != now.Add(9*time.Minute).Unix() {
		t.Fatalf("exp = %d", got)
	}

	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, digest[:], signature); err != nil {
		t.Fatalf("signature verification failed: %v", err)
	}
}

func TestGenerateJWTRejectsMissingInputs(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := GenerateJWT("", key, time.Now()); err == nil {
		t.Fatal("expected empty App ID error")
	}
	if _, err := GenerateJWT("123", nil, time.Now()); err == nil {
		t.Fatal("expected nil key error")
	}
}
