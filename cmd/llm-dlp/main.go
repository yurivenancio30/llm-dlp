package main

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"

	"github.com/yurivenancio30/llm-dlp/internal/config"
	"github.com/yurivenancio30/llm-dlp/internal/mask"
	"github.com/yurivenancio30/llm-dlp/internal/version"
)

// llm-dlp: entry point and command list.

const help = `llm-dlp %s — masks sensitive data between you and the LLM (Claude Code)

Usage: llm-dlp COMMAND [arguments]

Install and update
  llm-dlp install               installs and configures everything, explaining each step (can run again)
  sudo llm-dlp install-lock     (recommended) Claude Code only works going through llm-dlp
  llm-dlp uninstall             undoes the Claude Code configuration
  sudo llm-dlp uninstall-lock   removes the lock
  llm-dlp update [--version X.Y.Z] [--file FILE.tar.gz]
                                downloads the new version (or the one asked), checks it and swaps the
                                one online without taking it offline: no need to close Claude Code

Day to day (usually you need none of this)
  llm-dlp status                shows whether it is online, which version, and what is missing (OCR, new binary)
  llm-dlp reload                applies a configuration change, without taking llm-dlp offline
  llm-dlp stop                  stops llm-dlp, with Claude Code closed; it comes back by itself on the next
                                message
  llm-dlp import-people FILE.csv --group CODE:NAME:EMAIL [--name-separator /]
                                teaches people's names and e-mails (stored only as hashes)
  sudo llm-dlp emergency [30m|off]
                                if llm-dlp breaks: lets Claude through UNMASKED for a limited time

Check what is masked
  llm-dlp last [N]              shows the last N messages as the API received them (already masked)
  llm-dlp audit [24h|7d]        sums up the log of the period: what was masked by type, pseudonyms that
                                came back without turning into the real value, unmasked sends, refusals
  llm-dlp test < FILE           shows the masked version of a text
  llm-dlp test-media FILE DIR   processes an image or PDF as the proxy does and writes the result to DIR
  llm-dlp columns FILE          shows how the fields of a file are understood
  llm-dlp simulate SESSION.jsonl replays an old Claude Code session, without talking to the API, and checks it
  llm-dlp measure FILE.jsonl... time and coverage over old sessions (counts only)

Other
  llm-dlp version               shows the version and the commit (also: --version)
  llm-dlp help                  shows this help (also: --help, -h)

Configuration: %s (after changing it: llm-dlp reload)
Log:           %s
Documentation: https://github.com/yurivenancio30/llm-dlp

Internal (called by the Claude Code hooks and the supervisor):
  ensure, check, serve, supervise
`

func helpText() string {
	return fmt.Sprintf(help, version.Version, config.Path("config.json"), config.Path("llm-dlp.log"))
}

// oldNames: the Portuguese names from before 0.9.0. They are still accepted (hooks already
// installed in Claude Code call "garantir" and "verificar", and the supervisor starts "servir" and
// "supervisionar", which every version understands: see startProxy), but they do not appear in the
// help nor in the docs.
var oldNames = map[string]string{
	"ajuda": "help", "servir": "serve", "supervisionar": "supervise", "garantir": "ensure",
	"verificar": "check", "atualizar": "update", "recarregar": "reload", "emergencia": "emergency",
	"parar": "stop", "importar-pessoas": "import-people", "testar": "test", "testar-midia": "test-media",
	"medir": "measure", "colunas": "columns", "ultima": "last", "simular": "simulate",
	"instalar": "install", "desinstalar": "uninstall", "instalar-trava": "install-lock",
	"desinstalar-trava": "uninstall-lock", "versao": "version",
}

