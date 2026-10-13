package main

import (
	"archive/tar"
	"bufio"
	"cmp"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/yurivenancio30/llm-dlp/internal/config"
)

// Comandos atualizar e recarregar: põem no ar outro binário (ou o mesmo, com a configuração
// nova) sem tirar o llm-dlp do ar. Como a troca funciona: troca.go.

const urlReleases = "https://github.com/yurivenancio30/llm-dlp/releases"

// primeiraEmIngles: a primeira versão com os comandos e os campos do config.json em inglês.
const primeiraEmIngles = "0.9.0"

var reVersaoExata = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

func atualizar(args []string) error {
	var pedida, arquivo string
	for i := 0; i < len(args); i++ {
		switch a := args[i]; a {
		case "--version", "--file", "--versao", "--arquivo": // (--versao e --arquivo: nomes antigos)
			if i+1 >= len(args) {
				return fmt.Errorf("%s precisa de um valor", a)
			}
			i++
			if a == "--file" || a == "--arquivo" {
				arquivo = args[i]
			} else {
				pedida = strings.TrimPrefix(args[i], "v")
			}
		default:
			return fmt.Errorf("opção desconhecida: %q (uso: llm-dlp update [--version X.Y.Z] [--file ARQ.tar.gz])", a)
		}
	}
	if os.Geteuid() == 0 {
		return errors.New("rode sem sudo: a instalação é do seu usuário")
	}
	destino := binarioInstalado()
	if st, err := os.Stat(destino); err != nil || !st.Mode().IsRegular() {
		return fmt.Errorf("o llm-dlp não está instalado em %s; para instalar, use o install.sh (README)", destino)
	}
	cfg, err := config.Carregar()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	// na pasta do destino: o binário novo entra no lugar com uma troca de nome (atômica)
	tmp, err := os.MkdirTemp(filepath.Dir(destino), ".llm-dlp-atualizar-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if arquivo == "" {
		v, err := versaoPublicada(urlReleases, pedida)
		if err != nil {
			return err
		}
		fmt.Printf("Baixando o llm-dlp %s (linux/%s)...\n", v, runtime.GOARCH)
		if arquivo, err = baixarRelease(urlReleases, v, tmp); err != nil {
			return err
		}
		fmt.Println("✓ checksum conferido")
	}
	novo := filepath.Join(tmp, "llm-dlp")
	if err := extrairBinario(arquivo, novo); err != nil {
		return err
	}
	vNovo, cNovo, err := versaoDe(novo)
	if err != nil {
		return fmt.Errorf("o binário novo não roda nesta máquina: %w", err)
	}
	vAtual, cAtual, _ := versaoDe(destino)
	info, noAr := saude(cfg)
	if info.SemProva {
		return errSemProva
	}
	mesmo := vNovo == vAtual && cNovo == cAtual
	if mesmo && (!noAr || info.Commit == cNovo && info.Versao == vNovo) {
		fmt.Printf("✓ já está na %s (%s)\n", vNovo, cNovo)
		home, _ := os.UserHomeDir()
		ajustarNomes(home, destino, vNovo)
		return nil
	}
	// uma versão anterior aos nomes em inglês ignoraria os campos do config.json com nome novo
	// (e deixaria de mascarar o que eles pedem): o arquivo volta aos nomes antigos antes de ela
	// subir. Indo para frente, os nomes novos entram só depois da troca (até lá, a versão no ar
	// pode precisar reler o arquivo).
	anterior := versaoMenor(vNovo, primeiraEmIngles)
	if anterior {
		if c, err := config.MigrarArquivo(true); err != nil {
			return fmt.Errorf("não consegui passar o config.json para os nomes da %s: %w", vNovo, err)
		} else if c != "" {
			fmt.Printf("✓ config.json com os nomes de campo da %s (o anterior em %s)\n", vNovo, c)
		}
	}
	bak := ""
	if !mesmo {
		if bak, err = trocarBinario(novo, destino); err != nil {
			return err
		}
		fmt.Printf("✓ %s (%s) → %s (%s) em %s\n", vAtual, cAtual, vNovo, cNovo, destino)
	}
	voltar := func() {
		if bak != "" {
			os.Rename(bak, destino)
		}
		if anterior {
			config.MigrarArquivo(false)
		}
	}
	log := config.Caminho("llm-dlp.log")
	home, _ := os.UserHomeDir()
	ajustarHooks := func() { ajustarNomes(home, destino, vNovo) }
	if !noAr {
		if !claudeLigado(home, cfg) {
			limparCopias(destino, bak)
			fmt.Println("– o llm-dlp não estava no ar: ele sobe já na versão nova quando for usado")
			fmt.Println("  (o Claude Code não passa pelo llm-dlp; para ligar: llm-dlp install)")
			return nil
		}
		if err := garantir(); err != nil {
			voltar()
			return fmt.Errorf("a versão nova não subiu (%v); voltei para a %s. Veja %s", err, vAtual, log)
		}
		limparCopias(destino, bak)
		ajustarHooks()
		fmt.Printf("✓ llm-dlp %s no ar\n", vNovo)
		return nil
	}
	if err := trocarServico(cfg, info, cNovo); err != nil {
		voltar()
		// uma versão sem troca já pode ter saído: sobe de novo, com o binário de volta
		if _, ok := saude(cfg); !ok {
			garantir()
		}
		return fmt.Errorf("%v; voltei para a %s. Veja %s", err, vAtual, log)
	}
	limparCopias(destino, bak)
	ajustarHooks()
	fmt.Printf("✓ llm-dlp %s no ar. Não precisa fechar o Claude Code.\n", vNovo)
	if !info.Troca {
		fmt.Println("  A versão anterior não sabia passar a vez: uma resposta que estivesse no meio foi cortada" +
			" (o Claude Code repete sozinho). As próximas atualizações não cortam nada.")
	}
	return nil
}

