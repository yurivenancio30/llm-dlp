package mask

import (
	"strings"
	"testing"
)

// Kubernetes, kubeconfig, Helm, compose, Terraform, Ansible, CloudFormation, ARM, Bicep and CI
// pipelines. Made-up names.

// yamlKeys: the key paths of a YAML (to check that the structure did not change).
func yamlKeys(s string) [][]string {
	w := &yCollector{}
	w.readYAML(s)
	var out [][]string
	for _, e := range w.ents {
		if e.ki >= 0 {
			out = append(out, e.path)
		}
	}
	return out
}

// sameStructure: the same keys at the same paths (a key may have become a pseudonym) and the same
// number of lines.
func sameStructure(t *testing.T, before, after string) {
	t.Helper()
	a, d := yamlKeys(before), yamlKeys(after)
	equal := len(a) == len(d)
	for i := 0; equal && i < len(a); i++ {
		equal = len(a[i]) == len(d[i])
		for j := 0; equal && j < len(a[i]); j++ {
			equal = a[i][j] == d[i][j] || rePseudoObj.MatchString(d[i][j])
		}
	}
	if !equal {
		t.Errorf("a estrutura mudou:\nantes:  %v\ndepois: %v\nsaída:\n%s", a, d, after)
	}
	if strings.Count(before, "\n") != strings.Count(after, "\n") {
		t.Errorf("número de linhas mudou:\n%s", after)
	}
}

const k8sManifest = `apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: svc-pedidos-x9
  namespace: ns-financeiro-demo
  labels:
    app.kubernetes.io/name: app-pedidos-x9
    app.kubernetes.io/managed-by: Helm
spec:
  serviceName: svc-pedidos-hl
  replicas: 3
  template:
    spec:
      serviceAccountName: sa-pedidos-leitor
      imagePullSecrets:
      - name: reg-cred-demo
      containers:
      - name: api
        image: registry.exemplo.interno/equipe-x/app-demo:1.2
        imagePullPolicy: IfNotPresent
        ports:
        - containerPort: 8080
          protocol: TCP
        env:
        - name: DB_HOST
          value: pg-pedidos.ns-financeiro-demo.svc.cluster.local
        - name: DB_CHAVE
          valueFrom:
            secretKeyRef: {name: segredo-pg-demo, key: chave-db}
        envFrom:
        - configMapRef:
            name: cm-pedidos-demo
      - name: proxy
        image: nginx:1.27
      volumes:
      - name: dados
        persistentVolumeClaim:
          claimName: pvc-pedidos-demo
---
apiVersion: v1
kind: Service
metadata:
  name: svc-pedidos-hl
  namespace: ns-financeiro-demo
spec:
  type: ClusterIP
  selector:
    app.kubernetes.io/name: app-pedidos-x9
  ports:
  - port: 80
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: ing-pedidos-demo
spec:
  tls:
  - hosts:
    - pedidos.exemplo.interno
    secretName: tls-pedidos-demo
  rules:
  - host: pedidos.exemplo.interno
    http:
      paths:
      - path: /
        pathType: Prefix
        backend:
          service:
            name: svc-pedidos-hl
            port:
              number: 80
`

