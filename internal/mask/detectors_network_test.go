package mask

import (
	"fmt"
	"net/netip"
	"strings"
	"testing"

	"github.com/yurivenancio30/llm-dlp/internal/config"
)

// Made-up addresses. Each case: the text, what has to disappear and what has to stay.
func TestInternalIPByData(t *testing.T) {
	m := newTest(t)
	cases := []struct {
		s           string
		gone, stays []string
	}{
		// shared range (RFC 6598), like the private one
		{"cgnat 100.72.5.9 e 100.127.255.1", []string{"100.72.5.9", "100.127.255.1"}, nil},
		{"fora da faixa 100.128.0.1 e 100.63.255.255", nil, []string{"100.128.0.1", "100.63.255.255"}},
		// with hyphens, in the machine name (AWS, Kubernetes)
		{"ubuntu@ip-10-42-7-15:~$ ls", []string{"10-42-7-15"}, []string{"ubuntu@ip-"}},
		{"[ec2-user@ip-172-31-20-7 ~]$", []string{"172-31-20-7"}, []string{"ec2-user@ip-"}},
		{"pod 10-42-7-15.default.pod.cluster.local", []string{"10-42-7-15"}, []string{".default.pod.cluster.local"}},
		{"versão 10-1-2-3 do pacote, data 2024-10-05, faixa 10-20-30-40-50", nil, []string{"10-1-2-3", "2024-10-05", "10-20-30-40-50"}},
		// reverse name (RFC 1035, 3.5): address and /24 zone
		{"PTR 15.7.42.10.in-addr.arpa", []string{"15.7.42.10"}, []string{".in-addr.arpa"}},
		{"zona 7.42.10.in-addr.arpa", []string{"7.42.10."}, []string{".in-addr.arpa"}},
		{"PTR 21.88.14.52.in-addr.arpa", nil, []string{"21.88.14.52.in-addr.arpa"}}, // 52.14.88.21: public
		// IPv6: unique local, link-local with an interface identifier, internal IPv4 in hexadecimal
		{"ula fd12:3456:789a:1::15 e [fd12:3456:789a:1::15]:8080", []string{"fd12:3456:789a:1::15"}, []string{"]:8080"}},
		{"link fe80::1c2d:3e4f:5a6b:7c8d%eth0", []string{"fe80::1c2d:3e4f:5a6b:7c8d"}, []string{"%eth0"}},
		{"mapped ::ffff:a2a:70f", []string{"a2a:70f"}, nil},
		{"PTR 5.1.0.0.0.0.0.0.0.0.0.0.0.0.0.0.1.0.0.0.a.9.8.7.6.5.4.3.2.1.d.f.ip6.arpa", []string{"a.9.8.7"}, []string{".ip6.arpa"}},
		// they identify nobody's network: a range written in an explanation, an address set by
		// hand, fixed AWS addresses, public IPv6 (only with public_ip)
		{"a faixa fd00::/8 e fc00::/7; gateway fe80::1; metadados [fd00:ec2::254] e DNS fd00:ec2::253", nil,
			[]string{"fd00::/8", "fc00::/7", "fe80::1", "fd00:ec2::254", "fd00:ec2::253"}},
		{"global 2804:14c:5b81:8a00::15 link-local 169.254.12.3", nil, []string{"2804:14c:5b81:8a00::15", "169.254.12.3"}},
	}
	for _, c := range cases {
		out, ents := m.Mask(c.s)
		for _, x := range c.gone {
			if strings.Contains(out, x) {
				t.Errorf("%q: ficou %q\n   saiu %q", c.s, x, out)
			}
		}
		for _, x := range c.stays {
			if !strings.Contains(out, x) {
				t.Errorf("%q: sumiu %q\n   saiu %q", c.s, x, out)
			}
		}
		if back := NewTable(ents).Unmask(out, false); back != c.s {
			t.Errorf("volta de %q:\n   %q", c.s, back)
		}
	}
}