// ajustarNomes passa os hooks do Claude Code e o config.json para os nomes que a versão v (a
// do binário bin) conhece. Roda depois de pôr v no ar, e também quando ela já estava no ar: de
// 0.8.0 para cá com "llm-dlp atualizar", quem rodou foi o código da 0.8.0, que não sabia
// fazer isso (os nomes antigos seguem valendo; só não tinham sido trocados).
func ajustarNomes(home, bin, v string) {
	if mudou, err := hooksParaVersao(home, bin, v); err != nil {
		fmt.Printf("! não consegui ajustar os hooks do Claude Code (%v); rode: llm-dlp install\n", err)
	} else if mudou {
		fmt.Println("✓ hooks do Claude Code com os nomes desta versão (valem nas sessões novas;" +
			" as já abertas seguem com os de antes, que também funcionam)")
	}
	if !versaoMenor(v, primeiraEmIngles) {
		if c, err := config.MigrarArquivo(false); err != nil {
			fmt.Printf("! o config.json continua com os nomes antigos (%v); eles seguem valendo\n", err)
		} else if c != "" {
			fmt.Printf("✓ config.json com os nomes de campo em inglês (o anterior em %s)\n", c)
		}
	}
}

// recarregar põe no ar de novo o binário instalado, com a configuração como está agora, sem
// tirar o llm-dlp do ar.
func recarregar() error {
	cfg, err := config.Carregar()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	home, _ := os.UserHomeDir()
	info, ok := saude(cfg)
	if info.SemProva {
		return errSemProva
	}
	if !ok {
		if supervisorNoAr() > 0 {
			return errors.New("o llm-dlp no ar não responde na porta da configuração (a porta mudou?): " +
				"feche o Claude Code, rode llm-dlp stop e depois llm-dlp install")
		}
		if v, _, err := versaoDe(binarioInstalado()); err == nil {
			ajustarNomes(home, binarioInstalado(), v)
		}
		fmt.Println("– o llm-dlp não está no ar: ele sobe com a configuração nova quando for usado")
		return nil
	}
	v, commit, err := versaoDe(binarioServico())
	if err != nil {
		return err
	}
	if err := trocarServico(cfg, info, commit); err != nil {
		return fmt.Errorf("%v; o llm-dlp anterior continua no ar. Veja %s", err, config.Caminho("llm-dlp.log"))
	}
	ajustarNomes(home, binarioServico(), v)
	fmt.Println("✓ configuração aplicada; o llm-dlp não saiu do ar")
	return nil
}

