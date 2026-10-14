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

// The supervisor holds the port; the proxy only borrows it.
//
// The supervisor opens the port once and passes it to each proxy it starts (as systemd does with
// socket activation). If the proxy goes down, the port stays open: connections wait in the kernel
// queue until the next proxy, instead of "Connection refused". Claude Code sends requests in the
// middle of a turn (after a tool) without going through the hook that starts llm-dlp, and an empty
// port at that moment was an error on the screen.
//
// Version swap (update, reload): SIGHUP to the supervisor. It starts a proxy with the installed
// binary, on the same port, and waits for the notice that it is ready; then it tells the old one to
// exit (SIGUSR2: stops accepting connections and finishes what is in progress) and re-executes
// itself with the installed binary, inheriting the port, the lock and both proxies. If the new one
// does not become ready, it is stopped, and the old one goes on. The result goes to troca.estado,
// which update reads.
//
// A version older than this one (its supervisor does not hold the port nor know SIGHUP) is swapped
// another way: a new supervisor with --assumir waits for the port, the old one is stopped, and the
// port is empty only for the milliseconds between one closing and the other opening.
//
// Protocol between versions: one version's supervisor starts and re-executes the other's binary, and
// one version's update reads what the other's supervisor wrote. So these names stay the ones every
// version since 0.8 knows, in Portuguese, and are not to be renamed: the commands "servir" and
// "supervisionar", the option --assumir, the environment variables below, the files troca.estado and
// supervisor.esperando and the "falhou: " in troca.estado (and, in service.go and the proxy, the path
// /__llm-dlp/saude and the fields of its answer).

const (
	envPort    = "LLM_DLP_PORTA_FD"  // the proxy receives the port from the supervisor on fd 3
	envReady   = "LLM_DLP_PRONTO_FD" // and notifies on fd 4 when it is already serving
	envInherit = "LLM_DLP_HERANCA"   // re-executed supervisor: "port lock proxy draining"

	stopDeadline = 3 * time.Second  // stop: whatever can be finished in a few seconds (stop waits 5)
	swapDeadline = 10 * time.Minute // swap: the new version is already serving; long responses finish
	// graceDeadline: during a swap, how long the connections already open are still served by the
	// old one. HTTP clients drop an idle connection before that (Node's, in 4 s).
	graceDeadline = 6 * time.Second
	// readyDeadline: how long the new proxy has to load the configuration and the records
	readyDeadline = 20 * time.Second
)

// servicePort: the port lent by the supervisor or, without it (llm-dlp serve by hand), one opened
// here. borrowed says which.
func servicePort(address string) (ln net.Listener, borrowed bool, err error) {
	if os.Getenv(envPort) == "" {
		ln, err = net.Listen("tcp", address)
		return ln, false, err
	}
	f := os.NewFile(3, "port")
	ln, err = net.FileListener(f)
	f.Close()
	return ln, true, err
}

// notifyReady: tells the supervisor the proxy is already serving (the swap waits for it).
func notifyReady() {
	if os.Getenv(envReady) != "" {
		f := os.NewFile(4, "pronto")
		f.Write([]byte{1})
		f.Close()
	}
}

// serveUntilSignal serves until it goes down or until a signal. SIGTERM/SIGINT (stop): stops
// accepting and gives a few seconds to responses in progress. SIGUSR2 (swap): the new proxy is
// already serving, so no hurry: it stops accepting connections, but what still arrives on
// connections already open (Claude Code reuses them) is served with "Connection: close", and the
// client opens the next one on the new proxy; idle ones are only closed after graceDeadline
// (closing an idle connection at the instant the client sends on it would make the request fail).
// At the end, end() writes what is pending. Closing the port here does not take it offline: it
// belongs to the supervisor.
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
			lg.Printf("exiting (version swap, pid %d): no longer accepts connections; the open ones are still served for %s", os.Getpid(), graceDeadline)
			time.Sleep(graceDeadline)
		}
		lg.Printf("exiting (%v, pid %d): finishing the responses in progress (up to %s)", s, os.Getpid(), deadline)
		ctx, cancel := context.WithTimeout(context.Background(), deadline)
		defer cancel()
		// (the port already closed during a swap is not an error)
		if err := srv.Shutdown(ctx); err != nil && !errors.Is(err, net.ErrClosed) {
			lg.Printf("exiting (pid %d): %v (responses cut)", os.Getpid(), err)
		}
		if end != nil {
			end()
		}
		lg.Printf("exited (pid %d)", os.Getpid())
		return nil
	}
}

// childProxy: a proxy the supervisor started (or inherited when re-executing).
type childProxy struct {
	p   *os.Process
	end chan error
}