func TestK8sYAML(t *testing.T) {
	m := newTest(t)
	out := check(t, m, k8sManifest,
		[]string{"svc-pedidos-x9", "ns-financeiro-demo", "svc-pedidos-hl", "sa-pedidos-leitor", "reg-cred-demo",
			"registry.exemplo.interno", "equipe-x", "app-demo", "segredo-pg-demo", "cm-pedidos-demo", "pvc-pedidos-demo",
			"pg-pedidos", "ing-pedidos-demo", "pedidos.exemplo.interno", "tls-pedidos-demo", "app-pedidos-x9"},
		[]string{"apiVersion: apps/v1", "kind: StatefulSet", "kind: Service", "kind: Ingress", "apiVersion: networking.k8s.io/v1",
			"name: api", "image: nginx:1.27", "IfNotPresent", "ClusterIP", "Prefix", "protocol: TCP", "app.kubernetes.io/managed-by: Helm",
			"key: chave-db", "name: DB_HOST", ":1.2\n", ".svc.cluster.local", "name: dados", "containerPort: 8080", "replicas: 3"})
	sameStructure(t, k8sManifest, out)
	for real, typ := range map[string]string{"svc-pedidos-x9": "obj.servico", "ns-financeiro-demo": "obj.namespace",
		"sa-pedidos-leitor": "obj.usuario", "pvc-pedidos-demo": "obj.servico"} {
		if got := typeOf(m, k8sManifest, real); got != typ {
			t.Errorf("%s: tipo %q, esperado %q", real, got, typ)
		}
	}
	if got := typeOf(m, "apiVersion: v1\nkind: Namespace\nmetadata:\n  name: ns-contabil-demo\n", "ns-contabil-demo"); got != "obj.namespace" {
		t.Errorf("Namespace: tipo %q", got)
	}
}

func TestK8sJSON(t *testing.T) {
	m := newTest(t)
	s := `{"apiVersion": "v1", "kind": "List", "items": [{"apiVersion": "v1", "kind": "ConfigMap", "metadata": {"name": "cm-relatorios-x2", "namespace": "ns-financeiro-demo"}, "data": {"modo": "batch"}},
{"apiVersion": "apps/v1", "kind": "Deployment", "metadata": {"name": "svc-relatorios-x2"}, "spec": {"template": {"spec": {"containers": [{"name": "app", "image": "registry.exemplo.interno:5000/rel-demo@sha256:abc123", "imagePullPolicy": "Always"}]}}}}]}`
	check(t, m, s, []string{"cm-relatorios-x2", "ns-financeiro-demo", "svc-relatorios-x2", "registry.exemplo.interno", "rel-demo"},
		[]string{`"kind": "List"`, `"kind": "ConfigMap"`, `"kind": "Deployment"`, `"apiVersion": "apps/v1"`, `"Always"`, `"name": "app"`, ":5000/", "@sha256:abc123", `"modo": "batch"`})
}

func TestK8sPublicNotMasked(t *testing.T) {
	m := newTest(t)
	// example manifest from the documentation: only public vocabulary and images
	s := `apiVersion: apps/v1
kind: Deployment
metadata:
  name: nginx
  namespace: default
spec:
  selector:
    matchLabels:
      app: nginx
  template:
    spec:
      serviceAccountName: default
      containers:
      - name: nginx
        image: registry.k8s.io/nginx-slim:0.8
        imagePullPolicy: Always
      - name: side
        image: ghcr.io/prometheus/prometheus:2.0
      restartPolicy: Always
`
	out, _ := m.Mask(s)
	for _, v := range []string{"apiVersion: apps/v1", "kind: Deployment", "namespace: default", "serviceAccountName: default",
		"image: registry.k8s.io/nginx-slim:0.8", "image: ghcr.io/prometheus/prometheus:2.0", "imagePullPolicy: Always", "restartPolicy: Always", "app: nginx"} {
		if !strings.Contains(out, v) {
			t.Errorf("%q deveria ficar:\n%s", v, out)
		}
	}
	// "nginx" in metadata.name is masked in place, but a plain word does not propagate
	if o, _ := m.Mask("instale o nginx no servidor"); !strings.Contains(o, "nginx") {
		t.Errorf("palavra simples propagou: %q", o)
	}
	// without apiVersion/kind it is not a manifest
	for _, s := range []string{"metadata:\n  name: algo-x9\n", "kind: Pessoa\nnome: algo-x9\n", "apiVersion: str\nkind: str\nmetadata:\n  name: algo-x9\n"} {
		if o, _ := m.Mask(s); !strings.Contains(o, "algo-x9") {
			t.Errorf("não é manifesto, mas mascarou: %q", o)
		}
	}
}

