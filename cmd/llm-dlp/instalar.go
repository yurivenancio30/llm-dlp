package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/yurivenancio30/llm-dlp/internal/config"
	"github.com/yurivenancio30/llm-dlp/internal/mask"
	"github.com/yurivenancio30/llm-dlp/internal/ocr"
)

// Comandos instalar, desinstalar e a trava (managed-settings).

const arquivoTrava = "/etc/claude-code/managed-settings.json"

// regras que o llm-dlp acrescenta nas permissões do Claude Code
var denyLLMDLP = []string{
	"Read(~/.config/llm-dlp/**)",    // a chave e os hashes nunca são lidos pelo agente
	"Edit(~/.config/llm-dlp/**)",    // nem alterados
	"Bash(*llm-dlp emergencia*)",    // o agente não tenta ligar a emergência (a barreira real é o sudo)
	"Bash(*managed-settings.json*)", // nem mexer na trava
}

type entrada struct {
	r       *bufio.Reader
	semPerg bool // --sim: responde "sim" a tudo (para scripts)
}

func (e entrada) perguntar(msg string) string {
	if e.semPerg {
		return ""
	}
	fmt.Print(msg)
	l, _ := e.r.ReadString('\n')
	return strings.TrimSpace(l)
}

func (e entrada) confirmar(msg string) bool {
	if e.semPerg {
		return true
	}
	r := strings.ToLower(e.perguntar(msg + " [s/N] "))
	return r == "s" || r == "sim" || r == "y"
}

func passo(n int, titulo, porque string) {
	fmt.Printf("\n[%d/5] %s\n      %s\n", n, titulo, porque)
}

// instalar faz tudo, explicando cada passo. Pode ser rodado de novo sem estragar nada.
func instalar(args []string) error {
	in := entrada{r: bufio.NewReader(os.Stdin)}
	for _, a := range args {
		if a == "--sim" {
			in.semPerg = true
		}
	}
	if os.Geteuid() == 0 {
		return errors.New("rode sem sudo (a instalação é do seu usuário; o único passo com sudo é a trava, no fim)")
	}
	home, _ := os.UserHomeDir()

	// 1. programa
	passo(1, "Programa", "copia o llm-dlp para ~/.local/bin, para você e o Claude Code poderem chamá-lo de qualquer pasta.")
	destino := filepath.Join(home, ".local", "bin", "llm-dlp")
	eu, _ := os.Executable()
	if eu, _ = filepath.EvalSymlinks(eu); eu != destino {
		if err := copiarArquivo(eu, destino, 0o755); err != nil {
			return err
		}
		fmt.Println("      ✓ copiado para", destino)
	} else {
		fmt.Println("      ✓ já está em", destino)
	}
	if !strings.Contains(":"+os.Getenv("PATH")+":", ":"+filepath.Dir(destino)+":") {
		fmt.Printf("      ! %s não está no seu PATH. Acrescente ao ~/.bashrc:  export PATH=\"$HOME/.local/bin:$PATH\"\n", filepath.Dir(destino))
	}

	// 2. configuração e chave
	passo(2, "Configuração e chave", "cria "+config.Dir()+" com a configuração e a chave secreta que gera os pseudônimos.")
	cfg, err := config.Carregar()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	if len(cfg.DominiosInternos) == 0 {
		r := in.perguntar("      Domínios internos (hostnames e e-mails que contêm isto são mascarados), separados por vírgula.\n      Ex.: empresa,intranet  — ou Enter para pular: ")
		for _, d := range strings.Split(r, ",") {
			if d = strings.TrimSpace(strings.ToLower(d)); d != "" {
				cfg.DominiosInternos = append(cfg.DominiosInternos, d)
			}
		}
		if len(cfg.DominiosInternos) > 0 {
			if err := salvarConfig(cfg); err != nil {
				return err
			}
		}
	}
	if _, err := mask.Chave(config.Caminho("chave")); err != nil {
		return err
	}
	fmt.Printf("      ✓ configuração: %s (domínios internos: %v)\n", config.Caminho("config.json"), cfg.DominiosInternos)
	fmt.Printf("      ✓ chave: %s — guarde uma cópia no seu gerenciador de senhas\n", config.Caminho("chave"))

	// 3. OCR
	passo(3, "OCR de imagens e PDFs", "com o OCR, prints e PDFs têm os dados sensíveis cobertos antes de sair. Sem ele, ficam bloqueados.")
	if falta := faltaOCR(cfg); falta != "" {
		fmt.Printf("      ! falta %s. Para instalar (precisa de sudo, uma vez só):\n          %s\n", falta, ocr.DicaInstalacao())
		fmt.Println("        Até lá, imagens e PDFs enviados ao Claude serão bloqueados (nada vaza).")
	} else {
		fmt.Println("      ✓ tesseract (português) e poppler encontrados")
	}

	// 4. Claude Code
	passo(4, "Claude Code", "faz o Claude Code passar pelo llm-dlp. Muda o ~/.claude/settings.json (com backup):")
	fmt.Printf(`        • ANTHROPIC_BASE_URL=http://%s  → as mensagens passam pelo llm-dlp
        • ao abrir uma sessão: "llm-dlp garantir"   → sobe o llm-dlp se não estiver no ar
        • a cada mensagem:     "llm-dlp verificar"  → barra a mensagem se algo estiver errado
        • proíbe o agente de ler/editar ~/.config/llm-dlp e de mexer na emergência e na trava
`, endereco(cfg))
	if in.confirmar("      Aplicar?") {
		bak, err := aplicarClaude(home, destino, cfg)
		if err != nil {
			return err
		}
		if bak != "" {
			fmt.Println("      ✓ aplicado; backup em", bak)
		} else {
			fmt.Println("      ✓ aplicado")
		}
		if err := garantir(); err != nil {
			return err
		}
		fmt.Println("      ✓ llm-dlp no ar")
	} else {
		fmt.Println("      – pulado (o Claude Code NÃO está protegido)")
	}

	// 5. trava
	passo(5, "Trava contra vazamento silencioso (recomendada)",
		"se a configuração do passo 4 for apagada, o Claude Code iria direto para a API sem máscara.\n      A trava faz o Claude Code se recusar a funcionar fora do llm-dlp. Fica numa pasta do sistema,\n      que o agente não consegue alterar — por isso precisa de sudo:")
	if travaInstalada(endereco(cfg)) {
		fmt.Println("      ✓ trava já instalada")
	} else {
		fmt.Printf("          sudo %s instalar-trava\n", destino)
		if v := versaoClaudeCode(); v != "" && versaoMenor(v, "2.1.285") {
			fmt.Printf("      ! seu Claude Code está na %s; a trava exige a 2.1.285 ou mais nova — atualize antes.\n", v)
		}
	}
	fmt.Println("\nPronto. Feche e abra o Claude Code (ou comece uma sessão nova) para valer.")
	return nil
}

