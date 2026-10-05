package mask

import (
	"regexp"
	"strings"
)

// Leitores de DevOps (ver docs/estruturas.md, seções Kubernetes e contêineres, Infraestrutura
// como código e Pipelines de CI): manifestos Kubernetes (YAML e JSON), kubeconfig, Chart.yaml
// do Helm, docker-compose, Terraform/HCL e a saída do plan, Ansible (inventário INI e YAML,
// playbook), CloudFormation, ARM, Bicep e pipelines de CI. Em todos, o CAMINHO do campo diz o
// que o valor é; chaves, tipos, enums e expressões (${...}, !Ref, "[...]", {{ }}) ficam.
// Só padrões estáveis da especificação de cada formato.

// ---------------------------------------------------------------------------------------
// Leitura de YAML e JSON por caminho, sem biblioteca: cada par chave/valor (ou item de lista)
// vira uma entrada com o caminho desde a raiz do documento ("-" = item de lista).

type yEnt struct {
	path   []string
	ids    []int // id de cada elemento do caminho (o contêiner dos filhos dele)
	root   int   // id da raiz do documento
	ki, kf int   // a chave (-1: item de lista)
	vi, vf int   // o valor escalar, sem aspas (-1: sem valor na linha / aninhado)
	tag    string
	tmpl   bool // valor com {{ }} (template Helm/Jinja): não é tocado
}

func (e *yEnt) chave() string { return e.path[len(e.path)-1] }
func (e *yEnt) id() int       { return e.ids[len(e.ids)-1] }
func (e *yEnt) pai() int {
	if len(e.ids) < 2 {
		return e.root
	}
	return e.ids[len(e.ids)-2]
}

// limites para entrada patológica (memória e tempo): profundidade e número de entradas
const (
	maxProfYAML = 32
	maxEntsYAML = 50_000
)

type yColetor struct {
	ents []yEnt
	nid  int
}

func (w *yColetor) novo() int { w.nid++; return w.nid }

func (w *yColetor) emitir(path []string, ids []int, root, ki, kf, vi, vf int, tag string, tmpl bool) {
	if len(w.ents) >= maxEntsYAML || len(path) > maxProfYAML {
		return
	}
	w.ents = append(w.ents, yEnt{append([]string(nil), path...), append([]int(nil), ids...), root, ki, kf, vi, vf, tag, tmpl})
}

type yEl struct {
	col  int
	key  string
	id   int
	item bool
}

// lerYAML: linhas com indentação de espaços (linha com tab na indentação não é YAML), itens
// "- ", chaves simples ou entre aspas, valores simples, entre aspas, com tag (!Ref), em fluxo
// ({a: b}, [a, b]) e blocos | > (pulados). "---" separa documentos.
func (w *yColetor) lerYAML(s string) {
	root := w.novo()
	var st []yEl
	bloco := -1
	path := make([]string, 0, 16)
	ids := make([]int, 0, 16)
	caminho := func() ([]string, []int) {
		path, ids = path[:0], ids[:0]
		for _, e := range st {
			path = append(path, e.key)
			ids = append(ids, e.id)
		}
		return path, ids
	}
	for a := 0; a < len(s); {
		b := strings.IndexByte(s[a:], '\n')
		if b < 0 {
			b = len(s)
		} else {
			b += a
		}
		ini, fim := a, b
		a = b + 1
		if fim > ini && s[fim-1] == '\r' {
			fim--
		}
		ind := 0
		for ini+ind < fim && s[ini+ind] == ' ' {
			ind++
		}
		if ini+ind >= fim {
			continue
		}
		if bloco >= 0 {
			if ind > bloco {
				continue
			}
			bloco = -1
		}
		c := s[ini+ind]
		if c == '\t' {
			continue
		}
		if ind == 0 && (strings.HasPrefix(s[ini:fim], "---") || strings.HasPrefix(s[ini:fim], "...")) {
			root = w.novo()
			st = st[:0]
			continue
		}
		if c == '#' || ind == 0 && c == '%' {
			continue
		}
		p := ini + ind
		item := false
		for p < fim && s[p] == '-' && (p+1 == fim || s[p+1] == ' ') && len(st) <= maxProfYAML {
			col := p - ini
			for len(st) > 0 && (st[len(st)-1].col > col || st[len(st)-1].col == col && st[len(st)-1].item) {
				st = st[:len(st)-1]
			}
			st = append(st, yEl{col, "-", w.novo(), true})
			item = true
			p++
			for p < fim && s[p] == ' ' {
				p++
			}
		}
		if p >= fim || s[p] == '#' || len(st) > maxProfYAML {
			continue
		}
		col := p - ini
		ki, kf, vp, ok := chaveYAML(s, p, fim)
		if !ok {
			if item {
				pt, is := caminho()
				w.valorYAML(s, pt, is, root, -1, -1, p, fim, st[len(st)-1].col, &bloco)
			}
			continue
		}
		for len(st) > 0 && st[len(st)-1].col >= col {
			st = st[:len(st)-1]
		}
		st = append(st, yEl{col, s[ki:kf], w.novo(), false})
		pt, is := caminho()
		w.valorYAML(s, pt, is, root, ki, kf, vp, fim, col, &bloco)
	}
}

// chaveYAML: a chave que começa em p (simples ou entre aspas) e onde começa o valor.
func chaveYAML(s string, p, fim int) (ki, kf, vp int, ok bool) {
	c := s[p]
	if c == '"' || c == '\'' {
		q := fechaAspas(s, p, fim)
		if q < 0 {
			return
		}
		j := q + 1
		for j < fim && s[j] == ' ' {
			j++
		}
		if j < fim && s[j] == ':' && (j+1 == fim || s[j+1] == ' ' || s[j+1] == '\t') {
			return p + 1, q, j + 1, true
		}
		return
	}
	if strings.IndexByte("[{&*!|>%@`#,?", c) >= 0 {
		return
	}
	for j := p; j < fim; j++ {
		if s[j] == ':' && (j+1 == fim || s[j+1] == ' ' || s[j+1] == '\t') {
			k := j
			for k > p && s[k-1] == ' ' {
				k--
			}
			if k == p {
				return
			}
			return p, k, j + 1, true
		}
		if s[j] == '#' && s[j-1] == ' ' {
			return
		}
	}
	return
}

// fechaAspas: a aspa que fecha a que está em s[p], na mesma linha ('' e \" são escapes).
func fechaAspas(s string, p, fim int) int {
	q := s[p]
	for j := p + 1; j < fim; j++ {
		switch {
		case q == '"' && s[j] == '\\':
			j++
		case s[j] == q:
			if q == '\'' && j+1 < fim && s[j+1] == '\'' {
				j++
				continue
			}
			return j
		}
	}
	return -1
}

