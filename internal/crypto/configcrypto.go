// Package crypto provides modern (2026) encryption for ElectruVPN configuration
// payloads: AES-256-GCM with Argon2id password-based key derivation, and
// master-key encryption for at-rest secrets.
//
// Standard share links (vless://, trojan://, …) must carry the client
// credential so apps can import them. Confidential bulk configs use
// password-encrypted envelopes; secrets at rest use the panel master key.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/hkdf"
)

const (
	EnvelopeVersion = 1
	argonTime       = 3
	argonMemory     = 64 * 1024 // 64 MiB
	argonThreads    = 4
	argonKeyLen     = 32
	gcmNonceSize    = 12
)

// EncryptedEnvelope is a self-contained password-protected config blob.
type EncryptedEnvelope struct {
	Version    int    `json:"v"`
	Algorithm  string `json:"alg"`
	Salt       string `json:"salt"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ct"`
	CreatedAt  int64  `json:"ts"`
	Label      string `json:"label,omitempty"`
}

// EncryptWithPassword protects plaintext with Argon2id → AES-256-GCM.
func EncryptWithPassword(password string, plaintext []byte, label string) ([]byte, error) {
	password = strings.TrimSpace(password)
	if len(password) < 8 {
		return nil, errors.New("password must be at least 8 characters")
	}
	if len(plaintext) == 0 {
		return nil, errors.New("empty plaintext")
	}
	salt := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, salt); err != nil {
		return nil, fmt.Errorf("salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("nonce: %w", err)
	}
	ct := gcm.Seal(nil, nonce, plaintext, nil)
	env := EncryptedEnvelope{
		Version:    EnvelopeVersion,
		Algorithm:  "argon2id-aes-256-gcm",
		Salt:       base64.RawStdEncoding.EncodeToString(salt),
		Nonce:      base64.RawStdEncoding.EncodeToString(nonce),
		Ciphertext: base64.RawStdEncoding.EncodeToString(ct),
		CreatedAt:  time.Now().Unix(),
		Label:      label,
	}
	return json.Marshal(env)
}

// DecryptWithPassword reverses EncryptWithPassword.
func DecryptWithPassword(password string, envelopeJSON []byte) ([]byte, error) {
	var env EncryptedEnvelope
	if err := json.Unmarshal(envelopeJSON, &env); err != nil {
		return nil, fmt.Errorf("invalid envelope: %w", err)
	}
	if env.Version != EnvelopeVersion || env.Algorithm != "argon2id-aes-256-gcm" {
		return nil, errors.New("unsupported envelope version or algorithm")
	}
	salt, err := base64.RawStdEncoding.DecodeString(env.Salt)
	if err != nil || len(salt) < 8 {
		return nil, errors.New("invalid salt")
	}
	nonce, err := base64.RawStdEncoding.DecodeString(env.Nonce)
	if err != nil {
		return nil, errors.New("invalid nonce")
	}
	ct, err := base64.RawStdEncoding.DecodeString(env.Ciphertext)
	if err != nil {
		return nil, errors.New("invalid ciphertext")
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, errors.New("decryption failed (wrong password or corrupted data)")
	}
	return pt, nil
}

// MasterKeyFromEnv returns a 32-byte key or nil if unset.
func MasterKeyFromEnv(raw string) []byte {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if b, err := decodeHex(raw); err == nil && len(b) == 32 {
		return b
	}
	sum := sha256.Sum256([]byte(raw))
	return sum[:]
}

func decodeHex(s string) ([]byte, error) {
	if len(s)%2 != 0 {
		return nil, errors.New("odd hex")
	}
	out := make([]byte, len(s)/2)
	for i := 0; i < len(out); i++ {
		var v byte
		for j := 0; j < 2; j++ {
			c := s[i*2+j]
			var n byte
			switch {
			case c >= '0' && c <= '9':
				n = c - '0'
			case c >= 'a' && c <= 'f':
				n = c - 'a' + 10
			case c >= 'A' && c <= 'F':
				n = c - 'A' + 10
			default:
				return nil, errors.New("bad hex")
			}
			v = v<<4 | n
		}
		out[i] = v
	}
	return out, nil
}

// EncryptWithMaster encrypts small secrets for at-rest storage (ev1:…).
func EncryptWithMaster(master []byte, plaintext string) (string, error) {
	if len(master) != 32 {
		return plaintext, nil
	}
	if plaintext == "" {
		return "", nil
	}
	if strings.HasPrefix(plaintext, "ev1:") {
		return plaintext, nil
	}
	block, err := aes.NewCipher(master)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcmNonceSize)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ct := gcm.Seal(nil, nonce, []byte(plaintext), nil)
	blob := append(nonce, ct...)
	return "ev1:" + base64.RawStdEncoding.EncodeToString(blob), nil
}

// DecryptWithMaster reverses EncryptWithMaster.
func DecryptWithMaster(master []byte, value string) (string, error) {
	if value == "" || !strings.HasPrefix(value, "ev1:") {
		return value, nil
	}
	if len(master) != 32 {
		return "", errors.New("encrypted value present but ELECTRU_CONFIG_ENCRYPTION_KEY is not set")
	}
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(value, "ev1:"))
	if err != nil || len(raw) < gcmNonceSize+16 {
		return "", errors.New("corrupt encrypted value")
	}
	nonce, ct := raw[:gcmNonceSize], raw[gcmNonceSize:]
	block, err := aes.NewCipher(master)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", errors.New("master-key decryption failed")
	}
	return string(pt), nil
}

// SignPayload HMAC-SHA256 over data using HKDF-derived key from master.
func SignPayload(master []byte, data []byte) string {
	if len(master) == 0 {
		return ""
	}
	h := hkdf.New(sha256.New, master, nil, []byte("electruvpn-sub-sign-v1"))
	key := make([]byte, 32)
	_, _ = io.ReadFull(h, key)
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write(data)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// RandomToken returns a URL-safe high-entropy token.
func RandomToken(n int) (string, error) {
	if n < 16 {
		n = 16
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// PackTimestamped prefixes payload with a big-endian unix timestamp.
func PackTimestamped(ts int64, payload []byte) []byte {
	buf := make([]byte, 8+len(payload))
	binary.BigEndian.PutUint64(buf[:8], uint64(ts))
	copy(buf[8:], payload)
	return buf
}
