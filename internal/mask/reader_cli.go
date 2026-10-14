package mask

import (
	"regexp"
	"strings"
)

// Command-line reader: options that carry the name of a resource, the same in any tool that follows
// the convention.
//
//	-h/--host, -S/--server        server
//	-d/-D/--dbname/--database     database
//	-U/-u/--username/--user       user
//	-n/--namespace                namespace
//	<type>/<name>                 deploy/x, svc/x, sts/x, pod/x, cm/x, secret/x, ns/x, sa/x...
//	host:/path, user@host:        remote copy (scp, rsync)
//
// Long options already stay with the key-value reader (--host x, --host=x). Short ones are
// ambiguous (-h is help, -d is "detach" or "data" in many tools): they only count when the same
// command has at least two of them with a value (psql -h x -d y -U z), or, for -n, when the command
// also cites a <type>/<name> resource or an operation subcommand.

var shortOption = map[string]string{"-h": "server", "-S": "server", "-d": "database", "-D": "database",
	"-U": "user", "-u": "user", "-n": "namespace"}

var reTypeName = regexp.MustCompile(`^(deploy|deployment|deployments|svc|service|services|sts|statefulset|statefulsets|ds|daemonset|po|pod|pods|cm|configmap|configmaps|secret|secrets|job|jobs|cj|cronjob|cronjobs|ing|ingress|ns|namespace|namespaces|pvc|sa|serviceaccount|rs|replicaset|hpa|node|nodes)/([a-z0-9][a-z0-9.-]{0,252})$`)

var entTypeName = map[string]string{"ns": "namespace", "namespace": "namespace", "namespaces": "namespace",
	"sa": "user", "serviceaccount": "user", "node": "server", "nodes": "server"}

// operation subcommands that go with -n (namespace)
var operationSubcommands = setOf("get", "describe", "logs", "exec", "rollout", "scale", "delete", "apply", "edit",
	"patch", "port-forward", "top", "create", "expose", "attach", "cp", "install", "upgrade", "uninstall", "status")

// afterCommand: token k comes after an operation verb (get, logs, rollout restart...), an option
// (-n x, --all) or another type/name, on the same command line.
func afterCommand(s string, toks []cliToken, k int) bool {
	for j := k - 1; j >= 0 && j >= k-4; j-- {
		v := s[toks[j].a:toks[j].b]
		if operationSubcommands[v] || strings.HasPrefix(v, "-") || cliTypeName(v) != nil || v == "restart" || v == "history" || v == "undo" {
			return true
		}
		if cliNumber(v) {
			return false
		}
	}
	return false
}

// cliTypeName: "deploy/x" (the regex only runs on a token with "/" that starts with a lowercase
// letter)
func cliTypeName(v string) []int {
	i := strings.IndexByte(v, '/')
	if i < 2 || i > 14 || v[0] < 'a' || v[0] > 'z' {
		return nil
	}
	return reTypeName.FindStringSubmatchIndex(v)
}

func cliNumber(v string) bool {
	for i := 0; i < len(v); i++ {
		if !isDig(v[i]) && v[i] != '.' && v[i] != ',' {
			return false
		}
	}
	return v != ""
}

var reRemote = regexp.MustCompile(`^(?:([A-Za-z0-9_][\w.-]*)@)?([A-Za-z0-9][\w.-]*):(/[^\s]*)?$`)

type cliToken struct {
	a, b int
}

func findCLI(s string, add func(ObjFinding)) {
	if strings.IndexByte(s, '-') < 0 && strings.IndexByte(s, '/') < 0 {
		return
	}
	ini := 0
	for ini < len(s) {
		end := strings.IndexByte(s[ini:], '\n')
		if end < 0 {
			end = len(s)
		} else {
			end += ini
		}
		cliLine(s, ini, end, add)
		ini = end + 1
	}
}

