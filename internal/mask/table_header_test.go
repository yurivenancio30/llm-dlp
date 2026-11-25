package mask

import "testing"

// A first line without a separator line under it is a header only if it does not look like the
// lines below (headerVote). Headerless lists must not type the whole column by their first key;
// real headers must keep typing it. Made-up names.
func TestHeaderlessTable(t *testing.T) {
	for _, sep := range []string{" | ", "\t", ";", ","} {
		m := newTest(t) // fresh: the memory of one case would type the next
		headerless := "VNDX_ACCOUNT" + sep + "preenchido\nVNDX_USER_X" + sep + "preenchido\nVNDX_ROLE_XY" + sep + "preenchido\n"
		for _, key := range []string{"VNDX_USER_X", "VNDX_ROLE_XY"} {
			if got := typeOf(m, headerless, key); got == "obj.cloud_account" {
				t.Errorf("sep %q: %s typed by the first line as %q", sep, key, got)
			}
		}
		withHeader := "table_name" + sep + "owner\ntb_pedido_x9" + sep + "ana\ntb_cliente_x2" + sep + "bia\n"
		if got := typeOf(m, withHeader, "tb_pedido_x9"); got != "obj.table" {
			t.Errorf("sep %q: real header lost: type %q", sep, got)
		}
		numbered := "id" + sep + "table_name\n1" + sep + "tb_pedido_x9\n2" + sep + "tb_cliente_x2\n"
		if got := typeOf(m, numbered, "tb_cliente_x2"); got != "obj.table" {
			t.Errorf("sep %q: header over a number column lost: type %q", sep, got)
		}
	}
}

func TestHeaderVote(t *testing.T) {
	for _, c := range []struct {
		in   string
		want int // sign
	}{
		{"KEY_A | filled\nKEY_BB | filled\nKEY_CCC | filled", -1},
		{"uf | name\nSP | ana\nRJ | bia", 1},
		{"id | name\n1 | ana\n2 | bianca", 1},
		{"10 | ana\n20 | bia\n30 | caio", -1},
		{"name | city\nana | x\nbianca | yy", 0}, // nothing consistent: keeps the header
	} {
		lines := splitLines(c.in)
		cs := objCells(c.in, lines[0].a, lines[0].b, '|', nil)
		var rows [][]cell
		for _, l := range lines[1:] {
			rows = append(rows, objCells(c.in, l.a, l.b, '|', nil))
		}
		got := headerVote(c.in, cs, rows)
		if got > 0 && c.want <= 0 || got < 0 && c.want >= 0 || got == 0 && c.want != 0 {
			t.Errorf("%q: vote %d, expected sign %d", c.in, got, c.want)
		}
	}
}

func splitLines(s string) []cell {
	var out []cell
	a := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == '\n' {
			out = append(out, cell{a, i})
			a = i + 1
		}
	}
	return out
}

// The column's own name repeated below it is not a value of the column: two lines of grep with the
// same list of variables made the first the header, and each name typed itself (VNDX_USER a user).
func TestHeaderRepeatedBelow(t *testing.T) {
	for _, s := range []string{
		"app/check_conn.py:REQUIRED = [\"VNDX_ACCOUNT\", \"VNDX_USER\", \"VNDX_DATABASE\", \"VNDX_SCHEMA\"]\napp/run.py:NEEDED = [\"VNDX_ACCOUNT\", \"VNDX_USER\", \"VNDX_DATABASE\", \"VNDX_SCHEMA\"]\n",
		"VNDX_ACCOUNT | VNDX_USER | VNDX_DATABASE\nVNDX_ACCOUNT | VNDX_USER | VNDX_DATABASE\n",
	} {
		m := newTest(t)
		for _, v := range []string{"VNDX_ACCOUNT", "VNDX_USER", "VNDX_DATABASE"} {
			if got := typeOf(m, s, v); got != "" {
				t.Errorf("%s masked as %q in:\n%s", v, got, s)
			}
		}
	}
	// a real value under a typed header is still masked
	m := newTest(t)
	if got := typeOf(m, "DB_USER | DB_NAME\nsvc_etl_x9 | vendas_demo_x1\nsvc_bi_x2 | vendas_demo_x1\n", "svc_etl_x9"); got == "" {
		t.Errorf("value under DB_USER no longer masked")
	}
}