func TestK8sHelmTemplateUntouched(t *testing.T) {
	m := newTest(t)
	s := "apiVersion: v1\nkind: Service\nmetadata:\n  name: {{ include \"chart.fullname\" . }}\n  namespace: {{ .Release.Namespace }}\nspec:\n  type: {{ .Values.service.type }}\n"
	out, _ := m.Mask(s)
	if out != s {
		t.Errorf("template mudou:\n%s", out)
	}
}

func TestKubeconfig(t *testing.T) {
	m := newTest(t)
	s := `apiVersion: v1
kind: Config
clusters:
- name: cl-financeiro-demo
  cluster:
    server: https://api.cl-demo.exemplo.interno:6443
    certificate-authority-data: AAAA
contexts:
- name: ctx-financeiro-demo
  context:
    cluster: cl-financeiro-demo
    user: usr-deploy-demo
    namespace: ns-financeiro-demo
- name: arn:aws:eks:us-east-1:123456789012:cluster/eks-demo-x1
  context:
    cluster: arn:aws:eks:us-east-1:123456789012:cluster/eks-demo-x1
users:
- name: usr-deploy-demo
  user:
    token: abc
current-context: ctx-financeiro-demo
`
	out := check(t, m, s, []string{"cl-financeiro-demo", "api.cl-demo.exemplo.interno", "ctx-financeiro-demo", "usr-deploy-demo", "ns-financeiro-demo", "eks-demo-x1", "123456789012"},
		[]string{"kind: Config", "https://", ":6443", "arn:aws:eks:us-east-1:", ":cluster/", "current-context: "})
	sameStructure(t, s, out)
	if got := typeOf(m, s, "usr-deploy-demo"); got != "obj.usuario" {
		t.Errorf("users[].name: tipo %q", got)
	}
}

func TestServiceDNS(t *testing.T) {
	m := newTest(t)
	check(t, m, "conecte em pg-pedidos.ns-contabil-x3.svc.cluster.local:5432 e kubernetes.default.svc.cluster.local",
		[]string{"pg-pedidos", "ns-contabil-x3"}, []string{".svc.cluster.local:5432", "kubernetes.default.svc.cluster.local"})
	if got := typeOf(m, "x: pg-pedidos.ns-contabil-x3.svc.cluster.local", "ns-contabil-x3"); got != "obj.namespace" {
		t.Errorf("tipo %q", got)
	}
}

func TestHelmChart(t *testing.T) {
	m := newTest(t)
	s := "apiVersion: v2\nname: chart-pedidos-x9\ndescription: Um chart de exemplo\ntype: application\nversion: 0.1.0\nappVersion: \"1.16.0\"\n"
	out := check(t, m, s, []string{"chart-pedidos-x9"}, []string{"apiVersion: v2", "type: application", "version: 0.1.0", "appVersion: \"1.16.0\""})
	sameStructure(t, s, out)
	// values.yaml (without apiVersion) is not for this reader
	if o, _ := m.Mask("replicaCount: 1\nimage:\n  repository: nginx\n"); !strings.Contains(o, "repository: nginx") {
		t.Errorf("values.yaml mexido: %q", o)
	}
}

func TestCompose(t *testing.T) {
	m := newTest(t)
	s := `services:
  svc-pedidos-x9:
    image: registry.exemplo.interno/app-demo:1.2
    container_name: ctr-pedidos-x9
    hostname: srv-exemplo-01
    ports:
      - "8080:80"
    depends_on:
      - db-demo-x1
    networks:
      - rede-interna-x1
  db-demo-x1:
    image: postgres:16
    environment:
      POSTGRES_DB: base
    volumes:
      - dados-pg-x1:/var/lib/postgresql/data
networks:
  rede-interna-x1:
volumes:
  dados-pg-x1:
`
	out := check(t, m, s, []string{"svc-pedidos-x9", "registry.exemplo.interno", "app-demo", "ctr-pedidos-x9", "srv-exemplo-01", "db-demo-x1", "rede-interna-x1", "dados-pg-x1"},
		[]string{"services:\n", "image: postgres:16", ":1.2\n", `"8080:80"`, "POSTGRES_DB: ", "networks:\n", "volumes:\n", "depends_on:"})
	sameStructure(t, s, out)
	// any YAML with "services:" that does not look like compose
	if o, _ := m.Mask("services:\n  pagamento-x1:\n    descricao: algo\n"); !strings.Contains(o, "pagamento-x1") {
		t.Errorf("não é compose, mas mascarou: %q", o)
	}
}

