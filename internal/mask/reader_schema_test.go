package mask

import "testing"

// Name + data type. The column names must come out masked in every shape.

func TestSchemaNameType(t *testing.T) {
	cols := []string{"cd_conta", "vl_lancto", "dt_lancto"}
	cases := []struct{ name, text string }{
		{"dtypes", "cd_conta      int64\nvl_lancto   float64\ndt_lancto    datetime64[ns]\ndtype: object\n"},
		{"columns", "Index(['cd_conta', 'vl_lancto', 'dt_lancto'], dtype='object')\n"},
		{"info", `<class 'pandas.core.frame.DataFrame'>
RangeIndex: 3 entries, 0 to 2
Data columns (total 3 columns):
 #   Column     Non-Null Count  Dtype
---  ------     --------------  -----
 0   cd_conta   3 non-null      int64
 1   vl_lancto  3 non-null      float64
 2   dt_lancto  3 non-null      datetime64[ns]
dtypes: datetime64[ns](1), float64(1), int64(1)
`},
		{"printSchema", "root\n |-- cd_conta: integer (nullable = true)\n |-- vl_lancto: double (nullable = true)\n |-- dt_lancto: timestamp (nullable = true)\n"},
		{"pyarrow", "pyarrow.Table\ncd_conta: int64\nvl_lancto: double\ndt_lancto: timestamp[us]\n"},
		{"avro", `{"type": "record", "name": "Lancamento", "fields": [
  {"name": "cd_conta", "type": "long"},
  {"name": "vl_lancto", "type": "double"},
  {"name": "dt_lancto", "type": {"type": "long", "logicalType": "timestamp-millis"}}
]}`},
		{"dbt schema.yml", `version: 2
models:
  - name: lanc_diario
    columns:
      - name: cd_conta
        tests: [not_null]
      - name: vl_lancto
      - name: dt_lancto
`},
		{"protobuf", "message Lancamento {\n  int64 cd_conta = 1;\n  double vl_lancto = 2;\n  google.protobuf.Timestamp dt_lancto = 3;\n}\n"},
		{"name | type", "| name      | type          |\n|-----------|---------------|\n| cd_conta  | bigint        |\n| vl_lancto | numeric(12,2) |\n| dt_lancto | timestamp     |\n"},
	}
	for _, c := range cases {
		for _, tr := range transports {
			m := newTest(t)
			txt := tr.f(c.text)
			names := cols
			if c.name == "protobuf" {
				names = cols[:2] // the message type (Timestamp) is not a data type of the vocabulary
			}
			if c.name == "dbt schema.yml" {
				names = append(names, "lanc_diario")
			}
			if ok, tot := checkMasked(t, m, c.name+"/"+tr.name, txt, names); ok != tot {
				out, _ := m.Mask(txt)
				t.Errorf("%s / %s: %d/%d\n%s", c.name, tr.name, ok, tot, out)
			}
		}
	}
}

// negatives: type annotations in code and common configuration do not become a column
func TestSchemaNegatives(t *testing.T) {
	m := newTest(t)
	for _, s := range []string{
		"class Config:\n    host_padrao: str\n    porta_padrao: int\n",
		"type: string\nformat: date\n",
		"func f(nome_x string, idade_x int) {}\n",
		"requests   2.31.0\nurllib3    2.2.1\n",
		"Rodei: string\nde novo: hoje\n",
	} {
		if out, _ := m.Mask(s); out != s {
			t.Errorf("not a schema: %q -> %q", s, out)
		}
	}
}
