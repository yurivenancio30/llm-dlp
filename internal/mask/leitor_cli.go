package mask

import (
	"regexp"
	"strings"
)

// Leitor de linha de comando (item 11 da revisão): opções que levam o nome de um recurso,
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

var opcaoCurta = map[string]string{"-h": "servidor", "-S": "servidor", "-d": "database", "-D": "database",
	"-U": "usuario", "-u": "usuario", "-n": "namespace"}

var reTipoNome = regexp.MustCompile(`^(deploy|deployment|deployments|svc|service|services|sts|statefulset|statefulsets|ds|daemonset|po|pod|pods|cm|configmap|configmaps|secret|secrets|job|jobs|cj|cronjob|cronjobs|ing|ingress|ns|namespace|namespaces|pvc|sa|serviceaccount|rs|replicaset|hpa|node|nodes)/([a-z0-9][a-z0-9.-]{0,252})$`)

var entTipoNome = map[string]string{"ns": "namespace", "namespace": "namespace", "namespaces": "namespace",
	"sa": "usuario", "serviceaccount": "usuario", "node": "servidor", "nodes": "servidor"}

// subcomandos de operação que acompanham -n (namespace)
var subcomandosOperacao = conj("get", "describe", "logs", "exec", "rollout", "scale", "delete", "apply", "edit",
	"patch", "port-forward", "top", "create", "expose", "attach", "cp", "install", "upgrade", "uninstall", "status")

// tipoNomeCLI: "deploy/x" (a regex só roda em token com "/" que começa com letra minúscula)
func tipoNomeCLI(v string) []int {
	i := strings.IndexByte(v, '/')
	if i < 2 || i > 14 || v[0] < 'a' || v[0] > 'z' {
		return nil
	}
	return reTipoNome.FindStringSubmatchIndex(v)
}

func numeroCLI(v string) bool {
	for i := 0; i < len(v); i++ {
		if !ehDig(v[i]) && v[i] != '.' && v[i] != ',' {
			return false
		}
	}
	return v != ""
}

var reRemoto = regexp.MustCompile(`^(?:([A-Za-z0-9_][\w.-]*)@)?([A-Za-z0-9][\w.-]*):(/[^\s]*)?$`)

type tokenCLI struct {
	a, b int
}

func acharCLI(s string, add func(ObjAchado)) {
	if strings.IndexByte(s, '-') < 0 && strings.IndexByte(s, '/') < 0 {
		return
	}
	ini := 0
	for ini < len(s) {
		fim := strings.IndexByte(s[ini:], '\n')
		if fim < 0 {
			fim = len(s)
		} else {
			fim += ini
		}
		linhaCLI(s, ini, fim, add)
		ini = fim + 1
	}
}

// linhaCLI trata uma linha, dividida em comandos por | && || ;
func linhaCLI(s string, ini, fim int, add func(ObjAchado)) {
	var toks []tokenCLI
	flush := func() {
		if len(toks) > 0 {
			comandoCLI(s, toks, add)
		}
		toks = toks[:0]
	}
	for i := ini; i < fim; {
		for i < fim && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		if i >= fim {
			break
		}
		if s[i] == '|' || s[i] == ';' || s[i] == '&' {
			flush()
			for i < fim && (s[i] == '|' || s[i] == ';' || s[i] == '&') {
				i++
			}
			continue
		}
		j := i
		for j < fim && s[j] != ' ' && s[j] != '\t' && s[j] != '|' && s[j] != ';' {
			j++
		}
		a, b := i, j
		if b-a >= 2 && (s[a] == '"' || s[a] == '\'') && s[b-1] == s[a] { // "valor" entre aspas
			a, b = a+1, b-1
		}
		toks = append(toks, tokenCLI{a, b})
		i = j
	}
	flush()
}

func comandoCLI(s string, toks []tokenCLI, add func(ObjAchado)) {
	type par struct {
		ent  string
		a, b int
	}
	var pares []par
	curtas := map[string]bool{}
	temTipoNome, temOperacao := false, false
	for k, t := range toks {
		v := s[t.a:t.b]
		// "123 ns/op" (saída de benchmark) não é recurso: o tipo/nome vem depois de um comando
		if m := tipoNomeCLI(v); m != nil && k > 0 && !numeroCLI(s[toks[k-1].a:toks[k-1].b]) {
			temTipoNome = true
			ent := entTipoNome[v[m[2]:m[3]]]
			if ent == "" {
				ent = "servico"
			}
			if nome := v[m[4]:m[5]]; !publicoDev(nome) && !publicoDevops(nome) {
				add(ObjAchado{t.a + m[4], t.a + m[5], ent, "linha-de-comando", true})
			}
			continue
		}
		if subcomandosOperacao[v] {
			temOperacao = true
		}
		if ent, ok := opcaoCurta[v]; ok && k+1 < len(toks) {
			n := toks[k+1]
			if n.b > n.a && s[n.a] != '-' && valorRecurso(s[n.a:n.b], ent) {
				pares = append(pares, par{ent, n.a, n.b})
				curtas[v] = true
			}
		}
	}
	nCurtas := 0
	for f := range curtas {
		if f != "-n" {
			nCurtas++
		}
	}
	for _, p := range pares {
		if p.ent == "namespace" {
			if !(temTipoNome || temOperacao || nCurtas >= 1) {
				continue
			}
		} else if nCurtas < 2 {
			continue
		}
		if p.ent == "servidor" {
			addHost(s, p.a, p.b, "linha-de-comando", true, add)
		} else {
			add(ObjAchado{p.a, p.b, p.ent, "linha-de-comando", true})
		}
	}
	// cópia remota: scp/rsync [usuario@]host:/caminho
	if len(toks) > 0 {
		cmd := s[toks[0].a:toks[0].b]
		if cmd == "scp" || cmd == "rsync" || strings.HasSuffix(cmd, "/scp") || strings.HasSuffix(cmd, "/rsync") {
			for _, t := range toks[1:] {
				v := s[t.a:t.b]
				m := reRemoto.FindStringSubmatchIndex(v)
				if m == nil || len(v) < 3 {
					continue
				}
				if m[2] >= 0 && !publicoDev(v[m[2]:m[3]]) {
					add(ObjAchado{t.a + m[2], t.a + m[3], "usuario", "linha-de-comando", true})
				}
				if h := v[m[4]:m[5]]; !publicoDev(h) && !dominioPublico(strings.ToLower(h)) {
					add(ObjAchado{t.a + m[4], t.a + m[5], "servidor", "linha-de-comando", true})
				}
			}
		}
	}
}
