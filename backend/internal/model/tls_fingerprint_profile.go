// Package model 定义服务层使用的数据模型。
package model

import (
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
)

// TLSFingerprintProfile TLS 指纹配置模板
// 包含完整的 ClientHello 参数，用于模拟特定客户端的 TLS 握手特征
type TLSFingerprintProfile struct {
	ID                  int64     `json:"id"`
	Name                string    `json:"name"`
	Description         *string   `json:"description"`
	EnableGREASE        bool      `json:"enable_grease"`
	CipherSuites        []uint16  `json:"cipher_suites"`
	Curves              []uint16  `json:"curves"`
	PointFormats        []uint16  `json:"point_formats"`
	SignatureAlgorithms []uint16  `json:"signature_algorithms"`
	ALPNProtocols       []string  `json:"alpn_protocols"`
	SupportedVersions   []uint16  `json:"supported_versions"`
	KeyShareGroups      []uint16  `json:"key_share_groups"`
	PSKModes            []uint16  `json:"psk_modes"`
	Extensions          []uint16  `json:"extensions"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// TLS 扩展类型 ID（仅列出一致性校验需要引用的几个）。
const (
	tlsExtSupportedGroups      uint16 = 10 // supported_groups（曲线）
	tlsExtSignatureAlgorithms  uint16 = 13 // signature_algorithms
	tlsExtALPN                 uint16 = 16 // application_layer_protocol_negotiation
	tlsExtSupportedVersions    uint16 = 43 // supported_versions
	tlsExtKeyShare             uint16 = 51 // key_share
	tlsVersionTLS13            uint16 = 0x0304
)

// Validate 验证模板配置的有效性（基础校验 + 自洽性校验）。
func (p *TLSFingerprintProfile) Validate() error {
	if p.Name == "" {
		return &ValidationError{Field: "name", Message: "name is required"}
	}
	return p.ValidateConsistency()
}

// ValidateConsistency 校验各字段之间是否自洽。
//
// 指纹伪装最容易翻车的不是单层不像，而是各层互相矛盾——一个自相矛盾的 ClientHello
// 本身就是一个“假客户端”指纹。这里强制几条真实 TLS 客户端必然满足的不变量，
// 避免管理员手填 / 粘贴 YAML 时产出一个永远不可能由真实客户端发出的组合。
func (p *TLSFingerprintProfile) ValidateConsistency() error {
	// 1) key_share_groups 必须是 curves(supported_groups) 的子集。
	//    真实客户端绝不会为一个没在 supported_groups 里宣告的组发送 key_share。
	if len(p.KeyShareGroups) > 0 && len(p.Curves) > 0 {
		curveSet := make(map[uint16]struct{}, len(p.Curves))
		for _, c := range p.Curves {
			curveSet[c] = struct{}{}
		}
		for _, g := range p.KeyShareGroups {
			if _, ok := curveSet[g]; !ok {
				return &ValidationError{
					Field:   "key_share_groups",
					Message: "key_share group must also appear in curves (supported_groups)",
				}
			}
		}
	}

	// 2) cipher_suites / extensions 不应出现重复项（重复本身就是非真实客户端的特征）。
	if dup, ok := firstDuplicateUint16(p.CipherSuites); ok {
		return &ValidationError{Field: "cipher_suites", Message: "duplicate cipher suite: " + hexU16(dup)}
	}
	if dup, ok := firstDuplicateUint16(p.Extensions); ok {
		return &ValidationError{Field: "extensions", Message: "duplicate extension id: " + hexU16(dup)}
	}

	// 3) 若显式指定了扩展顺序，则被实际填充数据的扩展必须出现在顺序里，
	//    否则该字段配置了却不会被发送，前后矛盾。
	if len(p.Extensions) > 0 {
		extSet := make(map[uint16]struct{}, len(p.Extensions))
		for _, e := range p.Extensions {
			extSet[e] = struct{}{}
		}
		requireExt := func(present bool, extID uint16, field string) error {
			if !present {
				return nil
			}
			if _, ok := extSet[extID]; !ok {
				return &ValidationError{
					Field:   "extensions",
					Message: field + " is set but its extension (" + hexU16(extID) + ") is missing from extensions order",
				}
			}
			return nil
		}
		if err := requireExt(len(p.Curves) > 0, tlsExtSupportedGroups, "curves"); err != nil {
			return err
		}
		if err := requireExt(len(p.SignatureAlgorithms) > 0, tlsExtSignatureAlgorithms, "signature_algorithms"); err != nil {
			return err
		}
		if err := requireExt(len(p.ALPNProtocols) > 0, tlsExtALPN, "alpn_protocols"); err != nil {
			return err
		}
		if err := requireExt(len(p.KeyShareGroups) > 0, tlsExtKeyShare, "key_share_groups"); err != nil {
			return err
		}
		if err := requireExt(containsUint16(p.SupportedVersions, tlsVersionTLS13), tlsExtSupportedVersions, "supported_versions(TLS1.3)"); err != nil {
			return err
		}
	}

	// 4) ALPN 协议名不能为空 / 超长（TLS 单个协议名为 1 字节长度前缀，最长 255）。
	for _, proto := range p.ALPNProtocols {
		if proto == "" {
			return &ValidationError{Field: "alpn_protocols", Message: "alpn protocol must not be empty"}
		}
		if len(proto) > 255 {
			return &ValidationError{Field: "alpn_protocols", Message: "alpn protocol too long (max 255 bytes)"}
		}
	}

	return nil
}

func firstDuplicateUint16(vals []uint16) (uint16, bool) {
	if len(vals) < 2 {
		return 0, false
	}
	seen := make(map[uint16]struct{}, len(vals))
	for _, v := range vals {
		if _, ok := seen[v]; ok {
			return v, true
		}
		seen[v] = struct{}{}
	}
	return 0, false
}

func containsUint16(vals []uint16, target uint16) bool {
	for _, v := range vals {
		if v == target {
			return true
		}
	}
	return false
}

func hexU16(v uint16) string {
	const digits = "0123456789abcdef"
	return "0x" + string([]byte{digits[(v>>12)&0xf], digits[(v>>8)&0xf], digits[(v>>4)&0xf], digits[v&0xf]})
}

// ToTLSProfile 将领域模型转换为运行时使用的 tlsfingerprint.Profile
// 空切片字段会在 dialer 中 fallback 到内置默认值
func (p *TLSFingerprintProfile) ToTLSProfile() *tlsfingerprint.Profile {
	return &tlsfingerprint.Profile{
		Name:                p.Name,
		EnableGREASE:        p.EnableGREASE,
		CipherSuites:        p.CipherSuites,
		Curves:              p.Curves,
		PointFormats:        p.PointFormats,
		SignatureAlgorithms: p.SignatureAlgorithms,
		ALPNProtocols:       p.ALPNProtocols,
		SupportedVersions:   p.SupportedVersions,
		KeyShareGroups:      p.KeyShareGroups,
		PSKModes:            p.PSKModes,
		Extensions:          p.Extensions,
	}
}