func TestTerraform(t *testing.T) {
	m := newTest(t)
	s := `provider "google" {
  project = "proj-relatorios-demo"
  region  = "southamerica-east1"
}

resource "aws_s3_bucket" "relatorios" {
  bucket = "bkt-relatorios-demo"
  tags = {
    Name = "bkt-relatorios-demo"
    Ambiente = "dev"
  }
}

resource "aws_db_instance" "principal" {
  identifier     = "db-exemplo-01"
  db_name        = "vendas_demo"
  engine         = "postgres"
  instance_class = "db.t3.micro"
  vpc_security_group_ids = [aws_security_group.db.id]
  availability_zone = "us-east-1a"
}

data "aws_caller_identity" "atual" {}

resource "aws_sqs_queue" "fila" {
  name = "${var.prefixo}-fila-pedidos-x9"
}

resource "azurerm_mssql_server" "sql" {
  name = "sqlsrv-exemplo-01"
  location = "eastus2"
  sku {
    name = "Standard_LRS"
  }
}
`
	out := check(t, m, s, []string{"proj-relatorios-demo", "bkt-relatorios-demo", "db-exemplo-01", "vendas_demo", "fila-pedidos-x9", "sqlsrv-exemplo-01"},
		[]string{`resource "aws_s3_bucket" "relatorios"`, `resource "aws_db_instance" "principal"`, `region  = "southamerica-east1"`, `engine         = "postgres"`,
			`"db.t3.micro"`, "aws_security_group.db.id", `data "aws_caller_identity" "atual"`, "${var.prefixo}-", `"Standard_LRS"`, `"eastus2"`, `"us-east-1a"`, `Ambiente = "dev"`})
	if strings.Count(out, "\n") != strings.Count(s, "\n") {
		t.Errorf("linhas mudaram")
	}
	for real, typ := range map[string]string{"bkt-relatorios-demo": "obj.bucket", "db-exemplo-01": "obj.database", "vendas_demo": "obj.database",
		"sqlsrv-exemplo-01": "obj.servidor", "proj-relatorios-demo": "obj.conta_nuvem"} {
		if got := typeOf(m, s, real); got != typ {
			t.Errorf("%s: tipo %q, esperado %q", real, got, typ)
		}
	}
	if got := typeOf(m, "resource \"aws_iam_role\" \"x\" {\n  account_id = \"123456789012\"\n}\n", "123456789012"); got != "obj.conta_nuvem" {
		t.Errorf("account_id: tipo %q", got)
	}
}

func TestTerraformPlan(t *testing.T) {
	m := newTest(t)
	s := `Terraform will perform the following actions:

  # aws_db_instance.principal will be updated in-place
  ~ resource "aws_db_instance" "principal" {
      ~ identifier = "db-exemplo-01" -> "db-exemplo-02"
      + address    = (known after apply)
        id         = "db-xyz"
    }

  # aws_s3_bucket.novo will be created
  + resource "aws_s3_bucket" "novo" {
      + bucket = "bkt-arquivos-x7"
      + arn    = (known after apply)
    }

Plan: 1 to add, 1 to change, 0 to destroy.
`
	check(t, m, s, []string{"db-exemplo-01", "db-exemplo-02", "bkt-arquivos-x7"},
		[]string{"# aws_db_instance.principal will be updated in-place", `~ resource "aws_db_instance" "principal"`, " -> ", "(known after apply)", `"db-xyz"`, "Plan: 1 to add"})
}

