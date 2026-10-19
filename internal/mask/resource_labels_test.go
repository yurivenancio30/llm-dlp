package mask

import "testing"

// A name that is a single plain word (no "_", no digit) is only masked where a structure says it
// is a resource. Here the structures that did not say it yet: a resource label written by a person
// (Host: NAME, with the list indented under DataBase:), a path in the home folder with dots in a
// name, the endpoint of an EKS cluster and the private IP inside an EC2 host name. Each case has
// the other side: the public word next to it stays.

func TestResourceLabelsWrittenByAPerson(t *testing.T) {
	inv := "Tipo: SQL Server\nHost: SRVSQLHX\nIP: 10.20.30.40\nPorta: 1433\nUsuário: svc_carga_hx\n" +
		"Secret: SQLSERVER_DW_PASSWORD\nDataBase: \n    DWCOMERCIALHX\n    ESTOQUEHX\n    financeirohx\n\n" +
		"Tipo: Postgres\nHost: PGPRODHX / PGREPLHX\nPorta: 5432\nDataBase:\n    vendashx\n    logisticahx\n"
	checkShapes(t, []shapeCase{
		{"connection inventory", inv,
			[]string{"SRVSQLHX", "PGPRODHX", "PGREPLHX", "DWCOMERCIALHX", "ESTOQUEHX", "financeirohx", "vendashx", "logisticahx"},
			[]string{"SQL", "Server", "Postgres", "Porta", "DataBase"}},
		{"public values", "Host: localhost\nDatabase: master\nServidor: produção\nBanco de dados: postgres\n",
			nil, []string{"localhost", "master", "produção", "postgres"}},
		{"label in a sentence", "O host: foi trocado ontem. Database: veja abaixo.\n",
			nil, []string{"foi", "veja"}},
	})
	for _, c := range []struct{ s, v, ent string }{
		{inv, "SRVSQLHX", "server"},
		{inv, "DWCOMERCIALHX", "database"},
		{inv, "vendashx", "database"},
	} {
		if e := entOf(t, c.s, c.v); e != c.ent {
			t.Errorf("%s masked as %q, want %s", c.v, e, c.ent)
		}
	}
}

// DataHub recipes (docs.datahub.com, AllowDenyPattern): the allow/deny lists of <type>_pattern are
// regular expressions of names of that type; an anchored literal ("^DWVENDAS$", "^DW\.dbo\..*") is
// the name. And the name of a tag in its URN (urn:li:tag:<name>).
func TestRecipePatternsAndTags(t *testing.T) {
	recipe := "source:\n  type: mssql\n  config:\n    database_pattern:\n      allow:\n        - \"^DWVENDASHX$\"\n" +
		"        - \"^ESTOQUEHX$\"\n      deny:\n        - \"^master$\"\n    table_pattern:\n      allow:\n" +
		"        - \"^DWVENDASHX\\\\.dbo\\\\.tbpedidohx$\"\n        - '^FINANCEIROHX\\.dbo\\..*'\n"
	checkShapes(t, []shapeCase{
		{"YAML recipe", recipe, []string{"DWVENDASHX", "ESTOQUEHX", "tbpedidohx", "FINANCEIROHX"},
			[]string{"master", "dbo", "allow", "deny", "mssql"}},
		{"JSON recipe", `{"schema_pattern": {"allow": ["^vendashx$", "^logisticahx$"], "ignoreCase": true}}`,
			[]string{"vendashx", "logisticahx"}, []string{"allow", "ignoreCase"}},
		{"YAML items without quotes", "    database_pattern:\n      allow:\n        - ^DWVENDASHX$\n        - ^ESTOQUEHX$\n",
			[]string{"DWVENDASHX", "ESTOQUEHX"}, nil},
		{"escaped in a command", `{"command": "cat > r.yml <<EOF\n    database_pattern:\n      allow:\n        - \"^DWVENDASHX$\"\nEOF"}`,
			[]string{"DWVENDASHX"}, nil},
		// a regex that is not a literal, and a list that is not under a pattern key
		{"not a name", "database_pattern:\n  allow:\n    - \"^(dev|test)_.*\"\n    - \".*\"\nfiles:\n  - \"^README$\"\n",
			nil, []string{"dev", "test", "README"}},
		{"tag URN", `"tags": ["urn:li:tag:DWVENDASHX", "urn:li:tag:Legacy", "urn:li:tag:dados_pessoais", "urn:li:tag:TierGold"]`,
			[]string{"DWVENDASHX"}, []string{"Legacy", "dados_pessoais", "TierGold"}},
	})
	// the Snowflake tag (urn:li:tag:<database>.<schema>.<tag>:<value>) is a qualified name: the tag
	// reader leaves it to the readers of qualified names
	snow := `"urn:li:tag:dwvendashx.governanca.pii_cpf:sim"`
	if e := entOf(t, snow, "dwvendashx"); e == "service" {
		t.Errorf("the database of a Snowflake tag masked as a tag")
	}
	if e := entOf(t, recipe, "ESTOQUEHX"); e != "database" {
		t.Errorf("ESTOQUEHX masked as %q, want database", e)
	}
}

