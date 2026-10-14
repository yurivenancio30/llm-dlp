package mask

import (
	"regexp"
	"strings"
)

// Structured configuration reader (YAML and JSON): the engine that reads by path, the dispatcher
// that decides the family (Kubernetes, compose, CloudFormation, ARM, Helm, Ansible, CI) and the
// common helpers. Each family's rules are in the files reader_kubernetes.go, reader_iac.go,
// reader_ansible.go and reader_ci.go.

type yEnt struct {
	path   []string
	ids    []int // id of each element of the path (the container of its children)
	root   int   // id of the document root
	ki, kf int   // the key (-1: list item)
	vi, vf int   // the scalar value, unquoted (-1: no value on the line / nested)
	tag    string
	tmpl   bool // value with {{ }} (Helm/Jinja template): not touched
}

func (e *yEnt) key() string { return e.path[len(e.path)-1] }

func (e *yEnt) id() int { return e.ids[len(e.ids)-1] }

func (e *yEnt) parent() int {
	if len(e.ids) < 2 {
		return e.root
	}
	return e.ids[len(e.ids)-2]
}

// limits for pathological input (memory and time): depth and number of entries
const (
	maxYAMLDepth = 32
	maxEntsYAML  = 50_000
)

type yCollector struct {
	ents []yEnt
	nid  int
}

func (w *yCollector) newOne() int { w.nid++; return w.nid }