func (w *yColetor) valorYAML(s string, path []string, ids []int, root, ki, kf, v, fim, refCol int, bloco *int) {
	for v < fim && (s[v] == ' ' || s[v] == '\t') {
		v++
	}
	tag := ""
	for v < fim && (s[v] == '&' || s[v] == '!') { // âncora, tag
		j := v
		for j < fim && s[j] != ' ' {
			j++
		}
		if s[v] == '!' {
			tag = s[v:j]
		}
		v = j
		for v < fim && s[v] == ' ' {
			v++
		}
	}
	if v >= fim || s[v] == '#' || s[v] == '*' {
		w.emitir(path, ids, root, ki, kf, -1, -1, tag, false)
		return
	}
	switch s[v] {
	case '|', '>':
		*bloco = refCol
		w.emitir(path, ids, root, ki, kf, -1, -1, tag, false)
	case '"', '\'':
		q := fechaAspas(s, v, fim)
		if q < 0 {
			w.emitir(path, ids, root, ki, kf, -1, -1, tag, false)
			return
		}
		w.emitir(path, ids, root, ki, kf, v+1, q, tag, strings.Contains(s[v+1:q], "{{"))
	case '{', '[':
		if v+1 < fim && s[v] == '{' && s[v+1] == '{' {
			w.emitir(path, ids, root, ki, kf, v, fim, tag, true)
			return
		}
		w.emitir(path, ids, root, ki, kf, -1, -1, tag, false)
		w.fluxo(s, v, fim, path, ids, root, 0)
	default:
		e := v
		for e < fim && !(s[e] == '#' && (s[e-1] == ' ' || s[e-1] == '\t')) {
			e++
		}
		for e > v && (s[e-1] == ' ' || s[e-1] == '\t') {
			e--
		}
		w.emitir(path, ids, root, ki, kf, v, e, tag, strings.Contains(s[v:e], "{{"))
	}
}

// fluxo: {a: b, c: [d, e]} numa linha só. Devolve onde parou.
func (w *yColetor) fluxo(s string, i, fim int, path []string, ids []int, root, prof int) int {
	if prof > 20 {
		return fim
	}
	abre := s[i]
	fecha := byte('}')
	if abre == '[' {
		fecha = ']'
	}
	i++
	for i < fim {
		for i < fim && (s[i] == ' ' || s[i] == ',' || s[i] == '\t') {
			i++
		}
		if i >= fim {
			return fim
		}
		if s[i] == fecha {
			return i + 1
		}
		if abre == '[' {
			pp := append(path[:len(path):len(path)], "-")
			pi := append(ids[:len(ids):len(ids)], w.novo())
			n := w.escalarFluxo(s, i, fim, pp, pi, root, -1, -1, prof)
			if n <= i {
				return fim
			}
			i = n
			continue
		}
		var ki, kf, j int
		if s[i] == '"' || s[i] == '\'' {
			q := fechaAspas(s, i, fim)
			if q < 0 {
				return fim
			}
			ki, kf, j = i+1, q, q+1
			for j < fim && s[j] == ' ' {
				j++
			}
			if j >= fim || s[j] != ':' {
				return fim
			}
		} else {
			j = i
			for j < fim && s[j] != ',' && s[j] != fecha && !(s[j] == ':' && (j+1 == fim || s[j+1] == ' ')) {
				j++
			}
			if j >= fim || s[j] != ':' {
				if j < fim && s[j] == ',' {
					i = j + 1
					continue
				}
				return j
			}
			ki, kf = i, j
			for kf > ki && s[kf-1] == ' ' {
				kf--
			}
		}
		j++
		for j < fim && s[j] == ' ' {
			j++
		}
		pp := append(path[:len(path):len(path)], s[ki:kf])
		pi := append(ids[:len(ids):len(ids)], w.novo())
		n := w.escalarFluxo(s, j, fim, pp, pi, root, ki, kf, prof)
		if n <= i {
			return fim
		}
		i = n
	}
	return fim
}

func (w *yColetor) escalarFluxo(s string, i, fim int, path []string, ids []int, root, ki, kf, prof int) int {
	if i >= fim {
		w.emitir(path, ids, root, ki, kf, -1, -1, "", false)
		return fim
	}
	switch s[i] {
	case '{', '[':
		w.emitir(path, ids, root, ki, kf, -1, -1, "", false)
		return w.fluxo(s, i, fim, path, ids, root, prof+1)
	case '"', '\'':
		q := fechaAspas(s, i, fim)
		if q < 0 {
			return fim
		}
		w.emitir(path, ids, root, ki, kf, i+1, q, "", strings.Contains(s[i+1:q], "{{"))
		return q + 1
	}
	e := i
	for e < fim && s[e] != ',' && s[e] != ']' && s[e] != '}' {
		e++
	}
	f := e
	for f > i && s[f-1] == ' ' {
		f--
	}
	w.emitir(path, ids, root, ki, kf, i, f, "", false)
	return e
}

