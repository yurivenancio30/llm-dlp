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

// Commands atualizar e recarregar: põem no ar outro binário (ou o mesmo, com a configuração
// nova) sem tirar o llm-dlp do ar. Como a troca funciona: troca.go.

const urlReleases = "https://github.com/yurivenancio30/llm-dlp/releases"

// firstInEnglish: a primeira versão com os comandos e os campos do config.json em inglês.
const firstInEnglish = "0.9.0"

var reExactVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

func update(args []string) error {
	var asked, fileName string
	for i := 0; i < len(args); i++ {
		switch a := args[i]; a {
		case "--version", "--file", "--versao", "--arquivo": // (--versao e --arquivo: nomes antigos)
			if i+1 >= len(args) {
				return fmt.Errorf("%s precisa de um valor", a)
			}
			i++
			if a == "--file" || a == "--arquivo" {
				fileName = args[i]
			} else {
				asked = strings.TrimPrefix(args[i], "v")
			}
		default:
			return fmt.Errorf("opção desconhecida: %q (uso: llm-dlp update [--version X.Y.Z] [--file ARQ.tar.gz])", a)
		}
	}
	if os.Geteuid() == 0 {
		return errors.New("rode sem sudo: a instalação é do seu usuário")
	}
	destination := installedBinary()
	if st, err := os.Stat(destination); err != nil || !st.Mode().IsRegular() {
		return fmt.Errorf("o llm-dlp não está instalado em %s; para instalar, use o install.sh (README)", destination)
	}
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	// na pasta do destino: o binário novo entra no lugar com uma troca de nome (atômica)
	tmp, err := os.MkdirTemp(filepath.Dir(destination), ".llm-dlp-atualizar-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if fileName == "" {
		v, err := publishedVersion(urlReleases, asked)
		if err != nil {
			return err
		}
		fmt.Printf("Baixando o llm-dlp %s (linux/%s)...\n", v, runtime.GOARCH)
		if fileName, err = downloadRelease(urlReleases, v, tmp); err != nil {
			return err
		}
		fmt.Println("✓ checksum conferido")
	}
	newOne := filepath.Join(tmp, "llm-dlp")
	if err := extractBinary(fileName, newOne); err != nil {
		return err
	}
	vNew, cNew, err := versionOf(newOne)
	if err != nil {
		return fmt.Errorf("o binário novo não roda nesta máquina: %w", err)
	}
	vCurrent, cCurrent, _ := versionOf(destination)
	info, isUp := health(cfg)
	if info.WithoutProof {
		return errNoProof
	}
	same := vNew == vCurrent && cNew == cCurrent
	if same && (!isUp || info.Commit == cNew && info.Version == vNew) {
		fmt.Printf("✓ já está na %s (%s)\n", vNew, cNew)
		home, _ := os.UserHomeDir()
		adjustNames(home, destination, vNew)
		return nil
	}
	// uma versão anterior aos nomes em inglês ignoraria os campos do config.json com nome novo
	// (e deixaria de mascarar o que eles pedem): o arquivo volta aos nomes antigos antes de ela
	// subir. Indo para frente, os nomes novos entram só depois da troca (até lá, a versão no ar
	// pode precisar reler o arquivo).
	previous := minorVersion(vNew, firstInEnglish)
	if previous {
		if c, err := config.MigrateFile(true); err != nil {
			return fmt.Errorf("não consegui passar o config.json para os nomes da %s: %w", vNew, err)
		} else if c != "" {
			fmt.Printf("✓ config.json com os nomes de campo da %s (o anterior em %s)\n", vNew, c)
		}
	}
	bak := ""
	if !same {
		if bak, err = swapBinary(newOne, destination); err != nil {
			return err
		}
		fmt.Printf("✓ %s (%s) → %s (%s) em %s\n", vCurrent, cCurrent, vNew, cNew, destination)
	}
	goBack := func() {
		if bak != "" {
			os.Rename(bak, destination)
		}
		if previous {
			config.MigrateFile(false)
		}
	}
	log := config.Path("llm-dlp.log")
	home, _ := os.UserHomeDir()
	adjustHooks := func() { adjustNames(home, destination, vNew) }
	if !isUp {
		if !claudeEnabled(home, cfg) {
			cleanCopies(destination, bak)
			fmt.Println("– o llm-dlp não estava no ar: ele sobe já na versão nova quando for usado")
			fmt.Println("  (o Claude Code não passa pelo llm-dlp; para ligar: llm-dlp install)")
			return nil
		}
		if err := ensure(); err != nil {
			goBack()
			return fmt.Errorf("a versão nova não subiu (%v); voltei para a %s. Veja %s", err, vCurrent, log)
		}
		cleanCopies(destination, bak)
		adjustHooks()
		fmt.Printf("✓ llm-dlp %s no ar\n", vNew)
		return nil
	}
	if err := swapService(cfg, info, cNew); err != nil {
		goBack()
		// uma versão sem troca já pode ter saído: sobe de novo, com o binário de volta
		if _, ok := health(cfg); !ok {
			ensure()
		}
		return fmt.Errorf("%v; voltei para a %s. Veja %s", err, vCurrent, log)
	}
	cleanCopies(destination, bak)
	adjustHooks()
	fmt.Printf("✓ llm-dlp %s no ar. Não precisa fechar o Claude Code.\n", vNew)
	if !info.Swap {
		fmt.Println("  A versão anterior não sabia passar a vez: uma resposta que estivesse no meio foi cortada" +
			" (o Claude Code repete sozinho). As próximas atualizações não cortam nada.")
	}
	return nil
}

