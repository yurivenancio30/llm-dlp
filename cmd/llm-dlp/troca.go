package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/yurivenancio30/llm-dlp/internal/config"
)

// O supervisor segura a porta; o proxy só a recebe emprestada.
//
// O supervisor abre a porta uma vez e a passa a cada proxy que sobe (como o systemd faz com
// socket activation). Se o proxy cai, a porta continua aberta: as conexões esperam na fila do
// kernel até o proxy seguinte, em vez de "Connection refused". O Claude Code manda requisições
// no meio de um turno (depois de uma ferramenta) sem passar pelo hook que sobe o llm-dlp, e uma
// porta vazia nessa hora era um erro na tela.
//
// Swap de versão (atualizar, recarregar): SIGHUP no supervisor. Ele sobe um proxy com o
// binário instalado, na mesma porta, e espera o aviso de que está pronto; aí manda o antigo
// sair (SIGUSR2: para de aceitar conexões e termina o que está em andamento) e se reexecuta com
// o binário instalado, herdando a porta, a trava e os dois proxies. Se o novo não ficar pronto,
// é parado, e o antigo segue. O resultado vai para troca.estado, que o atualizar lê.
//
// Uma versão anterior a esta (o supervisor dela não segura a porta nem conhece o SIGHUP) é
// trocada de outro jeito: um supervisor novo com --assumir espera a porta, o antigo é parado, e
// a porta fica vazia só os milissegundos entre um fechar e o outro abrir.

const (
	envPort    = "LLM_DLP_PORTA_FD"  // o proxy recebe a porta do supervisor no fd 3
	envReady   = "LLM_DLP_PRONTO_FD" // e avisa no fd 4 quando já atende
	envInherit = "LLM_DLP_HERANCA"   // supervisor reexecutado: "porta trava proxy drenando"

	stopDeadline = 3 * time.Second  // parar: o que der para terminar em poucos segundos (o parar espera 5)
	swapDeadline = 10 * time.Minute // troca: a versão nova já atende; as respostas longas terminam
	// graceDeadline: na troca, quanto as conexões já abertas ainda são atendidas pelo antigo. Os
	// clientes HTTP largam uma conexão parada antes disso (o do Node, em 4 s).
	graceDeadline = 6 * time.Second
	// readyDeadline: quanto o proxy novo tem para carregar a configuração e os registros
	readyDeadline = 20 * time.Second
)

// servicePort: a porta emprestada pelo supervisor ou, sem ele (llm-dlp serve à mão), uma
// aberta aqui. emprestada diz qual.
func servicePort(address string) (ln net.Listener, borrowed bool, err error) {
	if os.Getenv(envPort) == "" {
		ln, err = net.Listen("tcp", address)
		return ln, false, err
	}
	f := os.NewFile(3, "porta")
	ln, err = net.FileListener(f)
	f.Close()
	return ln, true, err
}

// notifyReady: diz ao supervisor que o proxy já atende (a troca espera por isso).
func notifyReady() {
	if os.Getenv(envReady) != "" {
		f := os.NewFile(4, "pronto")
		f.Write([]byte{1})
		f.Close()
	}
}

// serveUntilSignal atende até cair ou até um sinal. SIGTERM/SIGINT (parar): para de aceitar e dá
// alguns segundos às respostas em andamento. SIGUSR2 (troca): o proxy novo já atende, então sem
// pressa: para de aceitar conexões, mas o que ainda chegar pelas conexões já abertas (o Claude
// Code as reaproveita) é atendido com "Connection: close", e o cliente abre a próxima no novo;
// as que ficarem paradas só são fechadas depois de graceDeadline (fechar uma conexão parada no
// instante em que o cliente manda por ela faria a requisição falhar). No fim, fim() grava o que
// estiver pendente. Close a porta aqui não a tira do ar: ela é do supervisor.
func serveUntilSignal(srv *http.Server, ln net.Listener, lg *log.Logger, end func()) error {
	var exiting atomic.Bool
	h := srv.Handler
	srv.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if exiting.Load() {
			w.Header().Set("Connection", "close")
		}
		h.ServeHTTP(w, r)
	})
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT, syscall.SIGUSR2)
	defer signal.Stop(signals)
	fellDown := make(chan error, 1)
	go func() { fellDown <- srv.Serve(ln) }()
	notifyReady()
	select {
	case err := <-fellDown:
		return err
	case s := <-signals:
		deadline := stopDeadline
		if s == syscall.SIGUSR2 {
			deadline = swapDeadline
			exiting.Store(true)
			ln.Close()
			lg.Printf("saindo (troca de versão, pid %d): não aceita mais conexões; as abertas ainda são atendidas por %s", os.Getpid(), graceDeadline)
			time.Sleep(graceDeadline)
		}
		lg.Printf("saindo (%v, pid %d): terminando as respostas em andamento (até %s)", s, os.Getpid(), deadline)
		ctx, cancel := context.WithTimeout(context.Background(), deadline)
		defer cancel()
		// (a porta já fechada na troca não é erro)
		if err := srv.Shutdown(ctx); err != nil && !errors.Is(err, net.ErrClosed) {
			lg.Printf("saindo (pid %d): %v (respostas cortadas)", os.Getpid(), err)
		}
		if end != nil {
			end()
		}
		lg.Printf("saiu (pid %d)", os.Getpid())
		return nil
	}
}

