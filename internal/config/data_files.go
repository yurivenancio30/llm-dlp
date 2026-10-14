package config

import (
	"errors"
	"os"
	"strconv"
)

// Data files in Dir(). Up to 0.9 they had Portuguese names (chave, pessoas.json, vistos.json): the
// first time a version from 0.10 on asks for one, the old file is copied to the new name. Copied, not
// moved: a version up to 0.9 (after going back with update) still finds its own. The key never
// changes, so both copies are the same key; a new one would change every pseudonym.
const (
	KeyFile    = "key"
	PeopleFile = "people.json"
	SeenFile   = "seen.json"
	SentFile   = "sent.log" // starts over with each version: nothing to bring
)

var oldDataFiles = map[string]string{KeyFile: "chave", PeopleFile: "pessoas.json", SeenFile: "vistos.json"}

// OldSentFile: the record of sent texts up to 0.9, which no version from 0.10 on reads.
const OldSentFile = "enviados.log"

// DataPath: the path of a data file, after copying the file with the old name if only it exists.
// On error the caller must stop: going on without the old key would create a new one.
func DataPath(name string) (string, error) {
	p := Path(name)
	old, ok := oldDataFiles[name]
	if !ok {
		return p, nil
	}
	if _, err := os.Stat(p); !errors.Is(err, os.ErrNotExist) {
		return p, err
	}
	b, err := os.ReadFile(Path(old))
	if errors.Is(err, os.ErrNotExist) {
		return p, nil
	}
	if err != nil {
		return p, err
	}
	tmp := p + ".tmp-" + strconv.Itoa(os.Getpid())
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		os.Remove(tmp)
		return p, err
	}
	if err := os.Rename(tmp, p); err != nil {
		os.Remove(tmp)
		return p, err
	}
	return p, nil
}
