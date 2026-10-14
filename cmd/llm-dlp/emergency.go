package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yurivenancio30/llm-dlp/internal/config"
	"github.com/yurivenancio30/llm-dlp/internal/proxy"
	"github.com/yurivenancio30/llm-dlp/internal/version"
)

// Emergency mode: lets Claude through without masking for a limited time (requires sudo).

// Emergency mode: when llm-dlp breaks, Claude Code stops working (fail closed) and you are left
// without it precisely to fix llm-dlp. Emergency replaces the proxy with a pass-through WITHOUT
// masking, for a limited time.
//
// Only the administrator turns it on (sudo): the state lives in emergencyDir, which only root
// writes. The proxy runs as the user and only READS that file. So nothing running as your user —
// including the agent, induced by a malicious page — can turn emergency on. (If your sudo does not
// ask for a password, this barrier does not exist. The configuration does not have this barrier:
// whoever changes it to remove protection gets a warning on every message, see reducedProtection.)
//
// The pass-through does not load config/key/detectors: it works even if that is what broke.

// emergencyDir is fixed in the production binary. Tests change it via -ldflags "-X
// main.emergencyDir=..." (on purpose it is NOT an environment variable: the agent could restart
// the proxy pointing to a folder it controls).
var emergencyDir = "/etc/llm-dlp"

const defaultEmergencyDuration = 30 * time.Minute

// emergencyState: the deadline, also under "ate", the name versions up to 0.9 read.
type emergencyState struct {
	Until    time.Time `json:"until"`
	OldUntil time.Time `json:"ate"`
}

// emergencyFiles: the state file and the one with the name up to 0.9. Emergency is written to both
// and turned off in both: whichever version is online (during an update or after going back) sees it.
func emergencyFiles() []string {
	return []string{filepath.Join(emergencyDir, "emergency.json"), filepath.Join(emergencyDir, "emergencia.json")}
}

func emergencyActive() (time.Time, bool) {
	for _, f := range emergencyFiles() {
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var e emergencyState
		if json.Unmarshal(b, &e) != nil {
			continue
		}
		until := e.Until
		if until.IsZero() {
			until = e.OldUntil
		}
		if time.Now().Before(until) {
			return until, true
		}
	}
	return time.Time{}, false
}

func emergency(args []string) error {
	if os.Geteuid() != 0 {
		return errors.New("emergency mode turns masking off, so only the administrator turns it on. Run: sudo llm-dlp emergency [30m|off]")
	}
	if len(args) > 0 && (args[0] == "off" || args[0] == "sair") { // ("sair": the old name)
		for _, f := range emergencyFiles() {
			if err := os.Remove(f); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		fmt.Println("emergency mode ended; within 2 seconds the normal proxy (with masking) is back.")
		return nil
	}
	dur := defaultEmergencyDuration
	for _, a := range args {
		d, err := time.ParseDuration(a)
		if err != nil || d <= 0 || d > 4*time.Hour {
			return fmt.Errorf("invalid duration %q (use, e.g., 15m or 1h; at most 4h)", a)
		}
		dur = d
	}
	if st, err := os.Stdin.Stat(); err != nil || st.Mode()&os.ModeCharDevice == 0 {
		return errors.New("run it in a terminal (the confirmation is typed)")
	}
	fmt.Printf(`
⚠️  EMERGENCY MODE: for %s, messages will go to the API UNMASKED.
    Use a NEW Claude Code session, with no customer data, only to fix llm-dlp.
    To leave earlier: sudo llm-dlp emergency off

Type UNLOCK to continue: `, dur)
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	if strings.TrimSpace(line) != "UNLOCK" {
		return errors.New("canceled")
	}
	if err := os.MkdirAll(emergencyDir, 0o755); err != nil {
		return err
	}
	until := time.Now().Add(dur)
	b, _ := json.Marshal(emergencyState{Until: until, OldUntil: until})
	for _, f := range emergencyFiles() {
		if err := os.WriteFile(f, b, 0o644); err != nil {
			return err
		}
	}
	fmt.Printf("on until %s (within 2 seconds the unmasked pass-through takes over). After that, the normal proxy comes back by itself.\n",
		time.Now().Add(dur).Format("15:04"))
	return nil
}

// watchEmergency ends the process when the mode changes (the supervisor starts the right mode).
func watchEmergency(inEmergency bool) {
	for range time.Tick(2 * time.Second) {
		if _, active := emergencyActive(); active != inEmergency {
			os.Exit(0)
		}
	}
}

// serveEmergency: minimal pass-through, without masking, until the deadline or until the file
// disappears.
func serveEmergency(until time.Time) error {
	cfg, err := config.Load()
	if err != nil {
		cfg = config.Default() // the config may be exactly what broke
	}
	ln, borrowed, err := servicePort(address(cfg))
	if err != nil {
		return err
	}
	target, err := url.Parse(cfg.Upstream)
	if err != nil {
		target, _ = url.Parse(config.Default().Upstream)
	}
	lg := openLog()
	rp := httputil.NewSingleHostReverseProxy(target)
	rp.FlushInterval = -1 // streaming without delay
	dir := rp.Director
	rp.Director = func(r *http.Request) {
		dir(r)
		r.Host = target.Host
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/__llm-dlp/saude", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "servico": "llm-dlp", "modo": "emergencia",
			"ate": until.Format(time.RFC3339), "versao": proxy.Version, "commit": version.Commit, "pid": os.Getpid(),
			"troca": borrowed})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		lg.Printf("EMERGENCY (unmasked): %s %s", r.Method, proxy.Clean(r.URL.Path))
		rp.ServeHTTP(w, r)
	})
	lg.Printf("EMERGENCY: unmasked pass-through online until %s", until.Local().Format("15:04"))
	go watchEmergency(true)
	return serveUntilSignal(&http.Server{Handler: proxy.LocalOnly(mux), ReadHeaderTimeout: 30 * time.Second}, ln, lg, nil)
}