// startProxy starts "servir" with the borrowed port and returns the read side of the ready notice.
// The old name ("servir", not "serve"; "supervisionar" on re-execution and in ensure) is on
// purpose: the installed binary may be from a version older than the English names (someone who
// went back a version), and every version understands the old one. The port goes as a raw number
// (syscall.ForkExec), never through an *os.File: passing an *os.File to a process
// (exec.Cmd.ExtraFiles) calls Fd(), which puts the socket in blocking mode, and the mode applies to
// every process sharing it. The running proxy would get stuck in accept and could no longer exit.
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
		output = f.Fd() // the log (a regular file: Fd() changes nothing)
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
	lg.Printf("supervisor: proxy started (pid %d)", pid)
	return inheritProxy(pid), r, nil
}

// inheritProxy: waits for a child proxy of this process (started here, or by the previous
// supervisor before re-executing: it is still a child).
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

// waitReady: the new proxy's notice, or the reason it did not come.
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
		err := <-f.end // closed without notice: it exited
		f.end <- err
		return fmt.Errorf("the new proxy exited without becoming ready (%v)", err)
	case <-time.After(readyDeadline):
		return fmt.Errorf("the new proxy did not become ready within %s", readyDeadline)
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

// supervise holds the port and keeps a proxy running: if it goes down, it starts it again (waits
// 1s, 2s, 4s... up to 10s; meanwhile connections wait in the queue). SIGHUP swaps the proxy for the
// installed binary (see the top of the file). With --assumir (the caller is update, to swap an
// earlier version), it waits for the port and the lock of the old supervisor.
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
	var port int // the socket, as a raw number (see startProxy)
	var current *childProxy
	if h := os.Getenv(envInherit); h != "" {
		// re-executed after a swap: the port, the lock and the proxies come from the previous
		// process
		os.Unsetenv(envInherit)
		var tfd, proxyPid, draining int
		if n, _ := fmt.Sscanf(h, "%d %d %d %d", &port, &tfd, &proxyPid, &draining); n != 4 {
			return fmt.Errorf("invalid inheritance: %q", h)
		}
		syscall.CloseOnExec(port)
		syscall.CloseOnExec(tfd)
		lock = os.NewFile(uintptr(tfd), "lock")
		current = inheritProxy(proxyPid)
		if draining > 0 {
			inheritProxy(draining) // only to reap when it exits
		}
		os.WriteFile(pidFile, []byte(mine), 0o600) // (the same pid; writes it again in case it disappeared)
		lg.Printf("supervisor: now on the installed binary (pid %d); proxy %d", os.Getpid(), proxyPid)
	} else {
		var err error
		if lock, err = os.OpenFile(config.Path("supervisor.lock"), os.O_CREATE|os.O_RDWR, 0o600); err != nil {
			return err
		}
		if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err == nil {
			os.WriteFile(pidFile, []byte(mine), 0o600)
		} else if !assume {
			return nil // a supervisor already exists
		} else {
			go func() {
				if syscall.Flock(int(lock.Fd()), syscall.LOCK_EX) == nil {
					os.WriteFile(pidFile, []byte(mine), 0o600)
				}
			}()
		}
		cfg, err := config.Load()
		if err != nil {
			cfg = config.Default() // the config may be what broke; the proxy decides (emergency)
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
				lg.Printf("supervisor: could not start: %v", err)
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
				lg.Printf("supervisor: stopped (%v)", s)
				stripPid()
				return nil
			case err := <-current.end:
				current = nil
				if err == nil {
					// intentional exit (switch between normal mode and emergency): comes back right
					// away
					lg.Printf("supervisor: mode change; restarting now")
					wait = time.Second
					continue
				}
				lg.Printf("supervisor: proxy went down (%v); restarting in %s (the port stays open)", err, wait)
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

// swapProxy starts a proxy with the installed binary and, once it is ready, tells the old one to
// exit and re-executes the supervisor with the installed binary. Returns the proxy that stays
// running. The result goes to troca.estado.
func swapProxy(me string, port int, lock *os.File, old *childProxy, lg *log.Logger) *childProxy {
	state := config.Path("troca.estado")
	newOne, ready, err := startProxy(me, port, lg)
	if err == nil {
		if err = waitReady(newOne, ready); err != nil {
			stopProxy(newOne)
		}
	}
	if err != nil {
		lg.Printf("supervisor: swap undone: %v", err)
		os.WriteFile(state, []byte("falhou: "+err.Error()), 0o600)
		return old
	}
	draining := 0
	if old != nil {
		old.p.Signal(syscall.SIGUSR2)
		draining = old.p.Pid
		// "ok" only when the old one released the socket: from then on, only the new one accepts
		// connections
		if !waitRelease(draining, port, 2*time.Second) {
			lg.Printf("supervisor: proxy %d still holds the port", draining)
		}
		lg.Printf("supervisor: proxy %d online; %d finishes what is in progress and exits", newOne.p.Pid, draining)
	}
	os.WriteFile(state, []byte("ok "+strconv.Itoa(newOne.p.Pid)), 0o600)
	// the supervisor also moves to the installed binary, without releasing the port or the lock
	tfd := int(lock.Fd()) // a regular file: Fd() changes nothing
	if err := noCloseOnExec(port, tfd); err == nil {
		env := append(os.Environ(), fmt.Sprintf("%s=%d %d %d %d", envInherit, port, tfd, newOne.p.Pid, draining))
		err = syscall.Exec(me, []string{me, "supervisionar"}, env)
		lg.Printf("supervisor: could not re-execute myself on the new binary (%v); staying on this one", err)
	}
	syscall.CloseOnExec(port)
	syscall.CloseOnExec(tfd)
	return newOne
}

// waitRelease waits until process pid no longer has the port's socket among its open files
// (/proc/PID/fd). Without /proc (outside Linux), it waits a little.
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
			return errors.Is(err, os.ErrNotExist) // exited
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

// noCloseOnExec lets the fds pass to the re-executed process.
func noCloseOnExec(fds ...int) error {
	for _, fd := range fds {
		if _, _, e := syscall.Syscall(syscall.SYS_FCNTL, uintptr(fd), syscall.F_SETFD, 0); e != 0 {
			return e
		}
	}
	return nil
}

// openPort opens the service port and returns the socket as a raw number (see startProxy). Taken:
// with assume (swap of an earlier version), it notes it in supervisor.esperando and tries again
// every 2 ms until the port is released; without it, tries again with growing waits (another
// program, or an old proxy without a supervisor).
func openPort(address string, assume bool, signals <-chan os.Signal, lg *log.Logger) (int, error) {
	mark := config.Path("supervisor.esperando")
	defer os.Remove(mark)
	wait, warnedIt := time.Second, false
	limitN := time.Now().Add(30 * time.Second)
	for {
		ln, err := net.Listen("tcp", address)
		if err == nil {
			// a copy of the socket outside the Go runtime (which would close it or change its mode)
			rc, err := ln.(*net.TCPListener).SyscallConn()
			fd := -1
			if err == nil {
				rc.Control(func(s uintptr) { fd, err = syscall.Dup(int(s)) })
			}
			ln.Close() // the copy keeps the port open
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
			lg.Printf("supervisor: port %s is taken; trying again", address)
			warnedIt = true
		}
		select {
		case <-time.After(wait):
		case s := <-signals:
			return -1, fmt.Errorf("stopped (%v) while waiting for the port", s)
		}
		if wait < 10*time.Second {
			wait *= 2
		}
	}
}

// installedBinary: where install and update put llm-dlp (and where the Claude Code hooks point).
func installedBinary() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "bin", "llm-dlp")
}