func TestAnsibleINI(t *testing.T) {
	m := newTest(t)
	s := "[dbservers]\nsrv-exemplo-01 ansible_host=srv-exemplo-01.exemplo.interno ansible_user=usr-ansible-x1\nsrv-exemplo-02\n\n[web:children]\ndbservers\n\n[all:vars]\nansible_user=usr-deploy-x2\nansible_port=2222\n"
	out := check(t, m, s, []string{"srv-exemplo-01", "srv-exemplo-02", "usr-ansible-x1", "usr-deploy-x2"},
		[]string{"[dbservers]", "ansible_host=", "ansible_user=", "[web:children]", "dbservers", "[all:vars]", "ansible_port=2222"})
	if strings.Count(out, "\n") != strings.Count(s, "\n") {
		t.Errorf("linhas mudaram: %q", out)
	}
	// common INI (my.cnf, setup.cfg, tox.ini) is not an inventory
	for _, s := range []string{"[mysqld]\nskip-name-resolve\nport=3306\n", "[tox]\nenvlist = py310\n[testenv]\ndeps =\n    pytest-x1\n", "[package]\nname = \"pacote-x1\"\n"} {
		if o, _ := m.Mask(s); o != s {
			t.Errorf("INI comum mexido:\n%q\n-> %q", s, o)
		}
	}
}

func TestAnsibleYAML(t *testing.T) {
	m := newTest(t)
	inv := "all:\n  hosts:\n    srv-exemplo-01:\n      ansible_host: 10.0.0.5\n  children:\n    web:\n      hosts:\n        srv-exemplo-02:\n          ansible_user: usr-deploy-x2\n"
	check(t, m, inv, []string{"srv-exemplo-01", "srv-exemplo-02", "usr-deploy-x2"}, []string{"all:\n", "children:", "web:", "ansible_host: "})
	play := "- hosts: grupo-web-x1\n  become: true\n  tasks:\n    - name: instalar pacote\n      ansible.builtin.apt:\n        name: nginx\n        state: present\n      delegate_to: srv-exemplo-03\n"
	check(t, m, play, []string{"grupo-web-x1", "srv-exemplo-03"}, []string{"ansible.builtin.apt:", "name: nginx", "state: present", "become: true", "name: instalar pacote"})
	if o, _ := m.Mask("- hosts: all\n  tasks: []\n"); !strings.Contains(o, "hosts: all") {
		t.Errorf("hosts: all mexido: %q", o)
	}
}

func TestCloudFormation(t *testing.T) {
	m := newTest(t)
	s := `AWSTemplateFormatVersion: "2010-09-09"
Resources:
  BancoRelatorios:
    Type: AWS::RDS::DBInstance
    Properties:
      DBInstanceIdentifier: db-exemplo-01
      Engine: postgres
      DBInstanceClass: !Ref Classe
  Arquivos:
    Type: AWS::S3::Bucket
    Properties:
      BucketName: !Sub 'bkt-relatorios-demo-${AWS::Region}'
  Fila:
    Type: AWS::SQS::Queue
    Properties:
      QueueName: !Ref NomeFila
  Tabela:
    Type: AWS::DynamoDB::Table
    Properties:
      TableName: tb-pedidos-x9
`
	out := check(t, m, s, []string{"db-exemplo-01", "bkt-relatorios-demo", "tb-pedidos-x9"},
		[]string{"BancoRelatorios:", "Type: AWS::RDS::DBInstance", "Engine: postgres", "!Ref Classe", "!Sub '", "-${AWS::Region}'", "QueueName: !Ref NomeFila", "AWSTemplateFormatVersion"})
	sameStructure(t, s, out)
	j := `{"Resources": {"Fila": {"Type": "AWS::SQS::Queue", "Properties": {"QueueName": "fila-pedidos-x9", "VisibilityTimeout": 30}}}}`
	check(t, m, j, []string{"fila-pedidos-x9"}, []string{`"Type": "AWS::SQS::Queue"`, `"VisibilityTimeout": 30`})
}

