package repository

import (
	"strings"
	"testing"
)

// reverseEncryptor 是一个确定性的假加密器，仅用于测试加解密接线，
// 不依赖真实 AES（真实 AES 已在 aes_encryptor 自身测试覆盖）。
type reverseEncryptor struct{ failDecrypt bool }

func (r reverseEncryptor) Encrypt(plaintext string) (string, error) {
	b := []byte(plaintext)
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}
	return string(b), nil
}

func (r reverseEncryptor) Decrypt(ciphertext string) (string, error) {
	if r.failDecrypt {
		return "", errInvalidForTest
	}
	return r.Encrypt(ciphertext) // 反转的反转 = 原文
}

var errInvalidForTest = &testErr{}

type testErr struct{}

func (*testErr) Error() string { return "decrypt failed" }

func resetCipher() { credentialCipherPtr.Store(nil) }

func TestCredentialCipher_DisabledIsPassthrough(t *testing.T) {
	resetCipher()
	in := map[string]any{"access_token": "secret", "project_id": "p"}
	if got := encryptCredentialsForStorage(in); !sameMap(got, in) {
		t.Fatalf("disabled encrypt should passthrough, got %v", got)
	}
	if got := decryptCredentialsFromStorage(in); !sameMap(got, in) {
		t.Fatalf("disabled decrypt should passthrough, got %v", got)
	}
}

func TestCredentialCipher_RoundTrip(t *testing.T) {
	resetCipher()
	ConfigureCredentialCipher(reverseEncryptor{}, true)
	defer resetCipher()

	in := map[string]any{
		"access_token":  "tok-123",
		"refresh_token": "ref-456",
		"api_key":       "sk-789",
		"project_id":    "my-project", // 非敏感键，不加密
		"model_mapping": map[string]any{"a": "b"}, // 非字符串敏感外键，不动
	}
	enc := encryptCredentialsForStorage(in)

	// 敏感字符串字段应被加密并带前缀；非敏感字段保持原样。
	for _, k := range []string{"access_token", "refresh_token", "api_key"} {
		s, _ := enc[k].(string)
		if !strings.HasPrefix(s, credEncMarker) {
			t.Fatalf("%s not encrypted: %v", k, enc[k])
		}
	}
	if enc["project_id"] != "my-project" {
		t.Fatalf("non-sensitive key should be untouched, got %v", enc["project_id"])
	}
	if _, ok := enc["model_mapping"].(map[string]any); !ok {
		t.Fatalf("non-string sensitive value should be untouched")
	}

	// 解密应还原原文。
	dec := decryptCredentialsFromStorage(enc)
	for k, want := range map[string]string{"access_token": "tok-123", "refresh_token": "ref-456", "api_key": "sk-789"} {
		if dec[k] != want {
			t.Fatalf("decrypt %s = %v, want %v", k, dec[k], want)
		}
	}
}

func TestCredentialCipher_EncryptIdempotent(t *testing.T) {
	resetCipher()
	ConfigureCredentialCipher(reverseEncryptor{}, true)
	defer resetCipher()

	in := map[string]any{"access_token": "tok"}
	once := encryptCredentialsForStorage(in)
	twice := encryptCredentialsForStorage(once)
	if once["access_token"] != twice["access_token"] {
		t.Fatalf("double-encrypt changed value: %v -> %v", once["access_token"], twice["access_token"])
	}
}

func TestCredentialCipher_ReadBackwardCompatPlaintext(t *testing.T) {
	resetCipher()
	ConfigureCredentialCipher(reverseEncryptor{}, true)
	defer resetCipher()

	// 历史明文（无前缀）读取时原样返回。
	plain := map[string]any{"access_token": "legacy-plaintext", "project_id": "p"}
	got := decryptCredentialsFromStorage(plain)
	if got["access_token"] != "legacy-plaintext" {
		t.Fatalf("plaintext should pass through, got %v", got["access_token"])
	}
}

func TestCredentialCipher_WriteOffButReadDecrypts(t *testing.T) {
	resetCipher()
	// 写开关关闭，但 encryptor 在位：历史密文仍应被解密。
	ConfigureCredentialCipher(reverseEncryptor{}, false)
	defer resetCipher()

	if got := encryptCredentialsForStorage(map[string]any{"access_token": "x"}); got["access_token"] != "x" {
		t.Fatalf("write-off should not encrypt, got %v", got["access_token"])
	}
	// 构造一个“已加密”值（reverse of "tok")。
	encVal := credEncMarker + reverseEncryptor{}.mustEnc("tok")
	dec := decryptCredentialsFromStorage(map[string]any{"access_token": encVal})
	if dec["access_token"] != "tok" {
		t.Fatalf("read should still decrypt with write off, got %v", dec["access_token"])
	}
}

func TestCredentialCipher_DecryptFailureDropsKey(t *testing.T) {
	resetCipher()
	ConfigureCredentialCipher(reverseEncryptor{failDecrypt: true}, true)
	defer resetCipher()

	in := map[string]any{"access_token": credEncMarker + "garbage", "project_id": "p"}
	got := decryptCredentialsFromStorage(in)
	if _, exists := got["access_token"]; exists {
		t.Fatalf("undecryptable key must be dropped, not emitted as ciphertext")
	}
	if got["project_id"] != "p" {
		t.Fatalf("non-secret key should remain")
	}
}

func (r reverseEncryptor) mustEnc(s string) string {
	out, _ := r.Encrypt(s)
	return out
}

func sameMap(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			// maps with nested values won't compare with != ; handle only scalars here
			if _, ok := v.(map[string]any); ok {
				continue
			}
			return false
		}
	}
	return true
}
