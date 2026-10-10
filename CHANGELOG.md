# Changelog

[Português](docs/pt-BR/CHANGELOG.md)

What changed in each version of llm-dlp. Versions follow `MAJOR.MINOR.PATCH`; what each
number means here is in [docs/versioning.md](docs/versioning.md). The newest comes first.

In each version: **Masking** is what started (or stopped) being masked; **Program** is
commands, installation, configuration, performance and documentation; **Code** is what only
matters to whoever changes the code.

## [0.2.0] - 2026-10-10

A public name only stays in the clear with proof. Every rule that lets a name through now
requires a structural guarantee, fail-closed: when in doubt, mask.

### Masking

- **Non-Latin scripts:** a database, schema, table or user name in any script (Cyrillic,
  Japanese, accents) is masked. So is a dotted user name (`maria.souza`).
- **Traceback** (Python, Java, JavaScript, Go): the line that points to the project has the
  function, the module, the code and the value quoted in the error masked. The line of a
  dependency or of the standard library (`pandas`, `net/http`) stays readable.
- **Container image:** it only stays in the clear with two keys, a vendor organization
  **and** a public software name. `acme/redis` and the image built in the project are masked.
- **Public software in the conversation:** the default name of a software (`airflow`,
  `datahub`, `superset`) is no longer spread through the conversation when the conversation
  shows that it is the software (installed, imported, run as a public image or executed). A
  dictionary word, a name defined in the project and a registered term never get this proof.
- **Origin by path:** the path readers stop at a public package
  (`site-packages/sqlalchemy/...` stays whole) and keep reading the project folders under a
  public user (`/home/ubuntu/<project>`). A dependency file that was read does not teach
  names to the conversation. Being in a dependency folder is not enough: the package, the
  module or the group must be public, because the client's internal package lives in the
  same folders.
- **docker-compose:** a service is recognized by any key of the specification, including
  `<<`.
- **Command-line listing:** the command in sight (`$ airflow dags list`) says the header
  belongs to the tool; for a type outside the vocabulary, only the identity column (`dag_id`)
  is typed. The table of `kubectl get` and `docker ps` is read by its header.
- **Managed cloud service hosts** (RDS, Redshift, S3, Azure, GCP): the client's label is
  masked and the provider's domain stays.

### Program

- Version and changes organized: `llm-dlp versao`, this file, ready-made binaries in each
  release and `make release` (see [docs/versioning.md](docs/versioning.md)).
- Estimate of CPU, memory and disk usage in
  [docs/how-it-works.md](docs/how-it-works.md#how-much-of-the-machine-it-uses).

### Code

- Generated lists in `internal/mask/dados/`; files of the `mask` package grouped by prefix
  (`detectores_`, `campos_`, `leitor_`, `chamada_`, `decisor_`, `memoria_`, `vocab_`). The map
  and the table of what is stored are in [docs/development.md](docs/development.md).
- New generated lists: `software_publico.txt` and `fornecedores.txt` (GitHub repositories
  with 3000 stars or more), `imagens_oficiais.txt` (Docker Official Images) and
  `palavras_dicionario.txt` (the 50 thousand most frequent words of Portuguese and English).

### Measured

Against 0.1.0, on hand-labelled names from 16 public repositories:

| | 0.1.0 | 0.2.0 |
|---|---|---|
| Owner's name that leaked (5 repositories, validation) | 6 of 22 | 1 of 22 |
| Owner's name that leaked (11 new repositories, validation) | 2 of 9 | 1 of 9 |
| Public name masked for nothing (5 repositories, validation) | 138 of 207 | 124 of 207 |
| Public name masked for nothing (11 new repositories, validation) | 40 of 122 | 46 of 122 |
| Words replaced in prose / code / configuration | 2.29% / 2.06% / 5.47% | 2.16% / 1.70% / 4.91% |
| Time for 21 MB of new text (2 cores) | 75 s | 77 s |
| Memory at startup | 21 MB | 25 MB |

The over-masking that went up in the new repositories is translation catalog labels (`"User":
"Gebruiker"`): a rule that let them through also let exported spreadsheet data through, and
was rejected.

## [0.1.0] - 2026-10-08

First version: everything that existed up to the commit `815a6ac`.

### Masking

- **Personal data and secrets by format:** e-mail, CPF, CNPJ, phone, card, documents with a
  check digit, tokens and keys (gitleaks), common passwords.
- **Personal data by the field name:** `field: value`, JSON, SQL, XML, CSV, spreadsheet,
  terminal table.
- **Registered people** (names, e-mails, codes), stored only as hashes.
- **Internal resource names by structure:** SQL and DDL, tables in any layout, schemas,
  connection strings, YAML/JSON of Kubernetes, Helm, compose, Terraform, Ansible and CI,
  key-value, addresses, paths, packages, cloud, code and command line.
- **Transport normalization:** the content is read as it arrives (line number, `grep`, diff,
  ANSI, escaped JSON).
- **Conversation memory and tracking:** a name decided where it enters holds for the whole
  conversation; what the model wrote before any data is public; a common word stays a word
  in prose.
- **Images and PDFs:** OCR and black bars.

### Program

- Local proxy for Claude Code, fail-closed, with an exact round trip and text already sent
  frozen (the API cache stays valid).
- `llm-dlp instalar` (program, configuration, OCR, Claude Code, lock), `status`, `parar`,
  `emergencia`, `desinstalar`.
