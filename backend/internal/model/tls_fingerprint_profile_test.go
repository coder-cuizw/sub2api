package model

import "testing"

// realisticProfile 返回一个自洽的、贴近真实 Node.js 客户端的 profile，
// 各用例在其基础上单独破坏某一处来验证对应的一致性校验。
func realisticProfile() *TLSFingerprintProfile {
	return &TLSFingerprintProfile{
		Name:                "Claude Code Linux x64 (Node.js v22.22.2)",
		CipherSuites:        []uint16{4866, 4867, 4865, 49199, 49195},
		Curves:              []uint16{4588, 29, 23, 30, 24, 25, 256, 257},
		PointFormats:        []uint16{0, 1, 2},
		SignatureAlgorithms: []uint16{0x0403, 0x0804},
		ALPNProtocols:       []string{"http/1.1"},
		SupportedVersions:   []uint16{0x0304, 0x0303},
		KeyShareGroups:      []uint16{4588, 29},
		PSKModes:            []uint16{1},
		Extensions:          []uint16{65281, 0, 11, 10, 35, 16, 22, 23, 13, 43, 45, 51},
	}
}

func TestValidate_RealisticProfilePasses(t *testing.T) {
	if err := realisticProfile().Validate(); err != nil {
		t.Fatalf("expected realistic profile to pass, got: %v", err)
	}
}

func TestValidate_EmptyNameFails(t *testing.T) {
	p := realisticProfile()
	p.Name = ""
	if err := p.Validate(); err == nil {
		t.Fatal("expected empty name to fail")
	}
}

func TestValidateConsistency(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(p *TLSFingerprintProfile)
		wantErr bool
	}{
		{
			name:   "valid",
			mutate: func(p *TLSFingerprintProfile) {},
		},
		{
			name:    "key_share not in curves",
			mutate:  func(p *TLSFingerprintProfile) { p.KeyShareGroups = []uint16{4588, 999} },
			wantErr: true,
		},
		{
			name:   "key_share subset ok",
			mutate: func(p *TLSFingerprintProfile) { p.KeyShareGroups = []uint16{29} },
		},
		{
			name:    "duplicate cipher suite",
			mutate:  func(p *TLSFingerprintProfile) { p.CipherSuites = []uint16{4865, 4865} },
			wantErr: true,
		},
		{
			name:    "duplicate extension",
			mutate:  func(p *TLSFingerprintProfile) { p.Extensions = append(p.Extensions, 13) },
			wantErr: true,
		},
		{
			name:    "curves set but ext 10 missing",
			mutate:  func(p *TLSFingerprintProfile) { p.Extensions = []uint16{65281, 0, 16, 13, 43, 51} },
			wantErr: true,
		},
		{
			name:    "alpn set but ext 16 missing",
			mutate:  func(p *TLSFingerprintProfile) { p.Extensions = []uint16{65281, 0, 10, 13, 43, 51} },
			wantErr: true,
		},
		{
			name:    "tls13 set but ext 43 missing",
			mutate:  func(p *TLSFingerprintProfile) { p.Extensions = []uint16{65281, 0, 10, 16, 13, 51} },
			wantErr: true,
		},
		{
			name: "no explicit extensions skips order checks",
			mutate: func(p *TLSFingerprintProfile) {
				p.Extensions = nil // 留空 = 用内置默认顺序，跳过顺序自洽检查
			},
		},
		{
			name:    "empty alpn protocol",
			mutate:  func(p *TLSFingerprintProfile) { p.ALPNProtocols = []string{""} },
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := realisticProfile()
			tc.mutate(p)
			err := p.ValidateConsistency()
			if tc.wantErr && err == nil {
				t.Fatalf("expected error, got nil")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
		})
	}
}
