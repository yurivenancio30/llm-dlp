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

// The update and reload commands: put another binary online (or the same one, with the new
// configuration) without taking llm-dlp offline. How the swap works: swap.go.

const urlReleases = "https://github.com/yurivenancio30/llm-dlp/releases"

// firstInEnglish: the first version with the commands and the config.json fields in English.
const firstInEnglish = "0.9.0"

// firstWithEnglishTypes: the first version with the detector and object type names in English
// (disabled_detectors, objects.mask...).
const firstWithEnglishTypes = "0.10.0"

var reExactVersion = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)

func update(args []string) error {
	var asked, fileName string
	for i := 0; i < len(args); i++ {
		switch a := args[i]; a {
		case "--version", "--file", "--versao", "--arquivo": // (--versao e --arquivo: nomes antigos)
			if i+1 >= len(args) {
				return fmt.Errorf("%s needs a value", a)
			}
			i++
			if a == "--file" || a == "--arquivo" {
				fileName = args[i]
			} else {
				asked = strings.TrimPrefix(args[i], "v")
			}
		default:
			return fmt.Errorf("unknown option: %q (usage: llm-dlp update [--version X.Y.Z] [--file FILE.tar.gz])", a)
		}
	}
	if os.Geteuid() == 0 {
		return errors.New("run it without sudo: the installation is your user's")
	}
	destination := installedBinary()
	if st, err := os.Stat(destination); err != nil || !st.Mode().IsRegular() {
		return fmt.Errorf("llm-dlp is not installed at %s; to install it, use install.sh (README)", destination)
	}
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	// in the destination folder: the new binary comes in with a rename (atomic)
	tmp, err := os.MkdirTemp(filepath.Dir(destination), ".llm-dlp-update-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if fileName == "" {
		v, err := publishedVersion(urlReleases, asked)
		if err != nil {
			return err
		}
		fmt.Printf("Downloading llm-dlp %s (linux/%s)...\n", v, runtime.GOARCH)
		if fileName, err = downloadRelease(urlReleases, v, tmp); err != nil {
			return err
		}
		fmt.Println("✓ checksum verified")
	}
	newOne := filepath.Join(tmp, "llm-dlp")
	if err := extractBinary(fileName, newOne); err != nil {
		return err
	}
	vNew, cNew, err := versionOf(newOne)
	if err != nil {
		return fmt.Errorf("the new binary does not run on this machine: %w", err)
	}
	vCurrent, cCurrent, _ := versionOf(destination)
	info, isUp := health(cfg)
	if info.WithoutProof {
		return errNoProof
	}
	same := vNew == vCurrent && cNew == cCurrent
	if same && (!isUp || info.Commit == cNew && info.Version == vNew) {
		fmt.Printf("✓ already on %s (%s)\n", vNew, cNew)
		home, _ := os.UserHomeDir()
		adjustNames(home, destination, vNew)
		return nil
	}
	// a version older than the English names would ignore the config.json fields with a new name
	// (and stop masking what they ask for): the file goes back to the old names before it starts.
	// Going forward, the new names come in only after the swap (until then, the running version may
	// need to reread the file).
	previous, typesPrevious := minorVersion(vNew, firstInEnglish), minorVersion(vNew, firstWithEnglishTypes)
	if previous || typesPrevious {
		if c, err := config.MigrateFile(previous, typesPrevious); err != nil {
			return fmt.Errorf("could not move config.json to the names of %s: %w", vNew, err)
		} else if c != "" {
			fmt.Printf("✓ config.json with the names of %s (the previous one at %s)\n", vNew, c)
		}
	}
	bak := ""
	if !same {
		if bak, err = swapBinary(newOne, destination); err != nil {
			return err
		}
		fmt.Printf("✓ %s (%s) → %s (%s) at %s\n", vCurrent, cCurrent, vNew, cNew, destination)
	}
	goBack := func() {
		if bak != "" {
			os.Rename(bak, destination)
		}
		if previous || typesPrevious {
			config.MigrateFile(false, false)
		}
	}
	log := config.Path("llm-dlp.log")
	home, _ := os.UserHomeDir()
	adjustHooks := func() { adjustNames(home, destination, vNew) }
	if !isUp {
		if !claudeEnabled(home, cfg) {
			cleanCopies(destination, bak)
			fmt.Println("– llm-dlp was not online: it starts on the new version when it is used")
			fmt.Println("  (Claude Code does not go through llm-dlp; to turn it on: llm-dlp install)")
			return nil
		}
		if err := ensure(); err != nil {
			goBack()
			return fmt.Errorf("the new version did not start (%v); went back to %s. See %s", err, vCurrent, log)
		}
		cleanCopies(destination, bak)
		adjustHooks()
		fmt.Printf("✓ llm-dlp %s online\n", vNew)
		return nil
	}
	if err := swapService(cfg, info, cNew); err != nil {
		goBack()
		// a version without swap may already have exited: start it again, with the binary back
		if _, ok := health(cfg); !ok {
			ensure()
		}
		return fmt.Errorf("%v; went back to %s. See %s", err, vCurrent, log)
	}
	cleanCopies(destination, bak)
	adjustHooks()
	fmt.Printf("✓ llm-dlp %s online. No need to close Claude Code.\n", vNew)
	if !info.Swap {
		fmt.Println("  The previous version did not know how to hand over: a response that was in the middle was cut" +
			" (Claude Code retries by itself). The next updates cut nothing.")
	}
	return nil
}

