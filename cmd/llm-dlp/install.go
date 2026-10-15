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

// Commands install, uninstall and the lock (managed-settings).

// lockFile: Claude Code managed settings (a variable only so the tests use a temporary file).
var lockFile = "/etc/claude-code/managed-settings.json"

// rules llm-dlp adds to Claude Code's permissions
var denyLLMDLP = []string{
	"Read(~/.config/llm-dlp/**)",    // the key and the hashes are never read by the agent
	"Edit(~/.config/llm-dlp/**)",    // nor changed
	"Bash(*.config/llm-dlp*)",       // nor by a command (the configuration decides what is masked and where it goes)
	"Bash(*llm-dlp emergency*)",     // the agent does not try to turn on emergency (the real barrier is sudo)
	"Bash(*llm-dlp emergencia*)",    // (the old command name is still accepted)
	"Bash(*managed-settings.json*)", // nor touch the lock
}

// hooksLLMDLP: the command of each Claude Code hook. oldHooks: the names from before 0.9.0, which
// install replaces with the new ones and uninstall also recognizes.
var (
	hooksLLMDLP = map[string]string{"SessionStart": "ensure", "UserPromptSubmit": "check"}
	oldHooks    = map[string]string{"SessionStart": "garantir", "UserPromptSubmit": "verificar"}
)

// isLLMDLPHook: does the hook group (in JSON) call llm-dlp, with the new or the old name?
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
	noQuestion bool // --yes: answers "yes" to everything (for scripts)
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
	r := strings.ToLower(e.askIt(msg + " [y/N] "))
	return r == "y" || r == "yes" || r == "s" || r == "sim" // s/sim: what versions up to 0.9 asked for
}

func step(n int, title, because string) {
	fmt.Printf("\n[%d/5] %s\n      %s\n", n, title, because)
}