// trocarServico põe no ar o binário instalado no lugar do proxy antigo (a saúde dele em
// antigo) e confere que é o novo (commit) quem responde. Ver troca.go.
func trocarServico(cfg config.Config, antigo infoSaude, commit string) error {
	sup := supervisorNoAr()
	if antigo.Troca && sup > 0 {
		// o supervisor segura a porta: ele mesmo troca o proxy e conta como foi
		estado := config.Caminho("troca.estado")
		os.Remove(estado)
		if err := syscall.Kill(sup, syscall.SIGHUP); err != nil {
			return err
		}
		r, ok := esperarConteudo(estado, prazoPronto+5*time.Second)
		if !ok {
			return errors.New("o supervisor não respondeu ao pedido de troca")
		}
		if motivo, falhou := strings.CutPrefix(r, "falhou: "); falhou {
			return errors.New("a versão nova não subiu: " + motivo)
		}
		if !esperarNovo(cfg, commit, antigo.PID, 5, nil) {
			return errors.New("a versão nova subiu mas não responde")
		}
		return nil
	}
	// versão anterior a esta (o supervisor não segura a porta): um supervisor novo espera a
	// porta; o antigo é parado, e a porta fica vazia só entre um fechar e o outro abrir
	novo := exec.Command(binarioServico(), "supervisionar", "--assumir")
	novo.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	devnull, _ := os.Open(os.DevNull)
	defer devnull.Close()
	novo.Stdin, novo.Stdout, novo.Stderr = devnull, devnull, devnull
	os.Remove(config.Caminho("supervisor.esperando"))
	if err := novo.Start(); err != nil {
		return err
	}
	saiu := make(chan struct{})
	go func() { novo.Wait(); close(saiu) }()
	desistir := func(motivo string) error {
		novo.Process.Signal(syscall.SIGTERM)
		select {
		case <-saiu:
		case <-time.After(5 * time.Second):
			novo.Process.Kill()
			<-saiu
		}
		return errors.New(motivo)
	}
	if !esperarArquivo(config.Caminho("supervisor.esperando"), saiu) {
		return desistir(fmt.Sprintf("a versão nova não ficou pronta em %s", prazoPronto))
	}
	// o supervisor antigo (senão ele sobe o proxy de novo); sem supervisor, o próprio proxy
	if alvo := cmp.Or(sup, antigo.PID); alvo > 0 {
		syscall.Kill(alvo, syscall.SIGTERM)
	}
	if !esperarNovo(cfg, commit, antigo.PID, 5, saiu) {
		return desistir("a versão nova não assumiu a porta")
	}
	return nil
}

// esperarConteudo: o conteúdo do arquivo p, quando ele aparecer (até prazo).
func esperarConteudo(p string, prazo time.Duration) (string, bool) {
	for limite := time.Now().Add(prazo); time.Now().Before(limite); time.Sleep(20 * time.Millisecond) {
		if b, err := os.ReadFile(p); err == nil && len(b) > 0 {
			return string(b), true
		}
	}
	return "", false
}

