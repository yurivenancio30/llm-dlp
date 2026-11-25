package mask

import "testing"

// A word that names a kind of object (the project's type words: tabela, schema, coluna, banco) is
// a label, never the name of one, in any format; the names around it still are. Made-up names.
func TestTypeWordIsNotAName(t *testing.T) {
	for _, c := range []struct {
		s          string
		gone, stay []string
	}{
		{"objeto | dono\ntabela | vndx_user_x1\nvndx_tb_x9 | vndx_user_x2\n", []string{"vndx_tb_x9"}, []string{"tabela | "}},
		{"TABLE_NAME=tabela\nDB_NAME=vndx_db_x1\n", []string{"vndx_db_x1"}, []string{"TABLE_NAME=tabela"}},
		{`{"table": "tabela", "schema": "vndx_sch_x1"}`, []string{"vndx_sch_x1"}, []string{`"table": "tabela"`}},
		{"--table coluna --schema vndx_sch_x2", []string{"vndx_sch_x2"}, []string{"--table coluna"}},
	} {
		check(t, newTest(t), c.s, c.gone, c.stay)
	}
	// a piece of a path is the folder's own name: see TestFolderIsNotADomainUser ("Banco de Dados")
}
