package mask

import (
	"regexp"
	"strconv"
	"strings"
)

// Electronic fiscal document access key and Pix copy-and-paste: both carry someone's document or
// key inside, and both are checked by the data itself.

// reDFeKey: access key (NF-e, NFC-e, CT-e, MDF-e), 44 positions in a row or in 11 groups of 4 (the
// form printed on the DANFE); in the XML, the Id attribute is "NFe" + the key (MOC, field A03).
// With the alphanumeric CNPJ, its positions may have letters.
var reDFeKey = regexp.MustCompile(`(?:\bNFe|\b)([0-9A-Z]{4}(?: ?[0-9A-Z]{4}){10})\b`)

// IBGE state codes (servicodados.ibge.gov.br/api/v1/localidades/estados)
var ufIBGE = setOf("11", "12", "13", "14", "15", "16", "17", "21", "22", "23", "24", "25", "26", "27",
	"28", "29", "31", "32", "33", "35", "41", "42", "43", "50", "51", "52", "53")

// ValidDFeKey checks the access key by the NF-e Taxpayer Guidance Manual (MOC 7.0, Annex I, and the
// validation rules of the referenced key): cUF(2) AAMM(4) CNPJ/CPF(14) model(2) series(3) number(9)
// tpEmis(1) code(8) check digit(1). IBGE state, month from 01 to 12, year since 06 (start of NF-e),
// valid issuer CNPJ or CPF, and the check digit by modulo 11 (weights 2 to 9 from right to left;
// remainder 0 or 1, check digit 0). Each character is worth its ASCII code minus 48, as in the
// alphanumeric CNPJ (Joint Technical Note 2025.001): for digits, the digit itself.
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

// findBRCode: Pix copy-and-paste (BR Code). Fields ID(2) + length(2) + value, starting with
// "000201" and ending with the CRC16 (ID 63, 4 hexadecimal digits), with the GUI "br.gov.bcb.pix"
// in one of the fields 26 to 51 (Pix Initiation Standards Manual, BCB). It carries the Pix key, the
// name and the city of the receiver: the whole code becomes one pseudonym.
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

// crc16CCITT: polynomial 0x1021, initial value 0xFFFF (the BR Code CRC).
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
