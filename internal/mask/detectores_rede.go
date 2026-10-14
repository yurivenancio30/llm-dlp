package mask

import (
	"fmt"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
)

// IPv4 interno pelo próprio dado, além da forma com pontos (detectores_formato.go): a faixa
// compartilhada dos provedores, o endereço escrito com hífens dentro do nome de uma máquina e
// o nome reverso do DNS. Todos viram o mesmo IP falso (240.0.0.0/4), na mesma grafia.

// redeCompartilhada: 100.64.0.0/10, "Shared Address Space" (RFC 6598): rede interna de
// provedor (CGNAT), também usada por VPNs e clusters; não é roteável na internet.
var redeCompartilhada = netip.MustParsePrefix("100.64.0.0/10")

// ipInterno: IPv4 de rede interna: privado (RFC 1918) ou compartilhado (RFC 6598).
func ipInterno(a netip.Addr) bool {
	return a.Is4() && (a.IsPrivate() || redeCompartilhada.Contains(a)) && !fakeNet.Contains(a)
}

// digHifenDig: pré-filtro barato de acharIPHifen (um dígito, hífen, dígito).
func digHifenDig(s string) bool {
	for i := strings.IndexByte(s, '-'); i >= 0; {
		if i > 0 && i+1 < len(s) && ehDig(s[i-1]) && ehDig(s[i+1]) {
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

// acharIPHifen chama f para cada IPv4 escrito com hífens (quatro números de 1 a 3 dígitos) num
// nome de máquina. As formas documentadas:
//   - AWS, nome privado da instância: ip-10-42-7-15.ec2.internal,
//     ip-10-42-7-15.sa-east-1.compute.internal (e o nome sozinho, no prompt: ubuntu@ip-10-42-7-15)
//   - AWS, nome público: ec2-52-14-88-21.compute-1.amazonaws.com
//     (https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/hostname-types.html)
//   - Kubernetes, pod: 172-17-0-3.default.pod.cluster.local
//     (https://kubernetes.io/docs/concepts/services-networking/dns-pod-service/)
//
// Vale logo depois de "ip-" ou "ec2-", ou como o primeiro rótulo de um nome com mais rótulos
// depois ("172-17-0-3.default..."). Fora disso ("versão 10-1-2-3") não é IP. pre: onde começa
// o rótulo, com o "ip-"/"ec2-"; ini e fim: só os números.
func acharIPHifen(s string, f func(pre, ini, fim int, a netip.Addr)) {
	colado := func(c byte) bool { return ehAlnum(c) || c == '-' || c == '_' || c == '.' }
	// fim dos quatro números a partir de i, ou -1 (sem regex: em "ec2-52-14-88-21" uma regex
	// casaria "2-52-14-88" e pularia o endereço)
	quatro := func(i int) int {
		for g := 0; g < 4; g++ {
			n := 0
			for i < len(s) && ehDig(s[i]) && n < 4 {
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
		if !ehDig(s[i]) || i > 0 && ehDig(s[i-1]) {
			continue
		}
		j := quatro(i)
		if j < 0 || j < len(s) && (ehAlnum(s[j]) || s[j] == '-' || s[j] == '_') {
			continue
		}
		ini := i
		switch {
		case i >= 3 && strings.EqualFold(s[i-3:i], "ip-"):
			ini = i - 3
		case i >= 4 && strings.EqualFold(s[i-4:i], "ec2-"):
			ini = i - 4
		}
		if ini > 0 && colado(s[ini-1]) {
			continue
		}
		if ini == i && !(j+1 < len(s) && s[j] == '.' && letraD(s[j+1])) {
			continue
		}
		if a, err := netip.ParseAddr(strings.ReplaceAll(s[i:j], "-", ".")); err == nil {
			f(ini, i, j, a)
		}
	}
}

// reReversoV4: nome reverso de IPv4 (RFC 1035, 3.5): os octetos ao contrário mais
// in-addr.arpa. Com 4 octetos é um endereço; com 3, a zona de uma rede /24.
var reReversoV4 = regexp.MustCompile(`(?i)\b(?:\d{1,3}\.){2,3}\d{1,3}\.in-addr\.arpa\b`)

// acharReversoV4 chama f para cada nome reverso de IPv4 interno; o trecho inclui o sufixo.
func acharReversoV4(s string, f func(ini, fim int)) {
	for _, ix := range reReversoV4.FindAllStringIndex(s, -1) {
		if ix[0] > 0 && (ehAlnum(s[ix[0]-1]) || s[ix[0]-1] == '.' || s[ix[0]-1] == '-') {
			continue
		}
		if o, ok := octetosReversos(s[ix[0]:ix[1]]); ok {
			a := netip.AddrFrom4([4]byte{o[0], o[1], o[2], 0})
			if len(o) == 4 {
				a = netip.AddrFrom4([4]byte{o[0], o[1], o[2], o[3]})
			}
			if ipInterno(a) {
				f(ix[0], ix[1])
			}
		}
	}
}

// octetosReversos: os octetos de "15.7.42.10.in-addr.arpa" na ordem normal (10, 42, 7, 15).
func octetosReversos(v string) ([]byte, bool) {
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

// pseudoIP: o IPv4 falso na grafia do real. A rede /24 vira outra, na faixa reservada
// 240.0.0.0/4 (RFC 1112), sempre a mesma para a mesma rede; o último octeto fica. Grafias: com
// pontos, com hífens (nome de máquina) e ao contrário (nome reverso, também da zona /24).
func (m *Masker) pseudoIP(real string) string {
	falso := func(o []byte) []byte {
		h := m.p.Bits("rede24", fmt.Sprintf("%d.%d.%d", o[0], o[1], o[2]), 3)
		f := []byte{240 | (h[0] & 0x0f), h[1], h[2]}
		if len(o) == 4 {
			f = append(f, o[3])
		}
		return f
	}
	junta := func(o []byte, sep string) string {
		ps := make([]string, len(o))
		for k, x := range o {
			ps[k] = strconv.Itoa(int(x))
		}
		return strings.Join(ps, sep)
	}
	if o, ok := octetosReversos(real); ok {
		f := falso(o)
		for a, b := 0, len(f)-1; a < b; a, b = a+1, b-1 {
			f[a], f[b] = f[b], f[a]
		}
		return junta(f, ".") + real[strings.Index(strings.ToLower(real), ".in-addr.arpa"):]
	}
	if strings.Contains(real, "-") {
		a := netip.MustParseAddr(strings.ReplaceAll(real, "-", ".")).As4()
		return junta(falso(a[:]), "-")
	}
	a := netip.MustParseAddr(real).As4()
	return junta(falso(a[:]), ".")
}