// cliLine handles a line, split into commands by | && || ;
func cliLine(s string, ini, end int, add func(ObjFinding)) {
	var toks []cliToken
	flush := func() {
		if len(toks) > 0 {
			cliCommand(s, toks, add)
		}
		toks = toks[:0]
	}
	for i := ini; i < end; {
		for i < end && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		if i >= end {
			break
		}
		if s[i] == '|' || s[i] == ';' || s[i] == '&' {
			flush()
			for i < end && (s[i] == '|' || s[i] == ';' || s[i] == '&') {
				i++
			}
			continue
		}
		j := i
		for j < end && s[j] != ' ' && s[j] != '\t' && s[j] != '|' && s[j] != ';' {
			j++
		}
		a, b := i, j
		b = noLooseClose(s, a, b)
		// argument list (["kubectl", "-n", "x"]) and call (exec.Command("kubectl", ...),
		// ProcessBuilder("kubectl", ...)): the token is the string, without what opens the list or
		// the call
		if k := strings.LastIndexAny(s[a:b], "[("); k >= 0 && a+k+1 < b && (s[a+k+1] == '"' || s[a+k+1] == '\'') {
			a += k + 1
		}
		if b-a >= 2 && (s[a] == '"' || s[a] == '\'') && s[b-1] == s[a] { // "value" in quotes
			a, b = a+1, b-1
		}
		toks = append(toks, cliToken{a, b})
		i = j
	}
	flush()
}

// noLooseClose: removes from the end of the token the glued punctuation that did not open in it:
// the closing of the surrounding string or object (command inside JSON: ...-U x"}), comma and
// period.
func noLooseClose(s string, a, b int) int {
	for b-a > 1 {
		v := s[a : b-1]
		switch c := s[b-1]; c {
		case '"', '\'', '`':
			if strings.Count(v, string(c))%2 == 1 {
				return b
			}
		case ')', ']', '}':
			if strings.IndexByte(v, map[byte]byte{')': '(', ']': '[', '}': '{'}[c]) >= 0 {
				return b
			}
		case ',', '.':
		default:
			return b
		}
		b--
	}
	return b
}

func cliCommand(s string, toks []cliToken, add func(ObjFinding)) {
	type pair struct {
		ent  string
		a, b int
	}
	var pairs []pair
	shortOnes := map[string]bool{}
	hasTypeName, hasOperation := false, false
	for k, t := range toks {
		v := s[t.a:t.b]
		// "123 ns/op", "benchmark ns/op" (benchmark output) is not a resource: the type/name comes
		// after an operation verb, an option or another type/name
		if m := cliTypeName(v); m != nil && k > 0 && afterCommand(s, toks, k) {
			hasTypeName = true
			ent := entTypeName[v[m[2]:m[3]]]
			if ent == "" {
				ent = "service"
			}
			if name := v[m[4]:m[5]]; !publicDev(name) && !publicDevops(name) {
				add(ObjFinding{t.a + m[4], t.a + m[5], ent, "command-line", true})
			}
			continue
		}
		if operationSubcommands[v] {
			hasOperation = true
		}
		if ent, ok := shortOption[v]; ok && k+1 < len(toks) {
			n := toks[k+1]
			if n.b > n.a && s[n.a] != '-' && resourceValue(s[n.a:n.b], ent) {
				pairs = append(pairs, pair{ent, n.a, n.b})
				shortOnes[v] = true
			}
		}
	}
	nShort := 0
	for f := range shortOnes {
		if f != "-n" {
			nShort++
		}
	}
	for _, p := range pairs {
		if p.ent == "namespace" {
			if !(hasTypeName || hasOperation || nShort >= 1) {
				continue
			}
		} else if nShort < 2 {
			continue
		}
		if p.ent == "server" {
			addHost(s, p.a, p.b, "command-line", true, add)
		} else {
			add(ObjFinding{p.a, p.b, p.ent, "command-line", true})
		}
	}
	// remote copy: scp/rsync [user@]host:/path
	if len(toks) > 0 {
		cmd := s[toks[0].a:toks[0].b]
		if cmd == "scp" || cmd == "rsync" || strings.HasSuffix(cmd, "/scp") || strings.HasSuffix(cmd, "/rsync") {
			for _, t := range toks[1:] {
				v := s[t.a:t.b]
				m := reRemote.FindStringSubmatchIndex(v)
				if m == nil || len(v) < 3 {
					continue
				}
				if m[2] >= 0 && !publicDev(v[m[2]:m[3]]) {
					add(ObjFinding{t.a + m[2], t.a + m[3], "user", "command-line", true})
				}
				if h := v[m[4]:m[5]]; !publicDev(h) && !publicDomain(strings.ToLower(h)) {
					add(ObjFinding{t.a + m[4], t.a + m[5], "server", "command-line", true})
				}
			}
		}
	}
}