func desinstalar(args []string) error {
	home, _ := os.UserHomeDir()
	alvo := filepath.Join(home, ".claude", "settings.json")
	s, orig, err := lerSettings(alvo)
	if err != nil {
		return err
	}
	if env, ok := s["env"].(map[string]any); ok {
		if u, _ := env["ANTHROPIC_BASE_URL"].(string); strings.HasPrefix(u, "http://127.0.0.1:") {
			delete(env, "ANTHROPIC_BASE_URL")
		}
	}
	if hooks, ok := s["hooks"].(map[string]any); ok {
		for ev, v := range hooks {
			lista, _ := v.([]any)
			var fica []any
			for _, g := range lista {
				b, _ := json.Marshal(g)
				if !strings.Contains(string(b), "llm-dlp garantir") && !strings.Contains(string(b), "llm-dlp verificar") {
					fica = append(fica, g)
				}
			}
			if len(fica) == 0 {
				delete(hooks, ev)
			} else {
				hooks[ev] = fica
			}
		}
	}
	if perms, ok := s["permissions"].(map[string]any); ok {
		deny, _ := perms["deny"].([]any)
		var fica []any
		for _, d := range deny {
			nosso := false
			for _, r := range denyLLMDLP {
				if d == r {
					nosso = true
				}
			}
			if !nosso {
				fica = append(fica, d)
			}
		}
		perms["deny"] = fica
	}
	bak, err := gravarSettings(alvo, s, orig)
	if err != nil {
		return err
	}
	parar()
	fmt.Println("✓ removido do Claude Code (backup em " + bak + ") e llm-dlp parado.")
	cfg, _ := config.Carregar()
	if travaInstalada(endereco(cfg)) {
		fmt.Println("! A trava continua instalada: sem o llm-dlp, o Claude Code vai se recusar a funcionar. Remova com:")
		fmt.Println("      sudo llm-dlp desinstalar-trava")
	}
	fmt.Println("A pasta", config.Dir(), "(config e chave) foi mantida; apague à mão se quiser.")
	return nil
}

