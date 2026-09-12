package tlsfingerprint

import (
	"reflect"
	"testing"

	utls "github.com/refraction-networking/utls"
)

// TestCodexRustlsProfileMatchesObservedClientHello 锁定 Codex rustls 探针得到的关键字段，
// 防止后续修改误把 OpenAI 出站模板退回 Node.js 或浏览器参数。
func TestCodexRustlsProfileMatchesObservedClientHello(t *testing.T) {
	profile := CodexRustlsProfile()
	if profile == nil {
		t.Fatal("Codex profile must not be nil")
	}

	if got, want := profile.CipherSuites, []uint16{0x1302, 0x1301, 0x1303, 0xc02c, 0xc02b, 0xcca9, 0xc030, 0xc02f, 0xcca8, 0x00ff}; !reflect.DeepEqual(got, want) {
		t.Fatalf("cipher suites = %v, want %v", got, want)
	}
	if got, want := profile.Curves, []uint16{29, 23, 24, 4588}; !reflect.DeepEqual(got, want) {
		t.Fatalf("curves = %v, want %v", got, want)
	}
	if got, want := profile.SignatureAlgorithms, []uint16{0x0503, 0x0403, 0x0603, 0x0807, 0x0806, 0x0805, 0x0804, 0x0601, 0x0501, 0x0401}; !reflect.DeepEqual(got, want) {
		t.Fatalf("signature algorithms = %v, want %v", got, want)
	}
	if got, want := profile.ALPNProtocols, []string{"h2", "http/1.1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ALPN = %v, want %v", got, want)
	}
	if got, want := profile.SupportedVersions, []uint16{0x0304, 0x0303}; !reflect.DeepEqual(got, want) {
		t.Fatalf("supported versions = %v, want %v", got, want)
	}
	if got, want := profile.KeyShareGroups, []uint16{29}; !reflect.DeepEqual(got, want) {
		t.Fatalf("key shares = %v, want %v", got, want)
	}
	if got, want := profile.Extensions, []uint16{0, 10, 11, 13, 16, 23, 35, 43, 45, 51}; !reflect.DeepEqual(got, want) {
		t.Fatalf("extensions = %v, want %v", got, want)
	}
	if profile.EnableGREASE {
		t.Fatal("Codex rustls profile must not enable uTLS GREASE")
	}
	if !ProfileSupportsHTTP2(profile) {
		t.Fatal("Codex rustls profile must advertise HTTP/2")
	}

	spec := buildClientHelloSpecFromProfile(profile)
	if got, want := spec.TLSVersMin, uint16(utls.VersionTLS12); got != want {
		t.Fatalf("TLS minimum = 0x%04x, want 0x%04x", got, want)
	}
	if got, want := spec.TLSVersMax, uint16(utls.VersionTLS13); got != want {
		t.Fatalf("TLS maximum = 0x%04x, want 0x%04x", got, want)
	}
}

// TestTLSVersionBoundsIgnoresGREASE 锁定版本边界推导不会把 GREASE 当成真实版本。
func TestTLSVersionBoundsIgnoresGREASE(t *testing.T) {
	minVersion, maxVersion := tlsVersionBounds([]uint16{0x0a0a, utls.VersionTLS13, utls.VersionTLS12})
	if minVersion != utls.VersionTLS12 || maxVersion != utls.VersionTLS13 {
		t.Fatalf("version bounds = 0x%04x-0x%04x, want TLS1.2-TLS1.3", minVersion, maxVersion)
	}
}