func main() {
	if len(os.Args) < 2 {
		fmt.Print(helpText())
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	if newOne, ok := oldNames[cmd]; ok {
		cmd = newOne
	}
	var err error
	switch cmd {
	case "help", "--help", "-h":
		fmt.Print(helpText())
	case "serve":
		err = serve(args)
	case "supervise":
		err = supervise(args)
	case "update":
		err = update(args)
	case "reload":
		err = reload()
	case "ensure":
		err = ensure()
	case "status":
		err = status()
	case "check":
		err = verifyIt()
	case "emergency":
		err = emergency(args)
	case "stop":
		err = stop()
	case "import-people":
		err = importPeople(args)
	case "test":
		err = test()
	case "test-media":
		err = testMedia(args)
	case "measure":
		err = measure(args)
	case "columns":
		err = columns(args)
	case "last":
		err = lastConv(args)
	case "simulate":
		err = simulate(args)
	case "audit":
		err = audit(args)
	case "install":
		err = install(args)
	case "uninstall":
		err = uninstall(args)
	case "install-lock":
		err = installLock(false)
	case "uninstall-lock":
		err = installLock(true)
	case "version", "--version", "-v":
		fmt.Println("llm-dlp", version.Complete())
	default:
		fmt.Fprintf(os.Stderr, "llm-dlp: unknown command: %q\n\n", cmd)
		fmt.Fprint(os.Stderr, helpText())
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "llm-dlp:", err)
		os.Exit(1)
	}
}

func loadAll() (config.Config, []byte, *mask.People, *mask.Seen, *mask.Masker, error) {
	cfg, err := config.Load()
	if err != nil {
		return cfg, nil, nil, nil, nil, fmt.Errorf("config: %w", err)
	}
	var paths [3]string
	for i, name := range []string{config.KeyFile, config.PeopleFile, config.SeenFile} {
		if paths[i], err = config.DataPath(name); err != nil {
			return cfg, nil, nil, nil, nil, err
		}
	}
	key, err := mask.Key(paths[0])
	if err != nil {
		return cfg, nil, nil, nil, nil, err
	}
	people, err := mask.LoadPeople(paths[1])
	if err != nil {
		return cfg, nil, nil, nil, nil, err
	}
	seenSet, err := mask.LoadSeen(paths[2])
	if err != nil {
		return cfg, nil, nil, nil, nil, err
	}
	m, err := mask.NewMasker(cfg, key, people, seenSet)
	return cfg, key, people, seenSet, m, err
}

func openLog() *log.Logger {
	l := &rotatingLog{p: config.Path("llm-dlp.log")}
	l.mu.Lock()
	l.reopen()
	l.mu.Unlock()
	if l.f == nil {
		return log.New(os.Stderr, "", log.LstdFlags)
	}
	return log.New(l, "", log.LstdFlags)
}

// maxLog: the size at which llm-dlp.log rotates. Only one old file stays (llm-dlp.log.1): on disk,
// about 20 MB at most.
var maxLog int64 = 10 << 20

// rotatingLog: llm-dlp.log, which rotates when it passes maxLog at any time (before, it only
// rotated when llm-dlp started, and a proxy running for weeks made the file grow without limit).
// More than one process writes to it (supervisor and proxy): whoever notices the other one rotated
// reopens it.
type rotatingLog struct {
	mu sync.Mutex
	p  string
	f  *os.File
}

func (l *rotatingLog) reopen() {
	if l.f != nil {
		l.f.Close()
	}
	l.f, _ = os.OpenFile(l.p, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
}

func (l *rotatingLog) Write(b []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	st, err := os.Stat(l.p)
	if err == nil && st.Size()+int64(len(b)) > maxLog {
		os.Rename(l.p, l.p+".1")
		l.reopen()
	} else if fst, e := statOf(l.f); err != nil || e != nil || !os.SameFile(st, fst) {
		l.reopen() // another process rotated it (or the file was deleted)
	}
	if l.f == nil {
		return os.Stderr.Write(b)
	}
	return l.f.Write(b)
}

func statOf(f *os.File) (os.FileInfo, error) {
	if f == nil {
		return nil, os.ErrClosed
	}
	return f.Stat()
}

// logFile: the file the log writes to now (the supervisor passes it as the proxy's error output, so
// a panic is recorded), or nil.
func logFile(lg *log.Logger) *os.File {
	switch w := lg.Writer().(type) {
	case *os.File:
		return w
	case *rotatingLog:
		w.mu.Lock()
		defer w.mu.Unlock()
		return w.f
	}
	return nil
}

func address(cfg config.Config) string { return "127.0.0.1:" + strconv.Itoa(cfg.Port) }