// instalarTrava grava allowedProviders + ANTHROPIC_BASE_URL no managed-settings (root).
func instalarTrava(remover bool) error {
	if os.Geteuid() != 0 {
		return errors.New("precisa de sudo (o arquivo da trava fica numa pasta do sistema)")
	}
	porta := 8787
	if u := os.Getenv("SUDO_USER"); u != "" { // usa a porta configurada pelo usuário
		if uu, err := user.Lookup(u); err == nil {
			if b, err := os.ReadFile(filepath.Join(uu.HomeDir, ".config", "llm-dlp", "config.json")); err == nil {
				var c struct {
					Porta int `json:"porta"`
				}
				if json.Unmarshal(b, &c) == nil && c.Porta > 0 {
					porta = c.Porta
				}
			}
		}
	}
	if err := os.MkdirAll(filepath.Dir(arquivoTrava), 0o755); err != nil {
		return err
	}
	bak, err := aplicarTrava(arquivoTrava, porta, remover)
	if err != nil {
		return err
	}
	if remover {
		fmt.Println("✓ trava removida de", arquivoTrava, "(backup em "+bak+")")
	} else {
		fmt.Printf("✓ trava instalada em %s (backup em %s)\n  O Claude Code só funcionará passando pelo llm-dlp em 127.0.0.1:%d.\n", arquivoTrava, bak, porta)
	}
	return nil
}

// aplicarTrava acrescenta (ou remove) a trava num managed-settings.json, preservando o resto.
func aplicarTrava(caminho string, porta int, remover bool) (string, error) {
	s, orig, err := lerSettings(caminho)
	if err != nil {
		return "", err
	}
	env, _ := s["env"].(map[string]any)
	if env == nil {
		env = map[string]any{}
	}
	perms, _ := s["permissions"].(map[string]any)
	if perms == nil {
		perms = map[string]any{}
	}
	deny, _ := perms["deny"].([]any)
	if remover {
		delete(s, "allowedProviders")
		delete(env, "ANTHROPIC_BASE_URL")
		var fica []any
		for _, d := range deny {
			nosso := false
			for _, r := range denyLLMDLP {
				nosso = nosso || d == r
			}
			if !nosso {
				fica = append(fica, d)
			}
		}
		deny = fica
	} else {
		s["allowedProviders"] = []any{"customEndpoint"}
		env["ANTHROPIC_BASE_URL"] = fmt.Sprintf("http://127.0.0.1:%d", porta)
		for _, r := range denyLLMDLP {
			tem := false
			for _, d := range deny {
				tem = tem || d == r
			}
			if !tem {
				deny = append(deny, r)
			}
		}
	}
	env2 := map[string]any(env)
	if len(env2) > 0 {
		s["env"] = env2
	} else {
		delete(s, "env")
	}
	if len(deny) > 0 {
		perms["deny"] = deny
		s["permissions"] = perms
	} else {
		delete(perms, "deny")
		if len(perms) == 0 {
			delete(s, "permissions")
		}
	}
	return gravarSettingsModo(caminho, s, orig, 0o644)
}

func aplicarClaude(home, bin string, cfg config.Config) (string, error) {
	alvo := filepath.Join(home, ".claude", "settings.json")
	s, orig, err := lerSettings(alvo)
	if err != nil {
		return "", err
	}
	sub := func(k string) map[string]any {
		if v, ok := s[k].(map[string]any); ok {
			return v
		}
		v := map[string]any{}
		s[k] = v
		return v
	}
	sub("env")["ANTHROPIC_BASE_URL"] = "http://" + endereco(cfg)
	hooks := sub("hooks")
	for evento, acao := range map[string]string{"SessionStart": "garantir", "UserPromptSubmit": "verificar"} {
		cmd := bin + " " + acao
		lista, _ := hooks[evento].([]any)
		tem := false
		for _, g := range lista {
			b, _ := json.Marshal(g)
			tem = tem || strings.Contains(string(b), cmd)
		}
		if !tem {
			// timeout explícito: hook que estoura o tempo é ignorado pelo Claude Code (falharia aberto)
			hooks[evento] = append(lista, map[string]any{"hooks": []any{
				map[string]any{"type": "command", "command": cmd, "timeout": timeoutHook}}})
		}
	}
	perms := sub("permissions")
	deny, _ := perms["deny"].([]any)
	for _, r := range denyLLMDLP {
		tem := false
		for _, d := range deny {
			tem = tem || d == r
		}
		if !tem {
			deny = append(deny, r)
		}
	}
	perms["deny"] = deny
	return gravarSettings(alvo, s, orig)
}

