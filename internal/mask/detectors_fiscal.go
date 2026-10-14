package mask

import (
	"regexp"
	"strconv"
	"strings"
)

// Key de acesso de documento fiscal eletrônico e Pix copia e cola: os dois levam um
// documento ou uma chave de alguém dentro, e os dois se conferem pelo próprio dado.

// reDFeKey: chave de acesso (NF-e, NFC-e, CT-e, MDF-e), 44 posições seguidas ou em 11 grupos
// de 4 (a forma impressa no DANFE); no XML, o atributo Id é "NFe" + a chave (MOC, campo A03).
// Com o CNPJ alfanumérico, as posições dele podem ter letras.
var reDFeKey = regexp.MustCompile(`(?:\bNFe|\b)([0-9A-Z]{4}(?: ?[0-9A-Z]{4}){10})\b`)

// códigos de UF do IBGE (servicodados.ibge.gov.br/api/v1/localidades/estados)
var ufIBGE = setOf("11", "12", "13", "14", "15", "16", "17", "21", "22", "23", "24", "25", "26", "27",
	"28", "29", "31", "32", "33", "35", "41", "42", "43", "50", "51", "52", "53")

// ValidDFeKey confere a chave de acesso pelo Manual de Orientação do Contribuinte da NF-e
// (MOC 7.0, Anexo I, e as regras de validação da chave referenciada): cUF(2) AAMM(4)
// CNPJ/CPF(14) modelo(2) série(3) número(9) tpEmis(1) código(8) DV(1). UF do IBGE, mês de 01 a
// 12, ano desde 06 (início da NF-e), CNPJ ou CPF do emitente válido, e o DV pelo módulo 11 (pesos
// 2 a 9 da direita para a esquerda; resto 0 ou 1, DV 0). Cada caractere vale o código ASCII
// menos 48, como no CNPJ alfanumérico (Nota Técnica Conjunta 2025.001): para dígitos, o próprio.
func ValidDFeKey(s string) bool {
	s = strings.ReplaceAll(s, " ", "")
	if len(s) != 44 || digitsOnly(s[:6]) != s[:6] || digitsOnly(s[20:]) != s[20:] {
		return false
	}
	if !ufIBGE[s[:2]] || s[2:4] < "06" || s[4:6] < "01" || s[4:6] > "12" {
		return false
	}
	emit := s[6:20]
	if !(ValidCNPJ(emit) || ValidAlphaCNPJ(emit) || strings.HasPrefix(emit, "000") && ValidCPF(emit[3:])) {
		return false
	}
	sum, p := 0, 2
	for i := 42; i >= 0; i-- {
		sum += int(s[i]-'0') * p
		if p++; p > 9 {
			p = 2
		}
	}
	dv := 0
	if r := sum % 11; r >= 2 {
		dv = 11 - r
	}
	return int(s[43]-'0') == dv
}

func findDFeKey(s string, add func(ini, end int, typ string)) {
	for _, ix := range reDFeKey.FindAllStringSubmatchIndex(s, -1) {
		if ValidDFeKey(s[ix[2]:ix[3]]) {
			add(ix[2], ix[3], "nfe")
		}
	}
}

// findBRCode: o Pix copia e cola (BR Code). Campos ID(2) + tamanho(2) + valor, começando por
// "000201" e terminando no CRC16 (ID 63, 4 hexadecimais), com o GUI "br.gov.bcb.pix" num dos
// campos 26 a 51 (Manual de Padrões para Iniciação do Pix, BCB). Leva a chave Pix, o nome e a
// cidade de quem recebe: o código inteiro vira um pseudônimo.
func findBRCode(s string, add func(ini, end int, typ string)) {
	for i := 0; ; {
		j := strings.Index(s[i:], "000201")
		if j < 0 {
			return
		}
		i += j
		if end, ok := brCodeIn(s, i); ok {
			add(i, end, "pix")
			i = end
		} else {
			i++
		}
	}
}

func brCodeIn(s string, i int) (int, bool) {
	pix := false
	for p := i; p+4 <= len(s); {
		id := s[p : p+2]
		n, err := strconv.Atoi(s[p+2 : p+4])
		if err != nil || digitsOnly(s[p:p+4]) != s[p:p+4] {
			return 0, false
		}
		if id == "63" {
			if n != 4 || p+8 > len(s) || !pix || p+8 < len(s) && isAlnum(s[p+8]) {
				return 0, false
			}
			crc := strconv.FormatUint(uint64(crc16CCITT(s[i:p+4])), 16)
			return p + 8, strings.EqualFold(strings.Repeat("0", 4-len(crc))+crc, s[p+4:p+8])
		}
		if p+4+n > len(s) {
			return 0, false
		}
		if id >= "26" && id <= "51" && strings.Contains(strings.ToLower(s[p+4:p+4+n]), "br.gov.bcb.pix") {
			pix = true
		}
		p += 4 + n
	}
	return 0, false
}

// crc16CCITT: polinômio 0x1021, valor inicial 0xFFFF (o CRC do BR Code).
func crc16CCITT(s string) uint16 {
	crc := uint16(0xFFFF)
	for i := 0; i < len(s); i++ {
		crc ^= uint16(s[i]) << 8
		for b := 0; b < 8; b++ {
			if crc&0x8000 != 0 {
				crc = crc<<1 ^ 0x1021
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}