// childProxy: um proxy que o supervisor subiu (ou herdou ao se reexecutar).
type childProxy struct {
	p   *os.Process
	end chan error
}

// startProxy sobe "servir" com a porta emprestada e devolve o lado de leitura do aviso de
// pronto. O nome antigo ("servir", não "serve"; "supervisionar" na reexecução e no garantir) é
// de propósito: o binário instalado pode ser de uma versão anterior aos nomes em inglês (quem
// voltou de versão), e toda versão entende o antigo. A porta vai como número cru (syscall.ForkExec), nunca por um *os.File: passar um
// *os.File a um processo (exec.Cmd.ExtraFiles) chama Fd(), que põe o socket em modo
// bloqueante, e o modo vale para todos os processos que o dividem. O proxy no ar ficaria
// preso no accept e não conseguiria mais sair.
func startProxy(bin string, port int, lg *log.Logger) (*childProxy, *os.File, error) {
	r, w, err := os.Pipe()
	if err != nil {
		return nil, nil, err
	}
	defer w.Close()
	devnull, err := os.Open(os.DevNull)
	if err != nil {
		r.Close()
		return nil, nil, err
	}
	defer devnull.Close()
	output := devnull.Fd()
	if f := logFile(lg); f != nil {
		output = f.Fd() // o log (arquivo comum: Fd() não muda nada)
	}
	var env []string
	for _, v := range os.Environ() {
		if !strings.HasPrefix(v, envInherit+"=") {
			env = append(env, v)
		}
	}
	env = append(env, envPort+"=3", envReady+"=4")
	pid, err := syscall.ForkExec(bin, []string{bin, "servir"}, &syscall.ProcAttr{
		Env:   env,
		Files: []uintptr{devnull.Fd(), output, output, uintptr(port), w.Fd()},
	})
	if err != nil {
		r.Close()
		return nil, nil, err
	}
	lg.Printf("supervisor: proxy iniciado (pid %d)", pid)
	return inheritProxy(pid), r, nil
}

// inheritProxy: espera por um proxy filho deste processo (subido aqui, ou pelo supervisor
// anterior antes de se reexecutar: continua filho).
func inheritProxy(pid int) *childProxy {
	p, _ := os.FindProcess(pid)
	f := &childProxy{p, make(chan error, 1)}
	go func() {
		st, err := p.Wait()
		if err == nil && !st.Success() {
			err = errors.New(st.String())
		}
		f.end <- err
	}()
	return f
}

// waitReady: o aviso do proxy novo, ou o motivo de não ter vindo.
func waitReady(f *childProxy, ready *os.File) error {
	defer ready.Close()
	came := make(chan bool, 1)
	go func() {
		b := make([]byte, 1)
		n, _ := ready.Read(b)
		came <- n == 1
	}()
	select {
	case ok := <-came:
		if ok {
			return nil
		}
		err := <-f.end // fechou sem avisar: saiu
		f.end <- err
		return fmt.Errorf("o proxy novo saiu sem ficar pronto (%v)", err)
	case <-time.After(readyDeadline):
		return fmt.Errorf("o proxy novo não ficou pronto em %s", readyDeadline)
	}
}

func stopProxy(f *childProxy) {
	f.p.Signal(syscall.SIGTERM)
	select {
	case <-f.end:
	case <-time.After(5 * time.Second):
		f.p.Kill()
		<-f.end
	}
}

