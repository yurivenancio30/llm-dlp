package mask

import "strings"

// Brakes of the rules above 2% in the real sessions (TestMeasureRealSessions, counts and shapes
// only). By the shape and by the public vocabularies that already exist.

// Connection (measured in the sessions: connection/tnsnames and connection/uri above 2%). The
// public values the connection rules learned all had the same shape: a type or attribute word
// followed by a number or glued to another one ("host1", "user2", "db01", "dbName", "serverHost").
// It is an example and documentation model name, not a resource name. The decision is by shape: at
// most two pieces, all from the vocabulary that already exists (entPiece, entBeforeName,
// keyAttribute, vocabDev), with or without a number at the end. A single word ("host") was already
// public. It only applies to the database URI and to tnsnames: in the other connection and address
// rules, "broker1" in a bootstrap.servers is still a name (TestReaderKeyValue).

// modelName: v is type word(s) with a number or glued, with nothing of its own.
func modelName(v string) bool {
	base := strings.TrimRight(v, "0123456789")
	if base == "" || len(v) > 24 {
		return false
	}
	var ps []string
	ini := 0
	for i := 1; i <= len(base); i++ {
		if i == len(base) || base[i] == '_' || base[i] == '-' || base[i] >= 'A' && base[i] <= 'Z' && base[i-1] >= 'a' && base[i-1] <= 'z' {
			if p := strings.Trim(base[ini:i], "_-"); p != "" {
				ps = append(ps, strings.ToLower(p))
			}
			ini = i
		}
	}
	if len(ps) == 0 || len(ps) > 2 || len(ps) == 1 && base == v {
		return false // one word without a number: the usual public lists decide
	}
	for _, p := range ps {
		_, t1 := entPiece[p]
		_, t2 := entBeforeName[p]
		if !(t1 || t2 || keyAttribute[p] || vocabDev[p]) {
			return false
		}
	}
	return true
}
