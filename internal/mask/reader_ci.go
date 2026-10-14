package mask

import (
	"regexp"
	"strings"
)

// CI pipelines: runners, environments and the Jenkinsfile (see docs/structures.md, CI pipelines).

// ciRule: GitHub Actions, GitLab CI, Azure Pipelines.
func ciRule(s string, ents []yEnt, e *yEnt, v string, child func(int, string) *yEnt, add func(ObjFinding)) {
	k := e.key()
	n := len(e.path)
	prev := ""
	if n >= 2 {
		prev = e.path[n-2]
	}
	prev2 := ""
	if n >= 3 {
		prev2 = e.path[n-3]
	}
	runner := func() { addName(s, e.vi, e.vf, "server", "ci", false, add) }
	job := func(id int) bool {
		for _, x := range []string{"runs-on", "script", "steps", "strategy", "deployment", "stage"} {
			if child(id, x) != nil {
				return true
			}
		}
		return false
	}
	if v == "" {
		return
	}
	switch {
	case k == "image" || k == "container" && n >= 2 || k == "name" && prev == "image" || k == "image" && prev == "container":
		addImage(s, e.vi, e.vf, "image", add)
	case k == "-" && prev == "services" || k == "name" && prev == "-" && prev2 == "services":
		addImage(s, e.vi, e.vf, "image", add)
	case k == "runs-on" || k == "-" && prev == "runs-on" || prev == "runs-on" && (k == "group" || k == "labels") || k == "-" && prev == "labels" && prev2 == "runs-on":
		runner()
	case k == "-" && prev == "tags" && n == 3 && e.path[0] != "-" && job(e.ids[0]):
		runner()
	case k == "pool" || k == "name" && prev == "pool":
		runner()
	case k == "environment" && job(e.parent()):
		addName(s, e.vi, e.vf, "service", "ci", false, add)
	case k == "name" && prev == "environment":
		owner := e.root // the container that has the environment key
		if n >= 3 {
			owner = e.ids[n-3]
		}
		if job(owner) {
			addName(s, e.vi, e.vf, "service", "ci", false, add)
		}
	}
}

// ---------------------------------------------------------------------------------------
// Terraform/HCL and plan output

var (
	reJkLabel = regexp.MustCompile(`\blabel[ \t]+['"]([^'"\s]+)['"]`)
	reJkNode  = regexp.MustCompile(`\bnode[ \t]*\([ \t]*['"]([^'"\s]+)['"][ \t]*\)`)
	reJkImage = regexp.MustCompile(`\bimage[ \t]+['"]([^'"\s]+)['"]|\bdocker\.image\([ \t]*['"]([^'"\s]+)['"]`)
)

func findJenkins(s string, add func(ObjFinding)) {
	if strings.IndexByte(s, '{') < 0 && strings.IndexByte(s, '(') < 0 {
		return
	}
	if !strings.Contains(s, "pipeline {") && !(strings.Contains(s, "node(") && strings.Contains(s, "stage(")) {
		return
	}
	for _, re := range []*regexp.Regexp{reJkLabel, reJkNode} {
		for _, m := range re.FindAllStringSubmatchIndex(s, -1) {
			addName(s, m[2], m[3], "server", "jenkins", false, add)
		}
	}
	for _, m := range reJkImage.FindAllStringSubmatchIndex(s, -1) {
		if m[2] >= 0 {
			addImage(s, m[2], m[3], "image", add)
		} else {
			addImage(s, m[4], m[5], "image", add)
		}
	}
}

// ---------------------------------------------------------------------------------------
// Kubernetes service DNS name: <svc>.<ns>.svc.cluster.local
