package mask

import "testing"

// Where a name stands in SQL. Made-up names.

// Snowflake IDENTIFIER('...') stands for the name in every name position: the string inside is the
// name (it went out in the clear and IDENTIFIER became a table); IDENTIFIER($var) is a variable.
func TestSQLIdentifierCall(t *testing.T) {
	m := newTest(t)
	for _, c := range []struct {
		s          string
		gone, stay []string
	}{
		{"SELECT * FROM IDENTIFIER('VNDX_DB_X1.SCH_X2.TB_X3')", []string{"VNDX_DB_X1", "SCH_X2", "TB_X3"}, []string{"FROM IDENTIFIER('"}},
		{"INSERT INTO IDENTIFIER('vndx_sch.tb_x3') SELECT 1;", []string{"vndx_sch", "tb_x3"}, []string{"INTO IDENTIFIER('"}},
		{"CREATE TABLE IDENTIFIER('vndx_sch.tb_x4') (id int);", []string{"vndx_sch", "tb_x4"}, []string{"TABLE IDENTIFIER('"}},
		{"USE DATABASE IDENTIFIER('VNDX_DB_X5');", []string{"VNDX_DB_X5"}, []string{"DATABASE IDENTIFIER('"}},
		{"USE IDENTIFIER('VNDX_DB_X6');", []string{"VNDX_DB_X6"}, []string{"USE IDENTIFIER('"}},
		{"use database identifier('VNDX_DB_X7');", []string{"VNDX_DB_X7"}, []string{"database identifier('"}},
		{"SELECT * FROM IDENTIFIER($vndx_tbl)", nil, []string{"IDENTIFIER($vndx_tbl)"}},
	} {
		check(t, m, c.s, c.gone, c.stay)
	}
	if got := typeOf(m, "SELECT * FROM IDENTIFIER('VNDX_DB_X1.SCH_X2.TB_X3')", "TB_X3"); got != "obj.table" {
		t.Errorf("TB_X3: type %q", got)
	}
}

// The words between CREATE/ALTER/DROP [OR REPLACE] and the name are the type of the object, never a
// column (SECRET became a column); a table the position rules know is still a table.
func TestDDLTypeWords(t *testing.T) {
	m := newTest(t)
	check(t, m, "CREATE OR REPLACE SECRET VNDX_PASSWORD_01 TYPE = PASSWORD", []string{"VNDX_PASSWORD_01"}, []string{"CREATE OR REPLACE SECRET "})
	check(t, m, "WARN:sf: CREATE OR REPLACE SECRET VNDX_PASSWORD_02 TYPE = PASSWORD", []string{"VNDX_PASSWORD_02"}, []string{"REPLACE SECRET "})
	check(t, m, "CREATE OR REPLACE NETWORK RULE VNDX_RULE_X1 MODE = EGRESS TYPE = HOST_PORT", []string{"VNDX_RULE_X1"}, []string{"NETWORK RULE "})
	check(t, m, "ALTER TABLE VNDX_TB_X9 ADD COLUMN vndx_col_x1 INT;", []string{"VNDX_TB_X9"}, []string{"ALTER TABLE "})
}

// USE DATABASE x: x is a database (the pair USE DATABASE was taken whole and x became a column).
func TestUseDatabase(t *testing.T) {
	m := newTest(t)
	for _, s := range []string{"USE DATABASE VNDX_DB_X1;", "use database vndx_db_x1;"} {
		if got := typeOf(m, s, "VNDX_DB_X1"); got != "obj.database" {
			t.Errorf("%q: type %q, expected obj.database", s, got)
		}
	}
	if got := typeOf(m, "USE ROLE VNDX_ROLE_X1;", "VNDX_ROLE_X1"); got != "obj.user" {
		t.Errorf("USE ROLE: type %q, expected obj.user", got)
	}
}

// The name before "=>" is a parameter in named notation (CALL p(TABELA => 'x')), a label, not a
// column: TABELA and TAG became columns and the memory carried them through the conversation.
func TestSQLNamedArgument(t *testing.T) {
	m := newTest(t)
	check(t, m, "CALL vndx_db.sch.vndx_proc_x1(TABELA => 'vndx_tb_x2', TAG => 'x', OBJETO => 'y');",
		[]string{"vndx_proc_x1", "vndx_tb_x2"}, []string{"(TABELA => '", "', TAG => 'x', OBJETO => 'y')"})
	check(t, m, "call vndx_db.sch.vndx_proc_x1(tabela => 'vndx_tb_x3', doc => 'z');",
		[]string{"vndx_tb_x3"}, []string{"(tabela => '", "', doc => 'z')"})
}
