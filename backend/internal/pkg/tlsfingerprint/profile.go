package tlsfingerprint

// CodexRustlsProfile 返回按当前 Codex CLI rustls ClientHello 参数整理的内置模板。
//
// Codex CLI 使用 reqwest/rustls/AWS-LC，官方没有公开可复制的固定指纹字符串；
// 这里保存的是从对应版本运行时 ClientHello 观测到的协议字段，供 OpenAI
// OAuth/SetupToken 请求使用。每次调用都返回独立切片，避免 uTLS 在握手期间
// 修改共享模板，确保并发请求之间不会互相污染。
func CodexRustlsProfile() *Profile {
	return &Profile{
		Name: "Codex CLI rustls 0.23",
		CipherSuites: []uint16{
			0x1302, // TLS_AES_256_GCM_SHA384
			0x1301, // TLS_AES_128_GCM_SHA256
			0x1303, // TLS_CHACHA20_POLY1305_SHA256
			0xc02c, // TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384
			0xc02b, // TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256
			0xcca9, // TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256
			0xc030, // TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384
			0xc02f, // TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256
			0xcca8, // TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256
			0x00ff, // TLS_EMPTY_RENEGOTIATION_INFO_SCSV
		},
		Curves:              []uint16{29, 23, 24, 4588},
		PointFormats:        []uint16{0},
		EnableGREASE:        false,
		SignatureAlgorithms: []uint16{0x0503, 0x0403, 0x0603, 0x0807, 0x0806, 0x0805, 0x0804, 0x0601, 0x0501, 0x0401},
		ALPNProtocols:       []string{"h2", "http/1.1"},
		SupportedVersions:   []uint16{0x0304, 0x0303},
		KeyShareGroups:      []uint16{29},
		PSKModes:            []uint16{1},
		Extensions:          []uint16{0, 10, 11, 13, 16, 23, 35, 43, 45, 51},
	}
}

// ProfileSupportsHTTP2 报告 profile 是否声明了 h2 ALPN。
// HTTP/2 是否真正可用仍由上层 Transport 决定，因此调用方必须同时配置
// 对应的 HTTP/2 RoundTripper，不能只修改 ClientHello 的 ALPN 字段。
func ProfileSupportsHTTP2(profile *Profile) bool {
	if profile == nil {
		return false
	}
	for _, protocol := range profile.ALPNProtocols {
		if protocol == "h2" {
			return true
		}
	}
	return false
}