func (w *yCollector) emitIt(path []string, ids []int, root, ki, kf, vi, vf int, tag string, tmpl bool) {
	if len(w.ents) >= maxEntsYAML || len(path) > maxYAMLDepth {
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

// readYAML: lines indented with spaces (a line with a tab in the indentation is not YAML), "- "
// items, plain or quoted keys, plain, quoted, tagged (!Ref) and flow ({a: b}, [a, b]) values, and |
// > blocks (skipped). "---" separates documents.
func (w *yCollector) readYAML(s string) {
	root := w.newOne()
	var st []yEl
	block := -1
	path := make([]string, 0, 16)
	ids := make([]int, 0, 16)
	curPath := func() ([]string, []int) {
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
		ini, end := a, b
		a = b + 1
		if end > ini && s[end-1] == '\r' {
			end--
		}
		ind := 0
		for ini+ind < end && s[ini+ind] == ' ' {
			ind++
		}
		if ini+ind >= end {
			continue
		}
		if block >= 0 {
			if ind > block {
				continue
			}
			block = -1
		}
		c := s[ini+ind]
		if c == '\t' {
			continue
		}
		if ind == 0 && (strings.HasPrefix(s[ini:end], "---") || strings.HasPrefix(s[ini:end], "...")) {
			root = w.newOne()
			st = st[:0]
			continue
		}
		if c == '#' || ind == 0 && c == '%' {
			continue
		}
		p := ini + ind
		item := false
		for p < end && s[p] == '-' && (p+1 == end || s[p+1] == ' ') && len(st) <= maxYAMLDepth {
			col := p - ini
			for len(st) > 0 && (st[len(st)-1].col > col || st[len(st)-1].col == col && st[len(st)-1].item) {
				st = st[:len(st)-1]
			}
			st = append(st, yEl{col, "-", w.newOne(), true})
			item = true
			p++
			for p < end && s[p] == ' ' {
				p++
			}
		}
		if p >= end || s[p] == '#' || len(st) > maxYAMLDepth {
			continue
		}
		col := p - ini
		ki, kf, vp, ok := yamlKey(s, p, end)
		if !ok {
			if item {
				pt, is := curPath()
				w.yamlValue(s, pt, is, root, -1, -1, p, end, st[len(st)-1].col, &block)
			}
			continue
		}
		for len(st) > 0 && st[len(st)-1].col >= col {
			st = st[:len(st)-1]
		}
		st = append(st, yEl{col, s[ki:kf], w.newOne(), false})
		pt, is := curPath()
		w.yamlValue(s, pt, is, root, ki, kf, vp, end, col, &block)
	}
}

// yamlKey: the key that starts at p (plain or quoted) and where the value starts.
func yamlKey(s string, p, end int) (ki, kf, vp int, ok bool) {
	c := s[p]
	if c == '"' || c == '\'' {
		q := closeQuotes(s, p, end)
		if q < 0 {
			return
		}
		j := q + 1
		for j < end && s[j] == ' ' {
			j++
		}
		if j < end && s[j] == ':' && (j+1 == end || s[j+1] == ' ' || s[j+1] == '\t') {
			return p + 1, q, j + 1, true
		}
		return
	}
	if strings.IndexByte("[{&*!|>%@`#,?", c) >= 0 {
		return
	}
	for j := p; j < end; j++ {
		if s[j] == ':' && (j+1 == end || s[j+1] == ' ' || s[j+1] == '\t') {
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

// closeQuotes: the quote that closes the one at s[p], on the same line (” and \" are escapes).
func closeQuotes(s string, p, end int) int {
	q := s[p]
	for j := p + 1; j < end; j++ {
		switch {
		case q == '"' && s[j] == '\\':
			j++
		case s[j] == q:
			if q == '\'' && j+1 < end && s[j+1] == '\'' {
				j++
				continue
			}
			return j
		}
	}
	return -1
}

func (w *yCollector) yamlValue(s string, path []string, ids []int, root, ki, kf, v, end, refCol int, block *int) {
	for v < end && (s[v] == ' ' || s[v] == '\t') {
		v++
	}
	tag := ""
	for v < end && (s[v] == '&' || s[v] == '!') { // anchor, tag
		j := v
		for j < end && s[j] != ' ' {
			j++
		}
		if s[v] == '!' {
			tag = s[v:j]
		}
		v = j
		for v < end && s[v] == ' ' {
			v++
		}
	}
	if v >= end || s[v] == '#' || s[v] == '*' {
		w.emitIt(path, ids, root, ki, kf, -1, -1, tag, false)
		return
	}
	switch s[v] {
	case '|', '>':
		*block = refCol
		w.emitIt(path, ids, root, ki, kf, -1, -1, tag, false)
	case '"', '\'':
		q := closeQuotes(s, v, end)
		if q < 0 {
			w.emitIt(path, ids, root, ki, kf, -1, -1, tag, false)
			return
		}
		w.emitIt(path, ids, root, ki, kf, v+1, q, tag, strings.Contains(s[v+1:q], "{{"))
	case '{', '[':
		if v+1 < end && s[v] == '{' && s[v+1] == '{' {
			w.emitIt(path, ids, root, ki, kf, v, end, tag, true)
			return
		}
		w.emitIt(path, ids, root, ki, kf, -1, -1, tag, false)
		w.stream(s, v, end, path, ids, root, 0)
	default:
		e := v
		for e < end && !(s[e] == '#' && (s[e-1] == ' ' || s[e-1] == '\t')) {
			e++
		}
		for e > v && (s[e-1] == ' ' || s[e-1] == '\t') {
			e--
		}
		w.emitIt(path, ids, root, ki, kf, v, e, tag, strings.Contains(s[v:e], "{{"))
	}
}

// flow: {a: b, c: [d, e]} on a single line. Returns where it stopped.
func (w *yCollector) stream(s string, i, end int, path []string, ids []int, root, depth int) int {
	if depth > 20 {
		return end
	}
	opener := s[i]
	closes := byte('}')
	if opener == '[' {
		closes = ']'
	}
	i++
	for i < end {
		for i < end && (s[i] == ' ' || s[i] == ',' || s[i] == '\t') {
			i++
		}
		if i >= end {
			return end
		}
		if s[i] == closes {
			return i + 1
		}
		if opener == '[' {
			pp := append(path[:len(path):len(path)], "-")
			pi := append(ids[:len(ids):len(ids)], w.newOne())
			n := w.scaleStream(s, i, end, pp, pi, root, -1, -1, depth)
			if n <= i {
				return end
			}
			i = n
			continue
		}
		var ki, kf, j int
		if s[i] == '"' || s[i] == '\'' {
			q := closeQuotes(s, i, end)
			if q < 0 {
				return end
			}
			ki, kf, j = i+1, q, q+1
			for j < end && s[j] == ' ' {
				j++
			}
			if j >= end || s[j] != ':' {
				return end
			}
		} else {
			j = i
			for j < end && s[j] != ',' && s[j] != closes && !(s[j] == ':' && (j+1 == end || s[j+1] == ' ')) {
				j++
			}
			if j >= end || s[j] != ':' {
				if j < end && s[j] == ',' {
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
		for j < end && s[j] == ' ' {
			j++
		}
		pp := append(path[:len(path):len(path)], s[ki:kf])
		pi := append(ids[:len(ids):len(ids)], w.newOne())
		n := w.scaleStream(s, j, end, pp, pi, root, ki, kf, depth)
		if n <= i {
			return end
		}
		i = n
	}
	return end
}

func (w *yCollector) scaleStream(s string, i, end int, path []string, ids []int, root, ki, kf, depth int) int {
	if i >= end {
		w.emitIt(path, ids, root, ki, kf, -1, -1, "", false)
		return end
	}
	switch s[i] {
	case '{', '[':
		w.emitIt(path, ids, root, ki, kf, -1, -1, "", false)
		return w.stream(s, i, end, path, ids, root, depth+1)
	case '"', '\'':
		q := closeQuotes(s, i, end)
		if q < 0 {
			return end
		}
		w.emitIt(path, ids, root, ki, kf, i+1, q, "", strings.Contains(s[i+1:q], "{{"))
		return q + 1
	}
	e := i
	for e < end && s[e] != ',' && s[e] != ']' && s[e] != '}' {
		e++
	}
	f := e
	for f > i && s[f-1] == ' ' {
		f--
	}
	w.emitIt(path, ids, root, ki, kf, i, f, "", false)
	return e
}

// readJSON: JSON objects in the middle of the text (they start with { followed by a quoted key). On
// anything that is not JSON, drops the object and keeps looking.
func (w *yCollector) readJSON(s string) {
	type ctx struct {
		obj, hasKey bool
		key         string
		ki, kf      int
	}
	var st []ctx
	var path []string
	var ids []int
	root := 0
	ws := func(c byte) bool { return c == ' ' || c == '\n' || c == '\t' || c == '\r' }
	wide := func() { st, path, ids = st[:0], path[:0], ids[:0] }
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
				root = w.newOne()
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
				wide()
				i++
				continue
			}
			if top.obj && !top.hasKey {
				k := q + 1
				for k < len(s) && ws(s[k]) {
					k++
				}
				if k >= len(s) || s[k] != ':' {
					wide()
					i = q + 1
					continue
				}
				top.key, top.ki, top.kf, top.hasKey = s[i+1:q], i+1, q, true
				i = k + 1
				continue
			}
			name, ki, kf := "-", -1, -1
			if top.obj {
				name, ki, kf = top.key, top.ki, top.kf
				top.hasKey = false
			}
			path = append(path, name)
			ids = append(ids, w.newOne())
			w.emitIt(path, ids, root, ki, kf, i+1, q, "", strings.Contains(s[i+1:q], "{{"))
			path, ids = path[:len(path)-1], ids[:len(ids)-1]
			i = q + 1
		case c == '{' || c == '[':
			if top.obj && !top.hasKey || len(st) > maxYAMLDepth {
				wide()
				i++
				continue
			}
			name, ki, kf := "-", -1, -1
			if top.obj {
				name, ki, kf = top.key, top.ki, top.kf
				top.hasKey = false
			}
			path = append(path, name)
			ids = append(ids, w.newOne())
			w.emitIt(path, ids, root, ki, kf, -1, -1, "", false)
			st = append(st, ctx{obj: c == '{'})
			i++
		case c == '}' || c == ']':
			if top.obj != (c == '}') {
				wide()
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
			for k < len(s) && (isAlnum(s[k]) || s[k] == '.' || s[k] == '-' || s[k] == '+') {
				k++
			}
			if k == i || top.obj && !top.hasKey {
				wide()
				i++
				continue
			}
			top.hasKey = false
			i = k
		}
	}
}

// --------------------------------------------------------------------------------------- Public
// vocabulary and value validation

var publicDevopsSet = func() map[string]bool {
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

// providers' regions and zones (official forms): us-east-1, sa-east-1, southamerica-east1,
// europe-west1-b
var reRegion = regexp.MustCompile(`^(?:(?:us|eu|ap|sa|ca|me|af|il|mx|cn)(?:-gov)?-(?:east|west|north|south|central|northeast|southeast|northwest|southwest)-\d[a-z]?|(?:us|europe|asia|australia|southamerica|northamerica|me|africa)-[a-z]+\d(?:-[a-z])?)$`)

func publicDevops(v string) bool {
	l := strings.ToLower(v)
	if publicDevopsSet[l] || reRegion.MatchString(l) {
		return true
	}
	for _, p := range []string{"ubuntu-", "windows-", "macos-"} {
		if strings.HasPrefix(l, p) {
			return true
		}
	}
	return publicConnection(v)
}

// resource name: letters, digits, _ . -; no space, quotes, expression or path
var reResourceName = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.\-]{0,252}$`)

func allDigits(v string) bool {
	for i := 0; i < len(v); i++ {
		if v[i] < '0' || v[i] > '9' {
			return false
		}
	}
	return v != ""
}

func isIPv4(v string) bool {
	if strings.Count(v, ".") != 3 {
		return false
	}
	for _, p := range strings.Split(v, ".") {
		if !allDigits(p) || len(p) > 3 {
			return false
		}
	}
	return true
}

func addName(s string, a, b int, ent, rule string, strong bool, add func(ObjFinding)) {
	if a < 0 || b > len(s) || b <= a {
		return
	}
	v := s[a:b]
	if !reResourceName.MatchString(v) || allDigits(v) && !(ent == "conta_nuvem" && len(v) == 12) || isIPv4(v) {
		return
	}
	add(ObjFinding{a, b, ent, rule, strong})
}

// addSegments: value with interpolation (${...}): only the literal pieces, and weak.
func addSegments(s string, a, b int, ent, rule string, add func(ObjFinding)) {
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
		if y-x >= 4 && identifierShape(s[x:y]) {
			addName(s, x, y, ent, rule, false, add)
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

// addARNOrName: a name that may come as an ARN (arn:aws:eks:region:account:cluster/name).
func addARNOrName(s string, a, b int, ent, rule string, add func(ObjFinding)) {
	v := s[a:b]
	if !strings.HasPrefix(v, "arn:") {
		addName(s, a, b, ent, rule, true, add)
		return
	}
	ps := strings.SplitN(v, ":", 6)
	if len(ps) < 6 {
		return
	}
	account := a + len(ps[0]) + len(ps[1]) + len(ps[2]) + len(ps[3]) + 4
	addName(s, account, account+len(ps[4]), "conta_nuvem", rule, true, add)
	if i := strings.LastIndexByte(v, '/'); i >= 0 {
		addName(s, a+i+1, b, ent, rule, true, add)
	}
}

// ---------------------------------------------------------------------------------------
// Structured YAML/JSON reader: Kubernetes, kubeconfig, Helm, compose, CloudFormation, ARM, Ansible
// (YAML) and CI pipelines.

// composeServiceKeys: the keys of a service in the Compose specification
// (https://github.com/compose-spec/compose-spec/blob/main/05-services.md), plus the "<<" merge (a
// service that inherits everything from an anchor, "<<: *common"). A child of "services:" with one
// of them is a service, whether or not it has its own image.
var composeServiceKeys = setOf("image", "build", "ports", "environment", "env_file", "command", "entrypoint",
	"depends_on", "volumes", "networks", "restart", "healthcheck", "deploy", "container_name", "hostname",
	"labels", "expose", "links", "extends", "profiles", "working_dir", "user", "logging", "ulimits",
	"cap_add", "cap_drop", "secrets", "configs", "extra_hosts", "tmpfs", "stdin_open", "tty", "platform",
	"pull_policy", "init", "privileged", "network_mode", "external_links", "shm_size", "stop_signal",
	"stop_grace_period", "sysctls", "devices", "dns", "<<")

var yamlSignals = []string{"apiVersion", "services:", "Resources:", "runs-on", "stages:", "script:", "pool:",
	"jobs:", "hosts:", "ansible_", "delegate_to"}

var jsonSignals = []string{`"apiVersion"`, `"Resources"`, `"resources"`}

func hasAny(s string, ss []string) bool {
	for _, x := range ss {
		if strings.Contains(s, x) {
			return true
		}
	}
	return false
}

func findStructuredDevops(s string, add func(ObjFinding)) {
	if strings.IndexByte(s, ':') < 0 {
		return
	}
	y, j := hasAny(s, yamlSignals), hasAny(s, jsonSignals)
	if !y && !j {
		return
	}
	w := &yCollector{}
	w.readYAML(s) // indented JSON is also read line by line
	if j {
		w.readJSON(s)
	}
	if len(w.ents) == 0 {
		return
	}
	seen := map[[2]int]bool{}
	structureRules(s, w.ents, func(o ObjFinding) {
		k := [2]int{o.Start, o.End}
		if !seen[k] {
			seen[k] = true
			add(o)
		}
	})
}

var reAPIVersion = regexp.MustCompile(`^(?:[a-z0-9.-]+/)?v\d+(?:(?:alpha|beta)\d+)?$`)

// CloudFormation properties that hold the real resource name
var cfnProps = map[string]string{"BucketName": "bucket", "DBInstanceIdentifier": "database", "DBClusterIdentifier": "database",
	"DBName": "database", "DatabaseName": "database", "TableName": "tabela", "ServerName": "servidor", "ClusterName": "servidor",
	"QueueName": "fila", "TopicName": "fila", "StreamName": "fila", "FunctionName": "servico", "RepositoryName": "repositorio",
	"ServiceName": "servico", "RoleName": "usuario", "UserName": "usuario", "MasterUsername": "usuario", "GroupName": "usuario"}

var k8sLabels = map[string]bool{"app.kubernetes.io/name": true, "app.kubernetes.io/instance": true, "app.kubernetes.io/part-of": true}

var playWords = []string{"tasks", "roles", "become", "gather_facts", "vars", "handlers", "pre_tasks", "post_tasks", "remote_user"}

func structureRules(s string, ents []yEnt, add func(ObjFinding)) {
	children := map[int][]int{}
	keys := map[int]map[string]int{} // container -> key -> first entry
	rootDir := map[int]map[string]int{}
	type resource struct{ api, kind string }
	res := map[int]*resource{}
	compose := map[int]bool{}
	composeSvc := map[int]bool{}
	composeDecl := map[int]map[string]bool{} // networks and volumes declared at the top
	ci := map[int]bool{}
	val := func(e *yEnt) string {
		if e.vi < 0 || e.tmpl {
			return ""
		}
		return s[e.vi:e.vf]
	}
	for i := range ents {
		e := &ents[i]
		p := e.parent()
		children[p] = append(children[p], i)
		k := e.key()
		if e.ki >= 0 {
			if keys[p] == nil {
				keys[p] = map[string]int{}
			}
			if _, ok := keys[p][k]; !ok {
				keys[p][k] = i
			}
		}
		if len(e.path) == 1 {
			if rootDir[e.root] == nil {
				rootDir[e.root] = map[string]int{}
			}
			if _, ok := rootDir[e.root][k]; !ok {
				rootDir[e.root][k] = i
			}
			switch k {
			case "jobs", "stages", "steps", "trigger", "workflow":
				ci[e.root] = true
			}
		}
		if e.vi >= 0 && (k == "apiVersion" || k == "kind") {
			r := res[p]
			if r == nil {
				r = &resource{}
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
			if composeDecl[e.root] == nil {
				composeDecl[e.root] = map[string]bool{}
			}
			composeDecl[e.root][e.path[0]+"\x00"+k] = true
		}
		if len(e.path) == 3 && e.path[0] == "services" && composeServiceKeys[k] {
			compose[e.root] = true
			composeSvc[e.ids[1]] = true
		}
	}
	child := func(id int, k string) *yEnt {
		if i, ok := keys[id][k]; ok {
			return &ents[i]
		}
		return nil
	}
	isResource := func(c int) (string, bool) {
		r := res[c]
		if r == nil || r.kind == "" || !reAPIVersion.MatchString(r.api) {
			return "", false
		}
		return r.kind, true
	}
	// Helm Chart.yaml: apiVersion v1/v2 + name + version, no kind
	chart := map[int]bool{}
	for root, ks := range rootDir {
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
		k := e.key()
		v := val(e)
		// Kubernetes: the closest resource (object with apiVersion and kind)
		inK8s := false
		for j := len(e.path) - 1; j >= 0; j-- {
			c := e.root
			if j > 0 {
				c = e.ids[j-1]
			}
			if kind, ok := isResource(c); ok {
				k8sRule(s, e, e.path[j:], kind, v, add)
				inK8s = true
				break
			}
		}
		if inK8s {
			continue
		}
		if chart[e.root] && len(e.path) == 1 && k == "name" && v != "" {
			addName(s, e.vi, e.vf, "servico", "helm", true, add)
		}
		if compose[e.root] && len(e.path) >= 2 {
			switch {
			case e.path[0] == "services" && len(e.path) == 2 && e.ki >= 0 && composeSvc[e.id()]:
				addName(s, e.ki, e.kf, "servico", "compose", true, add)
			case e.path[0] == "services" && len(e.path) == 3 && v != "":
				switch k {
				case "container_name":
					addName(s, e.vi, e.vf, "servico", "compose", true, add)
				case "hostname":
					addName(s, e.vi, e.vf, "servidor", "compose", true, add)
				case "image":
					addImage(s, e.vi, e.vf, "imagem", add)
				}
			case (e.path[0] == "networks" || e.path[0] == "volumes") && len(e.path) == 2 && e.ki >= 0:
				addName(s, e.ki, e.kf, "servico", "compose", false, add)
			case e.path[0] == "services" && len(e.path) == 4 && (e.path[2] == "networks" || e.path[2] == "volumes"):
				// reference to a network/volume declared at the top: "- net", "net:", "-
				// volume:/path"
				a, b := e.ki, e.kf
				if e.ki < 0 {
					a, b = e.vi, e.vf
					if a >= 0 {
						if c := strings.IndexByte(s[a:b], ':'); c >= 0 {
							b = a + c
						}
					}
				}
				if a >= 0 && b > a && composeDecl[e.root][e.path[2]+"\x00"+s[a:b]] {
					addName(s, a, b, "servico", "compose", false, add)
				}
			}
		}
		// CloudFormation: Resources.<logical>.Properties.<XxxName>
		if len(e.path) == 4 && e.path[0] == "Resources" && e.path[2] == "Properties" && v != "" {
			if ent, ok := cfnProps[k]; ok {
				switch e.tag {
				case "":
					addName(s, e.vi, e.vf, ent, "cfn", true, add)
				case "!Sub":
					addSegments(s, e.vi, e.vf, ent, "cfn", add)
				}
			}
		}
		// ARM: resources[].name, with the Microsoft.X/y type beside it; "[...]" is an expression
		if k == "name" && v != "" && v[0] != '[' && len(e.path) >= 3 && e.path[len(e.path)-2] == "-" && e.path[len(e.path)-3] == "resources" {
			if t := child(e.parent(), "type"); t != nil && strings.HasPrefix(strings.ToLower(val(t)), "microsoft.") {
				addAzureName(s, e.vi, e.vf, val(t), "arm", add)
			}
		}
		// Ansible (YAML)
		switch {
		case k == "hosts" && e.vi < 0:
			for _, f := range children[e.id()] {
				if c := &ents[f]; c.ki >= 0 && !c.tmpl {
					addName(s, c.ki, c.kf, "servidor", "ansible", true, add)
				}
			}
		case k == "hosts" && v != "" && len(e.path) == 2 && e.path[0] == "-":
			play := false
			for _, p := range playWords {
				if child(e.parent(), p) != nil {
					play = true
					break
				}
			}
			if play { // hosts pattern: a,b:c:&d:!e
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
					addName(s, a, b, "servidor", "ansible", false, add)
					x = y + 1
				}
			}
		case (k == "ansible_host" || k == "ansible_user") && v != "":
			ent := "servidor"
			if k == "ansible_user" {
				ent = "usuario"
			}
			addName(s, e.vi, e.vf, ent, "ansible", true, add)
		case k == "delegate_to" && v != "":
			addName(s, e.vi, e.vf, "servidor", "ansible", false, add)
		}
		if ci[e.root] {
			ciRule(s, ents, e, v, child, add)
		}
	}
}
