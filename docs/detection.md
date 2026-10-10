# What is detected

[← back to the README](../README.md) · [Português](pt-BR/deteccao.md)

There are five ways to recognize a piece of data. Any one of them is enough.

llm-dlp was built for Brazilian data, so many examples below are Brazilian documents (CPF,
CNPJ, RG, CNH, PIS, CEP) and Portuguese field names and keywords. English field names and
keywords are recognized as well.

## 1. By format (always, anywhere)

| Type | Example |
|---|---|
| Tokens and API keys | `ghp_k3Jd9s…`, also inside base64 ([gitleaks](https://github.com/gitleaks/gitleaks) rules) |
| Passwords | `DB_PASSWORD=…`, `senha: …`, "a senha é …", `mysql://usuario:senha@host`, `mysql -pSENHA` |
| Credential in an HTTP header | `Authorization: Basic …`, `Authorization: Bearer …`, `Cookie: sessionid=…` |
| E-mail | `joao.silva@empresa.com.br` (except the ones you allow) |
| CPF and CNPJ with punctuation | `529.982.247-25`, `11.222.333/0001-81`, `12.ABC.345/01DE-35` (new CNPJ, with letters) |
| Card | `4111 1111 1111 1111` |
| Phone | `(11) 98765-4321`, `+55 11 98765-4321` |
| Address | `Rua das Flores, 123` |
| Internal network IP | `10.42.7.15`, `192.168.0.10` |
| Internal hostname | `mysql.empresa.intra` (the ones containing one of your `dominios_internos`) |
| International account (IBAN) | `GB82 WEST 1234 5698 7654 32` |
| Court case number | `0001234-56.2023.8.13.0024` |
| MAC address | `aa:bb:cc:dd:ee:ff` |

## 2. With the word nearby

On their own these are just numbers, and would be confused with IDs and dates.

| Type | Example |
|---|---|
| CPF/CNPJ without punctuation | `cpf 52998224725` |
| RG, CNH, PIS | `RG: 12.345.678-9`, `CNH 12345678900` |
| CEP (postal code) | `CEP 01310-100` |
| Bank branch and account | `agência 1234-5 conta 123456-7` |
| Random PIX key | `pix 123e4567-e89b-…` |
| Date of birth | `nascimento: 12/03/1985` |
| Voter ID, SUS card, RENAVAM | `título de eleitor 1234 5678 9012` |
| Passport, licence plate, chassis, IMEI | `passaporte FZ123456`, `placa BRA2E19` |

CPF, CNPJ, PIS, CNH, card, voter ID, SUS card, RENAVAM, IBAN and IMEI only count if the
check digit is valid.

## 3. By the field name

A lot of personal data has no format (a name, a user code, a CPF without punctuation in a
column), but comes with the field name next to it. The field name decides what to do with
the value.

| Field named after... | Example name | The value becomes |
|---|---|---|
| A person's name | `NOM_CLIENTE`, `nome_mae`, `full_name`, `Data Owner`, `Cliente` | `Pessoa …` |
| Code, login, employee ID | `COD_USU_OWNER`, `matricula`, `login`, `user_id`, `id_cliente` | `USUARIO-…` |
| Document | `CPF`, `NUM_CNPJ`, `rg`, `cnh`, `titulo_eleitor`, `passaporte`, `placa` | `CPF-…`, `DOC-…` |
| Contact and address | `telefone`, `celular`, `endereco`, `logradouro`, `cep` | `TELEFONE-…`, `ENDERECO-…` |
| Birth | `DT_NASCIMENTO`, `birth_date` | `NASCIMENTO-…` |
| Account and card | `account_number`, `conta_corrente`, `credit_card_number`, `iban` | `CONTA-…`, `CARTAO-…` |
| Sensitive personal data (LGPD) | `religiao`, `raca_cor`, `orientacao_sexual`, `tipo_sanguineo`, `deficiencia`, `sindicato`, `doenca` | `SENSIVEL-…` |

The value is not the prefix of a Python literal (`r'...'`, `b"..."`, `f'...'`, `u'...'`: what
counts is what is between the quotes), nor 1 or 2 loose letters, nor a regex pattern
(`(?!...)`, `\d+`) or a text template (`{valor}`, `%s`).

**Where the field name is recognized:**

| Format | Example |
|---|---|
| CSV, TSV, `;` | `NOM_CLIENTE;CPF` on the first line |
| Markdown or terminal table (mysql, psql) | `\| nm_titular \| nr_cpf \|` |
| JSON, YAML, `key=value` | `"nome_cliente": "…"`, `matricula: …`, `user_id=…` |
| SQL `INSERT` | `INSERT INTO t (nome_cliente, cpf) VALUES (…)` |
| XML | `<nomeCliente>…</nomeCliente>` |
| Excel spreadsheet | See below |

**Excel spreadsheet.** The `.xlsx` file never goes to the API as a file: what goes is the
text a tool extracted from it. llm-dlp recognizes the usual shapes of that text: the table
printed by pandas (`print(df)`, with or without the index), `to_csv`, `to_markdown`,
`to_dict`, JSON, and openpyxl rows (`('NOME', 'CPF')`).

**How it decides whether a field name matches:**

1. It splits the name into words: `COD_USU_DATA_OWNER` becomes `cod`, `usu`, `data`, `owner`;
   `nomeCliente` becomes `nome`, `cliente`.
2. **Every** word must be known. One unknown word and the field does not match. That is what
   keeps `NOM_SISTEMA`, `file_name` or `login_timeout` from being masked.
3. The combination decides the type:

| Combination | Becomes | Example |
|---|---|---|
| A word that names the data | That data | `NUM_CPF_CLIENTE`, `matricula`, `telefone_celular` |
| "name" + whose it is | A person's name | `NOM_DATA_OWNER`, `nm_titular`, `customer_name` |
| "code" or "id" + whose it is | User code | `COD_USU_OWNER`, `id_cliente`, `user_id` |
| Only whose it is | A person's name, if the value has a first and a last name | `Cliente`, `Responsável`, `Owner` |

There is also a check on the value: in a name column it must be letters and spaces; in a CPF
column it must have digits. This avoids masking a `NULL` or a repeated header.

The default vocabulary combines what data classification tools use (piicatcher,
OpenMetadata, datahub-classify, in English) with the terms and prefixes used in Brazil
(`NM_`, `NO_`, `NU_`, `CO_`, `CD_`). Every company abbreviates in its own way, so no list is
complete: see [Teaching llm-dlp](detection.md#teaching-llm-dlp).

## 4. Because it is already known

| How it became known | What holds from then on |
|---|---|
| You imported the person (`importar-pessoas`) | Their name, e-mail and code are recognized anywhere and in any spelling ("João Silva", "JOAO SILVA") |
| The value was already masked once by ways 2 or 3 | It is recognized later on its own, without the word or the field next to it |
| You listed it in `termos` or `padroes_extras` | Always masked |

A user code that is short or has no digit (`jsilva`, `ana`) is only masked next to the field
name: searching for it in the whole text would mask the word everywhere. Service accounts
(`root`, `postgres`, `admin`) are not masked.

## 5. Internal resource names (by structure)

The name of a server, database, schema, table, column, procedure, index, user, namespace,
service, bucket or queue is recognized by its **position in the structure** of the content
(the SQL grammar, the key of a configuration, the part of a URI...), never because it looks
like a name. The rules for each format are described in [structures.md](structures.md).
Already enabled:

| Rule | Where |
|---|---|
| SQL and DDL | A statement with the shape of the grammar (`SELECT … FROM x`, `INSERT INTO x`, `CREATE TABLE x`, `EXEC x`…), in any dialect, loose or inside a code string. The name after `FROM`/`JOIN`/`INTO`/`UPDATE`/`TABLE`/`VIEW`/`PROCEDURE`/`DATABASE`/`SCHEMA`/`INDEX` is an object, with the parts of `server.database.schema.object`; the other names are columns. Keywords, types and built-in functions stay |
| Error message | The type word followed by the name between quotes or brackets (`relation "x"`, `object name 'x'`, `Table 'db.x'`), or `Table/Dataset project:dataset` |
| Connection | Connection strings (`Server=…;Database=…;User Id=…`, ODBC, JDBC, libpq DSN), database URIs (`postgresql://user@host/database`, `jdbc:…`, `mongodb://`), `tnsnames.ora`, DataHub URNs, dbt `ref()`/`source()` and Airflow `conn_id` |
| Table | Database client output and CSV/TSV: values of the catalog columns (`table_name`, `table_schema`, `column_name`, `TABNAME`, `owner`, `Tables_in_…`…) and header column names that look like identifiers |
| Kubernetes, Helm, docker-compose | Manifests (by the `apiVersion` + `kind` pair): `metadata.name` according to the kind, `namespace`, references to secret, configmap and service account, Ingress hosts, kubeconfig; DNS name `service.namespace.svc.cluster.local`; `Chart.yaml`; compose services, `container_name` and `hostname`; images from a private registry (the registry and the path; the tag stays) and, on Docker Hub or a public registry, the organization that is not public (`vendashx/api`) |
| Terraform, Ansible, CloudFormation, ARM/Bicep, CI | Literal values of attributes that are names (`name`, `bucket`, `identifier`, `*Name`…; the resource's local name, references and region stay), cloud account, inventory hosts, self-hosted runners. `name` in a nested block only counts in the `metadata` block (Kubernetes provider: `metadata { name = ... }`) |
| Key-value | In YAML, JSON, TOML, INI, `.env`, `.properties`, XML and `--key value` options: the value of a key whose last piece indicates a resource name (`host`, `database`, `schema`, `user`, `bucket`, `topic`, `namespace`, `service`, `repo`, `account`…). A dotted topic/queue (`KAFKA_TOPIC=fin.notas.emitidas`, `kafka.topic=`, `topic:`) counts when every piece is lowercase and none is a code receiver or attribute (`cfg.topic`), a public domain or a file extension. A value that is a code expression stays |
| Addresses and paths | Internal host in a URL or loose (single label or `.local`, `.internal`, `.corp`…), buckets and queues (`s3://`, `gs://`, `abfss://`, `amqp://`, `kafka://`), git remotes (organization and repository), internal packages (`go.mod`, groupId, npm scope), the user in `/home/user/` and folders, `DOMAIN\user`, `ssh user@host` |
| Cloud | AWS ARN, Azure `/subscriptions/…`, GCP `projects/…`: account and resource. Public IP and IPv6 only with `objetos.ip_publico` on |
| Code, in any language | The quoted text next to a name containing a type word (`DB_HOST = "x"`, `#define DB_NAME "x"`, `connect(host="x")`, `{ queue: 'x' }`, `'bucket' => 'x'`, `@Table(name = "x")`, `@KafkaListener(topics = "x")`, `getenv("BUCKET", "x")`, `process.env.X \|\| 'x'`), or passed to a function whose name contains the type word (`assertQueue("x")`, `new QueueClient(c, "x")`, `createBucket("x")`, `inNamespace("x")`), also inside a list (`subscribe(List.of("x"))`). In a connection call (`connect`, `dial`, `Redis`...), the text followed by a port number is the server (`connect('cache01', 6379)`). The names of the variable, the function and the class stay |
| Command line | `-h`/`-d`/`-U`/`-S` when the command has at least two of them, `-n` with a resource or an operation subcommand, `deploy/x`, `svc/x`, `ns/x`..., `host:/path` of `scp`/`rsync` |
| Paths outside the home folder | Any absolute path with 2 or more levels whose first level (4 or more characters) is neither a public system directory nor a web route (`/dados/Relatorios/...`); `/srv`, `/opt`, `/data`, `/mnt`, `/media`, `/var/www` (each folder), `/var/lib`, `/var/log`, `/etc` (only identifier-looking folders), other drives (`D:\...`) and `\\server\share\...`. Public system directories stay |
| DSN | Go MySQL driver (`user:password@tcp(host:port)/database`) and PDO (`mysql:host=...;dbname=...`, `pgsql:`, `sqlsrv:`, `oci:`); in PDO, the string right after the DSN is the user (`new PDO('mysql:...', 'user', $password)`) |
| Cloud URLs | Azure Storage (`<account>.blob/dfs/file/queue/table.core.windows.net/<container>`), Service Bus (`sb://<namespace>.servicebus.windows.net` and `EntityPath`), SQS queue (`sqs.<region>.amazonaws.com/<account>/<queue>`); Azure resource ID with the type of each resource |
| Placeholder default | `${NAME:value}` (Spring) and `${NAME:-value}` (shell, compose): the name gives the type of the value; a URL in the default has its internal host masked |
| Environment variable in a list | `- name: DB_HOST` + `value: x` (Kubernetes, CI) follows the rule of `DB_HOST: x` |
| Embedded company name | An identifier containing one of your `termos` as a piece (`acme_pedidos`) |

A table is only read as a catalog when every header cell has the shape of an identifier (no
space, operator or unit). A server name is case-insensitive (like SQL names). A lowercase
statement only counts with at least two clauses (`select … from … where`), so it is not
confused with prose. A name quoted in an error message is only learned when the line looks
like an error. A value with no owner stays in the clear: a placeholder and a name made only
of role, type and example number (`my-bucket`, `stub-user`, `source_db`, `XXXXXXXX`,
`000000000000`); one distinctive piece is enough for the name to be masked (`my-vendashx`).
In a traceback, the line that points to a dependency or the standard library stays; the one
that points to the project has the function, the module, the code and the value quoted in
the error masked. See "Public only with proof" in [structures.md](structures.md).

| Situation | What happens |
|---|---|
| The name appears in a structural position | Masked there, with a pseudonym of the type (`T_…` table, `HOST_…` server, `C_…` column) |
| The position is unambiguous (or the name was seen by two different rules) and it looks like an identifier (`_`, digit, dot, hyphen between parts or mixed case) | It is remembered and masked in any other text too, in any letter case, including after a restart |
| Plain word (`cliente`), column, index, vocabulary of the format itself, web content, doubtful position | Masked only where it appeared; it is not remembered |
| Not seen for more than 90 days | No longer searched for outside the structure |

**Limit:** a name that only appears in sentences, without ever having gone through a
structure llm-dlp recognizes ("the problem is in the orders table of system X"), **is not
caught**.

The readers accept the spelling variations of each format: any letter case where the language
does not distinguish, spaces, tabs and line breaks, every kind of quoting and escaped quotes,
comments in the middle, glued punctuation, qualified names of up to 4 parts and names with
`$`, `#`, `-` and accents (see the last section of [structures.md](structures.md)). The same
name always gets the same pseudonym, even when two rules find it with different types.

## When in doubt, mask

Near `senha`, `password` or `secret`, any value with a digit or a symbol is treated as a
password, even if it is just a name (`secretRef: db-credentials`). In a one-row table, where
the columns cannot be told apart, it masks more than needed. This breaks nothing: on the way
back, you and the commands see the real value.

## Teaching llm-dlp

### People

A person's name and code have no format. To have them recognized in any text, import a list
you already have:

```bash
llm-dlp importar-pessoas owners.csv --grupo COD_USUARIO:NOME:EMAIL --separador-nome /
```

- `--grupo` says which CSV columns hold each person's code, name and e-mail. You can repeat
  the option for more than one set of columns, and leave a part empty
  (`COD_STEWARD:NOM_STEWARD:`).
- `--separador-nome` is for when the cell brings the name followed by something else
  (`Fulano / Área X`): only the part before the separator counts.
- Everything is stored **only as a hash**. The name, code and e-mail of the same person get
  the same identifier.
- A code that is short or has no digit (`TBD`, `N/A`) is ignored, so common words are not
  masked.

It is not required: a name that appears in a recognized field (`NOM_CLIENTE`) enters the
record on its own.

### Your company's field names

To see how llm-dlp understands the fields of a file, and which ones it does not recognize:

```bash
llm-dlp colunas clientes.csv
```

To teach the missing ones, in `config.json`:

```json
"campos_extras": [
  {"tipo": "usuario", "palavras": ["chapa", "re"]},
  {"tipo": "pessoa",  "palavras": ["cooperado", "segurado"]}
]
```

| Type (`tipo`) | Meaning |
|---|---|
| `cpf`, `cnpj`, `rg`, `cnh`, `pis`, `doc`, `nascimento`, `telefone`, `cep`, `endereco`, `usuario`, `conta`, `cartao`, `sensivel`, `nome` | The word says which data the field holds |
| `pessoa` | The word says whose field it is (`cooperado` makes `NOME_COOPERADO` count) |
| `neutra` | The word does not change what the field holds (`bco`, `sis`) |

### Your own codes and terms

```json
"padroes_extras": [{"rotulo": "matricula", "regex": "\\bMAT\\d{6}\\b"}],
"termos": [{"rotulo": "projeto", "valores": ["Projeto Fenix"]}]
```

`padroes_extras` is for what has a format (a regex). `termos` is for exact words, such as the
name of a project or a system that must not go out.