// esperarNovo: até prazoPronto, espera seguidas respostas de saúde, com prova, de um proxy com
// esse commit que não é o antigo. Desiste se o supervisor novo sair.
func esperarNovo(cfg config.Config, commit string, pidAntigo, seguidas int, saiu <-chan struct{}) bool {
	n := 0
	for limite := time.Now().Add(prazoPronto); time.Now().Before(limite); {
		select {
		case <-saiu:
			return false
		default:
		}
		if s, ok := saude(cfg); ok && s.Commit == commit && s.PID != pidAntigo {
			if n++; n >= seguidas {
				return true
			}
		} else {
			n = 0
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

func esperarArquivo(p string, saiu <-chan struct{}) bool {
	for limite := time.Now().Add(prazoPronto); time.Now().Before(limite); time.Sleep(10 * time.Millisecond) {
		select {
		case <-saiu:
			return false
		default:
		}
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

// versaoPublicada: a pedida, ou a mais nova (a página "latest" redireciona para
// .../tag/vX.Y.Z, sem API e sem limite de chamadas).
func versaoPublicada(base, pedida string) (string, error) {
	if pedida != "" {
		if !reVersaoExata.MatchString(pedida) {
			return "", fmt.Errorf("versão inválida: %q (formato X.Y.Z)", pedida)
		}
		return pedida, nil
	}
	c := &http.Client{Timeout: 30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	r, err := c.Get(base + "/latest")
	if err != nil {
		return "", fmt.Errorf("não consegui falar com o GitHub para achar a versão mais nova: %w", err)
	}
	r.Body.Close()
	v := strings.TrimPrefix(path.Base(r.Header.Get("Location")), "v")
	if !reVersaoExata.MatchString(v) {
		return "", fmt.Errorf("não achei a versão mais nova (o GitHub respondeu %s)", r.Status)
	}
	return v, nil
}

// baixarRelease baixa o pacote da versão para esta máquina e confere o SHA-256 com o
// checksums.txt publicado junto. Devolve o caminho do pacote.
func baixarRelease(base, v, dir string) (string, error) {
	if runtime.GOOS != "linux" {
		return "", errors.New("só há binário pronto para Linux (inclusive WSL); em outro sistema, compile do código (README)")
	}
	nome := fmt.Sprintf("llm-dlp_%s_linux_%s.tar.gz", v, runtime.GOARCH)
	url := base + "/download/v" + v + "/"
	arq := filepath.Join(dir, nome)
	if err := baixar(url+nome, arq, 200<<20); err != nil {
		return "", err
	}
	sums := filepath.Join(dir, "checksums.txt")
	if err := baixar(url+"checksums.txt", sums, 1<<20); err != nil {
		return "", err
	}
	esperado, err := checksumDe(sums, nome)
	if err != nil {
		return "", err
	}
	if real, err := sha256De(arq); err != nil {
		return "", err
	} else if real != esperado {
		return "", fmt.Errorf("checksum não confere para %s (esperado %s, veio %s): nada foi trocado", nome, esperado, real)
	}
	return arq, nil
}

func baixar(url, para string, max int64) error {
	c := &http.Client{Timeout: 5 * time.Minute}
	r, err := c.Get(url)
	if err != nil {
		return fmt.Errorf("não consegui baixar %s: %w", url, err)
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		return fmt.Errorf("não consegui baixar %s: %s", url, r.Status)
	}
	f, err := os.OpenFile(para, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	n, err := io.Copy(f, io.LimitReader(r.Body, max+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && n > max {
		err = fmt.Errorf("%s passou de %d bytes", url, max)
	}
	return err
}

func checksumDe(sums, nome string) (string, error) {
	f, err := os.Open(sums)
	if err != nil {
		return "", err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if c := strings.Fields(sc.Text()); len(c) == 2 && c[1] == nome {
			return c[0], nil
		}
	}
	return "", fmt.Errorf("%s não está no checksums.txt", nome)
}

func sha256De(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// extrairBinario tira o llm-dlp do pacote (.tar.gz) e grava em para, executável.
func extrairBinario(pacote, para string) error {
	f, err := os.Open(pacote)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("%s não é um .tar.gz: %w", pacote, err)
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return fmt.Errorf("o pacote %s não tem o llm-dlp", pacote)
		}
		if err != nil {
			return fmt.Errorf("pacote %s: %w", pacote, err)
		}
		if h.Typeflag != tar.TypeReg || path.Base(h.Name) != "llm-dlp" {
			continue
		}
		g, err := os.OpenFile(para, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return err
		}
		_, err = io.Copy(g, io.LimitReader(tr, 200<<20))
		if cerr := g.Close(); err == nil {
			err = cerr
		}
		return err
	}
}

var reSaidaVersao = regexp.MustCompile(`^llm-dlp (\S+) \(([^)]*)\)`)

// versaoDe roda "BIN versao" e devolve a versão e o commit.
func versaoDe(bin string) (string, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "versao").Output()
	if err != nil {
		return "", "", err
	}
	m := reSaidaVersao.FindStringSubmatch(strings.TrimSpace(string(out)))
	if m == nil {
		return "", "", fmt.Errorf("%s versao respondeu %q", bin, strings.TrimSpace(string(out)))
	}
	return m[1], m[2], nil
}

// trocarBinario põe novo no lugar de destino e guarda o anterior ao lado (destino.bak-DATA),
// para voltar se a versão nova não subir. Devolve o caminho da cópia.
func trocarBinario(novo, destino string) (string, error) {
	bak := destino + ".bak-" + time.Now().Format("20060102-150405")
	if err := os.Link(destino, bak); err != nil {
		if err := copiarArquivo(destino, bak, 0o755); err != nil {
			return "", err
		}
	}
	if err := os.Rename(novo, destino); err != nil {
		os.Remove(bak)
		return "", err
	}
	return bak, nil
}

// limparCopias apaga as cópias antigas do binário (destino.bak-*), menos a que acabou de ser
// feita: uma volta basta, e cada cópia tem uns 15 MB.
func limparCopias(destino, manter string) {
	if manter == "" {
		return
	}
	copias, _ := filepath.Glob(destino + ".bak-*")
	for _, c := range copias {
		if c != manter {
			os.Remove(c)
		}
	}
}
