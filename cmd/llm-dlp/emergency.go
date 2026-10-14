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

// Mode emergência: libera o Claude sem máscara por tempo limitado (exige sudo).

// Mode emergência: quando o llm-dlp quebra, o Claude Code para de funcionar (falha
// fechada) e você fica sem ele justamente para consertar o llm-dlp. A emergência troca o
// proxy por um repasse SEM máscara, por tempo limitado.
//
// Só o administrador liga (sudo): o estado fica em dirEmergencia, que só o root escreve.
// O proxy roda como o usuário e apenas LÊ esse arquivo. Assim, nada que rode com o seu
// usuário — inclusive o agente, induzido por uma página maliciosa — consegue ligar a
// emergência. (Se o seu sudo não pede senha, essa barreira não existe. A configuração não tem
// essa barreira: quem a muda para tirar proteção gera um aviso a cada mensagem, ver
// reducedProtection.)
//
// O repasse não carrega config/chave/detectores: funciona mesmo se o que quebrou foi isso.

// dirEmergencia é fixo no binário de produção. Testes trocam via
// -ldflags "-X main.dirEmergencia=..." (de propósito NÃO é variável de ambiente: o agente
// poderia reiniciar o proxy apontando para uma pasta que ele mesmo controla).
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

// watchEmergency encerra o processo quando o modo muda (o supervisor sobe o modo certo).
func watchEmergency(inEmergency bool) {
	for range time.Tick(2 * time.Second) {
		if _, active := emergencyActive(); active != inEmergency {
			os.Exit(0)
		}
	}
}

// serveEmergency: repasse mínimo, sem máscara, até o prazo ou até o arquivo sumir.
func serveEmergency(until time.Time) error {
	cfg, err := config.Load()
	if err != nil {
		cfg = config.Default() // a config pode ser justamente o que quebrou
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
	rp.FlushInterval = -1 // streaming sem atraso
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