// adjustNames moves the Claude Code hooks and config.json to the names version v (that of binary
// bin) knows. It runs after putting v online, and also when it was already online: from 0.8.0 to
// here with "llm-dlp atualizar", the code that ran was 0.8.0's, which did not know how to do this
// (the old names still apply; they just had not been swapped).
func adjustNames(home, bin, v string) {
	if changed, err := hooksForVersion(home, bin, v); err != nil {
		fmt.Printf("! could not adjust the Claude Code hooks (%v); run: llm-dlp install\n", err)
	} else if changed {
		fmt.Println("✓ Claude Code hooks with this version's names (they apply to new sessions;" +
			" the ones already open keep the previous ones, which also work)")
	}
	if !minorVersion(v, firstInEnglish) {
		if c, err := config.MigrateFile(false, minorVersion(v, firstWithEnglishTypes)); err != nil {
			fmt.Printf("! config.json keeps the old names (%v); they still apply\n", err)
		} else if c != "" {
			fmt.Printf("✓ config.json with the English names (the previous one at %s)\n", c)
		}
	}
}

// reload puts the installed binary online again, with the configuration as it is now, without
// taking llm-dlp offline.
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
			return errors.New("the llm-dlp online does not answer on the configured port (did the port change?): " +
				"close Claude Code, run llm-dlp stop and then llm-dlp install")
		}
		if v, _, err := versionOf(installedBinary()); err == nil {
			adjustNames(home, installedBinary(), v)
		}
		fmt.Println("– llm-dlp is not online: it starts with the new configuration when it is used")
		return nil
	}
	v, commit, err := versionOf(serviceBinary())
	if err != nil {
		return err
	}
	if err := swapService(cfg, info, commit); err != nil {
		return fmt.Errorf("%v; the previous llm-dlp is still online. See %s", err, config.Path("llm-dlp.log"))
	}
	adjustNames(home, serviceBinary(), v)
	fmt.Println("✓ configuration applied; llm-dlp did not go offline")
	return nil
}

