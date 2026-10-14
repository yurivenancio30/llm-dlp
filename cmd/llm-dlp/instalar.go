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
	"github.com/yurivenancio30/llm-dlp/internal/version"
)

// Commands instalar, desinstalar e a trava (managed-settings).

const lockFile = "/etc/claude-code/managed-settings.json"

// regras que o llm-dlp acrescenta nas permissões do Claude Code
var denyLLMDLP = []string{
	"Read(~/.config/llm-dlp/**)",    // a chave e os hashes nunca são lidos pelo agente
	"Edit(~/.config/llm-dlp/**)",    // nem alterados
	"Bash(*.config/llm-dlp*)",       // nem por um comando (a configuração decide o que é mascarado e para onde vai)
	"Bash(*llm-dlp emergency*)",     // o agente não tenta ligar a emergência (a barreira real é o sudo)
	"Bash(*llm-dlp emergencia*)",    // (o nome antigo do comando continua aceito)
	"Bash(*managed-settings.json*)", // nem mexer na trava
}

// hooksLLMDLP: o comando de cada hook do Claude Code. oldHooks: os nomes de antes da 0.9.0,
// que o install troca pelos novos e o uninstall também reconhece.
var (
	hooksLLMDLP = map[string]string{"SessionStart": "ensure", "UserPromptSubmit": "check"}
	oldHooks    = map[string]string{"SessionStart": "garantir", "UserPromptSubmit": "verificar"}
)

// isLLMDLPHook: o grupo de hooks (em JSON) chama o llm-dlp, com o nome novo ou o antigo?
func isLLMDLPHook(group []byte) bool {
	for _, m := range []map[string]string{hooksLLMDLP, oldHooks} {
		for _, action := range m {
			if strings.Contains(string(group), "llm-dlp "+action) {
				return true
			}
		}
	}
	return false
}

type entry struct {
	r          *bufio.Reader
	noQuestion bool // --yes: responde "sim" a tudo (para scripts)
}

func (e entry) askIt(msg string) string {
	if e.noQuestion {
		return ""
	}
	fmt.Print(msg)
	l, _ := e.r.ReadString('\n')
	return strings.TrimSpace(l)
}

func (e entry) confirm(msg string) bool {
	if e.noQuestion {
		return true
	}
	r := strings.ToLower(e.askIt(msg + " [s/N] "))
	return r == "s" || r == "sim" || r == "y"
}

func step(n int, title, because string) {
	fmt.Printf("\n[%d/5] %s\n      %s\n", n, title, because)
}