// supervisionar segura a porta e mantém um proxy no ar: se ele cair, sobe de novo (espera 1s,
// 2s, 4s... até 10s; enquanto isso as conexões esperam na fila). SIGHUP troca o proxy pelo
// binário instalado (ver o começo do arquivo). Com --assumir (quem chama é o atualizar, para
// trocar uma versão anterior), espera a porta e a trava do supervisor antigo.
func supervise(args []string) error {
	assume := hasArg(args, "--assumir")
	lg := openLog()
	me := serviceBinary()
	pidFile, mine := config.Path("supervisor.pid"), strconv.Itoa(os.Getpid())
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	stripPid := func() {
		if b, _ := os.ReadFile(pidFile); strings.TrimSpace(string(b)) == mine {
			os.Remove(pidFile)
		}
	}

	var lock *os.File
	var port int // o socket, como número cru (ver startProxy)
	var current *childProxy
	if h := os.Getenv(envInherit); h != "" {
		// reexecutado depois de uma troca: a porta, a trava e os proxies vêm do processo anterior
		os.Unsetenv(envInherit)
		var tfd, proxyPid, draining int
		if n, _ := fmt.Sscanf(h, "%d %d %d %d", &port, &tfd, &proxyPid, &draining); n != 4 {
			return fmt.Errorf("herança inválida: %q", h)
		}
		syscall.CloseOnExec(port)
		syscall.CloseOnExec(tfd)
		lock = os.NewFile(uintptr(tfd), "trava")
		current = inheritProxy(proxyPid)
		if draining > 0 {
			inheritProxy(draining) // só para recolher quando sair
		}
		os.WriteFile(pidFile, []byte(mine), 0o600) // (o mesmo pid; grava de novo caso tenha sumido)
		lg.Printf("supervisor: agora no binário instalado (pid %d); proxy %d", os.Getpid(), proxyPid)
	} else {
		var err error
		if lock, err = os.OpenFile(config.Path("supervisor.lock"), os.O_CREATE|os.O_RDWR, 0o600); err != nil {
			return err
		}
		if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
			os.WriteFile(pidFile, []byte(mine), 0o600)
		} else if !assume {
			return nil // já existe um supervisor
		} else {
			go func() {
				if syscall.Flock(int(lock.Fd()), syscall.LOCK_EX) == nil {
					os.WriteFile(pidFile, []byte(mine), 0o600)
				}
			}()
		}
		cfg, err := config.Load()
		if err != nil {
			cfg = config.Default() // a config pode ser o que quebrou; o proxy decide (emergência)
		}
		if port, err = openPort(address(cfg), assume, signals, lg); err != nil {
			stripPid()
			return err
		}
	}

	wait := time.Second
	for {
		if current == nil {
			f, ready, err := startProxy(me, port, lg)
			if err != nil {
				lg.Printf("supervisor: não consegui iniciar: %v", err)
			} else {
				ready.Close()
				current = f
			}
		}
		if current != nil {
			ini := time.Now()
			select {
			case s := <-signals:
				if s == syscall.SIGHUP {
					current = swapProxy(me, port, lock, current, lg)
					continue
				}
				current.p.Signal(syscall.SIGTERM)
				<-current.end
				lg.Printf("supervisor: encerrado (%v)", s)
				stripPid()
				return nil
			case err := <-current.end:
				current = nil
				if err == nil {
					// saída intencional (troca de modo normal <-> emergência): volta na hora
					lg.Printf("supervisor: troca de modo; reiniciando já")
					wait = time.Second
					continue
				}
				lg.Printf("supervisor: proxy caiu (%v); reiniciando em %s (a porta segue aberta)", err, wait)
				if time.Since(ini) > time.Minute {
					wait = time.Second
				}
			}
		}
		select {
		case <-time.After(wait):
		case s := <-signals:
			if s == syscall.SIGHUP {
				current = swapProxy(me, port, lock, nil, lg)
				continue
			}
			stripPid()
			return nil
		}
		if wait < 10*time.Second {
			wait *= 2
		}
	}
}