// swapService puts the installed binary online in place of the old proxy (its health in old) and
// checks that the new one (commit) is the one answering. See swap.go.
func swapService(cfg config.Config, old healthInfo, commit string) error {
	sup := supervisorUp()
	if old.Swap && sup > 0 {
		// the supervisor holds the port: it swaps the proxy itself and reports how it went
		state := config.Path("troca.estado")
		os.Remove(state)
		if err := syscall.Kill(sup, syscall.SIGHUP); err != nil {
			return err
		}
		r, ok := waitContent(state, readyDeadline+5*time.Second)
		if !ok {
			return errors.New("the supervisor did not answer the swap request")
		}
		if reason, failed := strings.CutPrefix(r, "falhou: "); failed {
			return errors.New("the new version did not start: " + reason)
		}
		if !waitNew(cfg, commit, old.PID, 5, nil) {
			return errors.New("the new version started but does not answer")
		}
		return nil
	}
	// a version older than this one (the supervisor does not hold the port): a new supervisor waits
	// for the port; the old one is stopped, and the port is empty only between one closing and the
	// other opening
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
		return giveUp(fmt.Sprintf("the new version did not become ready within %s", readyDeadline))
	}
	// the old supervisor (otherwise it starts the proxy again); without a supervisor, the proxy
	// itself
	if target := cmp.Or(sup, old.PID); target > 0 {
		syscall.Kill(target, syscall.SIGTERM)
	}
	if !waitNew(cfg, commit, old.PID, 5, wentOut) {
		return giveUp("the new version did not take over the port")
	}
	return nil
}

// waitContent: the content of file p, when it appears (until the deadline).
func waitContent(p string, deadline time.Duration) (string, bool) {
	for limitN := time.Now().Add(deadline); time.Now().Before(limitN); time.Sleep(20 * time.Millisecond) {
		if b, err := os.ReadFile(p); err == nil && len(b) > 0 {
			return string(b), true
		}
	}
	return "", false
}

// waitNew: until readyDeadline, waits for consecutive health answers, with proof, from a proxy with
// this commit that is not the old one. Gives up if the new supervisor exits.
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

// publishedVersion: the requested one, or the newest (the "latest" page redirects to
// .../tag/vX.Y.Z, without an API and without a call limit).
func publishedVersion(base, asked string) (string, error) {
	if asked != "" {
		if !reExactVersion.MatchString(asked) {
			return "", fmt.Errorf("invalid version: %q (format X.Y.Z)", asked)
		}
		return asked, nil
	}
	c := &http.Client{Timeout: 30 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	r, err := c.Get(base + "/latest")
	if err != nil {
		return "", fmt.Errorf("could not talk to GitHub to find the newest version: %w", err)
	}
	r.Body.Close()
	v := strings.TrimPrefix(path.Base(r.Header.Get("Location")), "v")
	if !reExactVersion.MatchString(v) {
		return "", fmt.Errorf("could not find the newest version (GitHub answered %s)", r.Status)
	}
	return v, nil
}

// downloadRelease downloads the version's archive for this machine and checks the SHA-256 against
// the checksums.txt published with it. Returns the archive path.
func downloadRelease(base, v, dir string) (string, error) {
	if runtime.GOOS != "linux" {
		return "", errors.New("there is a ready binary only for Linux (including WSL); on another system, build from the source (README)")
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
		return "", fmt.Errorf("checksum does not match for %s (expected %s, got %s): nothing was swapped", name, expected, real)
	}
	return file, nil
}

func download(url, to string, max int64) error {
	c := &http.Client{Timeout: 5 * time.Minute}
	r, err := c.Get(url)
	if err != nil {
		return fmt.Errorf("could not download %s: %w", url, err)
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		return fmt.Errorf("could not download %s: %s", url, r.Status)
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
		err = fmt.Errorf("%s passed %d bytes", url, max)
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
	return "", fmt.Errorf("%s is not in checksums.txt", name)
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

// extractBinary takes llm-dlp out of the archive (.tar.gz) and writes it to to, executable.
func extractBinary(pkgName, to string) error {
	f, err := os.Open(pkgName)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("%s is not a .tar.gz: %w", pkgName, err)
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return fmt.Errorf("package %s does not have llm-dlp", pkgName)
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

// versionOf runs "BIN versao" and returns the version and the commit.
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

// swapBinary puts newOne in place of destination and keeps the previous one alongside
// (destination.bak-DATE), to go back if the new version does not start. Returns the path of the
// copy.
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

// cleanCopies deletes the old copies of the binary (destination.bak-*), except the one just made:
// one way back is enough, and each copy is about 15 MB.
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
