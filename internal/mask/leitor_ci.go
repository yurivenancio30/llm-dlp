package mask

import (
	"regexp"
	"strings"
)

// Pipelines de CI: runners, ambientes e o Jenkinsfile (ver docs/pt-BR/estruturas.md, Pipelines de CI).

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