// withSudo runs a command as administrator, attached to the terminal (sudo asks for the password).
func withSudo(args ...string) error {
	c := exec.Command("sudo", args...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	return c.Run()
}

// stdinTerminal: whether a person can answer on stdin. /dev/null is a character device too (the
// stdin of a hook or of a command run by an agent), so it does not count.
func stdinTerminal() bool {
	st, err := os.Stdin.Stat()
	if err != nil || st.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	null, err := os.Stat(os.DevNull)
	return err != nil || !os.SameFile(st, null)
}

// install does everything, explaining each step. It can be run again without breaking anything.
func install(args []string) error {
	in := entry{r: bufio.NewReader(os.Stdin)}
	for _, a := range args {
		if a == "--yes" || a == "--sim" {
			in.noQuestion = true
		}
	}
	if os.Geteuid() == 0 {
		return errors.New("run it without sudo (the installation is your user's; the only step with sudo is the lock, at the end)")
	}
	home, _ := os.UserHomeDir()
	fmt.Println(`llm-dlp installation. There are 5 steps; it asks what it needs and shows what will change.
In two steps (OCR and lock) it uses sudo: your password will be asked at that time.`)

	// 1. program
	step(1, "Program", "copies llm-dlp to ~/.local/bin, so you and Claude Code can call it from any folder.")
	destination := filepath.Join(home, ".local", "bin", "llm-dlp")
	me, _ := os.Executable()
	if me, _ = filepath.EvalSymlinks(me); me != destination {
		if err := copyFile(me, destination, 0o755); err != nil {
			return err
		}
		fmt.Println("      ✓ copied to", destination)
	} else {
		fmt.Println("      ✓ already at", destination)
	}
	if !strings.Contains(":"+os.Getenv("PATH")+":", ":"+filepath.Dir(destination)+":") {
		fmt.Printf("      ! %s is not in your PATH. Add to ~/.bashrc:  export PATH=\"$HOME/.local/bin:$PATH\"\n", filepath.Dir(destination))
	}

	// 2. configuration and key
	step(2, "Configuration and key", "creates "+config.Dir()+" with the configuration and the secret key that generates the pseudonyms.")
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	changed := map[string]any{}
	if len(cfg.InternalDomains) == 0 {
		fmt.Println("\n      a) Internal domain: the piece that appears in the names of the company's servers.")
		fmt.Println("         With \"empresa\", mysql-prd.empresa.intra becomes mysql-x7k2.invalid. Without it, server")
		fmt.Println("         names go out as they are. (E-mails are always masked, no need to tell.)")
		r := in.askIt("         Type one or more, separated by commas (Enter to skip): ")
		for _, d := range strings.Split(r, ",") {
			if d = strings.TrimSpace(strings.ToLower(d)); d != "" {
				cfg.InternalDomains = append(cfg.InternalDomains, d)
				changed["internal_domains"] = cfg.InternalDomains
			}
		}
	}
	if len(cfg.Terms) == 0 {
		fmt.Println("\n      b) Names that can never go out: the name of the company or customer, of projects, of systems.")
		fmt.Println("         They are masked anywhere, including in folder and file names.")
		r := in.askIt("         Type one or more, separated by commas (Enter to skip): ")
		var vs []string
		for _, v := range strings.Split(r, ",") {
			if v = strings.TrimSpace(v); v != "" {
				vs = append(vs, v)
			}
		}
		if len(vs) > 0 {
			cfg.Terms = append(cfg.Terms, config.Term{Label: "name", Values: vs})
			changed["terms"] = cfg.Terms
		}
	}
	if _, err := os.Stat(config.Path("config.json")); errors.Is(err, os.ErrNotExist) {
		// first installation: the whole file, with the defaults visible
		if err := saveConfig(cfg); err != nil {
			return err
		}
	} else if len(changed) > 0 {
		// an existing file: only what was answered goes in; the rest stays as it was
		bak, err := config.SetFields(changed)
		if err != nil {
			return err
		}
		fmt.Println("      ✓ config.json updated (the previous one at " + bak + ")")
	}
	kp, err := config.DataPath(config.KeyFile)
	if err != nil {
		return err
	}
	if _, err := mask.Key(kp); err != nil {
		return err
	}
	fmt.Printf("\n      ✓ configuration: %s (internal domains: %d, protected names: %d)\n", config.Path("config.json"), len(cfg.InternalDomains), countTerms(cfg))
	fmt.Printf("      ✓ key: %s — keep a copy in your password manager\n", kp)

	// 3. OCR
	step(3, "OCR of images and PDFs", "with OCR, screenshots and PDFs have the sensitive data covered before going out. Without it, they are not sent (Claude gets a notice in their place).")
	if missingOne := missingOCR(cfg); missingOne != "" {
		cmd := ocr.InstallCommand()
		fmt.Printf("      %s missing. Command:  sudo sh -c '%s'\n", missingOne, cmd)
		if cmd != "" && !in.noQuestion && in.confirm("      Install now? (asks for the sudo password)") {
			if err := withSudo("sh", "-c", cmd); err != nil {
				fmt.Println("      ! the installation failed; run the command above by hand and then run llm-dlp install again")
			}
		}
		if missingOne := missingOCR(cfg); missingOne != "" {
			fmt.Println("      ✗ OCR missing: images and PDFs will not be sent to Claude (nothing leaks; it gets a notice in their place)")
		} else {
			fmt.Println("      ✓ OCR installed")
		}
	} else {
		fmt.Println("      ✓ tesseract (Portuguese) and poppler found")
	}

	// 4. Claude Code
	step(4, "Claude Code", "only for those who use Claude Code (with Gemini CLI, Codex, opencode or Copilot, answer n:\n      they are pointed by hand, see the README). Makes Claude Code go through llm-dlp. Changes\n      ~/.claude/settings.json (with a backup):")
	fmt.Printf(`        • ANTHROPIC_BASE_URL=http://%s  → messages go through llm-dlp
        • when a session opens: "llm-dlp ensure"   → starts llm-dlp if it is not online
        • on every message:     "llm-dlp check"    → blocks the message if something is wrong
        • forbids the agent from reading/editing ~/.config/llm-dlp and from touching emergency and the lock
`, address(cfg))
	if in.confirm("      Apply?") {
		bak, err := applyClaude(home, destination, cfg)
		if err != nil {
			return err
		}
		if bak != "" {
			fmt.Println("      ✓ applied; backup at", bak)
		} else {
			fmt.Println("      ✓ applied")
		}
		if err := ensure(); errors.Is(err, errNoProof) {
			fmt.Println("      ! another llm-dlp is online (from before this installation): close Claude Code and run \"llm-dlp stop\" for this one to take over")
		} else if err != nil {
			return err
		} else if info, _ := health(cfg); info.Commit != version.Commit || info.Version != version.Version {
			// an earlier version running: swap it for this one without taking llm-dlp offline
			if err := swapService(cfg, info, version.Commit); err != nil {
				fmt.Printf("      ! the previous version (%s) is still online: %v. Try: llm-dlp update\n", info.Version, err)
			} else {
				fmt.Printf("      ✓ llm-dlp online (%s in place of %s, without going offline)\n", version.Version, info.Version)
			}
		} else {
			fmt.Println("      ✓ llm-dlp online")
		}
	} else {
		fmt.Println("      – skipped (if you use Claude Code, it is NOT protected)")
	}
	withClaude := claudeEnabled(home, cfg)

	// 5. lock
	step(5, "Lock against silent leaks (recommended)",
		"if the configuration of step 4 is erased, Claude Code would go straight to the API unmasked.\n      The lock makes Claude Code refuse to work outside llm-dlp. It lives in a system folder,\n      which the agent cannot change — that is why it needs sudo:")
	if lockInstalled(address(cfg)) {
		if missing := lockMissingRules(); len(missing) == 0 {
			fmt.Println("      ✓ lock already installed")
		} else {
			fmt.Printf("      ! the lock lacks rules of this version: %s\n", strings.Join(missing, ", "))
			if !in.noQuestion && in.confirm("      Add them now? (asks for the sudo password)") {
				if err := withSudo(destination, "install-lock"); err != nil {
					fmt.Println("      ! failed; run by hand:  sudo", destination, "install-lock")
				}
			}
		}
	} else if !withClaude {
		fmt.Println("      – skipped: the lock is Claude Code's, which was not turned on in step 4")
	} else {
		if v := claudeCodeVersion(); v != "" && minorVersion(v, "2.1.285") {
			fmt.Printf("      ! your Claude Code is at %s; the lock requires 2.1.285 or newer. Update it and run llm-dlp install again.\n", v)
		} else if !in.noQuestion && in.confirm("      Install the lock now? (asks for the sudo password)") {
			if err := withSudo(destination, "install-lock"); err != nil {
				fmt.Println("      ! failed; run by hand:  sudo", destination, "install-lock")
			}
		}
	}

	// summary
	cfg, _ = config.Load()
	fmt.Println("\nSummary")
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
		fmt.Println("  ✓ llm-dlp online")
	case withClaude:
		ok(false, "", "llm-dlp offline: see "+config.Path("llm-dlp.log"))
	default:
		nothing("llm-dlp is not online yet: with another client, it starts with llm-dlp ensure (the README shows how to make that automatic)")
	}
	if withClaude {
		ok(true, "Claude Code goes through llm-dlp", "")
	} else {
		nothing("Claude Code not turned on (step 4 skipped). If you use Claude Code, run llm-dlp install again and accept step 4")
	}
	ok(missingOCR(cfg) == "", "OCR of images and PDFs", "no OCR: images and PDFs are not sent (to install: "+ocr.InstallHint()+")")
	switch {
	case lockInstalled(address(cfg)) && len(lockMissingRules()) > 0:
		ok(false, "", "lock without all the rules of this version: run sudo llm-dlp install-lock")
	case lockInstalled(address(cfg)):
		fmt.Println("  ✓ lock installed")
	case withClaude:
		ok(false, "", "no lock: run llm-dlp install again and accept step 5")
	default:
		nothing("no lock (it is only Claude Code's)")
	}
	if len(cfg.InternalDomains) > 0 || countTerms(cfg) > 0 {
		fmt.Println("  ✓ internal domains and protected names configured")
	} else {
		fmt.Println("  – no internal domain nor protected names (optional: run llm-dlp install again to add them)")
	}
	if withClaude {
		fmt.Println("\nTo take effect: close and open Claude Code (in VS Code, reload the window).")
	}
	fmt.Println(`
Other clients, in beta (Gemini CLI, Codex, opencode, Copilot with your key): this
installation neither points them to llm-dlp nor locks them. The llm-dlp address still has to go
into the client; it stays protected while it points here. How to do it, client by client: README,
section "Gemini CLI, Codex, opencode and Copilot (beta)".`)
	return nil
}