// swapProxy sobe um proxy com o binário instalado e, pronto, manda o antigo sair e reexecuta
// o supervisor no binário instalado. Devolve o proxy que fica no ar. O resultado vai para
// troca.estado.
func swapProxy(me string, port int, lock *os.File, old *childProxy, lg *log.Logger) *childProxy {
	state := config.Path("troca.estado")
	newOne, ready, err := startProxy(me, port, lg)
	if err == nil {
		if err = waitReady(newOne, ready); err != nil {
			stopProxy(newOne)
		}
	}
	if err != nil {
		lg.Printf("supervisor: troca desfeita: %v", err)
		os.WriteFile(state, []byte("falhou: "+err.Error()), 0o600)
		return old
	}
	draining := 0
	if old != nil {
		old.p.Signal(syscall.SIGUSR2)
		draining = old.p.Pid
		// "ok" só quando o antigo largou o socket: daí em diante, só o novo aceita conexões
		if !waitRelease(draining, port, 2*time.Second) {
			lg.Printf("supervisor: o proxy %d ainda segura a porta", draining)
		}
		lg.Printf("supervisor: proxy %d no ar; o %d termina o que está em andamento e sai", newOne.p.Pid, draining)
	}
	os.WriteFile(state, []byte("ok "+strconv.Itoa(newOne.p.Pid)), 0o600)
	// o supervisor também passa para o binário instalado, sem soltar a porta nem a trava
	tfd := int(lock.Fd()) // arquivo comum: Fd() não muda nada
	if err := noCloseOnExec(port, tfd); err == nil {
		env := append(os.Environ(), fmt.Sprintf("%s=%d %d %d %d", envInherit, port, tfd, newOne.p.Pid, draining))
		err = syscall.Exec(me, []string{me, "supervisionar"}, env)
		lg.Printf("supervisor: não consegui me reexecutar no binário novo (%v); sigo neste", err)
	}
	syscall.CloseOnExec(port)
	syscall.CloseOnExec(tfd)
	return newOne
}

// waitRelease espera o processo pid não ter mais o socket da porta entre os seus arquivos
// abertos (/proc/PID/fd). Sem /proc (fora do Linux), espera um pouco.
func waitRelease(pid, port int, deadline time.Duration) bool {
	var st syscall.Stat_t
	if err := syscall.Fstat(port, &st); err != nil {
		time.Sleep(200 * time.Millisecond)
		return false
	}
	target := fmt.Sprintf("socket:[%d]", st.Ino)
	dir := fmt.Sprintf("/proc/%d/fd", pid)
	for limitN := time.Now().Add(deadline); time.Now().Before(limitN); time.Sleep(5 * time.Millisecond) {
		fds, err := os.ReadDir(dir)
		if err != nil {
			return errors.Is(err, os.ErrNotExist) // saiu
		}
		has := false
		for _, f := range fds {
			if l, _ := os.Readlink(filepath.Join(dir, f.Name())); l == target {
				has = true
				break
			}
		}
		if !has {
			return true
		}
	}
	return false
}

// noCloseOnExec deixa os fds passarem para o processo reexecutado.
func noCloseOnExec(fds ...int) error {
	for _, fd := range fds {
		if _, _, e := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), syscall.F_SETFD, 0); e != 0 {
			return e
		}
	}
	return nil
}

// openPort abre a porta do serviço e devolve o socket como número cru (ver startProxy).
// Ocupada: com assumir (troca de uma versão anterior), avisa em supervisor.esperando e tenta
// de novo a cada 2 ms até ela sair; sem, tenta de novo com espera crescente (outro programa,
// ou um proxy antigo sem supervisor).
func openPort(address string, assume bool, signals <-chan os.Signal, lg *log.Logger) (int, error) {
	mark := config.Path("supervisor.esperando")
	defer os.Remove(mark)
	wait, warnedIt := time.Second, false
	limitN := time.Now().Add(30 * time.Second)
	for {
		ln, err := net.Listen("tcp", address)
		if err == nil {
			// uma cópia do socket fora do runtime do Go (que fecharia ou mudaria o modo dele)
			rc, err := ln.(*net.TCPListener).SyscallConn()
			fd := -1
			if err == nil {
				rc.Control(func(s uintptr) { fd, err = syscall.Dup(int(s)) })
			}
			ln.Close() // a cópia mantém a porta aberta
			if err != nil {
				return -1, err
			}
			syscall.CloseOnExec(fd)
			return fd, nil
		}
		if !errors.Is(err, syscall.EADDRINUSE) {
			return -1, err
		}
		if assume {
			if !warnedIt {
				os.WriteFile(mark, []byte(strconv.Itoa(os.Getpid())), 0o600)
				warnedIt = true
			}
			if time.Now().After(limitN) {
				return -1, err
			}
			time.Sleep(2 * time.Millisecond)
			continue
		}
		if !warnedIt {
			lg.Printf("supervisor: a porta %s está ocupada; tentando de novo", address)
			warnedIt = true
		}
		select {
		case <-time.After(wait):
		case s := <-signals:
			return -1, fmt.Errorf("encerrado (%v) esperando a porta", s)
		}
		if wait < 10*time.Second {
			wait *= 2
		}
	}
}