// adjustNames passa os hooks do Claude Code e o config.json para os nomes que a versão v (a
// do binário bin) conhece. Roda depois de pôr v no ar, e também quando ela já estava no ar: de
// 0.8.0 para cá com "llm-dlp atualizar", quem rodou foi o código da 0.8.0, que não sabia
// fazer isso (os nomes antigos seguem valendo; só não tinham sido trocados).
func adjustNames(home, bin, v string) {
	if changed, err := hooksForVersion(home, bin, v); err != nil {
		fmt.Printf("! não consegui ajustar os hooks do Claude Code (%v); rode: llm-dlp install\n", err)
	} else if changed {
		fmt.Println("✓ hooks do Claude Code com os nomes desta versão (valem nas sessões novas;" +
			" as já abertas seguem com os de antes, que também funcionam)")
	}
	if !minorVersion(v, firstInEnglish) {
		if c, err := config.MigrateFile(false); err != nil {
			fmt.Printf("! o config.json continua com os nomes antigos (%v); eles seguem valendo\n", err)
		} else if c != "" {
			fmt.Printf("✓ config.json com os nomes de campo em inglês (o anterior em %s)\n", c)
		}
	}
}

// recarregar põe no ar de novo o binário instalado, com a configuração como está agora, sem
// tirar o llm-dlp do ar.
func reload() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	home, _ := os.UserHomeDir()
	info, ok := health(cfg)
	if info.WithoutProof {
		return errNoProof
	}
	if !ok {
		if supervisorUp() > 0 {
			return errors.New("o llm-dlp no ar não responde na porta da configuração (a porta mudou?): " +
				"feche o Claude Code, rode llm-dlp stop e depois llm-dlp install")
		}
		if v, _, err := versionOf(installedBinary()); err == nil {
			adjustNames(home, installedBinary(), v)
		}
		fmt.Println("– o llm-dlp não está no ar: ele sobe com a configuração nova quando for usado")
		return nil
	}
	v, commit, err := versionOf(serviceBinary())
	if err != nil {
		return err
	}
	if err := swapService(cfg, info, commit); err != nil {
		return fmt.Errorf("%v; o llm-dlp anterior continua no ar. Veja %s", err, config.Path("llm-dlp.log"))
	}
	adjustNames(home, serviceBinary(), v)
	fmt.Println("✓ configuração aplicada; o llm-dlp não saiu do ar")
	return nil
}