// lerJSON: objetos JSON no meio do texto (começam com { seguido de chave entre aspas). Em
// qualquer coisa que não seja JSON, larga o objeto e segue procurando.
func (w *yColetor) lerJSON(s string) {
	type ctx struct {
		obj, temChave bool
		chave         string
		ki, kf        int
	}
	var st []ctx
	var path []string
	var ids []int
	root := 0
	ws := func(c byte) bool { return c == ' ' || c == '\n' || c == '\t' || c == '\r' }
	larga := func() { st, path, ids = st[:0], path[:0], ids[:0] }
	for i := 0; i < len(s); {
		if len(st) == 0 {
			j := strings.IndexByte(s[i:], '{')
			if j < 0 {
				return
			}
			i += j + 1
			k := i
			for k < len(s) && ws(s[k]) {
				k++
			}
			if k < len(s) && s[k] == '"' {
				root = w.novo()
				st = append(st, ctx{obj: true})
			}
			continue
		}
		c := s[i]
		top := &st[len(st)-1]
		switch {
		case ws(c) || c == ',' || c == ':':
			i++
		case c == '"':
			q := -1
			for k := i + 1; k < len(s); k++ {
				if s[k] == '\\' {
					k++
					continue
				}
				if s[k] == '"' {
					q = k
					break
				}
				if s[k] == '\n' {
					break
				}
			}
			if q < 0 {
				larga()
				i++
				continue
			}
			if top.obj && !top.temChave {
				k := q + 1
				for k < len(s) && ws(s[k]) {
					k++
				}
				if k >= len(s) || s[k] != ':' {
					larga()
					i = q + 1
					continue
				}
				top.chave, top.ki, top.kf, top.temChave = s[i+1:q], i+1, q, true
				i = k + 1
				continue
			}
			nome, ki, kf := "-", -1, -1
			if top.obj {
				nome, ki, kf = top.chave, top.ki, top.kf
				top.temChave = false
			}
			path = append(path, nome)
			ids = append(ids, w.novo())
			w.emitir(path, ids, root, ki, kf, i+1, q, "", strings.Contains(s[i+1:q], "{{"))
			path, ids = path[:len(path)-1], ids[:len(ids)-1]
			i = q + 1
		case c == '{' || c == '[':
			if top.obj && !top.temChave || len(st) > maxProfYAML {
				larga()
				i++
				continue
			}
			nome, ki, kf := "-", -1, -1
			if top.obj {
				nome, ki, kf = top.chave, top.ki, top.kf
				top.temChave = false
			}
			path = append(path, nome)
			ids = append(ids, w.novo())
			w.emitir(path, ids, root, ki, kf, -1, -1, "", false)
			st = append(st, ctx{obj: c == '{'})
			i++
		case c == '}' || c == ']':
			if top.obj != (c == '}') {
				larga()
				i++
				continue
			}
			st = st[:len(st)-1]
			if len(st) > 0 && len(path) > 0 {
				path, ids = path[:len(path)-1], ids[:len(ids)-1]
			}
			i++
		default:
			k := i
			for k < len(s) && (ehAlnum(s[k]) || s[k] == '.' || s[k] == '-' || s[k] == '+') {
				k++
			}
			if k == i || top.obj && !top.temChave {
				larga()
				i++
				continue
			}
			top.temChave = false
			i = k
		}
	}
}

// ---------------------------------------------------------------------------------------
// Vocabulário público e validação de valor

var publicosDevops = func() map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(`default kube-system kube-public kube-node-lease kubernetes kube-dns coredns
		cluster-admin admin edit view localhost all ungrouped latest stable true false null none yes no
		ubuntu-latest windows-latest macos-latest self-hosted linux windows macos x64 arm64 arm x86 docker
		production staging development dev prod test qa homolog hml preview review master main built-in any
		clusterip nodeport loadbalancer externalname always ifnotpresent never onfailure tcp udp sctp opaque
		prefix exact implementationspecific retain delete recycle readwriteonce readonlymany readwritemany
		cluster local svc pod`) {
		m[w] = true
	}
	return m
}()

// regiões e zonas dos provedores (formas oficiais): us-east-1, sa-east-1, southamerica-east1, europe-west1-b
var reRegiao = regexp.MustCompile(`^(?:(?:us|eu|ap|sa|ca|me|af|il|mx|cn)(?:-gov)?-(?:east|west|north|south|central|northeast|southeast|northwest|southwest)-\d[a-z]?|(?:us|europe|asia|australia|southamerica|northamerica|me|africa)-[a-z]+\d(?:-[a-z])?)$`)

func publicoDevops(v string) bool {
	l := strings.ToLower(v)
	if publicosDevops[l] || reRegiao.MatchString(l) {
		return true
	}
	for _, p := range []string{"ubuntu-", "windows-", "macos-"} {
		if strings.HasPrefix(l, p) {
			return true
		}
	}
	return publicoConexao(v)
}

