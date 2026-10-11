package proxy

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// Quem pode falar com a porta local.
//
// A porta só escuta em 127.0.0.1, mas isso não basta: o navegador também chega nela. Uma página
// maliciosa faz o próprio domínio apontar para 127.0.0.1 (DNS rebinding) e passa a ler as
// respostas como se fossem dela, inclusive a última conversa (/__llm-dlp/ultima). E, numa máquina
// compartilhada, outro usuário alcança a porta do mesmo jeito. Três barreiras:
//
//   - Local: só atende quem chama pelo endereço local e não é uma página da web;
//   - a última conversa só sai com a prova de quem lê a chave (o dono da pasta do llm-dlp);
//   - o llm-dlp prova que é ele (Prova) a quem pergunta se está no ar: outro programa que
//     pegue a porta não se passa por ele, e não recebe a conversa sem máscara.

func enderecoLocal(h string) bool {
	return h == "127.0.0.1" || h == "localhost" || h == "::1"
}

// Local: a requisição veio de um programa da máquina falando com o endereço local, e não de uma
// página aberta no navegador. No rebinding o Host é o domínio do atacante; uma página de outro
// site manda a Origin dela.
//
// Só recusa o que um programa local nunca manda: o Claude Code chama por 127.0.0.1 (o gancho
// exige) e não tem Origin de site. Outros sinais de navegador (Origin "null", Sec-Fetch-Site)
// não entram: um cliente que os mandasse ficaria sem funcionar, e o que eles barrariam a mais
// (uma requisição às cegas, sem ler a resposta e sem a credencial) não lê nem envia nada.
func Local(r *http.Request) bool {
	h := r.Host
	if hh, _, err := net.SplitHostPort(h); err == nil {
		h = hh
	}
	if !enderecoLocal(strings.Trim(h, "[]")) {
		return false
	}
	if u, err := url.Parse(r.Header.Get("Origin")); err == nil && (u.Scheme == "http" || u.Scheme == "https") && !enderecoLocal(u.Hostname()) {
		return false
	}
	return true
}

// SoLocal recusa (403) o que não passa em Local, antes de chegar ao handler.
func SoLocal(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !Local(r) {
			recusarNaoLocal(w)
			return
		}
		h.ServeHTTP(w, r)
	})
}

func recusarNaoLocal(w http.ResponseWriter) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	w.Write([]byte(`{"type":"error","error":{"type":"llm_dlp_error","message":"llm-dlp: só atende programas desta máquina, pelo endereço 127.0.0.1."}}` + "\n"))
}

// Prova: o que só quem lê a chave do llm-dlp consegue calcular para um uso ("ultima", ou
// "saude" mais o desafio de quem pergunta). Não é a chave nem deixa chegar nela, e é separada
// dos pseudônimos pelo prefixo.
func Prova(chave []byte, uso string) string {
	h := hmac.New(sha256.New, chave)
	h.Write([]byte("llm-dlp/porta-local/v1\x00" + uso))
	return hex.EncodeToString(h.Sum(nil))
}

// ProvaConfere compara em tempo constante.
func ProvaConfere(chave []byte, uso, prova string) bool {
	return len(chave) > 0 && hmac.Equal([]byte(prova), []byte(Prova(chave, uso)))
}

// CabecalhoProva: o cabeçalho com que "llm-dlp ultima" se apresenta.
const CabecalhoProva = "X-Llm-Dlp-Prova"

// desafioValido: 16 a 64 dígitos hexadecimais (o llm-dlp não assina texto livre).
func desafioValido(d string) bool {
	if len(d) < 16 || len(d) > 64 {
		return false
	}
	for _, c := range d {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

// limpo: s sem caracteres de controle, para o log (um caminho com quebra de linha não forja
// outra linha).
func limpo(s string) string {
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return '?'
		}
		return r
	}, s)
}

// Limpo: como limpo, para quem escreve no mesmo log fora deste pacote.
func Limpo(s string) string { return limpo(s) }
