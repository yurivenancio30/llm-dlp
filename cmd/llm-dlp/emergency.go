package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yurivenancio30/llm-dlp/internal/config"
	"github.com/yurivenancio30/llm-dlp/internal/proxy"
	"github.com/yurivenancio30/llm-dlp/internal/version"
)

// Emergency mode: lets Claude through without masking for a limited time (requires sudo).

// Emergency mode: when llm-dlp breaks, Claude Code stops working (fail closed) and you are left
// without it precisely to fix llm-dlp. Emergency replaces the proxy with a pass-through WITHOUT
// masking, for a limited time.
//
// Only the administrator turns it on (sudo): the state lives in dirEmergencia, which only root
// writes. The proxy runs as the user and only READS that file. So nothing running as your user —
// including the agent, induced by a malicious page — can turn emergency on. (If your sudo does not
// ask for a password, this barrier does not exist. The configuration does not have this barrier:
// whoever changes it to remove protection gets a warning on every message, see reducedProtection.)
//
// The pass-through does not load config/key/detectors: it works even if that is what broke.

// dirEmergencia is fixed in the production binary. Tests change it via -ldflags "-X
// main.dirEmergencia=..." (on purpose it is NOT an environment variable: the agent could restart
// the proxy pointing to a folder it controls).
var dirEmergencia = "/etc/llm-dlp"

const defaultEmergencyDuration = 30 * time.Minute

type emergencyState struct {
	Until time.Time `json:"ate"`
}

func emergencyFile() string { return filepath.Join(dirEmergencia, "emergencia.json") }

func emergencyActive() (time.Time, bool) {
	b, err := os.ReadFile(emergencyFile())
	if err != nil {
		return time.Time{}, false
	}
	var e emergencyState
	if json.Unmarshal(b, &e) != nil || time.Now().After(e.Until) {
		return time.Time{}, false
	}
	return e.Until, true
}

func emergency(args []string) error {
	if os.Geteuid() != 0 {
		return errors.New("o modo emergência desliga a máscara, então só o administrador liga. Rode: sudo llm-dlp emergency [30m|off]")
	}
	if len(args) > 0 && (args[0] == "off" || args[0] == "sair") {
		if err := os.Remove(emergencyFile()); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		fmt.Println("modo emergência encerrado; em até 2 segundos o proxy normal (com máscara) volta.")
		return nil
	}
	dur := defaultEmergencyDuration
	for _, a := range args {
		d, err := time.ParseDuration(a)
		if err != nil || d <= 0 || d > 4*time.Hour {
			return fmt.Errorf("duração inválida %q (use, p.ex., 15m ou 1h; máximo 4h)", a)
		}
		dur = d
	}
	if st, err := os.Stdin.Stat(); err != nil || st.Mode()&os.ModeCharDevice == 0 {
		return errors.New("rode num terminal (a confirmação é digitada)")
	}
	fmt.Printf(`
⚠️  MODO EMERGÊNCIA: por %s, as mensagens irão para a API SEM MÁSCARA.
    Use uma sessão NOVA do Claude Code, sem dados de cliente, só para consertar o llm-dlp.
    Para sair antes: sudo llm-dlp emergency off

Digite LIBERAR para continuar: `, dur)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	if strings.TrimSpace(line) != "LIBERAR" {
		return errors.New("cancelado")
	}
	if err := os.MkdirAll(dirEmergencia, 0o755); err != nil {
		return err
	}
	b, _ := json.Marshal(emergencyState{Until: time.Now().Add(dur)})
	if err := os.WriteFile(emergencyFile(), b, 0o644); err != nil {
		return err
	}
	fmt.Printf("ligado até %s (em até 2 segundos o repasse sem máscara assume). Depois disso, o proxy normal volta sozinho.\n",
		time.Now().Add(dur).Format("15:04"))
	return nil
}

// watchEmergency ends the process when the mode changes (the supervisor starts the right mode).
func watchEmergency(inEmergency bool) {
	for range time.Tick(2 * time.Second) {
		if _, active := emergencyActive(); active != inEmergency {
			os.Exit(0)
		}
	}
}

// serveEmergency: minimal pass-through, without masking, until the deadline or until the file
// disappears.
func serveEmergency(until time.Time) error {
	cfg, err := config.Load()
	if err != nil {
		cfg = config.Default() // the config may be exactly what broke
	}
	ln, borrowed, err := servicePort(address(cfg))
	if err != nil {
		return err
	}
	target, err := url.Parse(cfg.Upstream)
	if err != nil {
		target, _ = url.Parse(config.Default().Upstream)
	}
	lg := openLog()
	rp := httputil.NewSingleHostReverseProxy(target)
	rp.FlushInterval = -1 // streaming without delay
	dir := rp.Director
	rp.Director = func(r *http.Request) {
		dir(r)
		r.Host = target.Host
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/__llm-dlp/saude", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "servico": "llm-dlp", "modo": "emergencia",
			"ate": until.Format(time.RFC3339), "versao": proxy.Version, "commit": version.Commit, "pid": os.Getpid(),
			"troca": borrowed})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		lg.Printf("EMERGÊNCIA (sem máscara): %s %s", r.Method, proxy.Clean(r.URL.Path))
		rp.ServeHTTP(w, r)
	})
	lg.Printf("EMERGÊNCIA: repasse sem máscara no ar até %s", until.Local().Format("15:04"))
	go watchEmergency(true)
	return serveUntilSignal(&http.Server{Handler: proxy.LocalOnly(mux), ReadHeaderTimeout: 30 * time.Second}, ln, lg, nil)
}
