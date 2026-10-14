package mask

import (
	"net/netip"
	"regexp"
	"strings"
)

// Internal-network IPv6 (always) and public IP (option objects.public_ip, off by default): they
// become a server name. Internal IPv4 belongs to the "ip" detector (detectors_format.go).

// Public DNS (Google, Cloudflare, Quad9, OpenDNS): they appear in all documentation. And AWS's
// fixed IPv6 addresses, the same in every VPC: the DNS (Route 53 Resolver) and the instance
// metadata service (docs.aws.amazon.com: AmazonDNS-concepts, instancedata-data-retrieval).
var publicDNS = setOf("8.8.8.8", "8.8.4.4", "1.1.1.1", "1.0.0.1", "9.9.9.9", "208.67.222.222",
	"2001:4860:4860::8888", "2606:4700:4700::1111", "fd00:ec2::253", "fd00:ec2::254")

// documentation ranges (RFC 5737, RFC 3849 and RFC 9637): not a real address
var docNetworks = []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24"), netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("2001:db8::/32"), netip.MustParsePrefix("3fff::/20")}

func publicIP(a netip.Addr) bool {
	if !a.IsGlobalUnicast() || a.Is4() && (internalIP(a) || fakeNet.Contains(a)) || publicDNS[a.String()] || internalIPv6(a) {
		return false
	}
	for _, p := range docNetworks {
		if p.Contains(a) {
			return false
		}
	}
	return true
}

// internalIPv6: IPv6 that identifies an internal network or machine:
//   - unique local address, fc00::/7 (RFC 4193), with a nonzero Global ID (40 bits, drawn at
//     random by whoever creates the network): "fd00::/8" written in an explanation is nobody's network;
//   - link-local, fe80::/10 (RFC 4291), with an interface identifier beyond the last 16
//     bits: derived from the MAC (EUI-64, RFC 4291 appendix A) or random (RFC 7217, RFC 8981)
//     it identifies the machine (RFC 7721, section 3); "fe80::1", set by hand, does not;
//   - internal IPv4 written in hexadecimal inside IPv6 (::ffff:a2a:70f, RFC 4291 2.5.5.2).
//     With dots (::ffff:10.42.7.15), the IPv4 already belongs to the "ip" detector.
func internalIPv6(a netip.Addr) bool {
	if !a.Is6() || publicDNS[a.String()] {
		return false
	}
	b := a.As16()
	switch {
	case a.Is4In6():
		return internalIP(a.Unmap())
	case b[0]&0xfe == 0xfc:
		return b[1]|b[2]|b[3]|b[4]|b[5] != 0
	case a.IsLinkLocalUnicast():
		return b[8]|b[9]|b[10]|b[11]|b[12]|b[13] != 0
	}
	return false
}

// findInternalIP: the always-on reader of internal IPv6 (also in the reverse name, ip6.arpa).
func findInternalIP(s string, add func(ObjFinding)) {
	ok := func(a netip.Addr, v string) bool { return internalIPv6(a) && !strings.Contains(v, ".") }
	if strings.Count(s, ":") >= 2 {
		scanIPv6(s, func(i, k int, a netip.Addr) {
			if ok(a, s[i:k]) {
				add(ObjFinding{i, k, "server", "ip-interno", true})
			}
		})
	}
	if strings.Contains(s, "ip6.arpa") || strings.Contains(s, "IP6.ARPA") {
		findReverseV6(s, func(i, k int, a netip.Addr) {
			if internalIPv6(a) {
				add(ObjFinding{i, k, "server", "ip-interno", true})
			}
		})
	}
}

func findPublicIP(s string, add func(ObjFinding)) {
	pub := func(i, k int) { add(ObjFinding{i, k, "server", "ip-público", true}) }
	if strings.Count(s, ".") >= 3 {
		for _, ix := range reIPv4.FindAllStringIndex(s, -1) {
			if !numberEdge(s, ix[0], ix[1]) || ix[0] > 0 && (letterD(s[ix[0]-1]) || s[ix[0]-1] == '.') {
				continue
			}
			// "6.0.6.1", "8.2.4.44": version or section number (first three with one digit)
			if v := s[ix[0]:ix[1]]; len(v) >= 6 && v[1] == '.' && v[3] == '.' && v[5] == '.' {
				continue
			}
			if a, err := netip.ParseAddr(s[ix[0]:ix[1]]); err == nil && publicIP(a) {
				pub(ix[0], ix[1])
			}
		}
	}
	// in a machine name: ec2-52-14-88-21.compute-1.amazonaws.com (the whole label, with the "ec2-":
	// the numbers alone would be discarded as numbering, with no owner)
	if digitHyphenDigit(s) {
		findHyphenIP(s, func(pre, _, k int, a netip.Addr) {
			if publicIP(a) {
				pub(pre, k)
			}
		})
	}
	if strings.Count(s, ":") >= 2 {
		scanIPv6(s, func(i, k int, a netip.Addr) {
			if !a.Is4In6() && publicIP(a) {
				pub(i, k)
			}
		})
	}
	if strings.Contains(s, "ip6.arpa") || strings.Contains(s, "IP6.ARPA") {
		findReverseV6(s, func(i, k int, a netip.Addr) {
			if publicIP(a) {
				pub(i, k)
			}
		})
	}
}

// scanIPv6 calls f for each IPv6 in s (without the "%eth0" zone and without the "/64" after it).
func scanIPv6(s string, f func(i, k int, a netip.Addr)) {
	hex := func(c byte) bool { return isDig(c) || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' }
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(hex(c) || c == ':') || i > 0 && (isAlnum(s[i-1]) || s[i-1] == ':' || s[i-1] == '.' || s[i-1] == '_') {
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
		if j < len(s) && (isAlnum(s[j]) || s[j] == '_') {
			i = j
			continue
		}
		v := s[i:k]
		two, longV, group := strings.Count(v, ":"), false, 0
		for x := 0; x < len(v); x++ {
			if v[x] == ':' {
				group = 0
			} else if group++; group >= 3 {
				longV = true
			}
		}
		if two >= 2 && (two >= 3 || longV) {
			if a, err := netip.ParseAddr(v); err == nil && a.Is6() {
				f(i, k, a)
			}
		}
		i = j
	}
}

// reReverseV6: IPv6 reverse name (RFC 3596, 2.5): the 32 hexadecimal digits reversed, separated by
// dots, plus ip6.arpa.
var reReverseV6 = regexp.MustCompile(`(?i)\b(?:[0-9a-f]\.){32}ip6\.arpa\b`)

// findReverseV6 calls f with the digits part (without ".ip6.arpa") and the address.
func findReverseV6(s string, f func(i, k int, a netip.Addr)) {
	for _, ix := range reReverseV6.FindAllStringIndex(s, -1) {
		if ix[0] > 0 && (isAlnum(s[ix[0]-1]) || s[ix[0]-1] == '.' || s[ix[0]-1] == '-') {
			continue
		}
		var b [16]byte
		for n := 0; n < 32; n++ {
			c := s[ix[0]+2*n] | 0x20
			v := c - '0'
			if c >= 'a' {
				v = c - 'a' + 10
			}
			// the first digit of the name is the last one of the address
			p := 31 - n
			if p%2 == 0 {
				b[p/2] |= v << 4
			} else {
				b[p/2] |= v
			}
		}
		f(ix[0], ix[0]+63, netip.AddrFrom16(b))
	}
}