// nome de recurso: letras, dígitos, _ . -; sem espaço, aspas, expressão ou caminho
var reNomeRecurso = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.\-]{0,252}$`)

func todoDigitos(v string) bool {
	for i := 0; i < len(v); i++ {
		if v[i] < '0' || v[i] > '9' {
			return false
		}
	}
	return v != ""
}

func ehIPv4(v string) bool {
	if strings.Count(v, ".") != 3 {
		return false
	}
	for _, p := range strings.Split(v, ".") {
		if !todoDigitos(p) || len(p) > 3 {
			return false
		}
	}
	return true
}

func addNome(s string, a, b int, ent, regra string, forte bool, add func(ObjAchado)) {
	if a < 0 || b > len(s) || b <= a {
		return
	}
	v := s[a:b]
	if !reNomeRecurso.MatchString(v) || todoDigitos(v) && !(ent == "conta_nuvem" && len(v) == 12) || ehIPv4(v) {
		return
	}
	add(ObjAchado{a, b, ent, regra, forte})
}

// addSegmentos: valor com interpolação (${...}): só os pedaços literais, e fracos.
func addSegmentos(s string, a, b int, ent, regra string, add func(ObjAchado)) {
	for a < b {
		j := strings.Index(s[a:b], "${")
		f := b
		if j >= 0 {
			f = a + j
		}
		x, y := a, f
		for x < y && strings.IndexByte("-_./:", s[x]) >= 0 {
			x++
		}
		for y > x && strings.IndexByte("-_./:", s[y-1]) >= 0 {
			y--
		}
		if y-x >= 4 && caraDeIdentificador(s[x:y]) {
			addNome(s, x, y, ent, regra, false, add)
		}
		if j < 0 {
			return
		}
		k := strings.IndexByte(s[f:b], '}')
		if k < 0 {
			return
		}
		a = f + k + 1
	}
}

// registros públicos oficiais: a imagem fica
var registrosPublicos = map[string]bool{"docker.io": true, "ghcr.io": true, "quay.io": true, "gcr.io": true,
	"registry.k8s.io": true, "mcr.microsoft.com": true, "public.ecr.aws": true}

// addImagem: [registro/]caminho[:tag][@digest]. Só registro privado (primeiro pedaço com ponto
// ou porta, fora da lista pública): o registro é servidor, cada pedaço do caminho é serviço; a
// tag e o digest ficam.
func addImagem(s string, a, b int, regra string, add func(ObjAchado)) {
	v := s[a:b]
	if strings.ContainsAny(v, " {}$") {
		return
	}
	bar := strings.IndexByte(v, '/')
	if bar <= 0 {
		return
	}
	reg := v[:bar]
	if !strings.ContainsAny(reg, ".:") {
		return
	}
	host := reg
	if i := strings.IndexByte(host, ':'); i >= 0 {
		host = host[:i]
	}
	if strings.EqualFold(host, "localhost") || registrosPublicos[strings.ToLower(host)] {
		return
	}
	fim := len(v)
	if strings.IndexByte(reg, '@') >= 0 {
		return
	}
	if i := strings.IndexByte(v[bar:], '@'); i >= 0 {
		fim = bar + i
	}
	if i := strings.LastIndexByte(v[:fim], ':'); i > bar {
		fim = i
	}
	if !ehIPv4(host) {
		addNome(s, a, a+len(host), "servidor", regra, true, add)
	}
	k := a + bar + 1
	for _, p := range strings.Split(v[bar+1:fim], "/") {
		addNome(s, k, k+len(p), "servico", regra, true, add)
		k += len(p) + 1
	}
}

// addARNouNome: nome que pode vir como ARN (arn:aws:eks:região:conta:cluster/nome).
func addARNouNome(s string, a, b int, ent, regra string, add func(ObjAchado)) {
	v := s[a:b]
	if !strings.HasPrefix(v, "arn:") {
		addNome(s, a, b, ent, regra, true, add)
		return
	}
	ps := strings.SplitN(v, ":", 6)
	if len(ps) < 6 {
		return
	}
	conta := a + len(ps[0]) + len(ps[1]) + len(ps[2]) + len(ps[3]) + 4
	addNome(s, conta, conta+len(ps[4]), "conta_nuvem", regra, true, add)
	if i := strings.LastIndexByte(v, '/'); i >= 0 {
		addNome(s, a+i+1, b, ent, regra, true, add)
	}
}

// ---------------------------------------------------------------------------------------
// Leitor de YAML/JSON estruturado: Kubernetes, kubeconfig, Helm, compose, CloudFormation, ARM,
// Ansible (YAML) e pipelines de CI.

var sinaisYAML = []string{"apiVersion", "services:", "Resources:", "runs-on", "stages:", "script:", "pool:",
	"jobs:", "hosts:", "ansible_", "delegate_to"}

var sinaisJSON = []string{`"apiVersion"`, `"Resources"`, `"resources"`}

func temAlgum(s string, ss []string) bool {
	for _, x := range ss {
		if strings.Contains(s, x) {
			return true
		}
	}
	return false
}

func acharDevopsEstruturado(s string, add func(ObjAchado)) {
	if strings.IndexByte(s, ':') < 0 {
		return
	}
	y, j := temAlgum(s, sinaisYAML), temAlgum(s, sinaisJSON)
	if !y && !j {
		return
	}
	w := &yColetor{}
	w.lerYAML(s) // o JSON indentado também é lido linha a linha
	if j {
		w.lerJSON(s)
	}
	if len(w.ents) == 0 {
		return
	}
	visto := map[[2]int]bool{}
	regrasEstrutura(s, w.ents, func(o ObjAchado) {
		k := [2]int{o.Ini, o.Fim}
		if !visto[k] {
			visto[k] = true
			add(o)
		}
	})
}

var reAPIVersion = regexp.MustCompile(`^(?:[a-z0-9.-]+/)?v\d+(?:(?:alpha|beta)\d+)?$`)

// propriedades de CloudFormation que guardam o nome real do recurso
var propsCFN = map[string]string{"BucketName": "bucket", "DBInstanceIdentifier": "database", "DBClusterIdentifier": "database",
	"DBName": "database", "DatabaseName": "database", "TableName": "tabela", "ServerName": "servidor", "ClusterName": "servidor",
	"QueueName": "fila", "TopicName": "fila", "StreamName": "fila", "FunctionName": "servico", "RepositoryName": "repositorio",
	"ServiceName": "servico", "RoleName": "usuario", "UserName": "usuario", "MasterUsername": "usuario", "GroupName": "usuario"}

var rotulosK8s = map[string]bool{"app.kubernetes.io/name": true, "app.kubernetes.io/instance": true, "app.kubernetes.io/part-of": true}

var palavrasPlay = []string{"tasks", "roles", "become", "gather_facts", "vars", "handlers", "pre_tasks", "post_tasks", "remote_user"}

func regrasEstrutura(s string, ents []yEnt, add func(ObjAchado)) {
	filhos := map[int][]int{}
	chaves := map[int]map[string]int{} // contêiner -> chave -> primeira entrada
	raiz := map[int]map[string]int{}
	type recurso struct{ api, kind string }
	res := map[int]*recurso{}
	compose := map[int]bool{}
	servCompose := map[int]bool{}
	declCompose := map[int]map[string]bool{} // redes e volumes declarados no topo
	ci := map[int]bool{}
	val := func(e *yEnt) string {
		if e.vi < 0 || e.tmpl {
			return ""
		}
		return s[e.vi:e.vf]
	}
	for i := range ents {
		e := &ents[i]
		p := e.pai()
		filhos[p] = append(filhos[p], i)
		k := e.chave()
		if e.ki >= 0 {
			if chaves[p] == nil {
				chaves[p] = map[string]int{}
			}
			if _, ok := chaves[p][k]; !ok {
				chaves[p][k] = i
			}
		}
		if len(e.path) == 1 {
			if raiz[e.root] == nil {
				raiz[e.root] = map[string]int{}
			}
			if _, ok := raiz[e.root][k]; !ok {
				raiz[e.root][k] = i
			}
			switch k {
			case "jobs", "stages", "steps", "trigger", "workflow":
				ci[e.root] = true
			}
		}
		if e.vi >= 0 && (k == "apiVersion" || k == "kind") {
			r := res[p]
			if r == nil {
				r = &recurso{}
				res[p] = r
			}
			if k == "kind" {
				r.kind = val(e)
			} else {
				r.api = val(e)
			}
		}
		if k == "script" || k == "runs-on" {
			ci[e.root] = true
		}
		if len(e.path) == 2 && e.ki >= 0 && (e.path[0] == "networks" || e.path[0] == "volumes") {
			if declCompose[e.root] == nil {
				declCompose[e.root] = map[string]bool{}
			}
			declCompose[e.root][e.path[0]+"\x00"+k] = true
		}
		if len(e.path) == 3 && e.path[0] == "services" {
			switch k {
			case "image", "build", "ports", "environment":
				compose[e.root] = true
				servCompose[e.ids[1]] = true
			}
		}
	}
	filho := func(id int, k string) *yEnt {
		if i, ok := chaves[id][k]; ok {
			return &ents[i]
		}
		return nil
	}
	ehRecurso := func(c int) (string, bool) {
		r := res[c]
		if r == nil || r.kind == "" || !reAPIVersion.MatchString(r.api) {
			return "", false
		}
		return r.kind, true
	}
	// Helm Chart.yaml: apiVersion v1/v2 + name + version, sem kind
	chart := map[int]bool{}
	for root, ks := range raiz {
		_, n := ks["name"]
		_, v := ks["version"]
		_, k := ks["kind"]
		if i, ok := ks["apiVersion"]; ok && n && v && !k {
			if a := val(&ents[i]); a == "v1" || a == "v2" {
				chart[root] = true
			}
		}
	}
	for i := range ents {
		e := &ents[i]
		k := e.chave()
		v := val(e)
		// Kubernetes: o recurso mais próximo (objeto com apiVersion e kind)
		emK8s := false
		for j := len(e.path) - 1; j >= 0; j-- {
			c := e.root
			if j > 0 {
				c = e.ids[j-1]
			}
			if kind, ok := ehRecurso(c); ok {
				regraK8s(s, e, e.path[j:], kind, v, add)
				emK8s = true
				break
			}
		}
		if emK8s {
			continue
		}
		if chart[e.root] && len(e.path) == 1 && k == "name" && v != "" {
			addNome(s, e.vi, e.vf, "servico", "helm", true, add)
		}
		if compose[e.root] && len(e.path) >= 2 {
			switch {
			case e.path[0] == "services" && len(e.path) == 2 && e.ki >= 0 && servCompose[e.id()]:
				addNome(s, e.ki, e.kf, "servico", "compose", true, add)
			case e.path[0] == "services" && len(e.path) == 3 && v != "":
				switch k {
				case "container_name":
					addNome(s, e.vi, e.vf, "servico", "compose", true, add)
				case "hostname":
					addNome(s, e.vi, e.vf, "servidor", "compose", true, add)
				case "image":
					addImagem(s, e.vi, e.vf, "imagem", add)
				}
			case (e.path[0] == "networks" || e.path[0] == "volumes") && len(e.path) == 2 && e.ki >= 0:
				addNome(s, e.ki, e.kf, "servico", "compose", false, add)
			case e.path[0] == "services" && len(e.path) == 4 && (e.path[2] == "networks" || e.path[2] == "volumes"):
				// referência a rede/volume declarado no topo: "- rede", "rede:", "- volume:/caminho"
				a, b := e.ki, e.kf
				if e.ki < 0 {
					a, b = e.vi, e.vf
					if a >= 0 {
						if c := strings.IndexByte(s[a:b], ':'); c >= 0 {
							b = a + c
						}
					}
				}
				if a >= 0 && b > a && declCompose[e.root][e.path[2]+"\x00"+s[a:b]] {
					addNome(s, a, b, "servico", "compose", false, add)
				}
			}
		}
		// CloudFormation: Resources.<lógico>.Properties.<XxxName>
		if len(e.path) == 4 && e.path[0] == "Resources" && e.path[2] == "Properties" && v != "" {
			if ent, ok := propsCFN[k]; ok {
				switch e.tag {
				case "":
					addNome(s, e.vi, e.vf, ent, "cfn", true, add)
				case "!Sub":
					addSegmentos(s, e.vi, e.vf, ent, "cfn", add)
				}
			}
		}
		// ARM: resources[].name, com o tipo Microsoft.X/y ao lado; "[...]" é expressão
		if k == "name" && v != "" && v[0] != '[' && len(e.path) >= 3 && e.path[len(e.path)-2] == "-" && e.path[len(e.path)-3] == "resources" {
			if t := filho(e.pai(), "type"); t != nil && strings.HasPrefix(strings.ToLower(val(t)), "microsoft.") {
				addNomeAzure(s, e.vi, e.vf, val(t), "arm", add)
			}
		}
		// Ansible (YAML)
		switch {
		case k == "hosts" && e.vi < 0:
			for _, f := range filhos[e.id()] {
				if c := &ents[f]; c.ki >= 0 && !c.tmpl {
					addNome(s, c.ki, c.kf, "servidor", "ansible", true, add)
				}
			}
		case k == "hosts" && v != "" && len(e.path) == 2 && e.path[0] == "-":
			play := false
			for _, p := range palavrasPlay {
				if filho(e.pai(), p) != nil {
					play = true
					break
				}
			}
			if play { // padrão de hosts: a,b:c:&d:!e
				for x := e.vi; x < e.vf; {
					y := x
					for y < e.vf && s[y] != ',' && s[y] != ':' {
						y++
					}
					a, b := x, y
					for a < b && (s[a] == '&' || s[a] == '!' || s[a] == ' ') {
						a++
					}
					for b > a && s[b-1] == ' ' {
						b--
					}
					addNome(s, a, b, "servidor", "ansible", false, add)
					x = y + 1
				}
			}
		case (k == "ansible_host" || k == "ansible_user") && v != "":
			ent := "servidor"
			if k == "ansible_user" {
				ent = "usuario"
			}
			addNome(s, e.vi, e.vf, ent, "ansible", true, add)
		case k == "delegate_to" && v != "":
			addNome(s, e.vi, e.vf, "servidor", "ansible", false, add)
		}
		if ci[e.root] {
			regraCI(s, ents, e, v, filho, add)
		}
	}
}

func entKind(kind string) string {
	switch kind {
	case "Namespace":
		return "namespace"
	case "ServiceAccount":
		return "usuario"
	}
	return "servico"
}

func regraK8s(s string, e *yEnt, rel []string, kind, v string, add func(ObjAchado)) {
	n := len(rel)
	k := rel[n-1]
	ant := ""
	if n >= 2 {
		ant = rel[n-2]
	}
	if kind == "Config" { // kubeconfig
		regraKubeconfig(s, e, rel, v, add)
		return
	}
	if v == "" {
		return
	}
	a, b := e.vi, e.vf
	switch {
	case n == 2 && rel[0] == "metadata" && k == "name":
		addNome(s, a, b, entKind(kind), "k8s", true, add)
	case k == "namespace" && ant == "metadata":
		addNome(s, a, b, "namespace", "k8s", true, add)
	case k == "serviceName" || k == "claimName" || k == "secretName":
		addNome(s, a, b, "servico", "k8s", true, add)
	case k == "serviceAccountName" || k == "serviceAccount":
		addNome(s, a, b, "usuario", "k8s", true, add)
	case k == "name" && (ant == "secretKeyRef" || ant == "configMapKeyRef" || ant == "secretRef" || ant == "configMapRef" || ant == "configMap" || ant == "service"):
		addNome(s, a, b, "servico", "k8s", true, add)
	case k == "name" && ant == "-" && n >= 3 && rel[n-3] == "imagePullSecrets":
		addNome(s, a, b, "servico", "k8s", true, add)
	case kind == "Ingress" && (k == "host" && n >= 3 && rel[n-3] == "rules" || k == "-" && ant == "hosts"):
		if strings.HasPrefix(v, "*.") {
			a += 2
		}
		addNome(s, a, b, "servidor", "k8s", true, add)
	case k == "image":
		addImagem(s, a, b, "imagem", add)
	case rotulosK8s[k] && (ant == "labels" || ant == "matchLabels" || ant == "selector"):
		addNome(s, a, b, "servico", "k8s-label", false, add)
	}
}

func regraKubeconfig(s string, e *yEnt, rel []string, v string, add func(ObjAchado)) {
	if v == "" {
		return
	}
	p := strings.Join(rel, ".")
	switch p {
	case "clusters.-.name", "contexts.-.name", "current-context", "contexts.-.context.cluster":
		addARNouNome(s, e.vi, e.vf, "servidor", "kubeconfig", add)
	case "users.-.name", "contexts.-.context.user":
		addARNouNome(s, e.vi, e.vf, "usuario", "kubeconfig", add)
	case "contexts.-.context.namespace":
		addNome(s, e.vi, e.vf, "namespace", "kubeconfig", true, add)
	case "clusters.-.cluster.server":
		i := strings.Index(v, "://")
		if i < 0 {
			return
		}
		a := e.vi + i + 3
		b := a
		for b < e.vf && s[b] != ':' && s[b] != '/' {
			b++
		}
		addNome(s, a, b, "servidor", "kubeconfig", true, add)
	}
}

// entAzure: entidade pelo tipo do recurso (Microsoft.Sql/servers/databases -> último pedaço).
func entAzure(seg string) string {
	switch strings.ToLower(seg) {
	case "servers", "flexibleservers", "managedclusters", "clusters", "virtualmachines", "registries", "managedinstances":
		return "servidor"
	case "databases", "databaseaccounts":
		return "database"
	case "storageaccounts", "containers":
		return "bucket"
	case "queues", "topics", "subscriptions", "eventhubs":
		return "fila"
	case "namespaces":
		return "namespace"
	}
	return "servico"
}

// addNomeAzure: o nome de um recurso ARM/Bicep; filho vem como "pai/filho", um pedaço por tipo.
func addNomeAzure(s string, a, b int, tipo, regra string, add func(ObjAchado)) {
	segT := strings.Split(tipo, "/")[1:]
	ps := strings.Split(s[a:b], "/")
	for k, p := range ps {
		ent := "servico"
		if t := len(segT) - len(ps) + k; t >= 0 && t < len(segT) {
			ent = entAzure(segT[t])
		}
		addNome(s, a, a+len(p), ent, regra, true, add)
		a += len(p) + 1
	}
}

// regraCI: GitHub Actions, GitLab CI, Azure Pipelines.
func regraCI(s string, ents []yEnt, e *yEnt, v string, filho func(int, string) *yEnt, add func(ObjAchado)) {
	k := e.chave()
	n := len(e.path)
	ant := ""
	if n >= 2 {
		ant = e.path[n-2]
	}
	ant2 := ""
	if n >= 3 {
		ant2 = e.path[n-3]
	}
	runner := func() { addNome(s, e.vi, e.vf, "servidor", "ci", false, add) }
	job := func(id int) bool {
		for _, x := range []string{"runs-on", "script", "steps", "strategy", "deployment", "stage"} {
			if filho(id, x) != nil {
				return true
			}
		}
		return false
	}
	if v == "" {
		return
	}
	switch {
	case k == "image" || k == "container" && n >= 2 || k == "name" && ant == "image" || k == "image" && ant == "container":
		addImagem(s, e.vi, e.vf, "imagem", add)
	case k == "-" && ant == "services" || k == "name" && ant == "-" && ant2 == "services":
		addImagem(s, e.vi, e.vf, "imagem", add)
	case k == "runs-on" || k == "-" && ant == "runs-on" || ant == "runs-on" && (k == "group" || k == "labels") || k == "-" && ant == "labels" && ant2 == "runs-on":
		runner()
	case k == "-" && ant == "tags" && n == 3 && e.path[0] != "-" && job(e.ids[0]):
		runner()
	case k == "pool" || k == "name" && ant == "pool":
		runner()
	case k == "environment" && job(e.pai()):
		addNome(s, e.vi, e.vf, "servico", "ci", false, add)
	case k == "name" && ant == "environment":
		dono := e.root // o contêiner que tem a chave environment
		if n >= 3 {
			dono = e.ids[n-3]
		}
		if job(dono) {
			addNome(s, e.vi, e.vf, "servico", "ci", false, add)
		}
	}
}

// ---------------------------------------------------------------------------------------
// Terraform/HCL e saída do plan

var (
	reCabHCL   = regexp.MustCompile(`^[ \t]*(?:[+~-]|-/\+|\+/-|<=)?[ \t]*(resource|data)[ \t]+"([^"\s]+)"[ \t]+"[^"\s]+"[ \t]*\{`)
	reCabHCL1  = regexp.MustCompile(`^[ \t]*(?:provider|backend)[ \t]+"[^"\s]+"[ \t]*\{`)
	reAtribHCL = regexp.MustCompile(`^[ \t]*(?:[+~-]|-/\+|\+/-)?[ \t]*"?([A-Za-z_][\w-]*)"?[ \t]*=[ \t]*`)
	reTagNome  = regexp.MustCompile(`"?\bName"?[ \t]*[=:][ \t]*"([^"\n]*)"`)
)