func TestARMAndBicep(t *testing.T) {
	m := newTest(t)
	arm := `{"$schema": "https://schema.management.azure.com/schemas/2019-04-01/deploymentTemplate.json#", "contentVersion": "1.0.0.0",
 "resources": [
  {"type": "Microsoft.Sql/servers", "apiVersion": "2023-08-01", "name": "sqlsrv-exemplo-01", "location": "[resourceGroup().location]"},
  {"type": "Microsoft.Sql/servers/databases", "apiVersion": "2023-08-01", "name": "sqlsrv-exemplo-01/db-vendas-x1"},
  {"type": "Microsoft.Storage/storageAccounts", "apiVersion": "2023-01-01", "name": "[parameters('nomeConta')]"}
 ]}`
	check(t, m, arm, []string{"sqlsrv-exemplo-01", "db-vendas-x1"}, []string{`"Microsoft.Sql/servers"`, "[resourceGroup().location]", "[parameters('nomeConta')]", `"apiVersion": "2023-08-01"`})
	if got := typeOf(m, arm, "db-vendas-x1"); got != "obj.database" {
		t.Errorf("ARM filho: tipo %q", got)
	}
	bicep := "param location string = resourceGroup().location\nresource sql 'Microsoft.Sql/servers@2023-08-01' = {\n  name: 'sqlsrv-exemplo-02'\n  location: location\n  properties: {\n    name: 'interno'\n  }\n}\n"
	check(t, m, bicep, []string{"sqlsrv-exemplo-02"}, []string{"resource sql 'Microsoft.Sql/servers@2023-08-01'", "location: location", "name: 'interno'"})
}

func TestPipelinesCI(t *testing.T) {
	m := newTest(t)
	gha := `name: ci
on: [push]
jobs:
  build:
    runs-on: [self-hosted, runner-financeiro-x1]
    container: registry.exemplo.interno/ci/builder-x1:3
    environment:
      name: amb-pedidos-x9
    services:
      banco:
        image: postgres:16
    steps:
      - uses: actions/checkout@v4
  outro:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
`
	check(t, m, gha, []string{"runner-financeiro-x1", "registry.exemplo.interno", "builder-x1", "amb-pedidos-x9"},
		[]string{"self-hosted", "runs-on: ubuntu-latest", "image: postgres:16", "actions/checkout@v4", ":3\n", "on: [push]"})
	gl := "stages: [build]\nbuild:\n  image: registry.exemplo.interno/ci/node-x2:20\n  services:\n    - name: registry.exemplo.interno/ci/pg-x2:16\n  tags:\n    - runner-gl-x3\n  environment: amb-homolog-x4\n  script:\n    - make\n"
	check(t, m, gl, []string{"registry.exemplo.interno", "node-x2", "pg-x2", "runner-gl-x3", "amb-homolog-x4"}, []string{"stages: [build]", "script:", "- make", ":20\n", ":16\n"})
	az := "trigger:\n  - main\npool:\n  name: pool-financeiro-x5\nsteps:\n  - script: echo ok\n"
	check(t, m, az, []string{"pool-financeiro-x5"}, []string{"trigger:", "- main", "script: echo ok"})
	az2 := "pool:\n  vmImage: ubuntu-latest\nsteps:\n  - script: echo ok\n"
	if o, _ := m.Mask(az2); o != az2 {
		t.Errorf("rótulo público mexido: %q", o)
	}
	jk := "pipeline {\n  agent { label 'agente-build-x6' }\n  stages {\n    stage('b') {\n      agent { docker { image 'registry.exemplo.interno/ci/mvn-x7:3' } }\n      steps { sh 'mvn package' }\n    }\n  }\n}\n"
	check(t, m, jk, []string{"agente-build-x6", "registry.exemplo.interno", "mvn-x7"}, []string{"pipeline {", "stage('b')", "sh 'mvn package'", ":3'"})
	// Ansible task tags are not runners
	ans := "- hosts: all\n  tasks:\n    - name: x\n      script: run.sh\n      tags:\n        - configuracao-x1\n"
	if o, _ := m.Mask(ans); !strings.Contains(o, "configuracao-x1") {
		t.Errorf("tag de tarefa mascarada: %q", o)
	}
}