// swapService põe no ar o binário instalado no lugar do proxy antigo (a saúde dele em
// antigo) e confere que é o novo (commit) quem responde. Ver troca.go.
func swapService(cfg config.Config, old healthInfo, commit string) error {
	sup := supervisorUp()
	if old.Swap && sup > 0 {
		// o supervisor segura a porta: ele mesmo troca o proxy e conta como foi
		state := config.Path("troca.estado")
		os.Remove(state)
		if err := syscall.Kill(sup, syscall.SIGHUP); err != nil {
			return err
		}
		r, ok := waitContent(state, readyDeadline+5*time.Second)
		if !ok {
			return errors.New("o supervisor não respondeu ao pedido de troca")
		}
		if reason, failed := strings.CutPrefix(r, "falhou: "); failed {
			return errors.New("a versão nova não subiu: " + reason)
		}
		if !waitNew(cfg, commit, old.PID, 5, nil) {
			return errors.New("a versão nova subiu mas não responde")
		}
		return nil
	}
	// versão anterior a esta (o supervisor não segura a porta): um supervisor novo espera a
	// porta; o antigo é parado, e a porta fica vazia só entre um fechar e o outro abrir
	newOne := exec.Command(serviceBinary(), "supervisionar", "--assumir")
	newOne.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	devnull, _ := os.Open(os.DevNull)
	defer devnull.Close()
	newOne.Stdin, newOne.Stdout, newOne.Stderr = devnull, devnull, devnull
	os.Remove(config.Path("supervisor.esperando"))
	if err := newOne.Start(); err != nil {
		return err
	}
	wentOut := make(chan struct{})
	go func() { newOne.Wait(); close(wentOut) }()
	giveUp := func(reason string) error {
		newOne.Process.Signal(syscall.SIGTERM)
		select {
		case <-wentOut:
		case <-time.After(5 * time.Second):
			newOne.Process.Kill()
			<-wentOut
		}
		return errors.New(reason)
	}
	if !waitFile(config.Path("supervisor.esperando"), wentOut) {
		return giveUp(fmt.Sprintf("a versão nova não ficou pronta em %s", readyDeadline))
	}
	// o supervisor antigo (senão ele sobe o proxy de novo); sem supervisor, o próprio proxy
	if target := cmp.Or(sup, old.PID); target > 0 {
		syscall.Kill(target, syscall.SIGTERM)
	}
	if !waitNew(cfg, commit, old.PID, 5, wentOut) {
		return giveUp("a versão nova não assumiu a porta")
	}
	return nil
}

// waitContent: o conteúdo do arquivo p, quando ele aparecer (até prazo).
func waitContent(p string, deadline time.Duration) (string, bool) {
	for limitN := time.Now().Add(deadline); time.Now().Before(limitN); time.Sleep(20 * time.Millisecond) {
		if b, err := os.ReadFile(p); err == nil && len(b) > 0 {
			return string(b), true
		}
	}
	return "", false
}

