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

// comSudo roda um comando como administrador, ligado ao terminal (o sudo pede a senha).
func comSudo(args ...string) error {
	c := exec.Command("sudo", args...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c.Run()
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
	fmt.Println(`Instalação do llm-dlp. São 5 passos; ele pergunta o que precisa e mostra o que vai mudar.
Em dois passos (OCR e trava) ele usa sudo: a sua senha será pedida nessa hora.`)

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
	mudou := false
	if len(cfg.DominiosInternos) == 0 {
		fmt.Println("\n      a) Domínios internos: um trecho do endereço dos servidores e e-mails da empresa.")
		fmt.Println("         Com \"empresa\", são mascarados mysql.empresa.intra e fulano@empresa.com.br.")
		r := in.perguntar("         Digite um ou mais, separados por vírgula (Enter para pular): ")
		for _, d := range strings.Split(r, ",") {
			if d = strings.TrimSpace(strings.ToLower(d)); d != "" {
				cfg.DominiosInternos = append(cfg.DominiosInternos, d)
				mudou = true
			}
		}
	}
	if len(cfg.Termos) == 0 {
		fmt.Println("\n      b) Nomes que nunca podem sair: o nome da empresa ou cliente, de projetos, de sistemas.")
		fmt.Println("         São mascarados em qualquer lugar, inclusive em nomes de pasta e arquivo.")
		r := in.perguntar("         Digite um ou mais, separados por vírgula (Enter para pular): ")
		var vs []string
		for _, v := range strings.Split(r, ",") {
			if v = strings.TrimSpace(v); v != "" {
				vs = append(vs, v)
			}
		}
		if len(vs) > 0 {
			cfg.Termos = append(cfg.Termos, config.Termo{Rotulo: "nome", Valores: vs})
			mudou = true
		}
	}
	if mudou {
		if err := salvarConfig(cfg); err != nil {
			return err
		}
	}
	if _, err := mask.Chave(config.Caminho("chave")); err != nil {
		return err
	}
	fmt.Printf("\n      ✓ configuração: %s (domínios internos: %d, nomes protegidos: %d)\n", config.Caminho("config.json"), len(cfg.DominiosInternos), contarTermos(cfg))
	fmt.Printf("      ✓ chave: %s — guarde uma cópia no seu gerenciador de senhas\n", config.Caminho("chave"))

	// 3. OCR
	passo(3, "OCR de imagens e PDFs", "com o OCR, prints e PDFs têm os dados sensíveis cobertos antes de sair. Sem ele, ficam bloqueados.")
	if falta := faltaOCR(cfg); falta != "" {
		cmd := ocr.ComandoInstalacao()
		fmt.Printf("      falta %s. Comando:  sudo sh -c '%s'\n", falta, cmd)
		if cmd != "" && !in.semPerg && in.confirmar("      Instalar agora? (pede a senha do sudo)") {
			if err := comSudo("sh", "-c", cmd); err != nil {
				fmt.Println("      ! a instalação falhou; rode o comando acima à mão e depois rode o llm-dlp instalar de novo")
			}
		}
		if falta := faltaOCR(cfg); falta != "" {
			fmt.Println("      ✗ OCR ausente: imagens e PDFs enviados ao Claude serão bloqueados (nada vaza)")
		} else {
			fmt.Println("      ✓ OCR instalado")
		}
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
		if v := versaoClaudeCode(); v != "" && versaoMenor(v, "2.1.285") {
			fmt.Printf("      ! seu Claude Code está na %s; a trava exige a 2.1.285 ou mais nova. Atualize e rode o llm-dlp instalar de novo.\n", v)
		} else if !in.semPerg && in.confirmar("      Instalar a trava agora? (pede a senha do sudo)") {
			if err := comSudo(destino, "instalar-trava"); err != nil {
				fmt.Println("      ! falhou; rode à mão:  sudo", destino, "instalar-trava")
			}
		}
	}

	// resumo
	cfg, _ = config.Carregar()
	fmt.Println("\nResumo")
	ok := func(b bool, sim, nao string) {
		if b {
			fmt.Println("  ✓", sim)
		} else {
			fmt.Println("  ✗", nao)
		}
	}
	ok(saudavel(cfg), "llm-dlp no ar", "llm-dlp fora do ar: rode llm-dlp garantir")
	ok(claudeLigado(home, cfg), "Claude Code passa pelo llm-dlp", "Claude Code NÃO passa pelo llm-dlp: rode o llm-dlp instalar de novo e aceite o passo 4")
	ok(faltaOCR(cfg) == "", "OCR de imagens e PDFs", "sem OCR: imagens e PDFs ficam bloqueados")
	ok(travaInstalada(endereco(cfg)), "trava instalada", "sem trava: rode o llm-dlp instalar de novo e aceite o passo 5")
	ok(len(cfg.DominiosInternos) > 0 || contarTermos(cfg) > 0, "domínios internos e nomes protegidos configurados",
		"nenhum domínio interno nem nome protegido: edite "+config.Caminho("config.json")+" (dominios_internos e termos)")
	fmt.Println("\nPara valer: feche e abra o Claude Code (no VS Code, recarregue a janela).")
	return nil
}

func contarTermos(cfg config.Config) int {
	n := 0
	for _, t := range cfg.Termos {
		n += len(t.Valores)
	}
	return n
}

// claudeLigado: o settings.json do Claude Code aponta para o llm-dlp?
func claudeLigado(home string, cfg config.Config) bool {
	s, _, err := lerSettings(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		return false
	}
	env, _ := s["env"].(map[string]any)
	return env["ANTHROPIC_BASE_URL"] == "http://"+endereco(cfg)
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
		if len(fica) == 0 {
			delete(perms, "deny")
		} else {
			perms["deny"] = fica
		}
	}
	// não deixa restos vazios que não existiam antes da instalação
	for _, k := range []string{"env", "hooks"} {
		if m, ok := s[k].(map[string]any); ok && len(m) == 0 {
			delete(s, k)
		}
	}
	bak, err := gravarSettings(alvo, s, orig)
	if err != nil {
		return err
	}
	// O proxy NÃO é parado aqui: o Claude Code aplica o settings.json às sessões já abertas,
	// mas uma sessão aberta continua apontando para o proxy até ser reiniciada. Parar agora
	// deixaria essas sessões sem resposta (falha fechada).
	fmt.Println("✓ removido do Claude Code (backup em " + bak + ").")
	fmt.Println("  O llm-dlp continua no ar para as sessões que já estão abertas. Feche e abra o Claude Code")
	fmt.Println("  (ou o VS Code) e depois rode: llm-dlp parar")
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