// A name learned in a strong position of one format is masked in prose and in another format.
func TestDevopsLearnedInOtherFormat(t *testing.T) {
	m := newTest(t)
	m.Mask("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: cm-x\n  namespace: ns-tesouraria-x8\n")
	m.Mask("resource \"aws_s3_bucket\" \"b\" {\n  bucket = \"bkt-extratos-x8\"\n}\n")
	for _, s := range []string{
		"o pod caiu no namespace ns-tesouraria-x8 de novo",
		"kubectl -n ns-tesouraria-x8 get pods",
		"services:\n  app:\n    environment:\n      NAMESPACE: ns-tesouraria-x8\n",
		"aws s3 ls s3://bkt-extratos-x8/2026/",
		"Server=srv-x;Database=bkt-extratos-x8;",
	} {
		out, _ := m.Mask(s)
		if l := strings.ToLower(out); strings.Contains(l, "ns-tesouraria-x8") || strings.Contains(l, "bkt-extratos-x8") {
			t.Errorf("não propagou:\n   %q\n-> %q", s, out)
		}
	}
	// weak evidence (just a label) does not teach
	m2 := newTest(t)
	m2.Mask("name: ci\njobs:\n  b:\n    runs-on: runner-fraco-x1\n    steps: []\n")
	if out, _ := m2.Mask("o runner-fraco-x1 está fora"); !strings.Contains(out, "runner-fraco-x1") {
		t.Errorf("evidência fraca ensinou: %q", out)
	}
}

// Common text, Go and Python code are not affected by the DevOps readers.
func TestDevopsSkipsCodeAndProse(t *testing.T) {
	ls := []Reader{}
	for _, l := range defaultReaders() {
		switch l.Name {
		case "devops-yaml", "terraform", "ansible-ini", "bicep", "jenkins", "k8s-dns":
			ls = append(ls, l)
		}
	}
	for _, s := range []string{
		"func (s *Server) Run(ctx context.Context) error {\n\tname := \"servidor-x1\"\n\tkind := \"Pod\"\n\tapiVersion := \"v1\"\n\treturn nil\n}\n",
		"class Recurso:\n    apiVersion: str = \"v1\"\n    kind: str = \"Pod\"\n    def __init__(self):\n        self.name = \"svc-x1\"\n",
		"config = {\n    \"host\": \"srv-x1\",\n    \"services\": [\"a\", \"b\"],\n}\nresource = \"tabela_x1\"\ndata = \"valor\"\n",
		"O deploy usa Kubernetes: defina apiVersion e kind no manifesto e rode kubectl apply.",
		"[1] Referência\nsrv-texto-x1 é citado no texto.\n",
		"def f(node):\n    stage = node('x')\n    return stage\n",
		"Os hosts: srv-a e srv-b; script: rodar à noite.",
	} {
		for _, l := range ls {
			l.Find(s, func(o ObjFinding) {
				t.Errorf("%s pegou %q (%s) em:\n%s", l.Name, s[o.Start:o.End], o.Rule, s)
			})
		}
	}
}

// Env in a list follows the same rule as "NAME: value"; the rest of the manifest stays.
func TestK8sEnvList(t *testing.T) {
	m := newTest(t)
	s := "apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: app\nspec:\n  replicas: 3\n  template:\n    spec:\n      containers:\n        - name: app\n          env:\n            - name: DB_HOST\n              value: pgprd01\n            - name: QUEUE_NAME\n              value: \"fila-x1\"\n            - name: LOG_LEVEL\n              value: debug\n          ports:\n            - containerPort: 8080\n          resources:\n            limits:\n              cpu: 500m\n          volumeMounts:\n            - name: dados\n              mountPath: /app/dados\n      tier: backend\n"
	check(t, m, s, []string{"pgprd01", "fila-x1"}, []string{"kind: Deployment", "replicas: 3", "value: debug", "containerPort: 8080", "cpu: 500m", "mountPath: /app/dados", "tier: backend"})
}
