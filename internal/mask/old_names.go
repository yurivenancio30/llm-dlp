package mask

import (
	"strings"

	"github.com/yurivenancio30/llm-dlp/internal/config"
)

// The type names were in Portuguese up to 0.9 (config.OldTypeNames). Two places still depend on them.

// oldTypeName: current name -> the name up to 0.9.
var oldTypeName = func() map[string]string {
	m := make(map[string]string, len(config.OldTypeNames))
	for stale, newOne := range config.OldTypeNames {
		m[newOne] = stale
	}
	return m
}()

// hmacDomain: the name the type had when its pseudonyms were first generated, which stays the HMAC
// domain. Renaming it would change every pseudonym of the type: the conversations already masked,
// the people registry and the seen values would stop matching.
func hmacDomain(typ string) string {
	if d, ok := oldTypeName[typ]; ok {
		return d
	}
	return typ
}

// currentType: the type of a seen-values entry with this version's name. The file written up to
// 0.9 (or by 0.9 during a version swap) has the Portuguese names.
func currentType(t string) string {
	rest, ok := strings.CutPrefix(t, objTypePrefix)
	if !ok {
		return config.NewTypeName(t)
	}
	e, tail, hasTail := strings.Cut(rest, "|")
	if hasTail {
		return objTypePrefix + config.NewTypeName(e) + "|" + tail
	}
	return objTypePrefix + config.NewTypeName(e)
}
