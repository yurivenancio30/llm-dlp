package mask

import (
	"net/netip"
	"strings"
)

// IP público e IPv6 (opção objetos.ip_publico, desligada por padrão).

// DNS públicos (Google, Cloudflare, Quad9, OpenDNS): aparecem em toda documentação.
var dnsPublicos = conj("8.8.8.8", "8.8.4.4", "1.1.1.1", "1.0.0.1", "9.9.9.9", "208.67.222.222",
	"2001:4860:4860::8888", "2606:4700:4700::1111")

// faixas de documentação (RFC 5737 e RFC 3849): não são endereço real
var redesDoc = []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("2001:db8::/32")}

func ipPublico(a netip.Addr) bool {
	if !a.IsGlobalUnicast() || a.Is4() && (a.IsPrivate() || fakeNet.Contains(a)) || dnsPublicos[a.String()] {
		return false
	}
	for _, p := range redesDoc {
		if p.Contains(a) {
			return false
		}
	}
	return true
}

func acharIPPublico(s string, add func(ObjAchado)) {
	if strings.Count(s, ".") >= 3 {
		for _, ix := range reIPv4.FindAllStringIndex(s, -1) {
			if !bordaNum(s, ix[0], ix[1]) || ix[0] > 0 && (letraD(s[ix[0]-1]) || s[ix[0]-1] == '.') {
				continue
			}
			// "6.0.6.1", "8.2.4.44": versão ou número de seção (três primeiros com um dígito)
			if v := s[ix[0]:ix[1]]; len(v) >= 6 && v[1] == '.' && v[3] == '.' && v[5] == '.' {
				continue
			}
			if a, err := netip.ParseAddr(s[ix[0]:ix[1]]); err == nil && ipPublico(a) {
				add(ObjAchado{ix[0], ix[1], "servidor", "ip-público", true})
			}
		}
	}
	if strings.Count(s, ":") < 2 {
		return
	}
	hex := func(c byte) bool { return ehDig(c) || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' }
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(hex(c) || c == ':') || i > 0 && (ehAlnum(s[i-1]) || s[i-1] == ':' || s[i-1] == '.' || s[i-1] == '_') {
			continue
		}
		j := i
		for j < len(s) && j-i < 45 && (hex(s[j]) || s[j] == ':' || s[j] == '.') {
			j++
		}
		k := j
		for k > i && s[k-1] == '.' {
			k--
		}
		if j < len(s) && (ehAlnum(s[j]) || s[j] == '_') {
			i = j
			continue
		}
		v := s[i:k]
		dois, longo, grupo := strings.Count(v, ":"), false, 0
		for x := 0; x < len(v); x++ {
			if v[x] == ':' {
				grupo = 0
			} else if grupo++; grupo >= 3 {
				longo = true
			}
		}
		if dois >= 2 && (dois >= 3 || longo) {
			if a, err := netip.ParseAddr(v); err == nil && a.Is6() && !a.Is4In6() && ipPublico(a) {
				add(ObjAchado{i, k, "servidor", "ip-público", true})
			}
		}
		i = j
	}
}