func lerSettings(alvo string) (map[string]any, []byte, error) {
	s := map[string]any{}
	orig, err := os.ReadFile(alvo)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, nil, err
	}
	if len(strings.TrimSpace(string(orig))) > 0 {
		if err := json.Unmarshal(orig, &s); err != nil {
			return nil, nil, fmt.Errorf("%s não é JSON válido: %w", alvo, err)
		}
	}
	return s, orig, nil
}

func gravarSettings(alvo string, s map[string]any, orig []byte) (string, error) {
	return gravarSettingsModo(alvo, s, orig, 0o600)
}

func gravarSettingsModo(alvo string, s map[string]any, orig []byte, modo os.FileMode) (string, error) {
	bak := ""
	if len(orig) > 0 {
		// nome único: nunca sobrescreve um backup anterior (duas execuções no mesmo segundo)
		base := alvo + ".bak-" + time.Now().Format("20060102-150405")
		bak = base
		for i := 2; ; i++ {
			f, err := os.OpenFile(bak, os.O_WRONLY|os.O_CREATE|os.O_EXCL, modo)
			if errors.Is(err, os.ErrExist) {
				bak = fmt.Sprintf("%s-%d", base, i)
				continue
			}
			if err != nil {
				return "", err
			}
			_, err = f.Write(orig)
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return "", err
			}
			break
		}
	}
	if err := os.MkdirAll(filepath.Dir(alvo), 0o755); err != nil {
		return "", err
	}
	novo, _ := json.MarshalIndent(s, "", "  ")
	return bak, os.WriteFile(alvo, append(novo, '\n'), modo)
}

func salvarConfig(cfg config.Config) error {
	b, _ := json.MarshalIndent(cfg, "", "  ")
	return os.WriteFile(config.Caminho("config.json"), append(b, '\n'), 0o600)
}

func copiarArquivo(de, para string, modo os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(para), 0o755); err != nil {
		return err
	}
	f, err := os.Open(de)
	if err != nil {
		return err
	}
	defer f.Close()
	tmp := para + ".novo"
	g, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, modo)
	if err != nil {
		return err
	}
	if _, err := io.Copy(g, f); err != nil {
		g.Close()
		return err
	}
	if err := g.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, para) // troca atômica (funciona mesmo com o binário antigo rodando)
}

func faltaOCR(cfg config.Config) string {
	var falta []string
	if _, err := exec.LookPath(cfg.OCR.Tesseract); err != nil {
		falta = append(falta, "tesseract")
	} else if out, err := exec.Command(cfg.OCR.Tesseract, "--list-langs").CombinedOutput(); err != nil || !strings.Contains(string(out), "\npor") {
		falta = append(falta, "o idioma português do tesseract")
	}
	if _, err := exec.LookPath(cfg.OCR.PDFToText); err != nil {
		falta = append(falta, "poppler (pdftotext)")
	}
	return strings.Join(falta, ", ")
}

func travaInstalada(endereco string) bool {
	b, err := os.ReadFile(arquivoTrava)
	if err != nil {
		return false
	}
	var s struct {
		Allowed []string          `json:"allowedProviders"`
		Env     map[string]string `json:"env"`
	}
	return json.Unmarshal(b, &s) == nil && len(s.Allowed) == 1 && s.Allowed[0] == "customEndpoint" &&
		s.Env["ANTHROPIC_BASE_URL"] == "http://"+endereco
}

var reVersao = regexp.MustCompile(`\d+\.\d+\.\d+`)

// versaoClaudeCode: a do CLI no PATH ou a maior entre as extensões do VS Code.
func versaoClaudeCode() string {
	if out, err := exec.Command("claude", "--version").Output(); err == nil {
		return reVersao.FindString(string(out))
	}
	home, _ := os.UserHomeDir()
	melhor := ""
	for _, d := range []string{".vscode-server", ".vscode", ".cursor-server"} {
		ms, _ := filepath.Glob(filepath.Join(home, d, "extensions", "anthropic.claude-code-*"))
		for _, m := range ms {
			if v := reVersao.FindString(filepath.Base(m)); v != "" && (melhor == "" || versaoMenor(melhor, v)) {
				melhor = v
			}
		}
	}
	return melhor
}

func versaoMenor(a, b string) bool {
	var x, y [3]int
	fmt.Sscanf(a, "%d.%d.%d", &x[0], &x[1], &x[2])
	fmt.Sscanf(b, "%d.%d.%d", &y[0], &y[1], &y[2])
	for i := 0; i < 3; i++ {
		if x[i] != y[i] {
			return x[i] < y[i]
		}
	}
	return false
}