// núcleo do nome do atributo -> entidade
var nucleoHCL = map[string]string{"bucket": "bucket", "database": "database", "db": "database", "server": "servidor",
	"instance": "servidor", "cluster": "servidor", "host": "servidor", "hostname": "servidor", "account": "conta_nuvem",
	"project": "conta_nuvem", "namespace": "namespace", "topic": "fila", "queue": "fila", "subscription": "fila",
	"repository": "repositorio", "repo": "repositorio", "dataset": "schema", "schema": "schema", "table": "tabela",
	"username": "usuario", "user": "usuario", "organization": "organizacao", "org": "organizacao", "folder": "pasta",
	"function": "servico"}

// entTipoHCL: a entidade do atributo name/identifier pelo tipo do recurso (aws_s3_bucket...).
func entTipoHCL(t string) string {
	t = strings.ToLower(t)
	for _, p := range [][2]string{{"storage_account", "bucket"}, {"bucket", "bucket"}, {"service_account", "usuario"},
		{"sql_database", "database"}, {"database", "database"}, {"db_instance", "database"}, {"rds_cluster", "database"},
		{"dataset", "schema"}, {"table", "tabela"}, {"namespace", "namespace"}, {"topic", "fila"}, {"queue", "fila"},
		{"subscription", "fila"}, {"repository", "repositorio"}, {"project", "conta_nuvem"}, {"folder", "pasta"},
		{"organization", "organizacao"}, {"user", "usuario"}, {"role", "usuario"}, {"cluster", "servidor"},
		{"server", "servidor"}, {"instance", "servidor"}, {"host", "servidor"}} {
		if strings.Contains(t, p[0]) {
			return p[1]
		}
	}
	return "servico"
}

