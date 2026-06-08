package repository

import (
	"log/slog"
	"strings"
	"sync/atomic"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// 账号上游凭证（access_token / refresh_token / api_key / service_account_json 等）默认
// 以明文存储在 accounts.credentials(JSONB) 里。本文件提供一层可选的字段级静态加密，
// 复用项目已有的 AES-256-GCM SecretEncryptor（与 TOTP / 渠道监控同一把密钥）。
//
// 设计目标（务必保持）：
//   - 默认关闭：未配置时所有读写都是原样透传，行为与现状完全一致，合并即生产安全。
//   - 读向后兼容：只有带 credEncMarker 前缀的值才会被解密；明文值原样返回。
//     因此即便先加密再关闭写开关，历史密文仍能正常解密。
//   - 字段级、键级幂等：只加密 service.IsSensitiveCredentialKey 命中的 string 值，
//     非敏感键、非字符串值、已加密值都不动；保持 JSONB 结构不变，
//     兼容 `credentials || $jsonb` 的服务端部分合并。
//   - 失败安全：解密失败（多为换错密钥）时丢弃该键并告警，绝不把密文当 token 外发。
const credEncMarker = "enc:v1:"

type credentialCipher struct {
	enc            service.SecretEncryptor
	encryptOnWrite bool
}

// 包级单例：nil 表示未配置加密能力（读写均透传）。
// 凭证 ent→service 的转换出口（accountEntityToService）分布在本包多个文件中，
// 用包级单例避免把 cipher 透传到每一个调用点。
var credentialCipherPtr atomic.Pointer[credentialCipher]

// ConfigureCredentialCipher 在启动期配置账号凭证加密能力。
// encryptOnWrite 为 true 时，写路径会加密敏感字段；无论该开关如何，
// 只要 enc 非空，读路径都会尝试解密历史密文。
func ConfigureCredentialCipher(enc service.SecretEncryptor, encryptOnWrite bool) {
	if enc == nil {
		credentialCipherPtr.Store(nil)
		return
	}
	credentialCipherPtr.Store(&credentialCipher{enc: enc, encryptOnWrite: encryptOnWrite})
	slog.Info("account_credentials_cipher_configured", "encrypt_on_write", encryptOnWrite)
}

func loadCredentialCipher() *credentialCipher {
	return credentialCipherPtr.Load()
}

// encryptCredentialsForStorage 在写入 DB 前加密敏感字段。
// 返回一个新 map（不修改入参）；未启用写加密时原样返回入参。
func encryptCredentialsForStorage(creds map[string]any) map[string]any {
	c := loadCredentialCipher()
	if c == nil || !c.encryptOnWrite || len(creds) == 0 {
		return creds
	}
	out := make(map[string]any, len(creds))
	for k, v := range creds {
		out[k] = v
		if !service.IsSensitiveCredentialKey(k) {
			continue
		}
		s, ok := v.(string)
		if !ok || s == "" || strings.HasPrefix(s, credEncMarker) {
			continue // 非字符串 / 空 / 已加密 → 不动
		}
		ciphertext, err := c.enc.Encrypt(s)
		if err != nil {
			// 加密失败时保持明文写入，宁可不加密也不要丢数据；同时告警。
			slog.Error("account_credentials_encrypt_failed", "key", k, "error", err)
			continue
		}
		out[k] = credEncMarker + ciphertext
	}
	return out
}

// decryptCredentialsFromStorage 在从 DB 读出后解密带标记的字段。
// 返回一个新 map（不修改入参）；无 cipher 时原样返回。
func decryptCredentialsFromStorage(creds map[string]any) map[string]any {
	c := loadCredentialCipher()
	if c == nil || len(creds) == 0 {
		return creds
	}
	var out map[string]any
	for k, v := range creds {
		s, ok := v.(string)
		if !ok || !strings.HasPrefix(s, credEncMarker) {
			continue // 明文 / 非字符串 → 走下面的浅拷贝兜底
		}
		if out == nil {
			out = make(map[string]any, len(creds))
			for kk, vv := range creds {
				out[kk] = vv
			}
		}
		plaintext, err := c.enc.Decrypt(strings.TrimPrefix(s, credEncMarker))
		if err != nil {
			// 解密失败（多半是换错密钥）：删除该键，绝不把密文当凭证外发。
			slog.Error("account_credentials_decrypt_failed", "key", k, "error", err)
			delete(out, k)
			continue
		}
		out[k] = plaintext
	}
	if out == nil {
		return creds // 没有任何密文字段，零拷贝返回
	}
	return out
}
