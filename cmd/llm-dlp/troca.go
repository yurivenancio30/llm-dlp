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
// Troca de versão (atualizar, recarregar): SIGHUP no supervisor. Ele sobe um proxy com o
// binário instalado, na mesma porta, e espera o aviso de que está pronto; aí manda o antigo
// sair (SIGUSR2: para de aceitar conexões e termina o que está em andamento) e se reexecuta com
// o binário instalado, herdando a porta, a trava e os dois proxies. Se o novo não ficar pronto,
// é parado, e o antigo segue. O resultado vai para troca.estado, que o atualizar lê.
//
// Uma versão anterior a esta (o supervisor dela não segura a porta nem conhece o SIGHUP) é
// trocada de outro jeito: um supervisor novo com --assumir espera a porta, o antigo é parado, e
// a porta fica vazia só os milissegundos entre um fechar e o outro abrir.

const (
	envPorta   = "LLM_DLP_PORTA_FD"  // o proxy recebe a porta do supervisor no fd 3
	envPronto  = "LLM_DLP_PRONTO_FD" // e avisa no fd 4 quando já atende
	envHeranca = "LLM_DLP_HERANCA"   // supervisor reexecutado: "porta trava proxy drenando"

	prazoParar = 3 * time.Second  // parar: o que der para terminar em poucos segundos (o parar espera 5)
	prazoTroca = 10 * time.Minute // troca: a versão nova já atende; as respostas longas terminam
	// prazoGraca: na troca, quanto as conexões já abertas ainda são atendidas pelo antigo. Os
	// clientes HTTP largam uma conexão parada antes disso (o do Node, em 4 s).
	prazoGraca = 6 * time.Second
	// prazoPronto: quanto o proxy novo tem para carregar a configuração e os registros
	prazoPronto = 20 * time.Second
)

// portaDoServico: a porta emprestada pelo supervisor ou, sem ele (llm-dlp serve à mão), uma
// aberta aqui. emprestada diz qual.
func portaDoServico(endereco string) (ln net.Listener, emprestada bool, err error) {
	if os.Getenv(envPorta) == "" {
		ln, err = net.Listen("tcp", endereco)
		return ln, false, err
	}
	f := os.NewFile(3, "porta")
	ln, err = net.FileListener(f)
	f.Close()
	return ln, true, err
}

// avisarPronto: diz ao supervisor que o proxy já atende (a troca espera por isso).
func avisarPronto() {
	if os.Getenv(envPronto) != "" {
		f := os.NewFile(4, "pronto")
		f.Write([]byte{1})
		f.Close()
	}
}

// servirAteSinal atende até cair ou até um sinal. SIGTERM/SIGINT (parar): para de aceitar e dá
// alguns segundos às respostas em andamento. SIGUSR2 (troca): o proxy novo já atende, então sem
// pressa: para de aceitar conexões, mas o que ainda chegar pelas conexões já abertas (o Claude
// Code as reaproveita) é atendido com "Connection: close", e o cliente abre a próxima no novo;
// as que ficarem paradas só são fechadas depois de prazoGraca (fechar uma conexão parada no
// instante em que o cliente manda por ela faria a requisição falhar). No fim, fim() grava o que
// estiver pendente. Fechar a porta aqui não a tira do ar: ela é do supervisor.
func servirAteSinal(srv *http.Server, ln net.Listener, lg *log.Logger, fim func()) error {
	var saindo atomic.Bool
	h := srv.Handler
	srv.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if saindo.Load() {
			w.Header().Set("Connection", "close")
		}
		h.ServeHTTP(w, r)
	})
	sinais := make(chan os.Signal, 1)
	signal.Notify(sinais, syscall.SIGTERM, syscall.SIGINT, syscall.SIGUSR2)
	defer signal.Stop(sinais)
	caiu := make(chan error, 1)
	go func() { caiu <- srv.Serve(ln) }()
	avisarPronto()
	select {
	case err := <-caiu:
		return err
	case s := <-sinais:
		prazo := prazoParar
		if s == syscall.SIGUSR2 {
			prazo = prazoTroca
			saindo.Store(true)
			ln.Close()
			lg.Printf("saindo (troca de versão, pid %d): não aceita mais conexões; as abertas ainda são atendidas por %s", os.Getpid(), prazoGraca)
			time.Sleep(prazoGraca)
		}
		lg.Printf("saindo (%v, pid %d): terminando as respostas em andamento (até %s)", s, os.Getpid(), prazo)
		ctx, cancel := context.WithTimeout(context.Background(), prazo)
		defer cancel()
		// (a porta já fechada na troca não é erro)
		if err := srv.Shutdown(ctx); err != nil && !errors.Is(err, net.ErrClosed) {
			lg.Printf("saindo (pid %d): %v (respostas cortadas)", os.Getpid(), err)
		}
		if fim != nil {
			fim()
		}
		lg.Printf("saiu (pid %d)", os.Getpid())
		return nil
	}
}