// entAtribHCL: (entidade, forte, ok) do atributo attr no recurso de tipo t, na profundidade prof.
func entAtribHCL(attr, t string, prof int, valor string) (string, bool, bool) {
	ps := strings.Split(strings.ToLower(attr), "_")
	ult := ps[len(ps)-1]
	nucleo := ult
	if ult == "name" || ult == "identifier" || ult == "id" || ult == "ids" {
		if len(ps) == 1 {
			if ult == "id" || ult == "ids" || t == "" || prof > 1 {
				return "", false, false
			}
			return entTipoHCL(t), prof == 1, true
		}
		nucleo = ps[len(ps)-2]
	}
	ent, ok := nucleoHCL[nucleo]
	if !ok {
		return "", false, false
	}
	if nucleo == "account" && !(todoDigitos(valor) && len(valor) == 12) {
		if strings.Contains(strings.ToLower(attr), "storage") {
			ent = "bucket"
		} else if todoDigitos(valor) {
			return "", false, false
		}
	}
	return ent, prof == 1, true
}

func acharTerraform(s string, add func(ObjAchado)) {
	if strings.IndexByte(s, '"') < 0 || strings.IndexByte(s, '{') < 0 {
		return
	}
	if !strings.Contains(s, `resource "`) && !strings.Contains(s, `data "`) && !strings.Contains(s, `provider "`) && !strings.Contains(s, `backend "`) {
		return
	}
	linhas := quebraLinhas(s)
	for i := 0; i < len(linhas); i++ {
		l := s[linhas[i][0]:linhas[i][1]]
		if !strings.Contains(l, `"`) || !strings.Contains(l, "{") {
			continue
		}
		tipo := ""
		if m := reCabHCL.FindStringSubmatch(l); m != nil {
			tipo = m[2]
		} else if !reCabHCL1.MatchString(l) {
			continue
		}
		pilha := []string{""} // blocos abertos (nome); o do cabeçalho é ""
		pilha = abreFechaHCL(l[strings.IndexByte(l, '{')+1:], pilha, "")
		j := i + 1
		for ; j < len(linhas) && len(pilha) > 0 && j-i < 5000; j++ {
			a, b := linhas[j][0], linhas[j][1]
			l := s[a:b]
			nome := ""
			if m := reAtribHCL.FindStringSubmatchIndex(l); m != nil {
				nome = l[m[2]:m[3]]
				atribHCL(s, a+m[1], b, nome, tipo, pilha, add)
			} else if t := strings.TrimLeft(l, " \t+~-"); t != "" {
				k := 0
				for k < len(t) && (ehIdent(t[k]) || t[k] == '-') {
					k++
				}
				nome = t[:k]
			}
			pilha = abreFechaHCL(l, pilha, nome)
		}
		i = j - 1
	}
}