// The fake IP has the spelling of the real one, and the same /24 network always becomes the same
// fake network, in any spelling: the model still sees they are the same machine and the same
// network.
func TestFakeIPSameSpelling(t *testing.T) {
	m := newTest(t)
	dot := m.Pseudonym("ip", "10.42.7.15")
	o := strings.Split(dot, ".")
	if len(o) != 4 || o[3] != "15" {
		t.Fatalf("pseudônimo do IP: %q", dot)
	}
	wants := map[string]string{
		"10-42-7-15":              strings.Join(o, "-"),
		"15.7.42.10.in-addr.arpa": o[3] + "." + o[2] + "." + o[1] + "." + o[0] + ".in-addr.arpa",
		"7.42.10.IN-ADDR.ARPA":    o[2] + "." + o[1] + "." + o[0] + ".IN-ADDR.ARPA",
		"10.42.7.200":             o[0] + "." + o[1] + "." + o[2] + ".200",
	}
	for real, q := range wants {
		if p := m.Pseudonym("ip", real); p != q {
			t.Errorf("%q -> %q, quer %q", real, p, q)
		}
	}
}

func TestReaderInternalIP(t *testing.T) {
	checkReader(t, "ip-interno", findInternalIP, nil, []readerCase{
		{"inet6 fd12:3456:789a:1::15/64 e fe80::2a0:c9ff:fe12:3456%eth0", []string{"servidor:fd12:3456:789a:1::15!", "servidor:fe80::2a0:c9ff:fe12:3456!"}},
		{"::ffff:a2a:70f ::ffff:342e:5815 ::ffff:10.42.7.15", []string{"servidor:::ffff:a2a:70f!"}},
		{"fd00::1 fe80::1 ::1 2001:db8::1 2600:1f18:abcd::12 fd00:ec2::254", nil},
	})
}

// Public IP in the machine name (AWS) and in the reverse name: only with public_ip, like the loose
// IP.
func TestPublicIPInName(t *testing.T) {
	b := netip.MustParseAddr("2600:1f18:abcd::3").As16()
	var ptr []string
	for i := 15; i >= 0; i-- {
		ptr = append(ptr, fmt.Sprintf("%x.%x", b[i]&0xf, b[i]>>4))
	}
	s := "ssh ec2-52-14-88-21.sa-east-1.compute.amazonaws.com; PTR " + strings.Join(ptr, ".") + ".ip6.arpa"
	cfg := config.Default()
	cfg.Objects.PublicIP = true
	m, _ := NewMasker(cfg, testKey, nil, nil)
	out, ents := m.Mask(s)
	if strings.Contains(out, "52-14-88-21") || strings.Contains(out, "d.c.b.a") || !strings.Contains(out, ".sa-east-1.compute.amazonaws.com") {
		t.Errorf("com public_ip: %q", out)
	}
	if back := NewTable(ents).Unmask(out, false); back != s {
		t.Errorf("volta: %q", back)
	}
	m2, _ := NewMasker(config.Default(), testKey, nil, nil)
	if out, _ := m2.Mask(s); out != s {
		t.Errorf("sem public_ip: %q", out)
	}
}

// A loose AWS resource ID: the same pseudonym it has inside the ARN.
func TestIDsAWS(t *testing.T) {
	checkReader(t, "nuvem", findCloud, nil, []readerCase{
		{"vpc-0a1b2c3d4e5f67890 subnet-0f1e2d3c i-0abc1234def567890 tgw-attach-0123456789abcdef0",
			[]string{"servico:i-0abc1234def567890!", "servico:subnet-0f1e2d3c!", "servico:tgw-attach-0123456789abcdef0!", "servico:vpc-0a1b2c3d4e5f67890!"}},
		// prefix outside the list, wrong length, no digit, glued, AWS public image
		{"fix-1a2b3c4d vpc-0a1b2c3 sg-deadbeef xvpc-0a1b2c3d ami-0abcdef1234567890", nil},
	})
	m := newTest(t)
	a, _ := m.Mask("vpc vpc-0a1b2c3d4e5f67890")
	b, _ := m.Mask("arn:aws:ec2:sa-east-1:123456789012:vpc/vpc-0a1b2c3d4e5f67890")
	p := strings.TrimPrefix(a, "vpc ")
	if strings.Contains(a, "0a1b2c3d") || !strings.HasSuffix(b, "/"+p) {
		t.Errorf("solto %q, no ARN %q", a, b)
	}
}