func countTerms(cfg config.Config) int {
	n := 0
	for _, t := range cfg.Terms {
		n += len(t.Values)
	}
	return n
}

// claudeEnabled: does Claude Code's settings.json point to llm-dlp?
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
	// leaves no empty leftovers that did not exist before the installation
	for _, k := range []string{"env", "hooks"} {
		if m, ok := s[k].(map[string]any); ok && len(m) == 0 {
			delete(s, k)
		}
	}
	bak, err := writeSettings(target, s, orig)
	if err != nil {
		return err
	}
	// The proxy is NOT stopped here: Claude Code applies settings.json to sessions already open,
	// but an open session keeps pointing to the proxy until it is restarted. Stopping now would
	// leave those sessions without an answer (fail closed).
	fmt.Println("✓ removed from Claude Code (backup at " + bak + ").")
	fmt.Println("  llm-dlp stays online for the sessions already open. Close and open Claude Code")
	fmt.Println("  (or VS Code) and then run: llm-dlp stop")
	cfg, _ := config.Load()
	if lockInstalled(address(cfg)) {
		fmt.Println("! The lock is still installed: without llm-dlp, Claude Code will refuse to work. Remove it with:")
		fmt.Println("      sudo llm-dlp uninstall-lock")
	}
	fmt.Println("The folder", config.Dir(), "(config and key) was kept; delete it by hand if you want.")
	return nil
}