// abreFechaHCL: atualiza a pilha de blocos com as chaves da linha (fora de strings).
func abreFechaHCL(l string, pilha []string, nome string) []string {
	aspas := false
	for k := 0; k < len(l); k++ {
		switch c := l[k]; {
		case c == '\\' && aspas:
			k++
		case c == '"':
			aspas = !aspas
		case aspas:
		case c == '{':
			pilha = append(pilha, nome)
		case c == '}':
			if len(pilha) > 0 {
				pilha = pilha[:len(pilha)-1]
			}
		}
	}
	return pilha
}

// atribHCL: o lado direito de "attr = ..." (a partir de v): literal, literal -> literal, ou
// mapa de tags em linha.
func atribHCL(s string, v, fim int, attr, tipo string, pilha []string, add func(ObjAchado)) {
	if v >= fim {
		return
	}
	prof := len(pilha)
	bloco := pilha[len(pilha)-1]
	tags := bloco == "tags" || bloco == "tags_all" || bloco == "labels"
	if s[v] == '{' && (attr == "tags" || attr == "tags_all") {
		if m := reTagNome.FindStringSubmatchIndex(s[v:fim]); m != nil {
			addNome(s, v+m[2], v+m[3], entTipoHCL(tipo), "terraform", false, add)
		}
		return
	}
	for k := 0; k < 2 && v < fim && s[v] == '"'; k++ {
		q := fechaAspas(s, v, fim)
		if q < 0 {
			return
		}
		a, b := v+1, q
		val := s[a:b]
		if tags && attr == "Name" {
			addNome(s, a, b, entTipoHCL(tipo), "terraform", false, add)
		} else if ent, forte, ok := entAtribHCL(attr, tipo, prof, val); ok {
			if strings.Contains(val, "${") || strings.Contains(val, "%{") {
				addSegmentos(s, a, b, ent, "terraform", add)
			} else {
				addNome(s, a, b, ent, "terraform", forte, add)
			}
		}
		v = q + 1
		for v < fim && s[v] == ' ' {
			v++
		}
		if !strings.HasPrefix(s[v:fim], "->") {
			return
		}
		v += 2
		for v < fim && s[v] == ' ' {
			v++
		}
	}
}

// quebraLinhas: [ini, fim) de cada linha (sem o \r).
func quebraLinhas(s string) [][2]int {
	var out [][2]int
	for i := 0; i < len(s); {
		j := strings.IndexByte(s[i:], '\n')
		f := len(s)
		if j >= 0 {
			f = i + j
		}
		e := f
		if e > i && s[e-1] == '\r' {
			e--
		}
		out = append(out, [2]int{i, e})
		i = f + 1
	}
	return out
}

// ---------------------------------------------------------------------------------------
// Ansible: inventário INI

var (
	reSecaoINI = regexp.MustCompile(`^\[([A-Za-z0-9_.\-]+)(:(?:vars|children))?\][ \t]*$`)
	reFaixaINI = regexp.MustCompile(`^[A-Za-z0-9_.\-]*\[[0-9a-z]+:[0-9a-z]+\][A-Za-z0-9_.\-]*$`)
)