// proxyFilho: um proxy que o supervisor subiu (ou herdou ao se reexecutar).
type proxyFilho struct {
	p   *os.Process
	fim chan error
}

// iniciarProxy sobe "servir" com a porta emprestada e devolve o lado de leitura do aviso de
// pronto. O nome antigo ("servir", não "serve"; "supervisionar" na reexecução e no garantir) é
// de propósito: o binário instalado pode ser de uma versão anterior aos nomes em inglês (quem
// voltou de versão), e toda versão entende o antigo. A porta vai como número cru (syscall.ForkExec), nunca por um *os.File: passar um
// *os.File a um processo (exec.Cmd.ExtraFiles) chama Fd(), que põe o socket em modo
// bloqueante, e o modo vale para todos os processos que o dividem. O proxy no ar ficaria
// preso no accept e não conseguiria mais sair.
func iniciarProxy(bin string, porta int, lg *log.Logger) (*proxyFilho, *os.File, error) {
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
	saida := devnull.Fd()
	if f, ok := lg.Writer().(*os.File); ok {
		saida = f.Fd() // o log (arquivo comum: Fd() não muda nada)
	}
	var env []string
	for _, v := range os.Environ() {
		if !strings.HasPrefix(v, envHeranca+"=") {
			env = append(env, v)
		}
	}
	env = append(env, envPorta+"=3", envPronto+"=4")
	pid, err := syscall.ForkExec(bin, []string{bin, "servir"}, &syscall.ProcAttr{
		Env:   env,
		Files: []uintptr{devnull.Fd(), saida, saida, uintptr(porta), w.Fd()},
	})
	if err != nil {
		r.Close()
		return nil, nil, err
	}
	lg.Printf("supervisor: proxy iniciado (pid %d)", pid)
	return herdarProxy(pid), r, nil
}

// herdarProxy: espera por um proxy filho deste processo (subido aqui, ou pelo supervisor
// anterior antes de se reexecutar: continua filho).
func herdarProxy(pid int) *proxyFilho {
	p, _ := os.FindProcess(pid)
	f := &proxyFilho{p, make(chan error, 1)}
	go func() {
		st, err := p.Wait()
		if err == nil && !st.Success() {
			err = errors.New(st.String())
		}
		f.fim <- err
	}()
	return f
}

// esperarPronto: o aviso do proxy novo, ou o motivo de não ter vindo.
func esperarPronto(f *proxyFilho, pronto *os.File) error {
	defer pronto.Close()
	veio := make(chan bool, 1)
	go func() {
		b := make([]byte, 1)
		n, _ := pronto.Read(b)
		veio <- n == 1
	}()
	select {
	case ok := <-veio:
		if ok {
			return nil
		}
		err := <-f.fim // fechou sem avisar: saiu
		f.fim <- err
		return fmt.Errorf("o proxy novo saiu sem ficar pronto (%v)", err)
	case <-time.After(prazoPronto):
		return fmt.Errorf("o proxy novo não ficou pronto em %s", prazoPronto)
	}
}

func pararProxy(f *proxyFilho) {
	f.p.Signal(syscall.SIGTERM)
	select {
	case <-f.fim:
	case <-time.After(5 * time.Second):
		f.p.Kill()
		<-f.fim
	}
}