// withSudo roda um comando como administrador, ligado ao terminal (o sudo pede a senha).
func withSudo(args ...string) error {
	c := exec.Command("sudo", args...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c.Run()
}

// instalar faz tudo, explicando cada passo. Pode ser rodado de novo sem estragar nada.
func install(args []string) error {
	in := entry{r: bufio.NewReader(os.Stdin)}
	for _, a := range args {
		if a == "--yes" || a == "--sim" {
			in.noQuestion = true
		}
	}
	if os.Geteuid() == 0 {
		return errors.New("rode sem sudo (a instalação é do seu usuário; o único passo com sudo é a trava, no fim)")
	}
	home, _ := os.UserHomeDir()
	fmt.Println(`Instalação do llm-dlp. São 5 passos; ele pergunta o que precisa e mostra o que vai mudar.
Em dois passos (OCR e trava) ele usa sudo: a sua senha será pedida nessa hora.`)

	// 1. programa
	step(1, "Programa", "copia o llm-dlp para ~/.local/bin, para você e o Claude Code poderem chamá-lo de qualquer pasta.")
	destination := filepath.Join(home, ".local", "bin", "llm-dlp")
	me, _ := os.Executable()
	if me, _ = filepath.EvalSymlinks(me); me != destination {
		if err := copyFile(me, destination, 0o755); err != nil {
			return err
		}
		fmt.Println("      ✓ copiado para", destination)
	} else {
		fmt.Println("      ✓ já está em", destination)
	}
	if !strings.Contains(":"+os.Getenv("PATH")+":", ":"+filepath.Dir(destination)+":") {
		fmt.Printf("      ! %s não está no seu PATH. Acrescente ao ~/.bashrc:  export PATH=\"$HOME/.local/bin:$PATH\"\n", filepath.Dir(destination))
	}

	// 2. configuração e chave
	step(2, "Configuração e chave", "cria "+config.Dir()+" com a configuração e a chave secreta que gera os pseudônimos.")
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	changed := false
	if len(cfg.InternalDomains) == 0 {
		fmt.Println("\n      a) Domínio interno: o trecho que aparece no nome dos servidores da empresa.")
		fmt.Println("         Com \"empresa\", mysql-prd.empresa.intra vira mysql-x7k2.invalid. Sem isso, nomes de")
		fmt.Println("         servidor saem como estão. (E-mails são mascarados sempre, não precisa informar.)")
		r := in.askIt("         Digite um ou mais, separados por vírgula (Enter para pular): ")
		for _, d := range strings.Split(r, ",") {
			if d = strings.TrimSpace(strings.ToLower(d)); d != "" {
				cfg.InternalDomains = append(cfg.InternalDomains, d)
				changed = true
			}
		}
	}
	if len(cfg.Terms) == 0 {
		fmt.Println("\n      b) Nomes que nunca podem sair: o nome da empresa ou cliente, de projetos, de sistemas.")
		fmt.Println("         São mascarados em qualquer lugar, inclusive em nomes de pasta e arquivo.")
		r := in.askIt("         Digite um ou mais, separados por vírgula (Enter para pular): ")
		var vs []string
		for _, v := range strings.Split(r, ",") {
			if v = strings.TrimSpace(v); v != "" {
				vs = append(vs, v)
			}
		}
		if len(vs) > 0 {
			cfg.Terms = append(cfg.Terms, config.Term{Label: "nome", Values: vs})
			changed = true
		}
	}
	if changed {
		if err := saveConfig(cfg); err != nil {
			return err
		}
	}
	if _, err := mask.Key(config.Path("chave")); err != nil {
		return err
	}
	fmt.Printf("\n      ✓ configuração: %s (domínios internos: %d, nomes protegidos: %d)\n", config.Path("config.json"), len(cfg.InternalDomains), countTerms(cfg))
	fmt.Printf("      ✓ chave: %s — guarde uma cópia no seu gerenciador de senhas\n", config.Path("chave"))

	// 3. OCR
	step(3, "OCR de imagens e PDFs", "com o OCR, prints e PDFs têm os dados sensíveis cobertos antes de sair. Sem ele, não são enviados (o Claude recebe um aviso no lugar).")
	if missingOne := missingOCR(cfg); missingOne != "" {
		cmd := ocr.InstallCommand()
		fmt.Printf("      falta %s. Comando:  sudo sh -c '%s'\n", missingOne, cmd)
		if cmd != "" && !in.noQuestion && in.confirm("      Instalar agora? (pede a senha do sudo)") {
			if err := withSudo("sh", "-c", cmd); err != nil {
				fmt.Println("      ! a instalação falhou; rode o comando acima à mão e depois rode o llm-dlp install de novo")
			}
		}
		if missingOne := missingOCR(cfg); missingOne != "" {
			fmt.Println("      ✗ OCR ausente: imagens e PDFs não serão enviados ao Claude (nada vaza; ele recebe um aviso no lugar)")
		} else {
			fmt.Println("      ✓ OCR instalado")
		}
	} else {
		fmt.Println("      ✓ tesseract (português) e poppler encontrados")
	}

	// 4. Claude Code
	step(4, "Claude Code", "só para quem usa o Claude Code (com o Gemini CLI, o Codex, o opencode ou o Copilot, responda n:\n      eles são apontados à mão, ver o README). Faz o Claude Code passar pelo llm-dlp. Muda o\n      ~/.claude/settings.json (com backup):")
	fmt.Printf(`        • ANTHROPIC_BASE_URL=http://%s  → as mensagens passam pelo llm-dlp
        • ao abrir uma sessão: "llm-dlp ensure"   → sobe o llm-dlp se não estiver no ar
        • a cada mensagem:     "llm-dlp check"  → barra a mensagem se algo estiver errado
        • proíbe o agente de ler/editar ~/.config/llm-dlp e de mexer na emergência e na trava
`, address(cfg))
	if in.confirm("      Aplicar?") {
		bak, err := applyClaude(home, destination, cfg)
		if err != nil {
			return err
		}
		if bak != "" {
			fmt.Println("      ✓ aplicado; backup em", bak)
		} else {
			fmt.Println("      ✓ aplicado")
		}
		if err := ensure(); errors.Is(err, errNoProof) {
			fmt.Println("      ! há outro llm-dlp no ar (de antes desta instalação): feche o Claude Code e rode \"llm-dlp stop\" para este assumir")
		} else if err != nil {
			return err
		} else if info, _ := health(cfg); info.Commit != version.Commit || info.Version != version.Version {
			// uma versão anterior no ar: troca por esta sem tirar o llm-dlp do ar
			if err := swapService(cfg, info, version.Commit); err != nil {
				fmt.Printf("      ! a versão anterior (%s) continua no ar: %v. Tente: llm-dlp update\n", info.Version, err)
			} else {
				fmt.Printf("      ✓ llm-dlp no ar (a %s no lugar da %s, sem sair do ar)\n", version.Version, info.Version)
			}
		} else {
			fmt.Println("      ✓ llm-dlp no ar")
		}
	} else {
		fmt.Println("      – pulado (se você usa o Claude Code, ele NÃO está protegido)")
	}
	withClaude := claudeEnabled(home, cfg)

	// 5. trava
	step(5, "Trava contra vazamento silencioso (recomendada)",
		"se a configuração do passo 4 for apagada, o Claude Code iria direto para a API sem máscara.\n      A trava faz o Claude Code se recusar a funcionar fora do llm-dlp. Fica numa pasta do sistema,\n      que o agente não consegue alterar — por isso precisa de sudo:")
	if lockInstalled(address(cfg)) {
		fmt.Println("      ✓ trava já instalada")
	} else if !withClaude {
		fmt.Println("      – pulado: a trava é do Claude Code, que não foi ligado no passo 4")
	} else {
		if v := claudeCodeVersion(); v != "" && minorVersion(v, "2.1.285") {
			fmt.Printf("      ! seu Claude Code está na %s; a trava exige a 2.1.285 ou mais nova. Atualize e rode o llm-dlp install de novo.\n", v)
		} else if !in.noQuestion && in.confirm("      Instalar a trava agora? (pede a senha do sudo)") {
			if err := withSudo(destination, "instalar-trava"); err != nil {
				fmt.Println("      ! falhou; rode à mão:  sudo", destination, "instalar-trava")
			}
		}
	}

	// resumo
	cfg, _ = config.Load()
	fmt.Println("\nResumo")
	ok := func(b bool, yes, negated string) {
		if b {
			fmt.Println("  ✓", yes)
		} else {
			fmt.Println("  ✗", negated)
		}
	}
	nothing := func(msg string) { fmt.Println("  –", msg) }
	switch {
	case healthy(cfg):
		fmt.Println("  ✓ llm-dlp no ar")
	case withClaude:
		ok(false, "", "llm-dlp fora do ar: veja "+config.Path("llm-dlp.log"))
	default:
		nothing("llm-dlp ainda não está no ar: com outro cliente, ele sobe com llm-dlp ensure (o README mostra como deixar isso automático)")
	}
	if withClaude {
		ok(true, "Claude Code passa pelo llm-dlp", "")
	} else {
		nothing("Claude Code não ligado (passo 4 pulado). Se você usa o Claude Code, rode o llm-dlp install de novo e aceite o passo 4")
	}
	ok(missingOCR(cfg) == "", "OCR de imagens e PDFs", "sem OCR: imagens e PDFs não são enviados (para instalar: "+ocr.InstallHint()+")")
	switch {
	case lockInstalled(address(cfg)):
		fmt.Println("  ✓ trava instalada")
	case withClaude:
		ok(false, "", "sem trava: rode o llm-dlp install de novo e aceite o passo 5")
	default:
		nothing("sem trava (ela é só do Claude Code)")
	}
	if len(cfg.InternalDomains) > 0 || countTerms(cfg) > 0 {
		fmt.Println("  ✓ domínios internos e nomes protegidos configurados")
	} else {
		fmt.Println("  – sem domínio interno nem nomes protegidos (opcional: rode o llm-dlp install de novo para informar)")
	}
	if withClaude {
		fmt.Println("\nPara valer: feche e abra o Claude Code (no VS Code, recarregue a janela).")
	}
	fmt.Println(`
Outros clientes, em beta (Gemini CLI, Codex, opencode, Copilot com a sua chave): esta
instalação não os aponta para o llm-dlp nem os trava. Falta pôr o endereço do llm-dlp no
cliente; ele fica protegido enquanto apontar para cá. Como fazer, cliente por cliente: README,
seção "Gemini CLI, Codex, opencode e Copilot (beta)".`)
	return nil
}

func countTerms(cfg config.Config) int {
	n := 0
	for _, t := range cfg.Terms {
		n += len(t.Values)
	}
	return n
}

// claudeEnabled: o settings.json do Claude Code aponta para o llm-dlp?
func claudeEnabled(home string, cfg config.Config) bool {
	s, _, err := readSettings(filepath.Join(home, ".claude", "settings.json"))
	if err != nil {
		return false
	}
	env, _ := s["env"].(map[string]any)
	return env["ANTHROPIC_BASE_URL"] == "http://"+address(cfg)
}

func uninstall(args []string) error {
	home, _ := os.UserHomeDir()
	target := filepath.Join(home, ".claude", "settings.json")
	s, orig, err := readSettings(target)
	if err != nil {
		return err
	}
	if env, ok := s["env"].(map[string]any); ok {
		if u, _ := env["ANTHROPIC_BASE_URL"].(string); strings.HasPrefix(u, "http://127.0.0.1:") {
			delete(env, "ANTHROPIC_BASE_URL")
		}
	}
	stripHooks(s)
	if perms, ok := s["permissions"].(map[string]any); ok {
		deny, _ := perms["deny"].([]any)
		var stays []any
		for _, d := range deny {
			ours := false
			for _, r := range denyLLMDLP {
				if d == r {
					ours = true
				}
			}
			if !ours {
				stays = append(stays, d)
			}
		}
		if len(stays) == 0 {
			delete(perms, "deny")
		} else {
			perms["deny"] = stays
		}
	}
	// não deixa restos vazios que não existiam antes da instalação
	for _, k := range []string{"env", "hooks"} {
		if m, ok := s[k].(map[string]any); ok && len(m) == 0 {
			delete(s, k)
		}
	}
	bak, err := writeSettings(target, s, orig)
	if err != nil {
		return err
	}
	// O proxy NÃO é parado aqui: o Claude Code aplica o settings.json às sessões já abertas,
	// mas uma sessão aberta continua apontando para o proxy até ser reiniciada. Parar agora
	// deixaria essas sessões sem resposta (falha fechada).
	fmt.Println("✓ removido do Claude Code (backup em " + bak + ").")
	fmt.Println("  O llm-dlp continua no ar para as sessões que já estão abertas. Feche e abra o Claude Code")
	fmt.Println("  (ou o VS Code) e depois rode: llm-dlp stop")
	cfg, _ := config.Load()
	if lockInstalled(address(cfg)) {
		fmt.Println("! A trava continua instalada: sem o llm-dlp, o Claude Code vai se recusar a funcionar. Remova com:")
		fmt.Println("      sudo llm-dlp uninstall-lock")
	}
	fmt.Println("A pasta", config.Dir(), "(config e chave) foi mantida; apague à mão se quiser.")
	return nil
}

// installLock grava allowedProviders + ANTHROPIC_BASE_URL no managed-settings (root).
func installLock(remove bool) error {
	if os.Geteuid() != 0 {
		return errors.New("precisa de sudo (o arquivo da trava fica numa pasta do sistema)")
	}
	port := 8787
	if u := os.Getenv("SUDO_USER"); u != "" { // usa a porta configurada pelo usuário
		if uu, err := user.Lookup(u); err == nil {
			if b, err := os.ReadFile(filepath.Join(uu.HomeDir, ".config", "llm-dlp", "config.json")); err == nil {
				var c config.Config // (com os nomes novos ou os antigos)
				if config.Read(b, &c) == nil && c.Port > 0 {
					port = c.Port
				}
			}
		}
	}
	if err := os.MkdirAll(filepath.Dir(lockFile), 0o755); err != nil {
		return err
	}
	bak, err := applyLock(lockFile, port, remove)
	if err != nil {
		return err
	}
	if remove {
		fmt.Println("✓ trava removida de", lockFile)
	} else {
		if bak != "" {
			bak = " (backup em " + bak + ")"
		}
		fmt.Printf("✓ trava instalada em %s%s\n  O Claude Code só funcionará passando pelo llm-dlp em 127.0.0.1:%d.\n", lockFile, bak, port)
	}
	return nil
}

// applyLock acrescenta (ou remove) a trava num managed-settings.json, preservando o resto.
func applyLock(path string, port int, remove bool) (string, error) {
	s, orig, err := readSettings(path)
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
	if remove {
		delete(s, "allowedProviders")
		delete(env, "ANTHROPIC_BASE_URL")
		var stays []any
		for _, d := range deny {
			ours := false
			for _, r := range denyLLMDLP {
				ours = ours || d == r
			}
			if !ours {
				stays = append(stays, d)
			}
		}
		deny = stays
	} else {
		s["allowedProviders"] = []any{"customEndpoint"}
		env["ANTHROPIC_BASE_URL"] = fmt.Sprintf("http://127.0.0.1:%d", port)
		for _, r := range denyLLMDLP {
			has := false
			for _, d := range deny {
				has = has || d == r
			}
			if !has {
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
	return writeSettingsMode(path, s, orig, 0o644)
}

func applyClaude(home, bin string, cfg config.Config) (string, error) {
	target := filepath.Join(home, ".claude", "settings.json")
	s, orig, err := readSettings(target)
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
	sub("env")["ANTHROPIC_BASE_URL"] = "http://" + address(cfg)
	// os hooks do llm-dlp que já estiverem lá (com o nome novo ou o antigo) saem e entram de
	// novo: nunca ficam dois
	stripHooks(s)
	byHooks(s, bin, hooksLLMDLP)
	perms := sub("permissions")
	deny, _ := perms["deny"].([]any)
	for _, r := range denyLLMDLP {
		has := false
		for _, d := range deny {
			has = has || d == r
		}
		if !has {
			deny = append(deny, r)
		}
	}
	perms["deny"] = deny
	return writeSettings(target, s, orig)
}

// stripHooks tira do settings.json os hooks do llm-dlp (com o nome novo ou o antigo).
func stripHooks(s map[string]any) {
	hooks, ok := s["hooks"].(map[string]any)
	if !ok {
		return
	}
	for ev, v := range hooks {
		list, _ := v.([]any)
		var stays []any
		for _, g := range list {
			if b, _ := json.Marshal(g); !isLLMDLPHook(b) {
				stays = append(stays, g)
			}
		}
		if len(stays) == 0 {
			delete(hooks, ev)
		} else {
			hooks[ev] = stays
		}
	}
}

// byHooks acrescenta os hooks do llm-dlp, com os nomes de comando dados.
func byHooks(s map[string]any, bin string, names map[string]string) {
	hooks, ok := s["hooks"].(map[string]any)
	if !ok {
		hooks = map[string]any{}
		s["hooks"] = hooks
	}
	for _, event := range []string{"SessionStart", "UserPromptSubmit"} {
		list, _ := hooks[event].([]any)
		// timeout explícito: hook que estoura o tempo é ignorado pelo Claude Code (falharia aberto)
		hooks[event] = append(list, map[string]any{"hooks": []any{
			map[string]any{"type": "command", "command": bin + " " + names[event], "timeout": timeoutHook}}})
	}
}

// hooksForVersion: depois de trocar o binário pela versão v, os hooks do Claude Code passam a
// chamar os nomes de comando que ela conhece: os em inglês desde a 0.9.0, os antigos antes
// (voltar para uma versão anterior com os nomes novos barraria cada mensagem com "comando
// desconhecido"). Só mexe no settings.json se ele já chama o llm-dlp e os nomes mudam.
// Devolve se mudou.
func hooksForVersion(home, bin, v string) (bool, error) {
	target := filepath.Join(home, ".claude", "settings.json")
	s, orig, err := readSettings(target)
	if err != nil {
		return false, err
	}
	names := hooksLLMDLP
	if minorVersion(v, firstInEnglish) {
		names = oldHooks
	}
	b, _ := json.Marshal(s["hooks"])
	if !isLLMDLPHook(b) || strings.Contains(string(b), bin+" "+names["UserPromptSubmit"]+`"`) {
		return false, nil
	}
	stripHooks(s)
	byHooks(s, bin, names)
	_, err = writeSettings(target, s, orig)
	return err == nil, err
}

func readSettings(target string) (map[string]any, []byte, error) {
	s := map[string]any{}
	orig, err := os.ReadFile(target)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, nil, err
	}
	if len(strings.TrimSpace(string(orig))) > 0 {
		if err := json.Unmarshal(orig, &s); err != nil {
			return nil, nil, fmt.Errorf("%s não é JSON válido: %w", target, err)
		}
	}
	return s, orig, nil
}

func writeSettings(target string, s map[string]any, orig []byte) (string, error) {
	return writeSettingsMode(target, s, orig, 0o600)
}

func writeSettingsMode(target string, s map[string]any, orig []byte, mode os.FileMode) (string, error) {
	bak := ""
	if len(orig) > 0 {
		// nome único: nunca sobrescreve um backup anterior (duas execuções no mesmo segundo)
		base := target + ".bak-" + time.Now().Format("20060102-150405")
		bak = base
		for i := 2; ; i++ {
			f, err := os.OpenFile(bak, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
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
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", err
	}
	newOne, _ := json.MarshalIndent(s, "", "  ")
	return bak, os.WriteFile(target, append(newOne, '\n'), mode)
}

func saveConfig(cfg config.Config) error {
	b, _ := json.MarshalIndent(cfg, "", "  ")
	return os.WriteFile(config.Path("config.json"), append(b, '\n'), 0o600)
}

func copyFile(de, to string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return err
	}
	f, err := os.Open(de)
	if err != nil {
		return err
	}
	defer f.Close()
	tmp := to + ".novo"
	g, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
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
	return os.Rename(tmp, to) // troca atômica (funciona mesmo com o binário antigo rodando)
}

func missingOCR(cfg config.Config) string {
	var missingOne []string
	if _, err := exec.LookPath(cfg.OCR.Tesseract); err != nil {
		missingOne = append(missingOne, "tesseract")
	} else if out, err := exec.Command(cfg.OCR.Tesseract, "--list-langs").CombinedOutput(); err != nil || !strings.Contains(string(out), "\npor") {
		missingOne = append(missingOne, "o idioma português do tesseract")
	}
	if _, err := exec.LookPath(cfg.OCR.PDFToText); err != nil {
		missingOne = append(missingOne, "poppler (pdftotext)")
	}
	return strings.Join(missingOne, ", ")
}

func lockInstalled(address string) bool {
	b, err := os.ReadFile(lockFile)
	if err != nil {
		return false
	}
	var s struct {
		Allowed []string          `json:"allowedProviders"`
		Env     map[string]string `json:"env"`
	}
	return json.Unmarshal(b, &s) == nil && len(s.Allowed) == 1 && s.Allowed[0] == "customEndpoint" &&
		s.Env["ANTHROPIC_BASE_URL"] == "http://"+address
}

var reVersion = regexp.MustCompile(`\d+\.\d+\.\d+`)

// claudeCodeVersion: a do CLI no PATH ou a maior entre as extensões do VS Code.
func claudeCodeVersion() string {
	if out, err := exec.Command("claude", "--version").Output(); err == nil {
		return reVersion.FindString(string(out))
	}
	home, _ := os.UserHomeDir()
	best := ""
	for _, d := range []string{".vscode-server", ".vscode", ".cursor-server"} {
		ms, _ := filepath.Glob(filepath.Join(home, d, "extensions", "anthropic.claude-code-*"))
		for _, m := range ms {
			if v := reVersion.FindString(filepath.Base(m)); v != "" && (best == "" || minorVersion(best, v)) {
				best = v
			}
		}
	}
	return best
}

func minorVersion(a, b string) bool {
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