// installLock writes allowedProviders + ANTHROPIC_BASE_URL into managed-settings (root).
func installLock(remove bool) error {
	if os.Geteuid() != 0 {
		return errors.New("needs sudo (the lock file lives in a system folder)")
	}
	port := 8787
	if u := os.Getenv("SUDO_USER"); u != "" { // uses the port configured by the user
		if uu, err := user.Lookup(u); err == nil {
			if b, err := os.ReadFile(filepath.Join(uu.HomeDir, ".config", "llm-dlp", "config.json")); err == nil {
				var c config.Config // (with the new or the old names)
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
		fmt.Println("✓ lock removed from", lockFile)
	} else {
		if bak != "" {
			bak = " (backup at " + bak + ")"
		}
		fmt.Printf("✓ lock installed at %s%s\n  Claude Code will only work going through llm-dlp at 127.0.0.1:%d.\n", lockFile, bak, port)
	}
	return nil
}

// applyLock adds (or removes) the lock in a managed-settings.json, keeping the rest.
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
	// the llm-dlp hooks already there (with the new or the old name) go out and come in again:
	// there are never two
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

// stripHooks removes the llm-dlp hooks from settings.json (with the new or the old name).
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

// byHooks adds the llm-dlp hooks, with the given command names.
func byHooks(s map[string]any, bin string, names map[string]string) {
	hooks, ok := s["hooks"].(map[string]any)
	if !ok {
		hooks = map[string]any{}
		s["hooks"] = hooks
	}
	for _, event := range []string{"SessionStart", "UserPromptSubmit"} {
		list, _ := hooks[event].([]any)
		// explicit timeout: a hook that runs out of time is ignored by Claude Code (it would fail
		// open)
		hooks[event] = append(list, map[string]any{"hooks": []any{
			map[string]any{"type": "command", "command": bin + " " + names[event], "timeout": timeoutHook}}})
	}
}

// hooksForVersion: after swapping the binary for version v, the Claude Code hooks call the command
// names it knows: the English ones since 0.9.0, the old ones before (going back to an earlier
// version with the new names would block every message with "unknown command"). It only touches
// settings.json if it already calls llm-dlp and the names change. Returns whether it changed.
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
			return nil, nil, fmt.Errorf("%s is not valid JSON: %w", target, err)
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
		// unique name: never overwrites an earlier backup (two runs in the same second)
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
	return os.Rename(tmp, to) // atomic swap (works even with the old binary running)
}

func missingOCR(cfg config.Config) string {
	var missingOne []string
	if _, err := exec.LookPath(cfg.OCR.Tesseract); err != nil {
		missingOne = append(missingOne, "tesseract")
	} else if out, err := exec.Command(cfg.OCR.Tesseract, "--list-langs").CombinedOutput(); err != nil || !strings.Contains(string(out), "\npor") {
		missingOne = append(missingOne, "tesseract's Portuguese language")
	}
	if _, err := exec.LookPath(cfg.OCR.PDFToText); err != nil {
		missingOne = append(missingOne, "poppler (pdftotext)")
	}
	return strings.Join(missingOne, ", ")
}

// missingDeny: the llm-dlp rules a deny list does not have.
func missingDeny(deny []any) []string {
	var missing []string
	for _, r := range denyLLMDLP {
		has := false
		for _, d := range deny {
			has = has || d == r
		}
		if !has {
			missing = append(missing, r)
		}
	}
	return missing
}

// denyOf: the deny list of a settings file (nil if the file or the list does not exist).
func denyOf(s map[string]any) []any {
	perms, _ := s["permissions"].(map[string]any)
	deny, _ := perms["deny"].([]any)
	return deny
}

// completeDenyRules adds to ~/.claude/settings.json the llm-dlp rules it does not have yet, when
// Claude Code goes through llm-dlp (it has the hooks). It keeps everything else, with a backup.
func completeDenyRules(home string) ([]string, error) {
	missing, target, s, orig, err := settingsMissingRules(home)
	if err != nil || len(missing) == 0 {
		return nil, err
	}
	perms, _ := s["permissions"].(map[string]any)
	if perms == nil {
		perms = map[string]any{}
		s["permissions"] = perms
	}
	deny := denyOf(s)
	for _, r := range missing {
		deny = append(deny, r)
	}
	perms["deny"] = deny
	if _, err := writeSettings(target, s, orig); err != nil {
		return nil, err
	}
	return missing, nil
}

// settingsMissingRules: the llm-dlp rules missing in ~/.claude/settings.json, if it calls llm-dlp.
func settingsMissingRules(home string) ([]string, string, map[string]any, []byte, error) {
	target := filepath.Join(home, ".claude", "settings.json")
	s, orig, err := readSettings(target)
	if err != nil {
		return nil, target, nil, nil, err
	}
	if b, _ := json.Marshal(s["hooks"]); !isLLMDLPHook(b) {
		return nil, target, s, orig, nil
	}
	return missingDeny(denyOf(s)), target, s, orig, nil
}

// lockMissingRules: the llm-dlp rules missing in the lock, when there is a lock (one installed by
// a version before them). Only reads: writing there needs sudo (llm-dlp install-lock).
func lockMissingRules() []string {
	if _, err := os.Stat(lockFile); err != nil {
		return nil
	}
	s, _, err := readSettings(lockFile)
	if err != nil {
		return nil
	}
	return missingDeny(denyOf(s))
}

// offerLockUpdate: if the lock lacks rules of this version, says which and, at a terminal, offers to
// add them now with sudo (bin install-lock keeps everything else in the file).
func offerLockUpdate(bin string) {
	missing := lockMissingRules()
	if len(missing) == 0 {
		return
	}
	fmt.Printf("! the lock (%s) lacks rules of this version: %s\n", lockFile, strings.Join(missing, ", "))
	if stdinTerminal() && (entry{r: bufio.NewReader(os.Stdin)}).confirm("  Add them now? (asks for the sudo password)") {
		if err := withSudo(bin, "install-lock"); err == nil {
			return
		}
	}
	fmt.Println("  To add them: sudo llm-dlp install-lock")
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

// claudeCodeVersion: the one of the CLI in PATH or the highest among the VS Code extensions.
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