// installedBinary: onde o instalar e o atualizar põem o llm-dlp (e para onde apontam os
// hooks do Claude Code).
func installedBinary() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "bin", "llm-dlp")
}

// serviceBinary: o programa que o supervisor e o proxy rodam: o instalado, se existir; senão,
// este. Não o de quem chamou: o install.sh roda o instalar de uma pasta temporária que apaga
// logo depois, e um supervisor preso a esse arquivo não conseguiria mais subir o proxy.
func serviceBinary() string {
	if d := installedBinary(); d != "" {
		if st, err := os.Stat(d); err == nil && st.Mode().IsRegular() && st.Mode()&0o111 != 0 {
			return d
		}
	}
	me, _ := os.Executable()
	return me
}

// supervisorUp: o pid do supervisor, se supervisor.pid aponta para um supervisor do llm-dlp
// vivo (o número pode ter sido reaproveitado pelo sistema); 0 se não. Sem o arquivo, procura
// quem segura a trava do supervisor e grava o arquivo de novo: na passagem da 0.7.0 para a
// 0.8.0, o supervisor antigo apagava ao sair o arquivo que o novo tinha acabado de gravar, e
// sem ele o update não achava o supervisor (trocava o proxy cortando a resposta em andamento,
// e o supervisor nunca era atualizado) e o stop dizia que não havia supervisor.
func supervisorUp() int {
	pidFile := config.Path("supervisor.pid")
	b, _ := os.ReadFile(pidFile)
	if pid, _ := strconv.Atoi(strings.TrimSpace(string(b))); pid > 0 && isSupervisor(pid) {
		return pid
	}
	if pid := lockOwner(config.Path("supervisor.lock")); pid > 0 && isSupervisor(pid) {
		os.WriteFile(pidFile, []byte(strconv.Itoa(pid)), 0o600)
		return pid
	}
	return 0
}

// isSupervisor: pid é um supervisor do llm-dlp ("supervisionar", o que o llm-dlp sobe: ver
// startProxy; ou "supervise").
func isSupervisor(pid int) bool {
	cmdline := commandLine(pid)
	return strings.Contains(cmdline, "supervis") && strings.Contains(cmdline, "llm-dlp")
}

// lockOwner: o processo que segura o flock do arquivo trava, por /proc/locks (só no Linux;
// 0 se ninguém segura ou se não dá para saber).
func lockOwner(lockPath string) int {
	st, err := os.Stat(lockPath)
	if err != nil {
		return 0
	}
	ino := st.Sys().(*syscall.Stat_t).Ino
	b, err := os.ReadFile("/proc/locks")
	if err != nil {
		return 0
	}
	// "231: FLOCK  ADVISORY  WRITE 57709 08:30:1297200 0 EOF"; quem espera a trava aparece
	// numa linha com "->"
	for _, l := range strings.Split(string(b), "\n") {
		c := strings.Fields(l)
		if len(c) < 6 || c[1] != "FLOCK" {
			continue
		}
		if dev := strings.Split(c[5], ":"); len(dev) == 3 && dev[2] == strconv.FormatUint(ino, 10) {
			pid, _ := strconv.Atoi(c[4])
			return pid
		}
	}
	return 0
}

// waitExit espera o processo pid terminar (até prazo).
func waitExit(pid int, deadline time.Duration) bool {
	for limitN := time.Now().Add(deadline); time.Now().Before(limitN); time.Sleep(20 * time.Millisecond) {
		if syscall.Kill(pid, 0) != nil {
			return true
		}
	}
	return false
}

func hasArg(args []string, a string) bool {
	for _, x := range args {
		if x == a {
			return true
		}
	}
	return false
}
