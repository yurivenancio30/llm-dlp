package config

import (
	"encoding/json"
	"errors"
	"os"
	"time"
)

// SetFields writes the given fields into config.json and keeps everything else in the file as it is
// (fields this program does not know included), with a copy of the previous file alongside
// (config.json.bak-DATE). Old field names become the new ones, as on load. Returns the path of the
// copy ("" if there was no file yet).
func SetFields(fields map[string]any) (string, error) {
	p := Path("config.json")
	m := map[string]any{}
	b, err := os.ReadFile(p)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if len(b) > 0 {
		if err := json.Unmarshal(b, &m); err != nil {
			return "", err
		}
		migrateNames(m, false, false)
	}
	for k, v := range fields {
		m[k] = v
	}
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(Dir(), 0o700); err != nil {
		return "", err
	}
	bak := ""
	if len(b) > 0 {
		bak = p + ".bak-" + time.Now().Format("20060102-150405")
		if err := os.WriteFile(bak, b, 0o600); err != nil {
			return "", err
		}
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, append(out, '\n'), 0o600); err != nil {
		return "", err
	}
	return bak, os.Rename(tmp, p)
}
