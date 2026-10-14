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

// Who can talk to the local port.
//
// The port only listens on 127.0.0.1, but that is not enough: the browser also reaches it. A
// malicious page makes its own domain point to 127.0.0.1 (DNS rebinding) and starts reading the
// responses as if they were its own, including the last conversation (/__llm-dlp/ultima). And, on a
// shared machine, another user reaches the port the same way. Three barriers:
//
//   - Local: only serves whoever calls through the local address and is not a web page;
//   - the last conversation only goes out with the proof of whoever reads the key (the owner of
//     the llm-dlp folder);
//   - llm-dlp proves it is itself (Proof) to whoever asks whether it is up: another program that
//     takes the port cannot pass for it, and does not receive the conversation without masking.

func localAddress(h string) bool {
	return h == "127.0.0.1" || h == "localhost" || h == "::1"
}

// Local: the request came from a program on the machine talking to the local address, and not from
// a page open in the browser. In rebinding the Host is the attacker's domain; a page from another
// site sends its own Origin.
//
// It only refuses what a local program never sends: Claude Code calls through 127.0.0.1 (the hook
// requires it) and has no site Origin. Other browser signals (Origin "null", Sec-Fetch-Site) are
// not used: a client that sent them would stop working, and what they would block in addition (a
// blind request, without reading the response and without the credential) neither reads nor sends
// anything.
func Local(r *http.Request) bool {
	h := r.Host
	if hh, _, err := net.SplitHostPort(h); err == nil {
		h = hh
	}
	if !localAddress(strings.Trim(h, "[]")) {
		return false
	}
	if u, err := url.Parse(r.Header.Get("Origin")); err == nil && (u.Scheme == "http" || u.Scheme == "https") && !localAddress(u.Hostname()) {
		return false
	}
	return true
}

// LocalOnly refuses (403) what does not pass Local, before it reaches the handler.
func LocalOnly(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !Local(r) {
			refuseNonLocal(w)
			return
		}
		h.ServeHTTP(w, r)
	})
}

func refuseNonLocal(w http.ResponseWriter) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	w.Write([]byte(`{"type":"error","error":{"type":"llm_dlp_error","message":"llm-dlp: only serves programs on this machine, at the address 127.0.0.1."}}` + "\n"))
}

// Proof: what only whoever reads the llm-dlp key can compute for a use ("ultima", or "saude" plus
// the challenge of whoever asks). It is not the key nor leads to it, and it is separated from the
// pseudonyms by the prefix.
func Proof(key []byte, usage string) string {
	h := hmac.New(sha256.New, key)
	h.Write([]byte("llm-dlp/porta-local/v1\x00" + usage))
	return hex.EncodeToString(h.Sum(nil))
}

// ProofMatches compares in constant time.
func ProofMatches(key []byte, usage, proof string) bool {
	return len(key) > 0 && hmac.Equal([]byte(proof), []byte(Proof(key, usage)))
}

// ProofHeader: the header "llm-dlp last" presents itself with.
const ProofHeader = "X-Llm-Dlp-Prova"

// validChallenge: 16 to 64 hexadecimal digits (llm-dlp does not sign free text).
func validChallenge(d string) bool {
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

// cleaned: s without control characters, for the log (a path with a line break does not forge
// another line).
func cleaned(s string) string {
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

// Clean: like cleaned, for whoever writes to the same log outside this package.
func Clean(s string) string { return cleaned(s) }