// waitNew: até readyDeadline, espera seguidas respostas de saúde, com prova, de um proxy com
// esse commit que não é o antigo. Desiste se o supervisor novo sair.
func waitNew(cfg config.Config, commit string, oldPid, consecutive int, wentOut <-chan struct{}) bool {
	n := 0
	for limitN := time.Now().Add(readyDeadline); time.Now().Before(limitN); {
		select {
		case <-wentOut:
			return false
		default:
		}
		if s, ok := health(cfg); ok && s.Commit == commit && s.PID != oldPid {
			if n++; n >= consecutive {
				return true
			}
		} else {
			n = 0
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

func waitFile(p string, wentOut <-chan struct{}) bool {
	for limitN := time.Now().Add(readyDeadline); time.Now().Before(limitN); time.Sleep(10 * time.Millisecond) {
		select {
		case <-wentOut:
			return false
		default:
		}
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

// publishedVersion: a pedida, ou a mais nova (a página "latest" redireciona para
// .../tag/vX.Y.Z, sem API e sem limite de chamadas).
func publishedVersion(base, asked string) (string, error) {
	if asked != "" {
		if !reExactVersion.MatchString(asked) {
			return "", fmt.Errorf("versão inválida: %q (formato X.Y.Z)", asked)
		}
		return asked, nil
	}
	c := &http.Client{Timeout: 30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	r, err := c.Get(base + "/latest")
	if err != nil {
		return "", fmt.Errorf("não consegui falar com o GitHub para achar a versão mais nova: %w", err)
	}
	r.Body.Close()
	v := strings.TrimPrefix(path.Base(r.Header.Get("Location")), "v")
	if !reExactVersion.MatchString(v) {
		return "", fmt.Errorf("não achei a versão mais nova (o GitHub respondeu %s)", r.Status)
	}
	return v, nil
}

// downloadRelease baixa o pacote da versão para esta máquina e confere o SHA-256 com o
// checksums.txt publicado junto. Devolve o caminho do pacote.
func downloadRelease(base, v, dir string) (string, error) {
	if runtime.GOOS != "linux" {
		return "", errors.New("só há binário pronto para Linux (inclusive WSL); em outro sistema, compile do código (README)")
	}
	name := fmt.Sprintf("llm-dlp_%s_linux_%s.tar.gz", v, runtime.GOARCH)
	url := base + "/download/v" + v + "/"
	file := filepath.Join(dir, name)
	if err := download(url+name, file, 200<<20); err != nil {
		return "", err
	}
	sums := filepath.Join(dir, "checksums.txt")
	if err := download(url+"checksums.txt", sums, 1<<20); err != nil {
		return "", err
	}
	expected, err := checksumOf(sums, name)
	if err != nil {
		return "", err
	}
	if real, err := sha256Of(file); err != nil {
		return "", err
	} else if real != expected {
		return "", fmt.Errorf("checksum não confere para %s (esperado %s, veio %s): nada foi trocado", name, expected, real)
	}
	return file, nil
}

func download(url, to string, max int64) error {
	c := &http.Client{Timeout: 5 * time.Minute}
	r, err := c.Get(url)
	if err != nil {
		return fmt.Errorf("não consegui baixar %s: %w", url, err)
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		return fmt.Errorf("não consegui baixar %s: %s", url, r.Status)
	}
	f, err := os.OpenFile(to, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
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

func checksumOf(sums, name string) (string, error) {
	f, err := os.Open(sums)
	if err != nil {
		return "", err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if c := strings.Fields(sc.Text()); len(c) == 2 && c[1] == name {
			return c[0], nil
		}
	}
	return "", fmt.Errorf("%s não está no checksums.txt", name)
}

func sha256Of(p string) (string, error) {
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

// extractBinary tira o llm-dlp do pacote (.tar.gz) e grava em para, executável.
func extractBinary(pkgName, to string) error {
	f, err := os.Open(pkgName)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("%s não é um .tar.gz: %w", pkgName, err)
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return fmt.Errorf("o pacote %s não tem o llm-dlp", pkgName)
		}
		if err != nil {
			return fmt.Errorf("pacote %s: %w", pkgName, err)
		}
		if h.Typeflag != tar.TypeReg || path.Base(h.Name) != "llm-dlp" {
			continue
		}
		g, err := os.OpenFile(to, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
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

var reVersionOutput = regexp.MustCompile(`^llm-dlp (\S+) \(([^)]*)\)`)

// versionOf roda "BIN versao" e devolve a versão e o commit.
func versionOf(bin string) (string, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "versao").Output()
	if err != nil {
		return "", "", err
	}
	m := reVersionOutput.FindStringSubmatch(strings.TrimSpace(string(out)))
	if m == nil {
		return "", "", fmt.Errorf("%s versao respondeu %q", bin, strings.TrimSpace(string(out)))
	}
	return m[1], m[2], nil
}

// swapBinary põe novo no lugar de destino e guarda o anterior ao lado (destino.bak-DATA),
// para voltar se a versão nova não subir. Devolve o caminho da cópia.
func swapBinary(newOne, destination string) (string, error) {
	bak := destination + ".bak-" + time.Now().Format("20060102-150405")
	if err := os.Link(destination, bak); err != nil {
		if err := copyFile(destination, bak, 0o755); err != nil {
			return "", err
		}
	}
	if err := os.Rename(newOne, destination); err != nil {
		os.Remove(bak)
		return "", err
	}
	return bak, nil
}

// cleanCopies apaga as cópias antigas do binário (destino.bak-*), menos a que acabou de ser
// feita: uma volta basta, e cada cópia tem uns 15 MB.
func cleanCopies(destination, keep string) {
	if keep == "" {
		return
	}
	copies, _ := filepath.Glob(destination + ".bak-*")
	for _, c := range copies {
		if c != keep {
			os.Remove(c)
		}
	}
}
