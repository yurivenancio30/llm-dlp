package mask

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/yurivenancio30/llm-dlp/internal/config"
)

// llm-dlp forget: a value learned by mistake stops spreading, is not learned again and does not come
// back through another process's file; where the structure shows it is a name, it is still masked.
// Made-up names.
func TestForget(t *testing.T) {
	path := filepath.Join(t.TempDir(), "seen.json")
	masker := func() *Masker {
		ps, _ := LoadPeople(filepath.Join(t.TempDir(), "p.json"))
		vs, err := LoadSeen(path)
		if err != nil {
			t.Fatal(err)
		}
		m, err := NewMasker(config.Default(), testKey, ps, vs)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	const learn = "DB_USER | DB_NAME\nvndx_svc_x9 | vndx_db_x1\nvndx_svc_x8 | vndx_db_x1\n"
	const loose = "erro ao conectar com vndx_svc_x9 agora"
	a := masker()
	a.Mask(learn)
	if err := a.seenSet.SaveNow(); err != nil {
		t.Fatal(err)
	}
	if out, _ := masker().Mask(loose); strings.Contains(out, "vndx_svc_x9") {
		t.Fatalf("setup: the learned name should spread: %q", out)
	}
	b := masker()
	if n := b.Forget([]string{"vndx_svc_x9"}); n == 0 {
		t.Fatal("nothing forgotten")
	}
	if b.Forget([]string{"never_seen_x7"}) != 0 {
		t.Error("an unknown value counted as forgotten")
	}
	if err := b.seenSet.SaveNow(); err != nil {
		t.Fatal(err)
	}
	// the other process (a), which still has the entry, writes after: the tombstone stays
	a.Mask("DB_USER | DB_NAME\nvndx_svc_x5 | vndx_db_x2\nvndx_svc_x4 | vndx_db_x2\n")
	if err := a.seenSet.SaveNow(); err != nil {
		t.Fatal(err)
	}
	c := masker()
	if out, _ := c.Mask(loose); !strings.Contains(out, "vndx_svc_x9") {
		t.Errorf("forgotten name still spread: %q", out)
	}
	// the structure still masks it, without learning it again
	if out, _ := c.Mask(learn); strings.Contains(out, "vndx_svc_x9") {
		t.Errorf("under DB_USER it should still be masked: %q", out)
	}
	if out, _ := c.Mask(loose); !strings.Contains(out, "vndx_svc_x9") {
		t.Errorf("forgotten name learned again in RAM by the same process: %q", out)
	}
	c.seenSet.SaveNow()
	if out, _ := masker().Mask(loose); !strings.Contains(out, "vndx_svc_x9") {
		t.Errorf("forgotten name was learned again: %q", out)
	}
	// the other learned names are untouched
	if out, _ := masker().Mask("erro com vndx_svc_x5"); strings.Contains(out, "vndx_svc_x5") {
		t.Errorf("a name learned after is gone: %q", out)
	}
}
