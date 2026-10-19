package mask

import (
	"fmt"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
)

// Internal IPv4 by the data itself, beyond the dotted form (detectors_format.go): the providers'
// shared range, the address written with hyphens inside a machine name and the DNS reverse name.
// All become the same fake IP (240.0.0.0/4), in the same spelling.

// sharedNetwork: 100.64.0.0/10, "Shared Address Space" (RFC 6598): a provider's internal network
// (CGNAT), also used by VPNs and clusters; not routable on the internet.
var sharedNetwork = netip.MustParsePrefix("100.64.0.0/10")

// internalIP: internal network IPv4: private (RFC 1918) or shared (RFC 6598).
func internalIP(a netip.Addr) bool {
	return a.Is4() && (a.IsPrivate() || sharedNetwork.Contains(a)) && !fakeNet.Contains(a)
}

// reAWSInternal: the private DNS domain of an EC2 instance after the "ip-" name: ".ec2.internal"
// (us-east-1) or ".<region>.compute.internal" (https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/hostname-types.html)
var reAWSInternal = regexp.MustCompile(`^\.(?:ec2|[a-z]{2}(?:-[a-z]+)+-\d\.compute)\.internal\b`)

// digitHyphenDigit: cheap prefilter of findHyphenIP (a digit, a hyphen, a digit).
func digitHyphenDigit(s string) bool {
	for i := strings.IndexByte(s, '-'); i >= 0; {
		if i > 0 && i+1 < len(s) && isDig(s[i-1]) && isDig(s[i+1]) {
			return true
		}
		j := strings.IndexByte(s[i+1:], '-')
		if j < 0 {
			return false
		}
		i += 1 + j
	}
	return false
}

// findHyphenIP calls f for each IPv4 written with hyphens (four numbers of 1 to 3 digits) in a
// machine name. The documented forms:
//   - AWS, private instance name: ip-10-42-7-15.ec2.internal,
//     ip-10-42-7-15.sa-east-1.compute.internal (and the name alone, in the prompt: ubuntu@ip-10-42-7-15)
//   - AWS, public name: ec2-52-14-88-21.compute-1.amazonaws.com
//     (https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/hostname-types.html)
//   - Kubernetes, pod: 172-17-0-3.default.pod.cluster.local
//     (https://kubernetes.io/docs/concepts/services-networking/dns-pod-service/)
//
// It counts right after "ip-" or "ec2-", or as the first label of a name with more labels after it
// ("172-17-0-3.default..."). Outside that ("versão 10-1-2-3") it is not an IP. pre: where the label
// starts, with the "ip-"/"ec2-"; start and end: only the numbers.
func findHyphenIP(s string, f func(pre, ini, end int, a netip.Addr)) {
	glued := func(c byte) bool { return isAlnum(c) || c == '-' || c == '_' || c == '.' }
	// end of the four numbers from i, or -1 (without regex: in "ec2-52-14-88-21" a regex would
	// match "2-52-14-88" and skip the address)
	four := func(i int) int {
		for g := 0; g < 4; g++ {
			n := 0
			for i < len(s) && isDig(s[i]) && n < 4 {
				i, n = i+1, n+1
			}
			if n == 0 || n > 3 {
				return -1
			}
			if g < 3 {
				if i >= len(s) || s[i] != '-' {
					return -1
				}
				i++
			}
		}
		return i
	}
	for i := 0; i < len(s); i++ {
		if !isDig(s[i]) || i > 0 && isDig(s[i-1]) {
			continue
		}
		j := four(i)
		if j < 0 || j < len(s) && (isAlnum(s[j]) || s[j] == '-' || s[j] == '_') {
			continue
		}
		ini := i
		switch {
		case i >= 3 && strings.EqualFold(s[i-3:i], "ip-"):
			ini = i - 3
		case i >= 4 && strings.EqualFold(s[i-4:i], "ec2-"):
			ini = i - 4
		}
		// glued to the name before it ("pod-x-ip-10-42-7-15.us-east-1.compute.internal-abc12"): only
		// with the AWS internal domain right after the numbers
		if ini > 0 && glued(s[ini-1]) && !(ini == i-3 && s[ini-1] == '-' && reAWSInternal.MatchString(s[j:min(len(s), j+40)])) {
			continue
		}
		if ini == i && !(j+1 < len(s) && s[j] == '.' && letterD(s[j+1])) {
			continue
		}
		if a, err := netip.ParseAddr(strings.ReplaceAll(s[i:j], "-", ".")); err == nil {
			f(ini, i, j, a)
		}
	}
}