// supervisionar segura a porta e mantém um proxy no ar: se ele cair, sobe de novo (espera 1s,
// 2s, 4s... até 10s; enquanto isso as conexões esperam na fila). SIGHUP troca o proxy pelo
// binário instalado (ver o começo do arquivo). Com --assumir (quem chama é o atualizar, para
// trocar uma versão anterior), espera a porta e a trava do supervisor antigo.
func supervisionar(args []string) error {
	assumir := temArg(args, "--assumir")
	lg := abrirLog()
	eu := binarioServico()
	pidArq, meu := config.Caminho("supervisor.pid"), strconv.Itoa(os.Getpid())
	sinais := make(chan os.Signal, 4)
	signal.Notify(sinais, syscall.SIGTERM, syscall.SIGINT, syscall.SIGHUP)
	tirarPid := func() {
		if b, _ := os.ReadFile(pidArq); strings.TrimSpace(string(b)) == meu {
			os.Remove(pidArq)
		}
	}

	var lock *os.File
	var porta int // o socket, como número cru (ver iniciarProxy)
	var atual *proxyFilho
	if h := os.Getenv(envHeranca); h != "" {
		// reexecutado depois de uma troca: a porta, a trava e os proxies vêm do processo anterior
		os.Unsetenv(envHeranca)
		var tfd, proxyPid, drenando int
		if n, _ := fmt.Sscanf(h, "%d %d %d %d", &porta, &tfd, &proxyPid, &drenando); n != 4 {
			return fmt.Errorf("herança inválida: %q", h)
		}
		syscall.CloseOnExec(porta)
		syscall.CloseOnExec(tfd)
		lock = os.NewFile(uintptr(tfd), "trava")
		atual = herdarProxy(proxyPid)
		if drenando > 0 {
			herdarProxy(drenando) // só para recolher quando sair
		}
		os.WriteFile(pidArq, []byte(meu), 0o600) // (o mesmo pid; grava de novo caso tenha sumido)
		lg.Printf("supervisor: agora no binário instalado (pid %d); proxy %d", os.Getpid(), proxyPid)
	} else {
		var err error
		if lock, err = os.OpenFile(config.Caminho("supervisor.lock"), os.O_CREATE|os.O_RDWR, 0o600); err != nil {
			return err
		}
		if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
			os.WriteFile(pidArq, []byte(meu), 0o600)
		} else if !assumir {
			return nil // já existe um supervisor
		} else {
			go func() {
				if syscall.Flock(int(lock.Fd()), syscall.LOCK_EX) == nil {
					os.WriteFile(pidArq, []byte(meu), 0o600)
				}
			}()
		}
		cfg, err := config.Carregar()
		if err != nil {
			cfg = config.Padrao() // a config pode ser o que quebrou; o proxy decide (emergência)
		}
		if porta, err = abrirPorta(endereco(cfg), assumir, sinais, lg); err != nil {
			tirarPid()
			return err
		}
	}

	espera := time.Second
	for {
		if atual == nil {
			f, pronto, err := iniciarProxy(eu, porta, lg)
			if err != nil {
				lg.Printf("supervisor: não consegui iniciar: %v", err)
			} else {
				pronto.Close()
				atual = f
			}
		}
		if atual != nil {
			ini := time.Now()
			select {
			case s := <-sinais:
				if s == syscall.SIGHUP {
					atual = trocarProxy(eu, porta, lock, atual, lg)
					continue
				}
				atual.p.Signal(syscall.SIGTERM)
				<-atual.fim
				lg.Printf("supervisor: encerrado (%v)", s)
				tirarPid()
				return nil
			case err := <-atual.fim:
				atual = nil
				if err == nil {
					// saída intencional (troca de modo normal <-> emergência): volta na hora
					lg.Printf("supervisor: troca de modo; reiniciando já")
					espera = time.Second
					continue
				}
				lg.Printf("supervisor: proxy caiu (%v); reiniciando em %s (a porta segue aberta)", err, espera)
				if time.Since(ini) > time.Minute {
					espera = time.Second
				}
			}
		}
		select {
		case <-time.After(espera):
		case s := <-sinais:
			if s == syscall.SIGHUP {
				atual = trocarProxy(eu, porta, lock, nil, lg)
				continue
			}
			tirarPid()
			return nil
		}
		if espera < 10*time.Second {
			espera *= 2
		}
	}
}