func TestHomePathWithDotsIsAFolder(t *testing.T) {
	checkShapes(t, []shapeCase{
		{"cd into it", "cd ~/HX.AWS.PRD.Cargahx   # pasta do projeto\ngit status\n",
			[]string{"HX.AWS.PRD.Cargahx", "Cargahx"}, []string{"status"}},
		{"file under it", "ls ~/HX.AWS.PRD.Cargahx/terraform/main.tf",
			[]string{"Cargahx"}, []string{"terraform", "main.tf"}},
		{"public dot folder", "cat ~/.aws/config ~/.kube/config ~/.ssh/id_rsa.pub",
			nil, []string{".aws", "config", ".kube", ".ssh", "id_rsa.pub"}},
		// still a file: the context line of grep -C, a language suffix, a number at the end
		{"file shapes", "~/docs/R01_texto_base.md-Este arquivo\n~/docs/guia_uso.pt-BR\n~/fotos/35.\n",
			nil, []string{"R01_texto_base.md", "guia_uso.pt", "35"}},
		{"full stop after the folder", "o projeto fica em ~/trabalho/carga_hx.", []string{"carga_hx"}, nil},
	})
}

func TestEKSEndpointAndEC2HostName(t *testing.T) {
	checkShapes(t, []shapeCase{
		{"EKS endpoint", "server: https://0A1B2C3D4E5F60718293A4B5C6D7E8F9.gr7.us-east-2.eks.amazonaws.com",
			[]string{"0A1B2C3D4E5F60718293A4B5C6D7E8F9"}, []string{"us-east-2", "eks.amazonaws.com"}},
		// the example of aws eks describe-cluster (docs.aws.amazon.com/cli/latest/reference/eks/describe-cluster.html)
		{"EKS endpoint, AWS example", `"endpoint": "https://JSA79429HJDASKJDJ8223829MNDNASW.yl4.us-east-2.eks.amazonaws.com"`,
			[]string{"JSA79429HJDASKJDJ8223829MNDNASW"}, []string{"yl4", "eks.amazonaws.com"}},
		{"EC2 private DNS name", "node ip-10-20-30-40.us-east-2.compute.internal is NotReady; ip-172-31-5-9.ec2.internal too",
			[]string{"10-20-30-40", "172-31-5-9"}, []string{"NotReady"}},
		// with a prefix and a suffix, as in the name of a node claim or of a pod (prefix-ip-…-suffix)
		{"inside a longer name", "Created pod-cargahx-ip-10-120-4-200.us-east-2.compute.internal-x7k2q on node",
			[]string{"10-120-4-200"}, []string{"Created", "node"}},
		{"public endpoint name", "the API server endpoint ends in eks.amazonaws.com; nodes are named ip-<a>-<b>-<c>-<d>",
			nil, []string{"eks.amazonaws.com"}},
	})
}