// reReverseV4: IPv4 reverse name (RFC 1035, 3.5): the octets reversed plus in-addr.arpa. With 4
// octets it is an address; with 3, the zone of a /24 network.
var reReverseV4 = regexp.MustCompile(`(?i)\b(?:\d{1,3}\.){2,3}\d{1,3}\.in-addr\.arpa\b`)

// findReverseV4 calls f for each reverse name of an internal IPv4; the piece includes the suffix.
func findReverseV4(s string, f func(ini, end int)) {
	for _, ix := range reReverseV4.FindAllStringIndex(s, -1) {
		if ix[0] > 0 && (isAlnum(s[ix[0]-1]) || s[ix[0]-1] == '.' || s[ix[0]-1] == '-') {
			continue
		}
		if o, ok := reverseOctets(s[ix[0]:ix[1]]); ok {
			a := netip.AddrFrom4([4]byte{o[0], o[1], o[2], 0})
			if len(o) == 4 {
				a = netip.AddrFrom4([4]byte{o[0], o[1], o[2], o[3]})
			}
			if internalIP(a) {
				f(ix[0], ix[1])
			}
		}
	}
}

// reverseOctets: the octets of "15.7.42.10.in-addr.arpa" in normal order (10, 42, 7, 15).
func reverseOctets(v string) ([]byte, bool) {
	i := strings.Index(strings.ToLower(v), ".in-addr.arpa")
	if i < 0 {
		return nil, false
	}
	ps := strings.Split(v[:i], ".")
	o := make([]byte, len(ps))
	for k, p := range ps {
		n, err := strconv.Atoi(p)
		if err != nil || n > 255 || len(p) > 1 && p[0] == '0' {
			return nil, false
		}
		o[len(ps)-1-k] = byte(n)
	}
	return o, len(o) == 3 || len(o) == 4
}

// pseudoIP: the fake IPv4 in the spelling of the real one. The /24 network becomes another one, in
// the reserved range 240.0.0.0/4 (RFC 1112), always the same for the same network; the last octet
// stays. Spellings: with dots, with hyphens (machine name) and reversed (reverse name, also of the
// /24 zone).
func (m *Masker) pseudoIP(real string) string {
	fakeVal := func(o []byte) []byte {
		h := m.p.Bits("rede24", fmt.Sprintf("%d.%d.%d", o[0], o[1], o[2]), 3)
		f := []byte{240 | (h[0] & 0x0f), h[1], h[2]}
		if len(o) == 4 {
			f = append(f, o[3])
		}
		return f
	}
	joins := func(o []byte, sep string) string {
		ps := make([]string, len(o))
		for k, x := range o {
			ps[k] = strconv.Itoa(int(x))
		}
		return strings.Join(ps, sep)
	}
	if o, ok := reverseOctets(real); ok {
		f := fakeVal(o)
		for a, b := 0, len(f)-1; a < b; a, b = a+1, b-1 {
			f[a], f[b] = f[b], f[a]
		}
		return joins(f, ".") + real[strings.Index(strings.ToLower(real), ".in-addr.arpa"):]
	}
	if strings.Contains(real, "-") {
		a := netip.MustParseAddr(strings.ReplaceAll(real, "-", ".")).As4()
		return joins(fakeVal(a[:]), "-")
	}
	a := netip.MustParseAddr(real).As4()
	return joins(fakeVal(a[:]), ".")
}