// serviceBinary: the program the supervisor and the proxy run: the installed one, if it exists;
// otherwise, this one. Not the caller's: install.sh runs install from a temporary folder it deletes
// right after, and a supervisor tied to that file could no longer start the proxy.
func serviceBinary() string {
	if d := installedBinary(); d != "" {
		if st, err := os.Stat(d); err == nil && st.Mode().IsRegular() && st.Mode()&0o111 != 0 {
			return d
		}
	}
	me, _ := os.Executable()
	return me
}

// supervisorUp: the supervisor's pid, if supervisor.pid points to a live llm-dlp supervisor (the
// number may have been reused by the system); 0 if not. Without the file, it looks for whoever
// holds the supervisor lock and writes the file again: when going from 0.7.0 to 0.8.0, the old
// supervisor deleted on exit the file the new one had just written, and without it update did not
// find the supervisor (it swapped the proxy cutting the response in progress, and the supervisor
// was never updated) and stop said there was no supervisor.
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

// isSupervisor: pid is an llm-dlp supervisor ("supervisionar", what llm-dlp starts: see startProxy;
// or "supervise").
func isSupervisor(pid int) bool {
	cmdline := commandLine(pid)
	return strings.Contains(cmdline, "supervis") && strings.Contains(cmdline, "llm-dlp")
}

// lockOwner: the process that holds the flock of the lock file, by /proc/locks (Linux only; 0 if
// nobody holds it or if it cannot be known).
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
	// "231: FLOCK ADVISORY WRITE 57709 08:30:1297200 0 EOF"; whoever waits for the lock appears on
	// a line with "->"
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

// waitExit waits for process pid to end (until the deadline).
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