// trocarProxy sobe um proxy com o binário instalado e, pronto, manda o antigo sair e reexecuta
// o supervisor no binário instalado. Devolve o proxy que fica no ar. O resultado vai para
// troca.estado.
func trocarProxy(eu string, porta int, lock *os.File, antigo *proxyFilho, lg *log.Logger) *proxyFilho {
	estado := config.Caminho("troca.estado")
	novo, pronto, err := iniciarProxy(eu, porta, lg)
	if err == nil {
		if err = esperarPronto(novo, pronto); err != nil {
			pararProxy(novo)
		}
	}
	if err != nil {
		lg.Printf("supervisor: troca desfeita: %v", err)
		os.WriteFile(estado, []byte("falhou: "+err.Error()), 0o600)
		return antigo
	}
	drenando := 0
	if antigo != nil {
		antigo.p.Signal(syscall.SIGUSR2)
		drenando = antigo.p.Pid
		// "ok" só quando o antigo largou o socket: daí em diante, só o novo aceita conexões
		if !esperarSoltar(drenando, porta, 2*time.Second) {
			lg.Printf("supervisor: o proxy %d ainda segura a porta", drenando)
		}
		lg.Printf("supervisor: proxy %d no ar; o %d termina o que está em andamento e sai", novo.p.Pid, drenando)
	}
	os.WriteFile(estado, []byte("ok "+strconv.Itoa(novo.p.Pid)), 0o600)
	// o supervisor também passa para o binário instalado, sem soltar a porta nem a trava
	tfd := int(lock.Fd()) // arquivo comum: Fd() não muda nada
	if err := semCloseOnExec(porta, tfd); err == nil {
		env := append(os.Environ(), fmt.Sprintf("%s=%d %d %d %d", envHeranca, porta, tfd, novo.p.Pid, drenando))
		err = syscall.Exec(eu, []string{eu, "supervisionar"}, env)
		lg.Printf("supervisor: não consegui me reexecutar no binário novo (%v); sigo neste", err)
	}
	syscall.CloseOnExec(porta)
	syscall.CloseOnExec(tfd)
	return novo
}

// esperarSoltar espera o processo pid não ter mais o socket da porta entre os seus arquivos
// abertos (/proc/PID/fd). Sem /proc (fora do Linux), espera um pouco.
func esperarSoltar(pid, porta int, prazo time.Duration) bool {
	var st syscall.Stat_t
	if err := syscall.Fstat(porta, &st); err != nil {
		time.Sleep(200 * time.Millisecond)
		return false
	}
	alvo := fmt.Sprintf("socket:[%d]", st.Ino)
	dir := fmt.Sprintf("/proc/%d/fd", pid)
	for limite := time.Now().Add(prazo); time.Now().Before(limite); time.Sleep(5 * time.Millisecond) {
		fds, err := os.ReadDir(dir)
		if err != nil {
			return errors.Is(err, os.ErrNotExist) // saiu
		}
		tem := false
		for _, f := range fds {
			if l, _ := os.Readlink(filepath.Join(dir, f.Name())); l == alvo {
				tem = true
				break
			}
		}
		if !tem {
			return true
		}
	}
	return false
}

// semCloseOnExec deixa os fds passarem para o processo reexecutado.
func semCloseOnExec(fds ...int) error {
	for _, fd := range fds {
		if _, _, e := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), syscall.F_SETFD, 0); e != 0 {
			return e
		}
	}
	return nil
}

