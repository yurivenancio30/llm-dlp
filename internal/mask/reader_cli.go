package mask

import (
	"regexp"
	"strings"
)

// Reader de linha de comando: opções que levam o nome de um recurso,
// iguais em qualquer ferramenta que siga a convenção.
//
//	-h/--host, -S/--server        servidor
//	-d/-D/--dbname/--database     banco
//	-U/-u/--username/--user       usuário
//	-n/--namespace                namespace
//	<tipo>/<nome>                 deploy/x, svc/x, sts/x, pod/x, cm/x, secret/x, ns/x, sa/x...
//	host:/caminho, usuario@host:  cópia remota (scp, rsync)
//
// As opções longas já ficam com o leitor de chave-valor (--host x, --host=x). As curtas são
// ambíguas (-h é ajuda, -d é "detach" ou "data" em muitas ferramentas): só valem quando o
// mesmo comando tem pelo menos duas delas com valor (psql -h x -d y -U z), ou, para -n,
// quando o comando também cita um recurso <tipo>/<nome> ou um subcomando de operação.

var shortOption = map[string]string{"-h": "servidor", "-S": "servidor", "-d": "database", "-D": "database",
	"-U": "usuario", "-u": "usuario", "-n": "namespace"}

var reTypeName = regexp.MustCompile(`^(deploy|deployment|deployments|svc|service|services|sts|statefulset|statefulsets|ds|daemonset|po|pod|pods|cm|configmap|configmaps|secret|secrets|job|jobs|cj|cronjob|cronjobs|ing|ingress|ns|namespace|namespaces|pvc|sa|serviceaccount|rs|replicaset|hpa|node|nodes)/([a-z0-9][a-z0-9.-]{0,252})$`)

var entTypeName = map[string]string{"ns": "namespace", "namespace": "namespace", "namespaces": "namespace",
	"sa": "usuario", "serviceaccount": "usuario", "node": "servidor", "nodes": "servidor"}

// subcomandos de operação que acompanham -n (namespace)
var operationSubcommands = setOf("get", "describe", "logs", "exec", "rollout", "scale", "delete", "apply", "edit",
	"patch", "port-forward", "top", "create", "expose", "attach", "cp", "install", "upgrade", "uninstall", "status")

// afterCommand: o token k vem depois de um verbo de operação (get, logs, rollout restart...),
// de uma opção (-n x, --all) ou de outro tipo/nome, na mesma linha de comando.
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

// cliTypeName: "deploy/x" (a regex só roda em token com "/" que começa com letra minúscula)
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

// cliLine trata uma linha, dividida em comandos por | && || ;
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
		// lista de argumentos (["kubectl", "-n", "x"]) e chamada (exec.Command("kubectl", ...),
		// ProcessBuilder("kubectl", ...)): o token é a string, sem o que abre a lista ou a chamada
		if k := strings.LastIndexAny(s[a:b], "[("); k >= 0 && a+k+1 < b && (s[a+k+1] == '"' || s[a+k+1] == '\'') {
			a += k + 1
		}
		if b-a >= 2 && (s[a] == '"' || s[a] == '\'') && s[b-1] == s[a] { // "valor" entre aspas
			a, b = a+1, b-1
		}
		toks = append(toks, cliToken{a, b})
		i = j
	}
	flush()
}

// noLooseClose: tira do fim do token a pontuação colada que não abriu nele: o fecho da
// string ou do objeto em volta (comando dentro de JSON: ...-U x"}), vírgula e ponto final.
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
		// "123 ns/op", "benchmark ns/op" (saída de benchmark) não é recurso: o tipo/nome vem
		// depois de um verbo de operação, de uma opção ou de outro tipo/nome
		if m := cliTypeName(v); m != nil && k > 0 && afterCommand(s, toks, k) {
			hasTypeName = true
			ent := entTypeName[v[m[2]:m[3]]]
			if ent == "" {
				ent = "servico"
			}
			if name := v[m[4]:m[5]]; !publicDev(name) && !publicDevops(name) {
				add(ObjFinding{t.a + m[4], t.a + m[5], ent, "linha-de-comando", true})
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
		if p.ent == "servidor" {
			addHost(s, p.a, p.b, "linha-de-comando", true, add)
		} else {
			add(ObjFinding{p.a, p.b, p.ent, "linha-de-comando", true})
		}
	}
	// cópia remota: scp/rsync [usuario@]host:/caminho
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
					add(ObjFinding{t.a + m[2], t.a + m[3], "usuario", "linha-de-comando", true})
				}
				if h := v[m[4]:m[5]]; !publicDev(h) && !publicDomain(strings.ToLower(h)) {
					add(ObjFinding{t.a + m[4], t.a + m[5], "servidor", "linha-de-comando", true})
				}
			}
		}
	}
}
