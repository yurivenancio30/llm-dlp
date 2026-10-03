package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yurivenancio30/llm-dlp/internal/config"
	"github.com/yurivenancio30/llm-dlp/internal/proxy"
)

// Modo emergência: libera o Claude sem máscara por tempo limitado (exige sudo).

// Modo emergência: quando o llm-dlp quebra, o Claude Code para de funcionar (falha
// fechada) e você fica sem ele justamente para consertar o llm-dlp. A emergência troca o
// proxy por um repasse SEM máscara, por tempo limitado.
//
// Só o administrador liga (sudo): o estado fica em dirEmergencia, que só o root escreve.
// O proxy roda como o usuário e apenas LÊ esse arquivo. Assim, nada que rode com o seu
// usuário — inclusive o agente, induzido por uma página maliciosa — consegue desligar a
// máscara. (Se o seu sudo não pede senha, essa barreira não existe.)
//
// O repasse não carrega config/chave/detectores: funciona mesmo se o que quebrou foi isso.

// dirEmergencia é fixo no binário de produção. Testes trocam via
// -ldflags "-X main.dirEmergencia=..." (de propósito NÃO é variável de ambiente: o agente
// poderia reiniciar o proxy apontando para uma pasta que ele mesmo controla).
var dirEmergencia = "/etc/llm-dlp"

const duracaoPadraoEmergencia = 30 * time.Minute

type estadoEmergencia struct {
	Ate time.Time `json:"ate"`
}

func arquivoEmergencia() string { return filepath.Join(dirEmergencia, "emergencia.json") }

func emergenciaAtiva() (time.Time, bool) {
	b, err := os.ReadFile(arquivoEmergencia())
	if err != nil {
		return time.Time{}, false
	}
	var e estadoEmergencia
	if json.Unmarshal(b, &e) != nil || time.Now().After(e.Ate) {
		return time.Time{}, false
	}
	return e.Ate, true
}

func emergencia(args []string) error {
	if os.Geteuid() != 0 {
		return errors.New("o modo emergência desliga a máscara, então só o administrador liga. Rode: sudo llm-dlp emergencia [30m|sair]")
	}
	if len(args) > 0 && args[0] == "sair" {
		if err := os.Remove(arquivoEmergencia()); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		fmt.Println("modo emergência encerrado; em até 2 segundos o proxy normal (com máscara) volta.")
		return nil
	}
	dur := duracaoPadraoEmergencia
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
    Para sair antes: sudo llm-dlp emergencia sair

Digite LIBERAR para continuar: `, dur)
	linha, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	if strings.TrimSpace(linha) != "LIBERAR" {
		return errors.New("cancelado")
	}
	if err := os.MkdirAll(dirEmergencia, 0o755); err != nil {
		return err
	}
	b, _ := json.Marshal(estadoEmergencia{Ate: time.Now().Add(dur)})
	if err := os.WriteFile(arquivoEmergencia(), b, 0o644); err != nil {
		return err
	}
	fmt.Printf("ligado até %s (em até 2 segundos o repasse sem máscara assume). Depois disso, o proxy normal volta sozinho.\n",
		time.Now().Add(dur).Format("15:04"))
	return nil
}

// vigiarEmergencia encerra o processo quando o modo muda (o supervisor sobe o modo certo).
func vigiarEmergencia(emEmergencia bool) {
	for range time.Tick(2 * time.Second) {
		if _, ativa := emergenciaAtiva(); ativa != emEmergencia {
			os.Exit(0)
		}
	}
}

// servirEmergencia: repasse mínimo, sem máscara, até o prazo ou até o arquivo sumir.
func servirEmergencia(ate time.Time) error {
	cfg, err := config.Carregar()
	if err != nil {
		cfg = config.Padrao() // a config pode ser justamente o que quebrou
	}
	alvo, err := url.Parse(cfg.Upstream)
	if err != nil {
		alvo, _ = url.Parse(config.Padrao().Upstream)
	}
	lg := abrirLog()
	rp := httputil.NewSingleHostReverseProxy(alvo)
	rp.FlushInterval = -1 // streaming sem atraso
	dir := rp.Director
	rp.Director = func(r *http.Request) {
		dir(r)
		r.Host = alvo.Host
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/__llm-dlp/saude", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "servico": "llm-dlp", "modo": "emergencia",
			"ate": ate.Format(time.RFC3339), "versao": proxy.Versao, "pid": os.Getpid()})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		lg.Printf("EMERGÊNCIA (sem máscara): %s %s", r.Method, r.URL.Path)
		rp.ServeHTTP(w, r)
	})
	ln, err := net.Listen("tcp", endereco(cfg))
	if err != nil {
		return err
	}
	lg.Printf("EMERGÊNCIA: repasse sem máscara no ar até %s", ate.Local().Format("15:04"))
	go vigiarEmergencia(true)
	return (&http.Server{Handler: mux, ReadHeaderTimeout: 30 * time.Second}).Serve(ln)
}