// abrirPorta abre a porta do serviço e devolve o socket como número cru (ver iniciarProxy).
// Ocupada: com assumir (troca de uma versão anterior), avisa em supervisor.esperando e tenta
// de novo a cada 2 ms até ela sair; sem, tenta de novo com espera crescente (outro programa,
// ou um proxy antigo sem supervisor).
func abrirPorta(endereco string, assumir bool, sinais <-chan os.Signal, lg *log.Logger) (int, error) {
	marca := config.Caminho("supervisor.esperando")
	defer os.Remove(marca)
	espera, avisou := time.Second, false
	limite := time.Now().Add(30 * time.Second)
	for {
		ln, err := net.Listen("tcp", endereco)
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
		if assumir {
			if !avisou {
				os.WriteFile(marca, []byte(strconv.Itoa(os.Getpid())), 0o600)
				avisou = true
			}
			if time.Now().After(limite) {
				return -1, err
			}
			time.Sleep(2 * time.Millisecond)
			continue
		}
		if !avisou {
			lg.Printf("supervisor: a porta %s está ocupada; tentando de novo", endereco)
			avisou = true
		}
		select {
		case <-time.After(espera):
		case s := <-sinais:
			return -1, fmt.Errorf("encerrado (%v) esperando a porta", s)
		}
		if espera < 10*time.Second {
			espera *= 2
		}
	}
}

// binarioInstalado: onde o instalar e o atualizar põem o llm-dlp (e para onde apontam os
// hooks do Claude Code).
func binarioInstalado() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "bin", "llm-dlp")
}

// binarioServico: o programa que o supervisor e o proxy rodam: o instalado, se existir; senão,
// este. Não o de quem chamou: o install.sh roda o instalar de uma pasta temporária que apaga
// logo depois, e um supervisor preso a esse arquivo não conseguiria mais subir o proxy.
func binarioServico() string {
	if d := binarioInstalado(); d != "" {
		if st, err := os.Stat(d); err == nil && st.Mode().IsRegular() && st.Mode()&0o111 != 0 {
			return d
		}
	}
	eu, _ := os.Executable()
	return eu
}

// supervisorNoAr: o pid do supervisor, se supervisor.pid aponta para um supervisor do llm-dlp
// vivo (o número pode ter sido reaproveitado pelo sistema); 0 se não. Sem o arquivo, procura
// quem segura a trava do supervisor e grava o arquivo de novo: na passagem da 0.7.0 para a
// 0.8.0, o supervisor antigo apagava ao sair o arquivo que o novo tinha acabado de gravar, e
// sem ele o update não achava o supervisor (trocava o proxy cortando a resposta em andamento,
// e o supervisor nunca era atualizado) e o stop dizia que não havia supervisor.
func supervisorNoAr() int {
	pidArq := config.Caminho("supervisor.pid")
	b, _ := os.ReadFile(pidArq)
	if pid, _ := strconv.Atoi(strings.TrimSpace(string(b))); pid > 0 && ehSupervisor(pid) {
		return pid
	}
	if pid := donoDaTrava(config.Caminho("supervisor.lock")); pid > 0 && ehSupervisor(pid) {
		os.WriteFile(pidArq, []byte(strconv.Itoa(pid)), 0o600)
		return pid
	}
	return 0
}

// ehSupervisor: pid é um supervisor do llm-dlp ("supervisionar", o que o llm-dlp sobe: ver
// iniciarProxy; ou "supervise").
func ehSupervisor(pid int) bool {
	cmdline := linhaDeComando(pid)
	return strings.Contains(cmdline, "supervis") && strings.Contains(cmdline, "llm-dlp")
}

// donoDaTrava: o processo que segura o flock do arquivo trava, por /proc/locks (só no Linux;
// 0 se ninguém segura ou se não dá para saber).
func donoDaTrava(trava string) int {
	st, err := os.Stat(trava)
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

// esperarSair espera o processo pid terminar (até prazo).
func esperarSair(pid int, prazo time.Duration) bool {
	for limite := time.Now().Add(prazo); time.Now().Before(limite); time.Sleep(20 * time.Millisecond) {
		if syscall.Kill(pid, 0) != nil {
			return true
		}
	}
	return false
}

func temArg(args []string, a string) bool {
	for _, x := range args {
		if x == a {
			return true
		}
	}
	return false
}