func acharAnsibleINI(s string, add func(ObjAchado)) {
	if strings.IndexByte(s, '[') < 0 {
		return
	}
	forte := strings.Contains(s, "ansible_") || strings.Contains(s, ":children]") || strings.Contains(s, ":vars]")
	if !forte && !strings.Contains(s, "]\n") {
		return
	}
	secao := "" // "" = fora de seção; "h" = hosts; "v" = vars/children
	for _, ln := range quebraLinhas(s) {
		a, b := ln[0], ln[1]
		if a >= b {
			continue
		}
		l := s[a:b]
		if l[0] == '[' {
			m := reSecaoINI.FindStringSubmatch(l)
			switch {
			case m == nil:
				secao = ""
			case m[2] != "":
				secao = "v"
			default:
				secao = "h"
			}
			continue
		}
		if l[0] == '#' || l[0] == ';' || l[0] == ' ' || l[0] == '\t' {
			if l[0] == ' ' || l[0] == '\t' {
				secao = "" // linha indentada: não é inventário
			}
			continue
		}
		if secao == "" {
			continue
		}
		campos := strings.Fields(l)
		if len(campos) == 0 {
			continue
		}
		pos := a
		ok := true
		for k, c := range campos {
			if k == 0 && secao == "h" && strings.IndexByte(c, '=') >= 0 || k > 0 && strings.IndexByte(c, '=') <= 0 {
				ok = false
				break
			}
		}
		if secao == "v" && !strings.Contains(campos[0], "=") {
			continue
		}
		if !ok {
			continue
		}
		for k, c := range campos {
			x := pos + strings.Index(s[pos:b], c)
			pos = x + len(c)
			if k == 0 && secao == "h" {
				y := x + len(c)
				if i := strings.LastIndexByte(c, ':'); i > 0 && todoDigitos(c[i+1:]) {
					y = x + i
				}
				h := s[x:y]
				if forte || strings.ContainsAny(h, ".0123456789") {
					addNomeHostINI(s, x, y, forte, add)
				}
				continue
			}
			eq := strings.IndexByte(c, '=')
			chave, va := c[:eq], x+eq+1
			vb := x + len(c)
			if vb-va >= 2 && (s[va] == '"' || s[va] == '\'') && s[vb-1] == s[va] {
				va, vb = va+1, vb-1
			}
			switch chave {
			case "ansible_host":
				addNome(s, va, vb, "servidor", "ansible", true, add)
			case "ansible_user":
				addNome(s, va, vb, "usuario", "ansible", true, add)
			}
		}
	}
}

// addNomeHostINI: host do inventário; a faixa www[01:50].x é mascarada como unidade.
func addNomeHostINI(s string, a, b int, forte bool, add func(ObjAchado)) {
	v := s[a:b]
	if strings.ContainsAny(v, "[]") {
		if reFaixaINI.MatchString(v) {
			add(ObjAchado{a, b, "servidor", "ansible", forte})
		}
		return
	}
	addNome(s, a, b, "servidor", "ansible", forte, add)
}

// ---------------------------------------------------------------------------------------
// Bicep

var (
	reBicepRes  = regexp.MustCompile(`(?m)^[ \t]*resource[ \t]+\w+[ \t]+'([A-Za-z0-9.]+/[A-Za-z0-9./]+)@[^'\n]*'[ \t]+(?:existing[ \t]+)?=[ \t]*\{`)
	reBicepNome = regexp.MustCompile(`^[ \t]*name:[ \t]*'([^'\n]*)'`)
)

func acharBicep(s string, add func(ObjAchado)) {
	if !strings.Contains(s, "'Microsoft.") || !strings.Contains(s, "resource ") {
		return
	}
	ms := reBicepRes.FindAllStringSubmatchIndex(s, -1)
	for k, m := range ms {
		tipo := s[m[2]:m[3]]
		lim := len(s) // o corpo vai no máximo até o próximo recurso
		if k+1 < len(ms) {
			lim = ms[k+1][0]
		}
		prof := 1
		for i := m[1]; i < lim && prof > 0; {
			j := strings.IndexByte(s[i:lim], '\n')
			f := lim
			if j >= 0 {
				f = i + j
			}
			l := s[i:f]
			if prof == 1 {
				if n := reBicepNome.FindStringSubmatchIndex(l); n != nil {
					a, b := i+n[2], i+n[3]
					if strings.Contains(s[a:b], "${") {
						addSegmentos(s, a, b, entAzure(tipo[strings.LastIndexByte(tipo, '/')+1:]), "bicep", add)
					} else {
						addNomeAzure(s, a, b, tipo, "bicep", add)
					}
				}
			}
			prof += strings.Count(l, "{") - strings.Count(l, "}")
			i = f + 1
		}
	}
}

// ---------------------------------------------------------------------------------------
// Jenkinsfile

var (
	reJkLabel = regexp.MustCompile(`\blabel[ \t]+['"]([^'"\s]+)['"]`)
	reJkNode  = regexp.MustCompile(`\bnode[ \t]*\([ \t]*['"]([^'"\s]+)['"][ \t]*\)`)
	reJkImage = regexp.MustCompile(`\bimage[ \t]+['"]([^'"\s]+)['"]|\bdocker\.image\([ \t]*['"]([^'"\s]+)['"]`)
)

func acharJenkins(s string, add func(ObjAchado)) {
	if strings.IndexByte(s, '{') < 0 && strings.IndexByte(s, '(') < 0 {
		return
	}
	if !strings.Contains(s, "pipeline {") && !(strings.Contains(s, "node(") && strings.Contains(s, "stage(")) {
		return
	}
	for _, re := range []*regexp.Regexp{reJkLabel, reJkNode} {
		for _, m := range re.FindAllStringSubmatchIndex(s, -1) {
			addNome(s, m[2], m[3], "servidor", "jenkins", false, add)
		}
	}
	for _, m := range reJkImage.FindAllStringSubmatchIndex(s, -1) {
		if m[2] >= 0 {
			addImagem(s, m[2], m[3], "imagem", add)
		} else {
			addImagem(s, m[4], m[5], "imagem", add)
		}
	}
}

// ---------------------------------------------------------------------------------------
// Nome DNS de serviço do Kubernetes: <svc>.<ns>.svc.cluster.local

func acharDNSK8s(s string, add func(ObjAchado)) {
	const suf = ".svc.cluster.local"
	for i := 0; ; {
		j := strings.Index(s[i:], suf)
		if j < 0 {
			return
		}
		f := i + j
		i = f + len(suf)
		a := f
		for a > 0 && f-a < 253 && (ehAlnum(s[a-1]) || s[a-1] == '-' || s[a-1] == '.') {
			a--
		}
		ps := strings.Split(s[a:f], ".")
		if len(ps) < 2 {
			continue
		}
		ns := f - len(ps[len(ps)-1])
		sv := ns - 1 - len(ps[len(ps)-2])
		addNome(s, ns, f, "namespace", "k8s-dns", true, add)
		addNome(s, sv, ns-1, "servico", "k8s-dns", true, add)
	}
}
