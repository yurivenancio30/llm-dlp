# Structures: where object names live in each format

[← back to the README](../README.md) · [Português](pt-BR/estruturas.md)

This document is the basis of llm-dlp's **structure readers**: the rules that recognize, by
the grammar of each format, where there is a server, database, schema, table, column,
procedure, user, namespace, bucket or queue name, and what must never be masked (reserved
words, built-in functions and types, standardized keys).

Principles:

- **The decision comes from the structure, not from the name.** No language dictionary, no
  per-client list. The only lists are technical, small and public, taken from the official
  documentation of each format (the source is in each section).
- **A structural position masks and teaches.** A name seen in a structural position starts
  being masked in loose text too, but only if it looks like an identifier (`_`, digit, dot,
  hyphen between parts or mixed case). A plain word stays only in the position.
- **The content stays valid.** The pseudonym is typed and consistent and respects the naming
  rules of the format (quotes, brackets, letter case, DNS-1123, Elasticsearch lowercase).

Each section brings, in this order: detection signals, a "position → entity" table, public
vocabulary, identifier rules, before/after examples (fictitious data), hard cases and limits,
and links.

**Status:** this document started as the research that came before the rules, and parts of it
still describe the plan, not the code. Where a section says "How it is implemented" or "What is
implemented", that is what the code does today. The phase 1 items were checked (see
[Check](#check-of-the-phase-1-verify-items)). The ones still marked `[VERIFY]`
belong to the following phases, were not confirmed on the official page and must be checked
before becoming a rule. Also in need of checking, although not marked: in the key-value
section, the lists of JSON Schema, OpenAPI, Protobuf, GraphQL, TOML, YAML and XML; in the
infrastructure as code section, the naming rules of the RDS instance identifier, of the S3
bucket and of a DNS hostname.

## Pseudonym format

A single format, for every family:

```
<PREFIX>_<ID>        e.g.: T_x7k2m9qa   (table tb_pedido_x9)
```

- **ID:** 8 lowercase base32 characters, the same length as the other llm-dlp pseudonyms,
  taken from `HMAC-SHA256(llm-dlp key, "objeto" ‖ type ‖ normalized name)`
  (`internal/mask/chave.go`).
- **Stable:** the same name of the same type gives the same pseudonym in any conversation and
  after a restart (it only depends on the key). It is never "per session" and never a
  counter.
- **Letter case:** in the SQL types (database, schema, table, column, procedure, index),
  unquoted identifiers are case-insensitive, so `TB_PEDIDO` and `tb_pedido` are the same
  object. In the other types (folder, bucket, queue, service...) the spelling counts as it
  is: `/dados/Relatorios` and `/dados/relatorios` are two folders, and each one comes back
  with its own spelling.
- **Prefix:** that of the entity type (table below); in lowercase when the name is all
  lowercase (`t_…`), in uppercase when it is all uppercase (`T_…`). A mixed-case name
  (`Pedido_Item`) gets the prefix with only the first letter uppercase and the letters of the
  ID in the case that a hash of the spelling itself dictates (`T_aBcDe…`): two spellings of
  the same SQL name in the same text come out with two pseudonyms, and each one comes back as
  it was. Where the format requires lowercase and hyphens (DNS-1123, S3 bucket), the same
  idea applies.
- **Way back:** unmasking accepts the pseudonym in any case (`T_abc…`, `t_abc…`, `T_ABC…`)
  and returns the real name; the exact pseudonym of one spelling takes priority over the case
  variant of another.
- **Only what we generated is skipped:** a name in the text with the shape of a pseudonym
  (`t_customer`) is masked like any other; only the pseudonym llm-dlp itself generated is
  left as it is.

### Entity types

| Type (key in `config.json`) | Prefix | Includes, per family |
|---|---|---|
| `servidor` | `HOST_` | server, host, instance, cluster, Snowflake account, DataHub `platform_instance` |
| `database` | `DB_` | database, catalog, BigQuery/GCP project |
| `schema` | `SCH_` | schema, BigQuery dataset, Cassandra keyspace, Avro namespace, Protobuf package |
| `tabela` | `T_` | table, view, MongoDB collection, Elasticsearch index, Redis key prefix, DynamoDB table, table pattern in allow/deny |
| `coluna` | `C_` | column, field (MongoDB, Elasticsearch, Avro, Protobuf, JSON Schema), column alias |
| `procedure` | `PROC_` | procedure, function, trigger, package |
| `indice` | `IDX_` | SQL index and constraint |
| `usuario` | `USR_` | user, login, role, service account |
| `namespace` | `NS_` | Kubernetes namespace |
| `servico` | `SVC_` | service, deployment, private image, DAG/job/task, connection (`conn_id`), Snowflake warehouse |
| `bucket` | `BKT_` | bucket, container, storage path |
| `fila` | `TOP_` | queue, topic, stream |

## Turning on and off per type

There are **two different switches**, per entity type:

- `mascarar` (mask): a name of that type is replaced with a pseudonym **where it was
  recognized** (in the structural position). Default: every type on.
- `propagar` (propagate): a name of that type, once learned, is also replaced **anywhere
  else** (loose text, another file). Default: on for server, database, schema, table,
  procedure, user, namespace, service, bucket and queue; **off for column**, because names
  like `user_id` and `created_at` exist in every codebase.

What is not listed stays at the default. Example to keep columns readable (neither mask nor
propagate) and hide the rest:

```json
"objetos": {
  "ligado": true,
  "mascarar": { "coluna": false },
  "propagar": { }
}
```

Where each switch is applied:

| Stage | `mascarar` | `propagar` |
|---|---|---|
| Decision on each span returned by the readers, before the replacement | a span of a type that is off is discarded and the text stays as it is | — |
| Learning (memory and `vistos.json`) | a type that is off is not stored | a type that is off is stored only for the structural position, with no propagation mark |
| Search for learned names in new text | — | only the names of a type that is on, and only the ones that passed the guards (below) |
| Record of what already went out (`enviados.log`) | changing the switch restarts the record (the cache is rewritten once) | same |

Limit: if the same name is a table (on) in one place and a column (off) in another, it stays
readable where it appears as a column, and that reveals the table name.

### Propagation guards

A learned name is only propagated if it **looks like an identifier** (`_`, digit, dot,
hyphen between parts or mixed case), if its type is in `propagar` and if the evidence is
**strong**:

- unambiguous position: the object name after `CREATE`/`ALTER`/`FROM`/`JOIN`/`INTO`/`UPDATE`
  in a statement recognized as SQL by at least 2 signals (the minimal shape of the grammar
  and the type of the block, or more than one clause, or a single clause with an unambiguous
  shape, in uppercase or lowercase: `SELECT * FROM fin.tb_x`, `update tb_x set ...`); the
  value of a catalog column (`table_name`, `TABSCHEMA`, `Key_name`…); a connection key
  (`Server=`, `host:`, `-S`, `jdbc:…//host`); a URN;
- or the same name seen by **2 different structural rules**.

A doubtful reading (the origin rule, any table header) masks in place, but does not teach.

### One name, one type

The same real name always keeps the same type and therefore the same pseudonym. If it was
already learned (in RAM or in `vistos.json`) with a type, the finding of another rule uses
that type (the path `/srv/x/` after `deployment/x` becomes `svc_…`, not `dir_…`), also after
a restart. In a text where two rules disagree and nothing was learned, the type of the first
occurrence wins. A dotted name (`fila.eventos`) is also looked up in `vistos.json` with the
following pieces joined by dots (up to 4 pieces), so it is recognized in prose after the
restart.

### Validity

A name not seen for **90 days** stops being propagated (it stays masked in the structural
position). `vistos.json` stores, along with the hash, the day the name was last seen (2 more
bytes per name).

## Check of the phase 1 [VERIFY] items

Checked on the official page before becoming a rule. Legend in the chapters: **[DOC ✓]** =
confirmed; **[not confirmed → tolerant rule]** = the
documentation does not bring the literal text, so the rule does not depend on it;
**[phase 2]** = left phase 1.

| Item | Result | Rule in phase 1 | Source |
|---|---|---|---|
| MySQL 1146 / 1054 / 1049 / 1045 | ✓ `Table '%s.%s' doesn't exist`, `Unknown column '%s' in '%s'`, `Unknown database '%s'`, `Access denied for user '%s'@'%s' …` | name between single quotes, in the position of the `%s` | [MySQL error reference](https://dev.mysql.com/doc/mysql-errors/8.0/en/server-error-reference.html) |
| SQL Server 208 | ✓ text `Invalid object name '%.*ls'.`; ✗ the line `Msg 208, Level 16, State 1, Server X, Line 1` is not on the page | name between quotes; server after `Server ` only by the shape of the line, without depending on the exact text | [MSSQLSERVER_208](https://learn.microsoft.com/en-us/sql/relational-databases/errors-events/mssqlserver-208-database-engine-error) |
| PostgreSQL 42P01 | ✓ `relation "x" does not exist` (ECPG example); ✗ 42703, 3F000, 3D000, 42883 with no text in the docs | name between double quotes after `relation`/`column`/`schema`/`database`; tolerant about the rest | [ECPG Error Handling](https://www.postgresql.org/docs/current/ecpg-errors.html) |
| Snowflake 002003 | ✓ `Object 'DB.SC.MYTABLE' does not exist or not authorized.` | qualified name between single quotes | [BCR-1858](https://docs.snowflake.com/en/release-notes/bcr-bundles/un-bundled/bcr-1858), [Error codes](https://docs.snowflake.com/en/user-guide/dynamic-tables/error-codes) |
| BigQuery notFound | ✓ `Not Found: Dataset myproject:foo`; ✗ shape of the table | after `Not found:`/`Not Found:` + `Dataset`/`Table`, case-insensitive; `proj:ds.tb` | [Error messages](https://docs.cloud.google.com/bigquery/docs/error-messages) |
| psql: footer, format, wrapping | ✓ `(n rows)`, `aligned` by default, `.`/`+` (ascii) and `…` (unicode) in the margin of `wrapped`; ✗ title `List of relations` | table by the header + dash line pair, not by the title | [psql](https://www.postgresql.org/docs/current/app-psql.html) |
| sqlcmd | ✓ `-s` separator, `-W` strips spaces, `-h` headers, message `<x> rows affected` | same as psql | [sqlcmd](https://learn.microsoft.com/en-us/sql/tools/sqlcmd/sqlcmd-utility) |
| SQL*Plus | ✓ `N rows selected.` (controlled by `SET FEEDBACK`); ✗ `no rows selected` | same as psql | [SQL*Plus Basics](https://docs.oracle.com/en/database/oracle/oracle-database/26/sqpug/SQL-Plus-basics.html) |
| snowsql | ✓ `N Row(s) produced. Time Elapsed: X.XXXs` | same as psql | [Using SnowSQL](https://docs.snowflake.com/en/user-guide/snowsql-use) |
| Db2 CLP | ✓ `N record(s) selected.` (with a period on LUW) | same as psql | [Db2 CLP tutorial](https://www.ibm.com/docs/en/SSEPEK_13.0.0/comref/src/tpc/db2z_tut_clp.html) |
| mysql client | ✓ `1 row in set (0.09 sec)`, `\G` with `*** 1. row ***`; ✗ `Empty set` | `+---+` frame and vertical mode `column: value` | [mysql tips](https://dev.mysql.com/doc/refman/8.4/en/mysql-tips.html) |
| Columns that indicate an object | ✓ Oracle `OWNER`, `TABLE_NAME`; Db2 `TABSCHEMA`, `TABNAME`; MySQL `Key_name`, `Column_name` (SHOW INDEX), `table`, `possible_keys`, `key` (EXPLAIN) | they enter the header → entity list | [ALL_TABLES](https://docs.oracle.com/en/database/oracle/oracle-database/19/refrn/ALL_TABLES.html), [SYSCAT.TABLES](https://www.ibm.com/docs/en/db2-warehouse?topic=views-syscattables), [SHOW INDEX](https://dev.mysql.com/doc/refman/8.4/en/show-index.html), [EXPLAIN](https://dev.mysql.com/doc/refman/8.4/en/explain-output.html) |
| `bq ls` / `bq show`, `snow sql` | ✗ no example in the official docs | table by the header + dash line; `tableId` in the header list | — |
| Read line numbering | ✗ no docs; **checked in the sessions**: `N<tab>` in 605 of 640 results, none with `N→` | `^\s*\d+\t` (accepts `→` too) | local count |
| PostgreSQL log line (`LOG:  statement:`) | ✗ the page describes it, but does not show the line | the SQL reader runs on each log line; it does not depend on the prefix | [runtime-config-logging](https://www.postgresql.org/docs/current/runtime-config-logging.html) |
| diff: `\ No newline at end of file`; git `diff.mnemonicPrefix`, `core.quotePath` | ✗ pages with a 429 error or cut off | a hunk line starting with `\ ` is ignored; prefixes `^[a-z0-9]/`; path between quotes with C escapes | [git-config](https://git-scm.com/docs/git-config) |
| ripgrep: long line omitted | ✗ the GUIDE does not show the text | a line without `file:line:` inside rg output is ignored | [ripgrep GUIDE](https://github.com/BurntSushi/ripgrep/blob/master/GUIDE.md) |
| SemVer (excluding versions from propagation) | ✓ official regex | the official regex without `^`/`$` | [semver.org](https://semver.org/) |
| BigQuery JDBC (`ProjectId`, `DefaultDataset`) | ✗ Google's docs refer to the vendor's PDF | key=value with name keys (`ProjectId` → DB, `DefaultDataset` → SCH) | [ODBC/JDBC drivers](https://docs.cloud.google.com/bigquery/docs/reference/odbc-jdbc-drivers) |
| Execution plans, translated psql headers, semi-structured fields | — | moved to phase 2 | — |

## Contents

- [Pseudonym format](#pseudonym-format) · [Turning on and off per type](#turning-on-and-off-per-type) · [Phase 1 check](#check-of-the-phase-1-verify-items)

1. [SQL and DDL](#sql-and-ddl)
2. [Database client output](#database-client-output)
3. [Connection strings and addresses](#connection-strings-and-addresses)
4. [Tabular data](#tabular-data)
5. [JSON, YAML, TOML, INI, .env, .properties, XML and schema languages](#json-yaml-toml-ini-env-properties-xml-and-schema-languages)
6. [Kubernetes and containers](#kubernetes-and-containers)
7. [Infrastructure as code](#infrastructure-as-code)
8. [Data tools](#data-tools)
9. [Code](#code)
10. [NoSQL and search](#nosql-and-search)
11. [Transport formats](#transport-formats-diff-grep-line-numbering-logs-markdown-heredoc)
    - [Loose script and shell output](#loose-script-and-shell-output)
12. [Generic fallback](#generic-fallback)


## SQL and DDL

Scope: ANSI/ISO, T-SQL, Oracle (SQL and PL/SQL), PostgreSQL, MySQL/MariaDB, Db2, Snowflake, BigQuery (GoogleSQL), Spark SQL/Databricks.
Principle: a token becomes an object name **by its position in the grammar**, never because it looks like a word of some language. The public lists only serve to say what is **not** a name.

### 1. Detection signals

**By the call that produced the text** (stronger than the content):
- Tool/command: `sqlcmd`, `bcp`, `Invoke-Sqlcmd` (T-SQL); `sqlplus`, `sqlcl` (Oracle); `psql`, `pg_dump` (PG); `mysql`, `mysqldump`, `mariadb` (MySQL); `db2`, `db2look` (Db2); `snowsql`, `snow sql`, a Snowflake MCP tool like `*_execute` (Snowflake); `bq query`, `bq show`, `jobs.query` API (BigQuery); `spark-sql`, `spark.sql("...")`, `databricks` CLI (Spark).
- A `.sql`, `.ddl`, `.pls/.pkb/.pks` (Oracle), `.prc` file; the `-q`/`-e`/`-c`/`--query` argument of one of those clients.
- Kind of result: a header with `TABLE_SCHEMA | TABLE_NAME`, the output of `SHOW TABLES`, `DESCRIBE`, `\d`.

**By the content** (it is SQL with ≥2 signals, at the start of a line/statement, outside prose):
- Start of a statement: `SELECT|WITH|INSERT|UPDATE|DELETE|MERGE|CREATE|ALTER|DROP|TRUNCATE|GRANT|REVOKE|USE|CALL|EXEC|BEGIN|DECLARE`.
- Structure: `SELECT … FROM`, `INSERT INTO … (… ) VALUES`, `CREATE TABLE x (col type, …)`, terminators `;`, `GO` (alone on a line), `/` (alone on a line, Oracle).

**Which dialect** (score; the highest wins; tie → ANSI rules):

| Dialect | Typical markers |
|---|---|
| T-SQL | `[x]`, `GO`, `TOP n`, `@var`, `@@ROWCOUNT`, `N'..'`, `#tmp`, `NOLOCK`, `sp_executesql`, `IDENTITY(1,1)`, `nvarchar(max)` |
| Oracle | `DUAL`, `ROWNUM`, `NVL`, `VARCHAR2`, `NUMBER(p,s)`, `:=`, `t@dblink`, `CONNECT BY`, `CREATE OR REPLACE PACKAGE [BODY]`, `EXECUTE IMMEDIATE` |
| PostgreSQL | `::type`, `$$ … $$`, `RETURNING`, `ILIKE`, `SERIAL`, `LANGUAGE plpgsql`, `pg_catalog`, `\d` |
| MySQL/MariaDB | `` `x` ``, `ENGINE=InnoDB`, `AUTO_INCREMENT`, `LIMIT a,b`, `# comment`, `DELIMITER //`, `ON DUPLICATE KEY` |
| Db2 | `FETCH FIRST n ROWS ONLY` + `SYSIBM.SYSDUMMY1`, `WITH UR`, `SYSCAT.`, `GENERATED ALWAYS AS IDENTITY` |
| Snowflake | `QUALIFY`, `VARIANT`, `FLATTEN`, `@stage`, `CREATE WAREHOUSE/STAGE/PIPE/TASK/STREAM`, `COPY INTO`, `col:field::type`, `IDENTIFIER('..')` |
| BigQuery | `` `proj-x.ds.t` ``, `STRUCT<>`/`ARRAY<>`, `SAFE_CAST`, `` `region-us`.INFORMATION_SCHEMA ``, `_TABLE_SUFFIX`, `OPTIONS(...)` |
| Spark/Databricks | `USING DELTA`, `OPTIMIZE … ZORDER BY`, `LOCATION 'dbfs:/…'`, `TBLPROPERTIES`, `catalog.schema.t` with backticks, `CACHE TABLE` |

### 2. Position → entity type

`X` = name (simple or qualified). In a qualified name, the parts on the left get the type of the dialect's hierarchy (see section 4).

| Grammatical position | Type |
|---|---|
| `FROM X`, `JOIN X`, `INTO X`, `UPDATE X`, `MERGE INTO X`, `USING X`, `TRUNCATE [TABLE] X`, `DELETE FROM X` | table/view (T_) |
| `CREATE/ALTER/DROP [OR REPLACE] [TEMP] TABLE|VIEW|MATERIALIZED VIEW X` | table / view (T_ / V_) |
| `CREATE/ALTER/DROP DATABASE|CATALOG X`, `USE [DATABASE] X` | database/catalog (DB_) |
| `CREATE/ALTER/DROP SCHEMA X`, `USE SCHEMA X`, `SET search_path TO X`, `ALTER SESSION SET CURRENT_SCHEMA = X` | schema (SCH_) |
| `CREATE PROCEDURE|PROC X`, `EXEC[UTE] X`, `CALL X` | procedure (PROC_) |
| `CREATE FUNCTION X`, `X(` qualified (`sch.X(`) or not built-in | function (FN_) |
| `CREATE TRIGGER X … ON Y` | trigger (TRG_), Y table |
| `CREATE [UNIQUE] INDEX X ON Y (c1, c2)` | index (IX_), Y table, c columns |
| `CONSTRAINT X PRIMARY KEY|FOREIGN KEY|CHECK|UNIQUE` | constraint (CK_) |
| `REFERENCES Y (c)` | table + column |
| `CREATE [PUBLIC] SYNONYM X FOR Y`, `CREATE ALIAS X FOR Y` (Db2) | synonym (SYN_) + target |
| `CREATE SEQUENCE X`, `X.NEXTVAL`, `NEXT VALUE FOR X`, `nextval('X')` | sequence (SEQ_) |
| `CREATE USER|ROLE|LOGIN X`, `GRANT … TO X`, `REVOKE … FROM X`, `AUTHORIZATION X`, `OWNER TO X`, `EXECUTE AS USER = 'X'` | user/role (U_ / R_) |
| `GRANT priv ON [type] Y TO X` | Y object of the stated type; X role |
| `CREATE WAREHOUSE X`, `USE WAREHOUSE X`, `WAREHOUSE = X` | warehouse (WH_) |
| `CREATE STAGE X`, `@X`, `@sch.X/path` | stage (STG_) |
| `CREATE TASK|PIPE|STREAM X`, `AFTER X`, `ON TABLE Y` (stream) | task/pipe/stream (TSK_/PIPE_/STR_) |
| `CREATE SCHEMA X` in BigQuery, `proj.X.t` | dataset (DS_) |
| `CREATE DATABASE LINK X`, `t@X` | dblink / server (SRV_) |
| `srv.db.sch.t` (T-SQL 4 parts), `OPENQUERY(X, …)`, `sp_addlinkedserver 'X'` | server (SRV_) |
| `SELECT a, b`, `INSERT INTO t (a, b)`, `CREATE TABLE t (a type, …)`, `ALTER TABLE t ADD|DROP|RENAME COLUMN a`, `WHERE a =`, `GROUP BY a`, `ORDER BY a`, `ON t1.a = t2.b`, `SET a = ` | column (C_) |
| `AS x` after a table/subquery; `t x` (implicit alias) | table alias (see 6) |
| `AS x` after an expression in the SELECT | column alias (C_) |
| `WITH x AS (` , `, x AS (` | CTE (CTE_) |
| A literal compared to `table_name`, `table_schema`, `column_name`, `OBJECT_ID('..')`, `IDENTIFIER('..')`, `'..'::regclass` | object inside a string (type by the column/function) |

### 3. Public vocabulary (NEVER mask)

Rule: a token that matches the list **and** is in the position of a keyword, type or built-in function call stays. In a name position (e.g. after `.`), the list does not protect it.

| List | Official source | How to extract | Approx. size |
|---|---|---|---|
| ANSI/ISO reserved | ISO 9075 is paid; use the SQL:2023 column of PostgreSQL's Appendix C and Microsoft's ODBC/ISO list | scrape table C.1, filter column SQL:2023 = reserved | ~340 |
| T-SQL reserved + ODBC + future | learn.microsoft.com, Reserved Keywords | copy the 3 tables of the page (or the .md in the public MicrosoftDocs/sql-docs repository) | ~185 + ~235 + ~250 |
| T-SQL types/functions | `sys.types` (~34); the T-SQL functions page | `SELECT name FROM sys.types WHERE is_user_defined=0` | ~34 types, ~300 functions |
| Oracle | SQL Reserved Words + PL/SQL Reserved Words and Keywords | `SELECT keyword, reserved FROM V$RESERVED_WORDS` (keep only reserved='Y' for the hard rule) | ~100 reserved; view ~2,400 |
| PostgreSQL | Appendix C | `SELECT word, catcode FROM pg_get_keywords()`; functions/types: `pg_proc`/`pg_type` with `pronamespace='pg_catalog'::regnamespace` | ~500 words; ~3,000 functions |
| MySQL 8.4 | Keywords and Reserved Words; Built-In Function Reference | `SELECT WORD, RESERVED FROM INFORMATION_SCHEMA.KEYWORDS` | ~730 (≈260 reserved); ~450 functions |
| Db2 11.5 | Reserved schema names and reserved words | copy the two lists of the page | ~407 + ~118 (SQL2003) |
| Snowflake | Reserved & limited keywords; All functions (alphabetical); Data types | page (92 lines) + `SHOW FUNCTIONS` filtering `is_builtin='Y'` | 92; ~1,000 functions |
| BigQuery | Lexical structure (reserved keywords); All functions | table of the Lexical page; function index | ~95; ~600 functions |
| Spark | ANSI Compliance (keyword table); Built-in Functions | table with columns Spark-ANSI/Default/SQL-2016; `SHOW SYSTEM FUNCTIONS` | ~400 keywords; ~500 functions |

**System schemas/catalogs (do not mask):** `INFORMATION_SCHEMA` (all); `sys`, `guest`, `db_owner`… (fixed roles), databases `master`, `msdb`, `tempdb`, `model` (T-SQL); `SYS`, `SYSTEM`, `PUBLIC`, `DUAL`, prefixes `DBA_`/`ALL_`/`USER_`/`V$`/`GV$` (Oracle); `pg_catalog`, `pg_toast`, `pg_temp_*`, `public` (PG); `mysql`, `performance_schema`, `sys` (MySQL); `SYSCAT`, `SYSIBM`, `SYSIBMADM`, `SYSFUN`, `SYSPROC`, `SYSPUBLIC`, `SYSSTAT`, `SYSTOOLS`, `SESSION` (Db2); `SNOWFLAKE`, `SNOWFLAKE_SAMPLE_DATA`, `ACCOUNT_USAGE`, `PUBLIC`, roles `ACCOUNTADMIN`/`SYSADMIN`/`SECURITYADMIN`/`USERADMIN`/`ORGADMIN`/`PUBLIC` (Snowflake); `` `region-xx` `` (BigQuery); `system`, `builtin`, `session`, `default`, `spark_catalog`, `hive_metastore` (Spark/Databricks).

**Decision about `dbo` (and `public`, `default`): do not mask.** It is the schema the product itself creates in every database, identical in any installation: it identifies no one, and replacing it only takes away from the model the hint that it is the default schema. The rule is "a name the product fixes in every installation stays". A schema created by the user (in Oracle, the schema **is** the user) is always masked.

### 4. Identifier rules per dialect

| Dialect | Quoting | Escape inside | Unquoted | Quoted | Qualification (max.) |
|---|---|---|---|---|---|
| ANSI | `"x"` | `""` | folds to UPPERCASE | exact | `catalog.schema.object` |
| T-SQL | `[x]`; `"x"` if `QUOTED_IDENTIFIER ON` | `]]` / `""` | depends on the collation (usually case-insensitive) | same as collation | `server.database.schema.object`; empty parts `srv..t`, `db..t`; `#t`/`##t` temp; 128 chars |
| Oracle | `"x"` | none (quotes forbidden) | UPPERCASE; `_ $ #` | exact | `schema.object[.part]@dblink`; 128 bytes/part; database and dblink names always uppercase |
| PostgreSQL | `"x"`, `U&"x"` | `""` | lowercase | exact | `database.schema.object` (database can only be the current one); 63 bytes |
| MySQL/MariaDB | `` `x` ``; `"x"` with `ANSI_QUOTES` | ` `` ` | columns are case-insensitive; tables/databases according to `lower_case_table_names` / OS | same | `database.table.column` (no schema: database = schema); a word after `.` needs no quoting; 64 chars |
| Db2 | `"x"` | `""` | UPPERCASE | exact | `location.schema.object`; 128 bytes |
| Snowflake | `"x"` | `""` | UPPERCASE; `$` allowed | exact (unless `QUOTED_IDENTIFIERS_IGNORE_CASE`) | `database.schema.object`; each part quoted separately; `IDENTIFIER('…')`; 255 chars |
| BigQuery | `` `x` `` (may cover the whole path: `` `p.d.t` ``) | string escapes (`\``) | columns are case-insensitive; dataset/table **are case-sensitive** (unless `is_case_insensitive`) | same | `project.dataset.table`; hyphen only in the 1st part in FROM; `ds.prefix_*` |
| Spark/Databricks | `` `x` `` | ` `` ` | case-insensitive (quoted too) | same | `catalog.schema.table` |

Consequence for the masker: the map key is the name **normalized** according to the dialect (fold the case where the dialect folds; keep it exact where it is quoted and case-sensitive). This way `tb_pedido_x9`, `TB_PEDIDO_X9` and `"TB_PEDIDO_X9"` (Snowflake/Oracle/Db2) get the same pseudonym; a quoted `"Tb_Pedido"` gets another.

### 5. Before/after examples

Pseudonym: in llm-dlp's single format (see [Pseudonym format](#pseudonym-format)): type prefix + 8 characters derived by HMAC from the key. It is always a valid unquoted identifier in every dialect. The output copies the **case shape** of the original (all uppercase → `T_A8F1`) and keeps quotes/brackets/backticks. On the way back, the search ignores case in the dialects that fold.

T-SQL:
```sql
-- before
SELECT p.[Valor Total], c.nome FROM [srv-exemplo-01].vendas_demo.financeiro.tb_pedido_x9 AS p
JOIN dbo.tb_cliente c ON c.id = p.id_cliente; EXEC financeiro.usp_fecha_mes @ano = 2024;
-- after
SELECT p.[C_xsdgeq7l], c.C_rr476ahy FROM [HOST_zvfrq7zb].DB_2vmfnowr.SCH_mvrhafae.T_cszwa3ri AS p
JOIN dbo.T_3ms3mjt3 c ON c.C_x6w4tokb = p.C_c5w3hetu; EXEC SCH_mvrhafae.PROC_zx55ztp5 @ano = 2024;
```
(`dbo`, `SELECT`, `JOIN`, `EXEC` stay; `@ano` is a local parameter: it stays, see 6.)

Snowflake:
```sql
-- before
CREATE OR REPLACE TASK VENDAS_DEMO.FINANCEIRO.TSK_CARGA WAREHOUSE = WH_ETL_DEMO
AS COPY INTO "Tb_Pedido" FROM @FINANCEIRO.STG_ENTRADA/2024/ ;
-- after
CREATE OR REPLACE TASK DB_X7K2.SCH_Q3M9.TSK_W2C4 WAREHOUSE = WH_N8L0
AS COPY INTO "T_qwea6mav" FROM @SCH_Q3M9.STG_F1Y6/2024/ ;
```
(The quoted, mixed-case `"Tb_Pedido"` is a different object from `TB_PEDIDO` and gets its own pseudonym. The path `/2024/` inside the stage is left to the path detector.)

BigQuery:
```sql
-- before
SELECT id_pedido FROM `projeto-exemplo-01.vendas_demo.tb_pedido_x9`
WHERE _TABLE_SUFFIX > '2024' AND status = 'ok';
-- after
SELECT C_yxptlwin FROM `DB_2t2xlad4.SCH_iardtg5m.T_cszwa3ri`
WHERE _TABLE_SUFFIX > '2024' AND C_hpawygue = 'ok';
```

PostgreSQL / Oracle:
```sql
-- before
CREATE INDEX ix_pedido_data ON financeiro.tb_pedido_x9 (dt_emissao);
GRANT SELECT ON financeiro.vw_resumo TO analista_ro;
SELECT * FROM financeiro.tb_pedido_x9@lk_srv_exemplo;
-- after
CREATE INDEX IX_c8r1 ON SCH_mvrhafae.T_cszwa3ri (C_zh6xlzrr);
GRANT SELECT ON SCH_mvrhafae.T_me3bezjy TO USR_f3kpe7dg;
SELECT * FROM SCH_mvrhafae.T_cszwa3ri@HOST_z2l74vie;
```

### 6. Hard cases and limits

- **Dynamic SQL in a string:** `EXEC('…')`, `sp_executesql N'…'`, `EXECUTE IMMEDIATE '…'` (Oracle, Snowflake, BigQuery, Db2), `$$ … $$` in a procedure body (PG/Snowflake), `spark.sql("…")`. If the string appears in the argument of these constructs, re-analyze the content as SQL of the same dialect. Concatenation (`'SELECT * FROM ' + @tab`) is only partially covered: the literal pieces are masked and the variable is not resolved.
- **Name inside a literal:** `OBJECT_ID('dbo.tb_x')`, `IDENTIFIER('db.sch.t')`, `'sch.t'::regclass`, `nextval('seq')`, `WHERE table_name = 'tb_x'` over `INFORMATION_SCHEMA`. Mask the content of the literal by the implicit type and keep the quotes.
- **Comments** (`--`, `/* */`, `#` in MySQL/BigQuery, `REM` in SQL*Plus): there is no grammar inside them. Only replace names **already in the map** of the session, comparing whole words. Never infer a new name from a comment.
- **Table alias:** it is local to the query, but `AS clientes_inadimplentes` reveals meaning. Rule: an alias of up to 3 characters (`p`, `t1`) stays; a longer alias is masked (prefix `A_`). A column alias always becomes `C_`, with the same pseudonym if it coincides with a real column.
- **CTE:** `WITH base_vendas AS (…)` becomes `CTE_`, and each later reference in `FROM base_vendas` uses the same pseudonym (resolve the scope before treating it as a table).
- **Non-reserved word used as a name** (`status`, `name`, `date`, `comment`, `user`, `INDEX` in PG, `VALUE`): the position decides. In a column list, `t.status` or `SET status =`, it is a column; in a type position (`CREATE TABLE t (dt date)`), `date` is a type and stays. A reserved word only appears as a name if it is quoted (`"select"`, `[order]`, `` `group` ``) or after `.` (MySQL, BigQuery). In those cases mask it keeping the quoting.
- **Variables and parameters** (`@x`, `:x`, `$1`, `?`, `v_total` in PL/SQL): they are not database objects. They stay, unless the name is already in the map.
- **Semi-structured fields** (`col:cliente.cpf` in Snowflake, `STRUCT.field` in BigQuery, `col->>'k'` in PG): the 1st part is a column and the rest are data keys. The keys are left to the JSON detector [phase 2].
- **Qualification ambiguity:** `a.b` can be `schema.table`, `table.column` or `alias.column`. Resolve by the position (FROM → object; SELECT/WHERE → `qualifier.column`) and by the aliases already declared in the statement.
- **Truncated SQL** (cut output, log `LIMIT`): the lexer tolerates an unclosed string, quote or comment and treats the rest as a literal until the end of the block. What is not classified still goes through the replacement by the known map. Fail-safe: when in doubt about a token in a name position, mask.
- **Tabular result:** values under the headers `TABLE_NAME`, `TABLE_SCHEMA`, `COLUMN_NAME`, `name` (from `SHOW …`) are object names. The column names in the header of a result are columns too.
- **Limits:** without a catalog there is no way to tell an unqualified user function from an unknown built-in function (`fn_calc(x)` outside the built-in list becomes `FN_`, accepting a false positive). Extensions (PostGIS, package UDFs) enlarge the built-in list. When MySQL's case rule depends on `lower_case_table_names`, treat it as case-sensitive.

### 7. Links used

- https://learn.microsoft.com/en-us/sql/t-sql/language-elements/reserved-keywords-transact-sql
- https://learn.microsoft.com/en-us/sql/relational-databases/databases/database-identifiers
- https://docs.oracle.com/en/database/oracle/oracle-database/23/sqlrf/Database-Object-Names-and-Qualifiers.html
- https://docs.oracle.com/en/database/oracle/oracle-database/23/sqlrf/Oracle-SQL-Reserved-Words.html
- https://www.postgresql.org/docs/current/sql-keywords-appendix.html
- https://www.postgresql.org/docs/current/sql-syntax-lexical.html
- https://dev.mysql.com/doc/refman/8.4/en/keywords.html
- https://dev.mysql.com/doc/refman/8.4/en/identifiers.html
- https://www.ibm.com/docs/en/db2/11.5?topic=sql-reserved-schema-names-reserved-words
- https://docs.snowflake.com/en/sql-reference/reserved-keywords
- https://docs.snowflake.com/en/sql-reference/identifiers-syntax
- https://cloud.google.com/bigquery/docs/reference/standard-sql/lexical
- https://spark.apache.org/docs/latest/sql-ref-identifier.html
- https://spark.apache.org/docs/latest/sql-ref-ansi-compliance.html

Checked in this research: the pages of T-SQL reserved, Snowflake reserved and identifiers, PostgreSQL Appendix C, MySQL keywords, Oracle object names, Db2 reserved, Spark identifiers and BigQuery lexical (this one by searching the official domain). The other links and the counts marked "approx." (functions, `V$RESERVED_WORDS`, `pg_get_keywords`) come from knowledge of the documentation and were not opened now `[VERIFY]`.


## Database client output

Goal: recognize, by the shape of the text, the output of a database client and know in which
**column** or **span** an object name is, so that only that name is replaced with a typed pseudonym.
Legend: **[DOC]** = confirmed in the official documentation in this research; **[VERIFY]** = a
format that is known but not confirmed in the docs (the page did not load or does not bring the
literal text). Those items need a real sample before becoming a rule.

### 1. Detection signals

General rule: one signal alone is not enough. Require **frame + known header**, or **frame + footer**,
or an **error prefix**. Evaluate the whole block (consecutive lines), not the loose line.

| Client / format | Frame | Footer / marker |
|---|---|---|
| psql `aligned` (default) [DOC] | header, `----+----` line, columns separated by ` \| ` (border 1) | `(N rows)` / `(1 row)`; gone with `\pset footer off` or `-t` |
| psql `border 2` [DOC] | `+----+` frame above and below, `\|` on the borders | same |
| psql `\d…` [not confirmed → tolerant rule] | centred title `List of relations`, `List of schemas`, `List of databases`, `List of roles`; `\d tb` → `Table "sch.tb"` | `Indexes:`, `Foreign-key constraints:`, `Referenced by:` |
| psql `unaligned` (`-A`) [DOC] | default separator `\|`, no padding spaces; `-F` changes it | `(N rows)` remains, unless `-t` |
| psql `csv` (`--csv`) [DOC] | RFC 4180, comma, header on the 1st line | **no** title and no footer |
| psql `wrapped` [DOC] | same as aligned, but a long value wraps into several lines | `(N rows)` |
| sqlcmd [DOC ✓] | header, a `-----` dash line per column separated by a space (or by `-s`) | `(N rows affected)`; `-h -1` removes the header, `-W` removes trailing spaces |
| sqlplus [DOC ✓] | header, a `-----` line per column; the header repeats every `PAGESIZE` | `N rows selected.` / `no rows selected` |
| sqlplus `DESCRIBE` | fixed header `Name  Null?  Type` | — |
| snowsql (`output_format=psql` default) [DOC] | `+----+` frame in the psql border 2 style | a line under the table with the number of rows and the time (`timing=True`); literal text `N Row(s) produced. Time Elapsed:` [DOC ✓] |
| snow sql (Snowflake CLI) [not confirmed → tolerant rule] | framed table (rich/box), or `--format json` | — |
| bq `--format=pretty` [DOC formats] | `+---+` frame | no count footer [not confirmed → tolerant rule] |
| bq `sparse` / `csv` / `json` / `prettyjson` [DOC] | sparse: header + dashes; csv with a header; json | — |
| bq `ls` / `show` (default) [not confirmed → tolerant rule] | `tableId  Type  Labels  Time Partitioning`; `show`: `Last modified  Schema  Total Rows ...` | — |
| db2 CLP [DOC ✓] | header + dashes per column | `N record(s) selected.` |
| mysql client (table) [DOC ✓] | `+------+` frame | `N rows in set (0.00 sec)`, `Empty set`; `Query OK, N rows affected` |
| mysql `\G` [DOC ✓] | `*************************** 1. row ***************************` and right-aligned `column: value` lines | `N rows in set` |
| Error message | prefix: `ERROR:`, `Msg NNN, Level`, `ORA-NNNNN:`, `ERROR NNNN (SSSSS):`, `SQL compilation error`, `Not found:` / `notFound` | — |

### 2. Position → entity type

**Column headers** (compare case-insensitively; the **value** of the column is the name to mask):

| Header | Entity | Origin |
|---|---|---|
| `table_catalog`, `catalog_name`, `routine_catalog`, `database_name` | DB | information_schema [DOC]; Snowflake SHOW [DOC] |
| `table_schema`, `schema_name`, `routine_schema`, `specific_schema`, `Schema`, `OWNER` (Oracle), `TABSCHEMA` (Db2) | SCH | same; Oracle/Db2 [DOC ✓] |
| `table_name`, `Name` (with `Type` = table/view), `TABLE_NAME`, `TABNAME`, `tableId`, `Tables_in_<db>` | T | same |
| `view_name` (Oracle `ALL_VIEWS`) [DOC example] | T (view) | ORA-00942 |
| `column_name`, `COLNAME`, `Field` (mysql SHOW COLUMNS), `Name` in sqlplus DESCRIBE | C | [DOC] for column_name |
| `routine_name`, `specific_name` | PROC | information_schema [DOC] |
| `domain_name`, `udt_name` when it is **not** a built-in type | user-defined TYPE | [DOC] |
| `Key_name`, `key`, `possible_keys` (mysql), `INDEX_NAME` | IDX | [DOC ✓] |
| `owner`, `schema_owner`, `Owner` (psql), `Role name`, `GRANTEE`, `User`, `Host` | USR / ROLE / HOST | Snowflake `owner` [DOC] |

Gotchas: (a) in MySQL `SHOW TABLES` the **database name is inside the header**
(`Tables_in_vendas_demo`) — mask the suffix and keep `Tables_in_`. (b) `Name` is generic: in `\dt`
it is a table, in `\dn` a schema, in `\l` a database, in sqlplus `DESCRIBE` a column — decide by the **title**
or by the set of neighbouring headers. (c) The value of `data_type`, `Type`, `kind`, `table_type` is **never** a name.

**Spans of an error message** (the group between quotes is the name):

| Message | Where the name is | Entity |
|---|---|---|
| PostgreSQL 42P01 [DOC code] — `relation "X" does not exist` [DOC ✓] | `"X"` (may come as `sch.tb`) | T |
| PostgreSQL 42703 — `column "X" does not exist`; 3F000 `schema "X" does not exist`; 3D000 `database "X" does not exist`; 42883 `function X(...) does not exist` [not confirmed → tolerant rule] | between quotes / before `(` | C / SCH / DB / PROC |
| SQL Server 208 [DOC] — `Invalid object name '%.*ls'.` | between single quotes, 1 to 4 parts | T |
| SQL Server header `Msg 208, Level 16, State 1, Server srv-exemplo-01, Line 1` [not confirmed → tolerant rule] | after `Server ` up to `,` | SRV |
| Oracle ORA-00942 [DOC] — 26ai: `table or view SCHEMA.OBJECT_NAME does not exist`; 19c/21c: no name | between `view ` and ` does not exist` | SCH.T |
| Oracle ORA-00904 [DOC template] — `identifier: invalid identifier`, in practice `"COL"` or `"ALIAS"."COL"` | before `: invalid identifier` | C |
| MySQL 1146 — `Table 'db.tb' doesn't exist`; 1054 `Unknown column 'c' in 'field list'`; 1049 `Unknown database 'db'` [DOC ✓] | between single quotes | DB.T / C / DB |
| MySQL 1045 — `Access denied for user 'u'@'h'` [DOC ✓] | both quoted parts | USR / HOST |
| Snowflake 002003 — `Object 'DB.SCH.TB' does not exist or not authorized.` [DOC ✓] | between single quotes | DB.SCH.T |
| BigQuery `notFound` [DOC] — `Not Found: Dataset myproject:foo`; table: `Not found: Table proj:ds.tb` [not confirmed → tolerant rule] | after `Dataset `/`Table ` | PROJ:DS(.T) |

**Execution plans** [phase 2]: PostgreSQL `Seq Scan on tb`, `Index Scan using idx on tb t`
(name after `on`/`using`, alias afterwards); SQL Server showplan `OBJECT:([db].[sch].[tb].[idx])`;
MySQL `EXPLAIN` columns `table`, `key`, `possible_keys`; Oracle plan with the column `Name` (object)
next to `Operation` (`TABLE ACCESS FULL`, vocabulary).

### 3. Public vocabulary (never mask)

| Set | Examples | Source and how to extract |
|---|---|---|
| information_schema column names | `table_catalog`, `table_schema`, `table_name`, `column_name`, `ordinal_position`, `is_nullable`, `data_type`, `routine_name`... | postgresql.org chapter "The Information Schema": scrape the names in `<code>` from the tables of each view; or run `SELECT table_name, column_name FROM information_schema.columns WHERE table_schema='information_schema'` on an empty database |
| Snowflake SHOW columns | `created_on`, `name`, `database_name`, `schema_name`, `kind`, `owner`, `rows`, `bytes`, `retention_time`... | docs.snowflake.com, "Output" section of each `SHOW <object>` (lowercase) |
| Footer and title words | `rows`, `row`, `rows affected`, `rows selected`, `record(s) selected`, `rows in set`, `Empty set`, `Row(s) produced`, `Time Elapsed`, `List of relations`, `Indexes`, `Referenced by` | fixed text of the client; extract from each client's pages |
| Data types | `integer`, `varchar`, `NUMBER`, `VARCHAR2`, `nvarchar`, `TIMESTAMP_NTZ`, `STRING`, `INT64` | "Data types" pages of each DBMS |
| "Object type" values | `table`, `view`, `sequence`, `BASE TABLE`, `VIEW`, `PROCEDURE`, `FUNCTION`, `T`/`V` (Db2) | docs of the `table_type`/`kind` column |
| Reserved words and plan operations | `SELECT`, `Seq Scan`, `Hash Join`, `TABLE ACCESS FULL`, `Clustered Index Scan` | official keyword lists (PostgreSQL appendix C, T-SQL Reserved Keywords, Oracle V$RESERVED_WORDS, MySQL Keywords) |
| System schemas and databases | `information_schema`, `pg_catalog`, `public`, `dbo`, `sys`, `SYSIBM`, `SYSCAT`, `mysql`, `performance_schema`, `INFORMATION_SCHEMA` (BQ/Snowflake), `SNOWFLAKE` | docs of each DBMS; they do not identify the client |

Extraction: generate the lists once, with version and URL, in a data file of the proxy; never add
a language dictionary. Case-insensitive comparison.

### 4. Identifier rules

- **Quoting in the message**: PostgreSQL uses double quotes `"x"`; SQL Server and MySQL, single quotes `'x'`;
  Oracle, double quotes per part (`"E"."SALARY"`); Snowflake, single quotes around the whole name;
  BigQuery, no quotes, with `project:dataset.table` (or `project.dataset.table` in SQL). SQL Server in a
  plan uses brackets `[x]`; MySQL in SQL uses backticks `` `x` ``.
- **Qualified name**: split on `.` (and `:` in BigQuery) and mask **each part** with its type
  by position: 4 parts SQL Server = SRV.DB.SCH.T; 3 parts Snowflake = DB.SCH.T; 2 parts = SCH.T.
  System parts (item 3) stay: `dbo.tb_pedido_x9` → `dbo.T_cszwa3ri`.
- **Letter case**: Oracle and Snowflake store unquoted names in UPPERCASE; PostgreSQL in lowercase; SQL Server
  depends on the collation (docs of error 208: `CS` is case-sensitive) [DOC]. The pseudonym map must be
  **case-insensitive** for the same object (`TB_PEDIDO_X9` and `tb_pedido_x9` → the same `T_cszwa3ri`) and return the
  pseudonym in the case of the original (`T_A8F1` in uppercase text) so reading is not broken.
- **Valid characters** unquoted: an initial letter, then alphanumerics, `_`, `$`, `#` (Oracle [DOC]);
  quoted, anything — the delimiter decides the end, not the character class.
- **Consistency**: the same name seen in a table header, in an error and in a plan gets the same
  pseudonym in the whole session.

### 5. Before/after examples

Pseudonyms: `DB_2vmfnowr` (vendas_demo), `SCH_mvrhafae` (financeiro), `T_cszwa3ri` (tb_pedido_x9),
`C_xsdgeq7l` (vl_total), `HOST_kdekbkuz` (srv-exemplo-01).

psql `aligned` (`\dt financeiro.*`) — **realigning is necessary** (widths change):
```
before                                  after
         List of relations                       List of relations
   Schema   |     Name     | Type  | Own    Schema  |  Name  | Type  | Own
------------+--------------+-------+----   ----------+--------+-------+----
 financeiro | tb_pedido_x9 | table | ...    SCH_mvrhafae | T_cszwa3ri | table | ...
(1 row)                                   (1 row)
```
Strategy: parse the cells by the position of the `+` in the dash line, replace, recompute the width
of each column (max. between header and values) and redraw. A simpler alternative: pad the
pseudonym with spaces up to the original width (it only works if pseudonym ≤ original; if it is longer,
redraw).

psql `csv` / `unaligned` — **no realignment**:
```
table_schema,table_name,column_name     ->  table_schema,table_name,column_name
financeiro,tb_pedido_x9,vl_total        ->  SCH_mvrhafae,T_cszwa3ri,C_xsdgeq7l
```

MySQL `SHOW TABLES` — name in the header, realign the frame:
```
before                           after (frame redrawn)
+-----------------------+        +-------------------+
| Tables_in_vendas_demo |        | Tables_in_DB_x7k2 |
+-----------------------+        +-------------------+
| tb_pedido_x9          |        | T_cszwa3ri            |
+-----------------------+        +-------------------+
1 row in set (0.00 sec)          1 row in set (0.00 sec)
```

Errors — no alignment to preserve:
```
ERROR:  relation "financeiro.tb_pedido_x9" does not exist   -> relation "SCH_mvrhafae.T_cszwa3ri" ...
Msg 208, Level 16, State 1, Server srv-exemplo-01, Line 1   -> Server HOST_kdekbkuz, Line 1
Invalid object name 'financeiro.tb_pedido_x9'.              -> 'SCH_mvrhafae.T_cszwa3ri'.
ORA-00904: "VL_TOTAL": invalid identifier                   -> "C_P2V6": invalid identifier
SQL compilation error: Object 'VENDAS_DEMO.FINANCEIRO.TB_PEDIDO_X9' does not exist or not authorized.
                                                            -> 'DB_X7K2.SCH_Q3M9.T_A8F1'
```

### 6. Hard cases and limits

- **Truncation**: sqlplus cuts a value to the size of `COLUMN ... FORMAT A10` or `LINESIZE`; mysql and
  snowsql may abbreviate with `...`. A truncated prefix (`tb_pedid`) does not match the map. Rule: if the
  cell ends at the column border with no space, or with `…`/`...`, mask the whole cell with a
  new pseudonym marked as truncated and **never** return partial original text.
- **Line wrapping**: psql `wrapped` wraps the value into several lines with `+`/`.` at the end [DOC ✓
  marker]; sqlplus with `WRAP ON` continues on the next line of the same column; sqlcmd and sqlplus repeat
  the header on every page. Join the fragments per column before masking.
- **Fixed width without a frame** (sqlcmd, sqlplus, db2, bq sparse): columns are defined by the dash
  line; use the positions of the groups of `-` as the limits. Without the dash line (`-h -1`, `HEADING OFF`),
  there is no header → the column cannot give the type; fall back only on the error/qualification signals or **do not
  mask by structure** and mark it as low confidence.
- **Cut output** (the user pasted only a piece): without header and footer there is no way to know the type.
  Require at least the header; orphan lines are only handled if the session already saw the header before.
- **`-W` and `-s` in sqlcmd**: they remove the alignment and the separator becomes any character; detect the
  separator from the header line (a character repeated between known names).
- **A value that contains the separator**: in psql `unaligned` the `|` inside the value is not escaped [DOC];
  prefer CSV when possible; in unaligned, check the number of fields against the header.
- **Ambiguous `Name` column** and translated headers (clients with a locale; e.g. psql in Portuguese shows
  `Esquema | Nome | Tipo | Dono`) [phase 2]: the technical list must include the translated texts of the
  client's own messages (PostgreSQL's public `.po` files), not a language dictionary.
- **ORA-00942 in 19c/21c** does not bring the name — nothing to mask; do not try to guess from the previous query.
- **JSON** (`bq --format=json`, `snow sql --format json`): handle it by the **keys** (`tableId`,
  `datasetId`, `projectId`, `name`, `schema_name`) and not by the frame.
- **Data values** in columns that are not object columns (e.g. `comment`, `view_definition`,
  `routine_definition`) contain free SQL: hand them to the SQL detector of the other family, not to this one.

### 7. Links used

- psql (formats, border, footer, fieldsep): https://www.postgresql.org/docs/current/app-psql.html
- SQLSTATE: https://www.postgresql.org/docs/current/errcodes-appendix.html
- information_schema.columns: https://www.postgresql.org/docs/current/infoschema-columns.html
- SQL Server error 208: https://learn.microsoft.com/en-us/sql/relational-databases/errors-events/mssqlserver-208-database-engine-error
- sqlcmd: https://learn.microsoft.com/en-us/sql/tools/sqlcmd/sqlcmd-utility (page loaded, options `-s -W -h` not extracted → [DOC ✓])
- ORA-00942: https://docs.oracle.com/en/error-help/db/ora-00942/
- ORA-00904: https://docs.oracle.com/en/error-help/db/ora-00904/
- Snowflake SHOW TABLES: https://docs.snowflake.com/en/sql-reference/sql/show-tables
- SnowSQL config (`output_format`, `header`, `timing`): https://docs.snowflake.com/en/user-guide/snowsql-config
- BigQuery errors (`notFound`): https://docs.cloud.google.com/bigquery/docs/error-messages
- bq CLI (`--format`): https://docs.cloud.google.com/bigquery/docs/reference/bq-cli-reference
- Pending reading (they did not load in this research): MySQL server error reference
  (https://dev.mysql.com/doc/mysql-errors/8.4/en/server-error-reference.html), Db2 LIST TABLES
  (ibm.com/docs), sqlplus DESCRIBE/SET (docs.oracle.com, SQL*Plus User's Guide), Snowflake CLI `snow sql`.


## Connection strings and addresses

Central idea: a connection string is text **with a grammar**. The detector does not guess whether a
word "looks like a name"; it recognizes the format (`xxx://` scheme, `jdbc:` prefix,
`key=value;` pairs, TNS parentheses, URN tuple), locates the **position** and masks the value
by the **type of the position**. The keys (public vocabulary) are never masked.

### 1. Detection signals

Two major formats, plus two special ones:

| Family | Structural signal (entry regex, simplified) |
|---|---|
| URI (RFC 3986) | `\b[a-z][a-z0-9+.-]*://` with a scheme from the list: `postgresql`, `postgres`, `mysql`, `mongodb`, `mongodb+srv`, `s3`, `s3a`, `s3n`, `gs`, `abfs`, `abfss`, `wasb`, `wasbs`, `hdfs`, and `dialect+driver://` (SQLAlchemy) |
| JDBC | `\bjdbc:(sqlserver|oracle:thin|postgresql|mysql(\+srv)?(:loadbalance|:replication)?|db2|snowflake|bigquery):` |
| Key=value pairs | ≥2 `key=value` pairs separated by `;` (ODBC/ADO.NET) or by a space (libpq), **and** at least one key from the vocabulary (section 3). Without a known key, it does not fire. |
| TNS (Oracle Net) | `(DESCRIPTION=` or `(ADDRESS=` with `(HOST=`; `name = (DESCRIPTION=...)` in tnsnames.ora |
| DataHub URN | `urn:li:(dataset|corpuser|corpGroup|dataJob|dataFlow|schemaField|dataPlatform):` |

Boundary of the string: it ends at a space, quotes, `` ` ``, an unbalanced `)`, `<`, `>` or the end
of the line. For ODBC/ADO.NET, a value between `{}` (ODBC) or quotes (ADO.NET) may contain `;`.

### 2. Position → entity type

Pseudonyms: `HOST_` server/host/account, `DB_` database/catalog/service/SID, `SCH_` schema,
`USR_` user/role, `BKT_` bucket/container/storage account, `PTH_` path/object,
`SVC_` service/DSN/application/cluster/warehouse, `PRJ_` GCP project.

| Format | Position | Entity |
|---|---|---|
| Generic URI | `userinfo` before the `@` (the part before `:`) | USR |
| Generic URI | `host` (reg-name; an IP is left to the IP detector) | HOST |
| Generic URI | `port` | **never** |
| Generic URI | 1st segment of the path | DB (postgres, mysql, mongo, SQLAlchemy) |
| SQLAlchemy | `dialect+driver` | **never** (vocabulary) |
| libpq k=v / query | `host`, `hostaddr` / `dbname` / `user` / `service` / `application_name` | HOST / DB / USR / SVC / SVC |
| MongoDB | list `h1:p,h2:p`; `/defaultauthdb`; `authSource`; `replicaSet`; `appName` | HOST each; DB; DB; SVC; SVC |
| JDBC sqlserver | `//server\instance:port`; `databaseName`/`database`; `user`; `serverName`; `instanceName`; `applicationName`; `hostNameInCertificate` | HOST\SVC; DB; USR; HOST; SVC; SVC; HOST |
| JDBC oracle | `@host:port:SID`, `@//host:port/service`, `@tcp[s]:h1,h2:port/service`, `@tns_alias`, `user/password@` | HOST, DB, SVC (alias), USR |
| TNS | `HOST=`; `SERVICE_NAME=`/`SID=`/`INSTANCE_NAME=`/`GLOBAL_NAME=`; the name to the left of the `=` | HOST; DB; SVC |
| JDBC mysql | hosts (also `address=(host=..)` and `(host=..,port=..)`); `/database`; `user` | HOST; DB; USR |
| JDBC db2 | `//server:port/DATABASE:user=..;` | HOST; DB; USR |
| JDBC snowflake | `//<account>.snowflakecomputing.com`; `db`; `schema`; `warehouse`; `role`; `user` | HOST (only the account); DB; SCH; SVC; USR; USR |
| JDBC bigquery | `ProjectId`, `AdditionalProjects`; `DefaultDataset`; `OAuthServiceAcctEmail`; `OAuthPvtKeyPath` | PRJ; DB; USR; PTH |
| ODBC | `DSN`, `FILEDSN`, `SAVEFILE`; `Server`; `Database`; `UID` | SVC, PTH, PTH; HOST; DB; USR |
| ADO.NET | `Data Source`/`Server`/`Address`/`Addr`/`Network Address`; `Initial Catalog`/`Database`; `User ID`/`UID`/`User`; `Failover Partner`; `Application Name`/`App`; `Workstation ID`/`WSID`; `AttachDBFilename` | HOST(\SVC); DB; USR; HOST; SVC; HOST; PTH |
| s3/s3a/gs | `s3://<bucket>/<key>` | BKT; PTH per segment |
| abfs[s]/wasb[s] | `abfss://<container>@<account>.dfs.core.windows.net/<path>` (wasbs: `.blob.`) | BKT (container); BKT (account, only the label); PTH |
| hdfs | `hdfs://<namenode>[:port]/<path>` (or a logical nameservice) | HOST; PTH |
| Dataset URN | `(urn:li:dataPlatform:<p>,<name>,<ENV>)` | p **never**; name = `db.schema.table` → DB.SCH.TAB per dot; ENV **never** |
| corpuser / corpGroup URN | `urn:li:corpuser:<id>` | USR |
| dataFlow / dataJob URN | `(orchestrator,flow_id,cluster)` / `(<flowUrn>,job_id)` | never; SVC; SVC (cluster is free text, e.g. prod); SVC |
| schemaField URN | `(<datasetUrn>,<field_path>)` | recursive on the dataset; column (type from the column detector) |

### 3. Public vocabulary (never mask)

Use only official, small lists; everything lives in a versioned data file, with the URL.

- **Schemes/prefixes**: the ones in section 1. SQLAlchemy: `postgresql+psycopg2`, `+pg8000`,
  `mysql+mysqldb`, `+pymysql`, `oracle+oracledb`, `+cx_oracle`, `mssql+pyodbc`, `+pymssql`,
  `sqlite` (docs.sqlalchemy.org). Rule: `dialect` and `driver` are `[a-z0-9_]+` before `://`.
- **ODBC keys (grammar)**: `DSN`, `FILEDSN`, `DRIVER`, `UID`, `PWD`, `SAVEFILE`; the others
  are "driver-defined" (`Server`, `Database`, `Encrypt`, `TrustServerCertificate`...).
- **ADO.NET keys**: the table of `SqlConnection.ConnectionString` (Data Source, Server,
  Initial Catalog, Database, User ID, Integrated Security, Trusted_Connection, Encrypt,
  TrustServerCertificate, ApplicationIntent, MultiSubnetFailover, Persist Security Info,
  Pooling, Min/Max Pool Size, Connect Timeout, Authentication, Network Library...).
- **libpq keys**: the "Parameter Key Words" table (host, hostaddr, port, dbname, user,
  passfile, sslmode, application_name, options, service, connect_timeout,
  target_session_attrs...). In the URI, the parameter name **must** be a valid key.
- **JDBC SQL Server**: "Setting the connection properties" (databaseName, user,
  integratedSecurity, authenticationScheme, encrypt, applicationName, instanceName...).
- **MySQL Connector/J** "Configuration Properties"; **Snowflake** JDBC (user, db, schema,
  warehouse, role, authenticator, privateKey..., plus any session parameter);
  **BigQuery** JDBC (ProjectId, OAuthType, OAuthServiceAcctEmail, OAuthPvtKeyPath,
  DefaultDataset, Location, EnableSession...); **MongoDB** "Connection String Options".
- **TNS**: DESCRIPTION_LIST, DESCRIPTION, ADDRESS_LIST, ADDRESS, PROTOCOL, HOST, PORT,
  CONNECT_DATA, SERVICE_NAME, SID, INSTANCE_NAME, SERVER, FAILOVER, LOAD_BALANCE, SDU...
- **Enumerated values (never)**: `true/false/yes/no/on/off/0/1/sspi`; `sslmode`
  (disable, allow, prefer, require, verify-ca, verify-full); `target_session_attrs`
  (any, read-write, read-only, primary, standby, prefer-standby); `ApplicationIntent`
  (ReadOnly, ReadWrite); `PROTOCOL` (tcp, tcps, ipc); `SERVER` (dedicated, shared, pooled);
  `readPreference` (primary, primaryPreferred, secondary, secondaryPreferred, nearest);
  `authMechanism` (SCRAM-SHA-256, SCRAM-SHA-1, MONGODB-X509, MONGODB-AWS, GSSAPI, PLAIN,
  MONGODB-OIDC); `authSource=admin`/`$external`; `OAuthType` 0–4; `(local)`, `localhost`, `.`.
- **DataHub ENV (FabricType, 17 values)**: DEV, TEST, QA, UAT, EI, PRE, STG, NON_PROD,
  PROD, CORP, RVW, PRD, TST, SIT, SBX, SANDBOX, CERT. **Platform**: the identifier
  after `dataPlatform:` (snowflake, bigquery, postgres, mssql, kafka, airflow...).
- **Value of `DRIVER=`**: never mask (e.g. `{ODBC Driver 18 for SQL Server}`); it is a product
  name, and the ODBC grammar separates it as a special case.
- **Cloud suffixes and regions**: see section 6.

How to extract: parse per format → list of `(key, value, offset)`; normalize the key
(case and spaces, see section 4); if the key is in the `key → type` map, mask the value with the
type; if the key is known but a "non-entity" (Encrypt, timeout), leave it; if the key is
unknown, leave the value (prefer not masking to breaking).

### 4. Identifier rules

- **RFC 3986**: `userinfo` and `reg-name` accept `unreserved / pct-encoded / sub-delims`;
  `%XX` must be **decoded before** comparing (same host = same pseudonym) and the
  pseudonym is emitted **without** characters that require encoding (`[A-Z0-9_]`), so the URI
  stays valid. Scheme and host are case-insensitive (RFC 3986 §3.1, §3.2.2); the path is not.
- **Mongo/MySQL/SQLAlchemy**: `: / ? # [ ] @` in the user/password come as `%XX`. MySQL requires
  encoding of `/ : @ ( ) [ ] & # = ?` and of the space in any part.
- **libpq k=v**: a space around `=` is optional; a value with a space comes between `'...'`, with
  `\'` and `\\` inside. A host starting with `/` is a Unix socket directory (type PTH, not HOST).
  `host`, `hostaddr` and `port` accept comma-separated lists: mask item by item.
- **ODBC**: the key is **not** case-sensitive; the value may be. A value between `{}` is passed
  intact (it may have `;`); a literal `}` inside braces is `}}` (JDBC SQL Server ≥ 8.4 likewise;
  `{;}` escapes `;`). The first occurrence of a repeated key wins: mask them all the same.
- **ADO.NET**: case-insensitive key; a value with `;`, `'` or `"` goes between double quotes (or single
  ones, if it contains `"`); a quote equal to the delimiter is doubled. `tcp:host,port`, `host\instance`,
  `np:\\host\pipe\name` and `(localdb)\inst`: separate prefix, host, instance and port.
- **JDBC SQL Server**: case-insensitive properties; `\` separates the instance.
- **MySQL**: property keys **are case-sensitive** (official docs); `[h1,h2]` is a sublist.
- **TNS**: case-insensitive keywords; values may have case (preserve the original on the real
  side, pseudonym always in uppercase).
- **Snowflake**: account `org-account` (up to 63 characters; `_` and `-` equivalent in the URL) or
  locator `xy12345[.region[.cloud]]`. Values in the query come URL-encoded.
- **Buckets**: S3 `[a-z0-9.-]{3,63}`, no `_` and no uppercase; GCS `[a-z0-9._-]`, 3–63
  (up to 222 with dots). Validating the format before masking avoids a false positive on `s3://${VAR}`.
- **DataHub URN**: `(`, `)` and `␟` (U+241F) are forbidden; `,` is forbidden inside a tuple field;
  special characters come URL-encoded (`first%20name`). Dataset name: split on
  `.` and mask each part with the type of the position (db/schema/table), keeping the dots.

### 5. Before/after examples

```
postgresql://svc_relatorio@db-exemplo-01.interno.exemplo:5432/vendas_demo?sslmode=require&application_name=etl_diario
postgresql://USR_4w6dj5i6@HOST_7rhd4fsp:5432/DB_gbzw6njk?sslmode=require&application_name=SVC_it27pom2

host=db-exemplo-01.interno.exemplo port=5432 dbname=vendas_demo user=svc_relatorio sslmode=verify-full
host=HOST_7rhd4fsp port=5432 dbname=DB_gbzw6njk user=USR_4w6dj5i6 sslmode=verify-full

jdbc:sqlserver://db-exemplo-01.interno.exemplo\INST01:1433;databaseName=vendas_demo;encrypt=true;integratedSecurity=true
jdbc:sqlserver://HOST_7rhd4fsp\SVC_tjhnxdvl:1433;databaseName=DB_gbzw6njk;encrypt=true;integratedSecurity=true

Driver={ODBC Driver 18 for SQL Server};Server=tcp:db-exemplo-01.interno.exemplo,1433;Database=vendas_demo;UID=svc_relatorio;Encrypt=yes
Driver={ODBC Driver 18 for SQL Server};Server=tcp:HOST_7rhd4fsp,1433;Database=DB_gbzw6njk;UID=USR_4w6dj5i6;Encrypt=yes

Data Source=db-exemplo-01.interno.exemplo;Initial Catalog=vendas_demo;Integrated Security=SSPI
Data Source=HOST_7rhd4fsp;Initial Catalog=DB_gbzw6njk;Integrated Security=SSPI

jdbc:oracle:thin:@//db-exemplo-01.interno.exemplo:1521/vendas_demo.interno.exemplo
jdbc:oracle:thin:@//HOST_7rhd4fsp:1521/DB_gbzw6njk

VENDAS_DEMO = (DESCRIPTION=(ADDRESS=(PROTOCOL=tcp)(HOST=db-exemplo-01.interno.exemplo)(PORT=1521))(CONNECT_DATA=(SERVICE_NAME=vendas_demo)))
SVC_3xwx6qbj = (DESCRIPTION=(ADDRESS=(PROTOCOL=tcp)(HOST=HOST_7rhd4fsp)(PORT=1521))(CONNECT_DATA=(SERVICE_NAME=DB_gbzw6njk)))

jdbc:snowflake://contaexemplo-lab01.snowflakecomputing.com/?user=svc_relatorio&db=vendas_demo&schema=bruto&warehouse=wh_carga
jdbc:snowflake://HOST_q6q2hfsb.snowflakecomputing.com/?user=USR_4w6dj5i6&db=DB_gbzw6njk&schema=SCH_nmnf5hmd&warehouse=SVC_or7lqk6i

jdbc:bigquery://https://www.googleapis.com/bigquery/v2:443;ProjectId=projeto-exemplo-123;OAuthType=3;DefaultDataset=vendas_demo
jdbc:bigquery://https://www.googleapis.com/bigquery/v2:443;ProjectId=DB_ky3aazqn;OAuthType=3;DefaultDataset=SCH_gbzw6njk

mongodb+srv://svc_relatorio@cluster-exemplo.interno.exemplo/vendas_demo?authSource=admin&replicaSet=rs-exemplo
mongodb+srv://USR_4w6dj5i6@HOST_fhoczv7p/DB_gbzw6njk?authSource=admin&replicaSet=SVC_o5rj6dbi

mysql+pymysql://svc_relatorio@db-exemplo-01.interno.exemplo/vendas_demo?charset=utf8mb4
mysql+pymysql://USR_4w6dj5i6@HOST_7rhd4fsp/DB_gbzw6njk?charset=utf8mb4

s3://bucket-exemplo-dados/bruto/vendas/2026/arquivo.parquet
s3://BKT_t6h4hj3h/BKT_vknckg5s/BKT_xxuwjapl/2026/BKT_u6vhnh6o.parquet
abfss://container-exemplo@contaexemplo.dfs.core.windows.net/bruto/vendas
abfss://BKT_t6h4hj3h@BKT_dreioxlg.dfs.core.windows.net/BKT_vknckg5s/BKT_xxuwjapl

urn:li:dataset:(urn:li:dataPlatform:snowflake,vendas_demo.bruto.pedidos,PROD)
urn:li:dataset:(urn:li:dataPlatform:snowflake,DB_gbzw6njk.SCH_nmnf5hmd.T_a4pg76j2,PROD)
urn:li:corpuser:svc_relatorio   →   urn:li:corpuser:USR_4w6dj5i6
```

The same real value → the same pseudonym in every family (the `vendas_demo` of the JDBC is the one of
the URN), otherwise the model loses the link between the texts.

### 6. Hard cases and limits

- **Password** (`PWD`, `Password`, `user:password@`, `oauthClientSecret`, `OAuthPvtKey`,
  `private_key_file_pwd`): it belongs to the secret detector. This detector only needs to **delimit**
  the password so it does not swallow the wrong `@`/`;` and does not mask the host along with it.
  Attention: in the texts collected for this research, the current secret detector pseudonymized the
  password **keyword itself** (in English) and even the 4-letter English verb that begins it, including
  in documentation prose. The key is public vocabulary; only the value is a secret.
- **Official cloud suffixes** (do not mask the suffix, mask the label to its left):
  `.snowflakecomputing.com` / `.snowflakecomputing.cn` (account = HOST; the region/cloud of the
  locator, e.g. `.us-east-2.aws`, is not); `.database.windows.net`, `.database.chinacloudapi.cn`,
  `.database.usgovcloudapi.net` (Azure SQL server); `.dfs.core.windows.net`,
  `.blob.core.windows.net` (storage account); `.mongodb.net` (cluster); `www.googleapis.com`
  (BigQuery endpoint, never). Official regions (`us-east-1`, `southamerica-east1`,
  `aws_us_west_2`) stay in the clear.
- **A host that is an internal FQDN**: mask the whole FQDN as one HOST (not piece by piece),
  except when it matches an official suffix from the list above.
- **Generic value in an entity position**: `Database=master`, `dbname=postgres`,
  `authSource=admin`, `schema=public`, `SID=ORCL`... are default names of the product. A short list
  per product (from the docs), in the clear; mask the rest.
- **Variables/placeholders**: `${DB_HOST}`, `{{ var }}`, `<host>`, `%(host)s`, `$PGHOST`:
  they are not a real value, do not mask (and do not confuse a template's `{...}` with ODBC braces).
- **Strings broken in the middle** (concatenation `"Server=" + host + ";"`, multi-line YAML,
  TNS over several lines): TNS needs multi-line parenthesis balancing; the others
  are left to the isolated-key detector (`Server=x` alone counts if the key is in the vocabulary).
- **IPv6 and IP**: `[2001:db8::1]` between brackets; an IP is left to the IP detector.
- **`hdfs://nameservice1/`**: a logical nameservice also becomes HOST.
- **Ambiguity with a web URL**: `https://` is not of this family, except inside the BigQuery JDBC.
- **Reversal**: the proxy needs to return the original in the response; a pseudonym with fixed case
  and format (`[A-Z]{2,4}_[a-z0-9]{4}`) makes the reverse search safe inside URIs.
- No evidence in the docs: the complete list of third-party Hadoop schemes (`oss://`, `cos://`)
  `[VERIFY]`; the Simba BigQuery docs exist only as a PDF in the vendor's bucket `[not confirmed → tolerant rule]`.

### 7. Links used

- RFC 3986: https://www.rfc-editor.org/rfc/rfc3986
- ODBC SQLDriverConnect (grammar): https://learn.microsoft.com/en-us/sql/odbc/reference/syntax/sqldriverconnect-function
- ODBC Connection Strings: https://learn.microsoft.com/en-us/sql/odbc/reference/develop-app/connection-strings
- ADO.NET SqlConnection.ConnectionString: https://learn.microsoft.com/en-us/dotnet/api/system.data.sqlclient.sqlconnection.connectionstring
- JDBC SQL Server URL: https://learn.microsoft.com/en-us/sql/connect/jdbc/building-the-connection-url
- libpq: https://www.postgresql.org/docs/current/libpq-connect.html
- Oracle JDBC URLs: https://docs.oracle.com/en/database/oracle/oracle-database/19/jjdbc/data-sources-and-URLs.html
- Oracle tnsnames.ora: https://docs.oracle.com/en/database/oracle/oracle-database/19/netrf/local-naming-parameters-in-tns-ora-file.html
- MySQL Connector/J URL: https://dev.mysql.com/doc/connector-j/en/connector-j-reference-jdbc-url-format.html
- Db2 JDBC type 4: https://www.ibm.com/docs/en/db2/11.5.x?topic=cdsudidsdjs-url-format-data-server-driver-jdbc-sqlj-type-4-connectivity
- Snowflake JDBC: https://docs.snowflake.com/en/developer-guide/jdbc/jdbc-configure
- Snowflake account identifier: https://docs.snowflake.com/en/user-guide/admin-account-identifier
- BigQuery JDBC: https://docs.cloud.google.com/bigquery/docs/jdbc-for-bigquery
- BigQuery ODBC/JDBC (Simba): https://docs.cloud.google.com/bigquery/docs/reference/odbc-jdbc-drivers
- SQLAlchemy URLs: https://docs.sqlalchemy.org/en/20/core/engines.html
- MongoDB: https://www.mongodb.com/docs/manual/reference/connection-string/ and .../connection-string-options/
- S3 bucket naming: https://docs.aws.amazon.com/AmazonS3/latest/userguide/bucketnamingrules.html
- GCS buckets: https://docs.cloud.google.com/storage/docs/buckets
- ABFS URI: https://learn.microsoft.com/en-us/azure/storage/blobs/data-lake-storage-introduction-abfs-uri
- DataHub URN: https://docs.datahub.com/docs/what/urn/
- DataHub Dataset (FabricType): https://docs.datahub.com/docs/generated/metamodel/entities/dataset
- DataHub SchemaField / DataJob: https://docs.datahub.com/docs/generated/metamodel/entities/schemafield , .../datajob


### 8. Storage and queues: what is implemented (the `endereço` reader)

Code: `internal/mask/leitor_enderecos.go` (`urlEm`). Database URIs stay with the `conexão`
reader; these are the storage and queue ones:

| Scheme | Position | Entity | Strong? |
|---|---|---|---|
| `s3://`, `s3a://`, `s3n://`, `gs://`, `gcs://`, `az://`, `oss://` | authority | bucket | yes |
| `abfs[s]://<container>@<account>.dfs.core.windows.net`, `wasb[s]://...blob...` | container / 1st label of the host | bucket / conta_nuvem (cloud account) | yes |
| `hdfs://`, `webhdfs://`, `viewfs://` | namenode (outside a public domain) | servidor (server) | yes |
| all of the above | a piece of the path that looks like an identifier (not the final file, nor `key=value`, a wildcard or a variable) | pasta (folder) | no |
| `amqp[s]://user@host/vhost`, `mqtt[s]://`, `stomp://` | user / host(s) outside a public domain / 1st piece of the path | usuario / servidor / fila (user / server / queue) | yes |
| `kafka://broker:9092/topic`, `pulsar[+ssl]://`, `nats://`, `tls+nats://` | same | usuario / servidor / fila | yes |

What stays: the default `vhost` (`/`, `%2F`) and variables (`s3://$BKT/`). A name that looks like an
example (`my-bucket`, `example-bucket`) is masked like any other: it may be real.

```text
s3://bkt-relatorios-demo/carga_diaria/2026/arquivo.csv  →  s3://BKT_.../DIR_.../2026/arquivo.csv
amqp://svc_relatorio:***@rabbit-01.interno:5672/vhost_pedidos  →  amqp://usr_...:***@host_...:5672/top_...
kafka://broker-01:9092/fila-pedidos-x9  →  kafka://host_...:9092/top_...
```

## Tabular data

**How it is implemented** (`leitor_tabela.go`): the table is recognized by its shape. Separator
`,` `;` TAB or `|` with the same count outside quotes in the header and in the next line (CSV
according to RFC 4180: the URN with commas between quotes is a single cell); a dash line (with or
without borders, `+---+`, `|===|`, double `||`); columns aligned by spaces without a dash line
(each word of the header is a column; each value goes to the column it overlaps the most,
right- or left-aligned; what comes before the first column is the index; it requires a gap of
2+ spaces, a TAB or an indentation, so prose does not become a table); box borders arrive as `|`/`-` after
normalization. A line of types (`str`, `<chr>`, `varchar`), truncated lines (`...`) and footers
(`[N rows x M columns]`, `(2 rows)`) are not values. Also: lines that are tuples or lists of
literals (per line, a list of tuples, a list of lists), with the first one as the header when it names
some type; `<table><tr><th>/<td>`; vertical record `-[ RECORD n ]-` + `key | value`; and
a TAB line with an uppercase type label in front (`BUCKETS<TAB>date<TAB>name`). Loose
column: a type word alone on a line (`bucket`, `table_name`) followed by 2+ lines of one
item with the shape of a name, up to the empty line. A one-letter value is not a name. Columns aligned by
spaces do not count on lines with code punctuation (`{ } ; := // == ( `, ` = `): indented
code with gaps is not a table.

The type of each column comes from the header: the catalog names (`table_schema`, `relname`...) or
a type word in any spelling (the same rule as the key-value reader, `entChave`).
`name` is a table when there is a schema column, takes the type of the section title when the line above is
a type word (`Buckets`), and is a column when there is a `type` next to it with data types
(DESCRIBE). A header cell with more than one word only counts in a `|`, TAB or dash table, and
all in lowercase or all in uppercase (`primary key`, `APP VERSION`); `Data Type` does not. Outside
CSV/TSV, the header only becomes a column with `_` or a digit (`CreationDate` is a tool label).

Scope: text that is a printed table (CSV, TSV, output of pandas/Polars/DuckDB/pyarrow,
markdown table). Central idea: **the position inside the grid says what the cell is**. The header
may bring real column names. A data cell only becomes an object name when the header
of its column says so (table_name, column_name...). No rule depends on a language dictionary.

### 1. Detection signals

First detect the block (consecutive lines that form a grid), then the dialect:

| Dialect | Structural signal (all measured in the block itself) |
|---|---|
| CSV (RFC 4180) | >= 2 lines with the **same number** of fields separated by `,` (or `;`), counting the quotes: `"` opens and closes the field, `""` is a literal quote. No line ends in a comma |
| TSV (IANA) | >= 2 lines with the same number of `\t`. There are no quotes, because the field cannot contain a TAB. By IANA the **1st line is always the header** |
| Markdown (GFM) | a header line with `\|`, followed by a delimiter line made only of `-`, `:` and `\|`, with the **same number of cells**. It ends at the first blank line |
| Polars | `shape: (N, M)`, then a border `┌─┬─┐`, separator `┆` between columns, a `---` line and a line of dtypes (`i64`, `str`, `f64`) before `╞═╪═╡` |
| DuckDB duckbox/box | border `┌─┐`/`│`, name line, type line (`varchar`, `double`, `int64`...), then `├─┤` |
| pandas `print(df)`/`to_string` | 1st line with labels only; the following lines start with the index label (`0`, `1`... in a RangeIndex). Columns aligned by spaces, with the values **on the right** (default justify: right). It may have the footer `[N rows x M columns]` |
| pandas `df.info()` | `<class 'pandas.DataFrame'>`, `RangeIndex: N entries`, `Data columns (total N columns):`, a header ` #  Column  Non-Null Count  Dtype` and the line `---  ------` |
| pandas `describe()` / `dtypes` / Series | fixed labels at the start of the line (`count`, `mean`...) or the footer `dtype: <type>` / `Name: x, dtype: ...` |
| pyarrow `Table` | a `pyarrow.Table` line, then `name: type` lines, then `----`, then `name: [[v1,v2]]` |

Generic detection for space-aligned tables: there is a **column boundary** when the same
character position is blank in every line of the block (>= 2 lines, >= 2 columns).

**Header or data?** In order of strength:
1. **Explicit marker**: a delimiter line (GFM `---`, Polars `---`/`╞═╡`, DuckDB `├─┤`,
   `df.info()` `---  ------`). What comes before it is the header, always.
2. **A format that defines it**: IANA TSV (1st line = header). In a CSV the MIME `header=present|absent`
   decides, if it exists.
3. **pandas index**: the header line has no index label (it starts with spaces) and the
   data lines do. In a MultiIndex there is a 2nd line with only the names of the levels (`first second`).
4. **Type heuristic (CSV without a marker)**: the same criterion as Python's `csv.Sniffer.has_header`.
   A column "votes header" if lines 2..n are numeric and the 1st is not, or if the length
   differs from the candidate. It is a header when more than half of the columns vote so. Our own reinforcement: the
   cells of the 1st line have the shape of an identifier (section 4) and are **unique** among themselves.
   The official docs warn that this produces false positives and false negatives. If it does not settle, treat the block
   as headerless (section 6).

### 2. Position → entity type

| Position | Entity | Note |
|---|---|---|
| header cell (CSV/TSV/MD/Polars/DuckDB/pandas) | **COLUMN** (`C_`) | only if it has the shape of an identifier (section 4) and is not vocabulary (section 3) |
| pyarrow `name: type` line; ` N  name  ... Dtype` line of `df.info()`; 1st field of `df.dtypes` | **COLUMN** | the name sits between the number and the count, or before the `:` |
| `Name: x` in the footer of a Series | **COLUMN** | `x` is the name of the source column |
| index level name (`first second` line) | **COLUMN** | an index level usually comes from a column (set_index) |
| value in a column with the header `table_name`/`TABLE_NAME`/`table` | **TABLE** (`T_`) | the trigger header names come from INFORMATION_SCHEMA (public list) |
| value under `table_schema`/`schema_name`/`schema` | **SCHEMA** (`S_`) | same |
| value under `table_catalog`/`catalog_name`/`database`/`database_name` | **DATABASE** (`D_`) | same |
| value under `column_name`/`column` | **COLUMN** | and link it to the `T_` of the same row |
| value with the shape `a.b.c` in any trigger column | DATABASE.SCHEMA.TABLE | part by part |
| other data values | none here | left to the other families (e-mail, CPF...) |
| index label (`0`, `1`, dates) | none | it only becomes COLUMN/value if the index has a trigger name |

A trigger header in Portuguese (`tabela`, `coluna`, `objeto`) is **not** built into the code,
because it would be a language list: it comes in through the user's configuration. By default only the
INFORMATION_SCHEMA names (SQL standard) are included, compared case-insensitively.

### 3. Public vocabulary (never mask)

These are fixed words the tool prints. Without this list, `count` and `Dtype` would become columns.

| Origin | Words | Official source |
|---|---|---|
| `df.info()` | `RangeIndex`, `entries`, `Data`, `columns`, `total`, `#`, `Column`, `Non-Null Count`, `non-null`, `Dtype`, `dtypes:`, `memory usage:` | pandas DataFrame.info |
| `describe()` | `count`, `mean`, `std`, `min`, `25%`, `50%`, `75%`, `max`, `unique`, `top`, `freq` (and generated `NN%` percentiles) | pandas DataFrame.describe |
| pandas/NumPy dtypes | `int8..int64`, `uint*`, `float32/64`, `bool`, `object`, `str`, `string`, `category`, `datetime64[ns|us, tz]`, `timedelta64[..]`, `period[..]`, `interval[..]`, `Int64`, `Float64`, `boolean`, `Sparse[..]`, `Index`, `RangeIndex`, `MultiIndex`, `<NA>`, `NaN`, `NaT` | pandas basics + arrays |
| pandas footers | `[N rows x M columns]`, `dtype:`, `Name:`, `Length:` | pandas options / basics |
| Polars | `shape:`, `---`, `str`, `i8..i64`, `u8..u64`, `f32`, `f64`, `bool`, `date`, `datetime[..]`, `list[..]`, `struct[..]`, `null` | docs.pola.rs |
| DuckDB | type names (`varchar`, `integer`, `bigint`, `double`, `boolean`, `date`, `timestamp`, `int64`...) | duckdb.org (Data Types) |
| pyarrow | `pyarrow.Table`, `----`, `int8..int64`, `string`, `large_string`, `double`, `timestamp[..]`, `chunk` | arrow.apache.org |
| pandas without a header | `Unnamed: N` (unnamed column on reading) [VERIFY in the read_csv docs] | pandas read_csv |

**How to extract** (generated at build time and versioned, never by hand):
- pandas/NumPy: `pandas.api.types` and `np.sctypeDict.keys()` for the dtype aliases. The labels
  of `describe`/`info` come from running the functions on a synthetic DataFrame and tokenizing the output.
- Polars: `[str(t) for t in polars.datatypes]` and the printing of a synthetic DataFrame.
- DuckDB: `SELECT DISTINCT type_name FROM duckdb_types()`.
- pyarrow: the constructors of `pyarrow.types` printed with `str()`.

Pin the version of each library in the generator, because pandas 3 replaced `object` with `str` for text.

### 4. Identifier rules

- **Shape of an identifier** (candidate header cell): `^[A-Za-z_][A-Za-z0-9_$]*$`,
  with >= 3 characters **or** containing `_` or a digit. `A`, `B`, `x` and `0..N` (RangeIndex) stay
  as they are: they are generic labels and masking them only gets in the way.
- **Header with a space** (`Valor Total`, `Non-Null Count`): do **not** mask by default. In a
  database this only exists as a quoted identifier (`"Valor Total"`), and in a spreadsheet it is a
  human label. Exception: if the same text appears between identifier quotes (`"..."`, `` `...` ``,
  `[...]`) in SQL in the same prompt, the SQL family has already registered it and here the same pseudonym is just reapplied.
- **Header rule: mask all the ones that have the shape of an identifier and are not in the
  vocabulary.** Do not use "only the ones that look like a database name": that would require guessing by language.
  Justification of the risk: reading does not break, because the pseudonym is typed (`C_`), stable in the
  whole prompt and undone in the response. It would break if we masked generic labels or the
  vocabulary, and that is why these two lists exist.
- **Quotes in CSV**: strip the outer quotes, undo `""` → `"`, evaluate the content. When writing, apply
  quotes again only if the pseudonym requires it (it never does, because `T_cszwa3ri` is `[A-Za-z0-9_]`). If the
  original was between quotes, **keep the quotes**: the difference stays minimal and the CSV stays valid.
- **Compound identifier in a cell** (`financeiro.tb_pedido_x9`): split on `.` outside quotes and
  mask each part with its type.
- **Alignment**: in space/box tables, the column limits come from the header line and
  from the borders. Never search for the token with a loose regex in the text, always by the cell.
- **Consistency**: a name equal to one already seen (in SQL, YAML...) gets the same pseudonym, and
  takes the most specific type (if it is already `T_`, it does not become `C_`).

### 5. Before/after examples

CSV (realigning does not apply, the CSV stays valid and with the same number of fields):
```
before: id_pedido,vl_total,"dt_emissao",obs
        1,"10,50",2026-01-02,"diz ""ok"""
after:  C_4bszwltr,C_xsdgeq7l,"C_4uhd5lap",obs
        1,"10,50",2026-01-02,"diz ""ok"""
```
`obs` stayed because it has 3 characters with no `_` or digit, and falls under the generic label rule. Adjust
the minimum if you prefer. The value `"10,50"` is not an identifier: nothing changes.

Inventory (values masked because of the trigger header):
```
before: table_schema	table_name	column_name
        financeiro	tb_pedido_x9	vl_total
after:  table_schema	table_name	column_name
        SCH_tkrm6wg5	T_cszwa3ri	C_xsdgeq7l
```

pandas `df.info()` (it realigns: the width of the `Column` column is recomputed):
```
before:  #   Column      Non-Null Count  Dtype
        ---  ------      --------------  -----
         0   id_pedido   5 non-null      int64
         1   vl_total    5 non-null      float64
after:   #   Column  Non-Null Count  Dtype
        ---  ------  --------------  -----
         0   C_4bszwltr  5 non-null      int64
         1   C_xsdgeq7l  5 non-null      float64
```

Polars / markdown (it realigns, redrawing the borders and `---` at the new width):
```
before: │ id_pedido ┆ vl_total │        | id_pedido | vl_total |
        │ ---       ┆ ---      │        |----------:|---------:|
        │ i64       ┆ f64      │
after:  │ C_4bszwltr ┆ C_xsdgeq7l │              | C_4bszwltr | C_xsdgeq7l |
        │ ---    ┆ ---    │              |-------:|-------:|
        │ i64    ┆ f64    │
```
**Realign? Yes, whenever the dialect is positional** (space, box, Polars, DuckDB, info). The
pseudonym has a different length from the original, and replacing only the text shifts the columns to
the right, which leaves the model not knowing which value belongs to which column. Procedure: parse the
cells → replace → recompute the width of each column (the maximum among the cells) → redraw,
keeping the justification of each column (pandas: right; GFM: the `:` of the delimiter line). CSV, TSV
and markdown without visual alignment do not need to be realigned (only the cells change). On the way back
(unmasking the response) nothing is realigned.

### 6. Hard cases and limits

- **Missing header** (a pure data CSV, pandas `header=None`, which becomes `0 1 2`): only
  the value rules that do not depend on a header apply (`a.b.c` in the shape of an identifier). The first
  line is not masked as a column. Accepted false negative.
- **Truncation `...`/`..`** (pandas `max_rows`/`max_columns`, Polars `…`): the `...` column and the `..`
  row are padding, not data. The footer `[N rows x M columns]` holds. Part of the columns never
  shows up, which is not a problem because there is nothing to mask. Polars `…` [VERIFY: the Config
  docs do not describe the marker].
- **pandas width wrapping** (`expand_frame_repr=True` and the width exceeds `display.width`):
  the table comes out in several blocks of columns, and the header line of each block that continues
  ends in `\` [VERIFY: known behavior, but not shown on the official options page].
  Treat it as a single block: the index label repeats in the parts, and that is the signal to join them.
  Realign each part separately and preserve the `\`.
- **Cell with a comma/line break** (CSV): only a parser with quotes solves it. An unbalanced `"`
  inside the block invalidates the detection, and then the block gets no tabular mask (it falls to the other families).
- **Ambiguous delimiter** (`,` vs `;` vs space): choose the one that gives the same count in every
  line. A tie follows the order `, \t ; space :` (the Sniffer's `preferred`).
- **Value with a space in an aligned table** (`São Paulo`): splitting on a space breaks the cell.
  So use the vertical boundaries (a position that is blank in every line), never `split()`.
- **Sparse MultiIndex**: the blank cell repeats the label above, so it is not an empty cell.
  Multiple header lines (`first`/`second` in the columns) are all header.
- **Printed Excel/Parquet**: there is no "Excel text". It arrives as a DataFrame (`read_excel`/
  `read_parquet` and then the print), as a `pyarrow.Table`, as a DuckDB box or as a list of openpyxl
  tuples (`ws.iter_rows(values_only=True)`), which is already another family (Python literal). Detect
  by the output dialect, not by the origin. The sheet name (`sheet_name`) is a human label: do not mask.
- **Honest limit**: without an explicit marker, the header/data decision is a heuristic. Prefer not
  masking to masking wrongly in the grid, because the identifier can still be caught by another family.

### 7. Links used

- RFC 4180: https://www.rfc-editor.org/rfc/rfc4180
- IANA text/tab-separated-values: https://www.iana.org/assignments/media-types/text/tab-separated-values
- GFM, tables (§4.10): https://github.github.com/gfm/
- pandas DataFrame.info: https://pandas.pydata.org/docs/reference/api/pandas.DataFrame.info.html
- pandas DataFrame.describe: https://pandas.pydata.org/docs/reference/api/pandas.DataFrame.describe.html
- pandas DataFrame.to_string: https://pandas.pydata.org/docs/reference/api/pandas.DataFrame.to_string.html
- pandas options (truncation, width, footer): https://pandas.pydata.org/docs/user_guide/options.html
- pandas MultiIndex: https://pandas.pydata.org/docs/user_guide/advanced.html
- pandas dtypes: https://pandas.pydata.org/docs/user_guide/basics.html and https://pandas.pydata.org/docs/reference/arrays.html
- Polars, printed format: https://docs.pola.rs/user-guide/getting-started/
- Polars Config: https://docs.pola.rs/api/python/stable/reference/config.html
- DuckDB CLI output formats: https://duckdb.org/docs/current/clients/cli/output_formats.html
- Arrow (pyarrow.Table): https://arrow.apache.org/docs/python/getstarted.html
- Python csv.Sniffer: https://docs.python.org/3/library/csv.html


## JSON, YAML, TOML, INI, .env, .properties, XML and schema languages

**Schemas: name + data type** (`leitor_esquema.go`, a new reader: no other one reads this
shape). Where a name comes with a data type, the name is a column: `name    type` per line (2+
lines, or 1 with the footer `dtype: object`), decided by the type and not by the indentation (see
[Guards](#guards)), `Index(['a', 'b'], dtype='object')`,
` |-- name: string` (printSchema), `name: int64` at the start of the line (2+ lines: a code
annotation is indented and does not count), `string name = 1;` (protobuf) and a `name | type` table with data
types in the `type` column. The vocabulary is only that of data types (SQL, pandas/numpy, Arrow,
Polars, Spark, Avro, Protobuf, R: `vocab_tipos.go`). In YAML/JSON, `name` counts by its container
(the YAML/JSON engine, `acharNomePorContexto`): under `columns`/`fields` it is a column (dbt
`schema.yml`, Avro), under `tables`/`models`/`seeds`/`views` it is a table, under `sources`/`datasets`
it is a schema, and under a key that is a type word (`table`, `dataset`, `database`...) it is of that
type; `owner` is a user; a qualified value (`db.schema.table`) is split into its parts.

**Key → value in any syntax** (the key-value reader, `leitor_chave_valor.go`, and the code
reader, extended). The type word is recognized in any spelling: the key name is
split into pieces (camelCase, snake_case, kebab, dots) and also glued to `name`/`path`
(`dbname`, `rolename`, `warehousename`, `accountname`, `fieldPath`); new type words:
`column`/`coluna`/`field` (column), `role` (user), `view` (table), `procedure`/`routine`;
`serviceAccountName` is a user. New shapes: several `k=v` on the same line (2+ pairs separated
by a space, with no `(` or `, ` on the line, which would be named arguments); a list value on the
same line (`"tables": ["a", "b"]`, `tabelas: [a, b]`) and a YAML list right below the key
(each item is of the key's type, in the singular or the plural, including in Portuguese); `name=`
inside an XML tag counts by the tag (`<column name="x">`); a key with `__` around it
(`__tablename__`); `Key Value` separated by a space in a block of 2+ such lines (ssh config);
an `IP name [name...]` line (/etc/hosts): the names are servers.

Central idea: in these formats, what says what a token is is not the token but its **position**
(the value of which key, or the fact that it is a key of a map under a known parent). The detector
analyzes the structure and uses the vocabulary of the specification to separate what is fixed and public
(`type`, `properties`) from what is a name chosen by someone (`tb_pedido_x9`).

### 1. Detection signals

| Format | Structural signal (cheap, without a full parser) |
|---|---|
| JSON | starts with `{` or `[` (after whitespace); `"k":` pairs; a successful RFC 8259 parse confirms |
| YAML | `---` at the start of the document; `key: value` lines with consistent indentation; `- ` items; `&anchor` / `*alias` |
| TOML | `[table]` / `[[array]]` headers; `key = value` with a quoted string; dotted keys `a.b = ` |
| INI | `[section]` headers + `key=value` without mandatory quotes; `;` or `#` comment (no spec: it is convention) |
| .env | `NAME=value` lines, UPPERCASE names with `_`, optional `export `, no sections |
| .properties | `key=value`, `key: value` or `key value`; `#`/`!` comment; dotted keys `spring.datasource.url` |
| XML | `<?xml`, `<root ...>`, balanced tags, `xmlns=` |
| JSON Schema | key `$schema` with a `json-schema.org` URL, or `"type":"object"` + `"properties"` |
| OpenAPI 3 | key `openapi: 3.x.y` at the root, with `info` and `paths` |
| Avro (.avsc) | object with `"type":"record"`, `"name"`, `"fields":[...]` |
| Protobuf | `syntax = "proto3";`/`edition = "2023";`, `package x;`, `message X {` |
| GraphQL SDL | `type X {`, `input X {`, `enum`, `schema {`, `extend type` |

Decision rule: only activate the family when the parse of the span works (or of a prefix of it, see §6).
Without a parse, fall back to the free-text detector; do not "guess" keys.

### 2. Position → entity type

**(a) Name in the VALUE.** The key is matched by its normalized form: lowercase, without `_`/`-`/`.`, and
looking at the last segment (`spring.datasource.username` → `username`; `DB_HOST` → `host`).

| Last segment of the key (normalized) | Entity | Pseudonym |
|---|---|---|
| `host`, `hostname`, `server`, `servername`, `address`, `endpoint` | server/host | `HOST_` |
| `account` (Snowflake), `instance`, `cluster` | server/account | `HOST_` |
| `database`, `db`, `dbname`, `catalog`, `project` (GCP) | database | `DB_` |
| `schema`, `dataset`, `keyspace`, `namespace` (outside Avro/k8s, see §6) | schema | `S_` |
| `table`, `tablename`, `view`, `collection`, `relation` | table | `T_` |
| `column`, `columnname`, `field`, `partitionby`, `clusterby`, `primarykey` | column | `C_` |
| `user`, `username`, `uid`, `login`, `role`, `owner`, `serviceaccount` | user | `U_` |
| `service`, `servicename`, `app`, `application` | service | `SVC_` |
| `bucket`, `container`, `stage`, `location` | bucket | `B_` |
| `topic`, `queue`, `subscription`, `stream`, `channel`, `exchange` | queue/topic | `Q_` |
| `url`, `uri`, `jdbcurl`, `dsn`, `connectionstring` | compound: decompose (host, port, db, user) | several |
| credential keys (`pwd`, `senha`, `token`, `apikey`, `privatekey`) | **credential** (another family: mask the whole value) | `CRED_` |

**(b) Name in the KEY.** The key is a name when the parent is a "map of free names":

| Parent / context | The child keys are | Entity |
|---|---|---|
| JSON Schema `properties`, `patternProperties` (no: they are regexes), `$defs`, `definitions`, `dependentRequired` | field / type names | `C_` / `T_` |
| OpenAPI `components.schemas`, `components.parameters`, `components.responses` ... | type/component names | `T_` |
| OpenAPI `paths` | path: only the literal segments are candidates (`/pedidos/{id}`) | `SVC_`/`T_` |
| Column map (`columns:`, `fields:` as an object, `mapping:`) | column name; the value is the type | `C_` |
| INI/TOML section `[db_exemplo]`, `[tables.tb_pedido_x9]` | free segment of the header | according to the parent |
| XML element under a data root (`<tb_pedido_x9>`) | table/column name (only if it is not vocabulary of the XSD/namespace) | `T_`/`C_` |

Cross-check: a child key whose value is a scalar from the type vocabulary (`"decimal"`, `"string"`,
`{"type": ...}`) reinforces that the key is a column name.

### 3. Public vocabulary per format

A fixed, versioned list, extracted from the spec. Everything in the list is never masked in a key position.

| Format | Vocabulary (words of the spec itself) | Source / how to extract |
|---|---|---|
| JSON | only the literals `true`, `false`, `null` | RFC 8259 §3; there are no reserved keys |
| YAML 1.2 | `true/false/null/~`, tags `!!str !!int !!map !!seq`, `<<` (merge; it is a 1.1 type, but many parsers accept it) | spec 1.2.2 chapter 10 (Failsafe/JSON/Core schemas) |
| TOML | `true`, `false`, `inf`, `nan`; RFC 3339 dates | toml.io/en/v1.0.0, sections "Boolean", "Float" |
| INI | none (no spec) | — |
| .env | `export` | python-dotenv README; Docker Compose docs |
| .properties | none (syntax only) | Javadoc `Properties.load(Reader)` |
| XML | `xml`, `xmlns`, `xmlns:*`, `xml:lang`, `xml:space`, `xml:base`; reserved prefix `xml` | W3C XML 1.0 §2.3, §2.12; Namespaces in XML |
| JSON Schema 2020-12 | `$schema $id $ref $anchor $dynamicRef $dynamicAnchor $defs $vocabulary $comment`, `type properties patternProperties additionalProperties required items prefixItems contains enum const format allOf anyOf oneOf not if then else dependentRequired dependentSchemas propertyNames unevaluatedProperties unevaluatedItems minimum maximum exclusiveMinimum exclusiveMaximum multipleOf minLength maxLength pattern minItems maxItems uniqueItems minProperties maxProperties title description default examples deprecated readOnly writeOnly contentEncoding contentMediaType`; types `object array string number integer boolean null` | json-schema.org/draft/2020-12 (Core + Validation): copy the keyword index of each vocabulary |
| OpenAPI 3.1 | fixed fields of the objects: `openapi info servers paths components security tags externalDocs webhooks`, `get put post delete options head patch trace`, `parameters requestBody responses operationId summary description`, `in name required schema content`, `in`: `query header path cookie`, `schemas responses parameters examples requestBodies headers securitySchemes links callbacks pathItems`, `discriminator propertyName mapping xml`; `x-*` keys are extensions | spec.openapis.org/oas/v3.1.0: "Fixed Fields" tables of each object. **Attention:** `operationId` and `servers[].url` are VALUES to mask |
| Avro | attributes `type name namespace doc aliases fields order default symbols items values size logicalType precision scale`; primitives `null boolean int long float double bytes string`; complex `record enum array map fixed`; logical `decimal uuid date time-millis time-micros timestamp-millis timestamp-micros timestamp-nanos local-timestamp-* duration big-decimal` | avro.apache.org/docs/1.12.0/specification |
| Protobuf | `syntax edition package import option message enum service rpc returns stream oneof map reserved extensions extend optional repeated required weak public`; scalars `double float int32 int64 uint32 uint64 sint32 sint64 fixed32 fixed64 sfixed32 sfixed64 bool string bytes`; `google.protobuf.*` | protobuf.dev/reference/protobuf/proto3-spec (EBNF grammar: list the terminals) |
| GraphQL SDL | `type interface union enum input scalar schema directive extend implements repeatable on query mutation subscription fragment`; scalars `Int Float String Boolean ID`; directives `@deprecated @skip @include @specifiedBy @oneOf`; `__*` names (introspection) | spec.graphql.org (October 2021 / draft): sections "Type System" and "Names" |

Where the internal names live (what to mask):
- **JSON Schema:** keys of `properties`/`$defs`; items of `required` (they are property names:
  they need the same pseudonym as the key); the fragment of `$ref` (`#/$defs/tb_pedido_x9`);
  `title`/`description` are free text (they go to the prose detector); `$id`/`$schema` are URLs (host).
- **OpenAPI:** keys of `components/*`; literal segments of `paths`; `parameters[].name`;
  `operationId`; `servers[].url`; `tags[].name`; `$ref` (`#/components/schemas/X`); `discriminator.mapping`.
- **Avro:** `namespace` (dots = segments), `name` of record/enum/fixed and of `fields[]`, `aliases[]`,
  `symbols[]` (they may be internal codes), `type` when it is the name of a named type already defined.
- **Protobuf:** `package` (dotted), the name after `message`/`enum`/`service`/`rpc`, the field name
  (`type name = N;`), referenced types (`tb.Pedido`), enum values. The field number is not.
- **GraphQL:** the name after `type`/`input`/`enum`/`interface`/`union`/`scalar`; field and
  argument names; enum values; types referenced in `field: Type!`.

### 4. Identifier rules

- **JSON string (RFC 8259 §7):** compare and replace on the **decoded** value
  (`"tb_pedido"` = `tb_pedido`). When rewriting, emit a valid JSON string (escape `"` `\` and
  control characters). Pseudonyms with only `[A-Za-z0-9_]` avoid the problem.
- **YAML:** a scalar can be plain, `'single'` (escape `''`) or `"double"` (`\` escapes). Preserve the
  original style. A plain scalar that becomes `true`, `null`, `1e3` or starts with `&*!|>%@` changes
  type: the pseudonym must start with a letter and not collide with the Core schema (`T_cszwa3ri` is safe).
  **Anchors/aliases** (`&base`, `*base`): the anchor name is a label of the document, normally not
  sensitive; the alias reuses the node, so masking at the node solves every occurrence. If the anchor
  has an internal name, rename `&x` and every `*x` together. Complex keys (`? `) are rare: handle as in §6.
- **TOML:** a bare key is only `A-Za-z0-9_-`; with another character it needs quotes. `a.b.c = 1` is a
  **dotted** key (three levels), but `"a.b.c" = 1` is a single key. The header `[tables.tb_pedido_x9]`
  likewise. Mask segment by segment.
- **INI:** no spec; handle `[section]` and `key=value`, quotes optional and not removed.
- **.env:** python-dotenv: bare key or between `'`; bare value, `'...'` (escapes only `\\`, `\'`) or
  `"..."` (plus `\n \t \"`...); `#` after the value is a comment; quotes allow multi-line; it expands
  only `${VAR}`. Docker Compose: accepts `=` or `:`, expands `$VAR` and `${VAR:-x}`, and `#` is only a comment
  with a space before it. Do not mask `${VAR}` (it is a reference, not a value); mask the final value.
- **.properties:** the key goes up to the first **unescaped** `=`, `:` or space; `\` at the end of the
  line continues the value; `\uXXXX` must be decoded before comparing; a dot in the key is only
  convention (there is no hierarchy in the spec). When rewriting, escape `= : # !` and the space in the key.
- **XML:** an element/attribute name follows `Name` (§2.3) and may have a prefix `ns:local`: mask only
  `local`. Values in an attribute or text: decode the entities (`&amp;`, `&#95;`). CDATA: free text.
- **Compound names inside the value:** split on `.` respecting SQL quoting
  (`"Meu Schema".tb`, `` `proj.ds.tb` ``) and pseudonymize each part with the type of the position.

### 5. Before/after examples

The same name → the same pseudonym in the whole file (and in the session). Structure and types preserved.

```json
// before
{"host": "db-exemplo-01", "database": "vendas_x", "table": "tb_pedido_x9",
 "columns": {"vl_total": "decimal(12,2)", "dt_pedido": "date"}}
// after
{"host": "HOST_7rhd4fsp", "database": "DB_dqrsyg6b", "table": "T_cszwa3ri",
 "columns": {"C_xsdgeq7l": "decimal(12,2)", "C_6kntohaz": "date"}}
```

```yaml
# before                                  # after
fonte: &origem                            fonte: &origem
  host: db-exemplo-01                       host: HOST_7rhd4fsp
  table: "tb_pedido_x9"                     table: "T_cszwa3ri"
destino: *origem                          destino: *origem
```

```toml
# before                                  # after
[tables.tb_pedido_x9]                     [tables.T_cszwa3ri]
cluster_by = ["dt_pedido"]                cluster_by = ["C_6kntohaz"]
```

```properties
# before
spring.datasource.url=jdbc:postgresql://db-exemplo-01:5432/vendas_x
spring.datasource.username=usr_etl_x
# after
spring.datasource.url=jdbc:postgresql://HOST_7rhd4fsp:5432/DB_dqrsyg6b
spring.datasource.username=USR_m5og72t3
```

```json
// JSON Schema: key, required and $ref get the same pseudonym
{"$defs": {"tb_pedido_x9": {"type": "object",
   "properties": {"vl_total": {"type": "number"}}, "required": ["vl_total"]}},
 "$ref": "#/$defs/tb_pedido_x9"}
// after
{"$defs": {"T_cszwa3ri": {"type": "object",
   "properties": {"C_xsdgeq7l": {"type": "number"}}, "required": ["C_xsdgeq7l"]}},
 "$ref": "#/$defs/T_cszwa3ri"}
```

```proto
// before                                 // after
package vendas_x.pedidos;                 package DB_dqrsyg6b.SCH_3wl5qkpz;
message TbPedidoX9 { double vl_total = 1; }  message T_c3wyfawv { double C_xsdgeq7l = 1; }
```

Avro: `"namespace": "vendas_x.pedidos"` → `"DB_dqrsyg6b.SCH_3wl5qkpz"`, `"name": "tb_pedido_x9"` → `"T_cszwa3ri"`
(the pseudonym respects `[A-Za-z_][A-Za-z0-9_]*`, so the .avsc stays valid).

### 6. Hard cases and limits

- **Truncated JSON** (cut log, streaming): use a tolerant parser/tokenizer that walks as far as it
  can and keeps the stack of keys; mask what has a known position and send the rest to the
  free-text detector. Never "close" the JSON when rewriting: replace only inside the tokens.
- **Multi-document YAML** (`---` ... `---`, `...`): each document has its own context, but the
  pseudonym map belongs to the whole session. An anchor does not cross documents (spec 1.2.2 §9.2).
- **Compound value `db.schema.table`:** split and type by the number of parts and by the key:
  3 parts under `table` → `DB_.S_.T_`; 2 parts → `S_.T_`. In BigQuery `proj.ds.tb` (or the
  legacy `proj:ds.tb`) the first part is the project (`DB_`). If the count does not add up, `[VERIFY]`
  and mask every part with a generic type.
- **URL/DSN/JDBC in the value:** parse as a URI (RFC 3986): host, user, path (db), query (`?schema=`).
- **Allow/deny lists with regex/glob** (`include: ["tb_pedido_.*"]`, `deny: ["stg_*"]`): the pattern
  contains a partial name. Options: (1) mask only the maximal literals of the pattern (`tb_pedido_` → a
  prefix pseudonym) keeping the metacharacters; (2) mask the whole pattern as opaque
  (`T_kwvciyut`). (1) breaks if the LLM generates a new regex; (2) is safe and is the suggested default.
  JSON Schema's `patternProperties` falls here.
- **Same word, different roles:** `name` is vocabulary in Avro/OpenAPI (key), but the value is an
  internal name; `namespace` in Kubernetes is an environment, in Avro a package. Decide by the detected format.
- **Generic key with a generic value** (`type: table`, `kind: view`): the value is in the vocabulary →
  do not mask.
- **Collision with the vocabulary:** a column called `type` or `items` inside `properties` is a name
  (the position rules); outside a map of free names, it is vocabulary.
- **Comments** (YAML/TOML/INI/.properties/.env `#`, XML `<!-- -->`): free text, another family.
- **Reversal:** the LLM's response may reformat (change quotes, reorder). Unmask by the
  `PREFIX_xxxx` token, not by position.
- **Limit:** without a language dictionary, an unknown key with an unknown value
  (`"origem": "tb_pedido_x9"`) is not detected by this family; it depends on the identifier-shape
  detector (snake_case with a `tb_`, `vl_` prefix) of another section.

### 7. Links used

- JSON: https://www.rfc-editor.org/rfc/rfc8259
- YAML 1.2.2: https://yaml.org/spec/1.2.2/
- TOML 1.0: https://toml.io/en/v1.0.0
- XML 1.0: https://www.w3.org/TR/xml/ ; Namespaces: https://www.w3.org/TR/xml-names/
- Java Properties: https://docs.oracle.com/javase/8/docs/api/java/util/Properties.html (load(Reader))
- .env (python-dotenv): https://github.com/theskumar/python-dotenv (README, "File format" section)
- .env (Docker Compose): https://docs.docker.com/compose/how-tos/environment-variables/variable-interpolation/
- JSON Schema 2020-12: https://json-schema.org/draft/2020-12/json-schema-core ; https://json-schema.org/draft/2020-12/json-schema-validation
- OpenAPI 3.1.0: https://spec.openapis.org/oas/v3.1.0
- Avro 1.12.0: https://avro.apache.org/docs/1.12.0/specification/
- Protobuf: https://protobuf.dev/reference/protobuf/proto3-spec/ ; https://protobuf.dev/reference/protobuf/edition-2023-spec/
- GraphQL: https://spec.graphql.org/October2021/
- URI (DSN/JDBC): https://www.rfc-editor.org/rfc/rfc3986


### 8. What is implemented (the `chave-valor` reader)

Code: `internal/mask/leitor_chave_valor.go`. A single reader, without a parser for each format: it finds the
separator (`:` or `=`), the key before it and the value after it. It works for YAML, JSON, TOML, INI,
.env, .properties, an XML attribute (`host="x"`), an XML element without attributes (`<host>x</host>`) and
a long command-line option (`--host x`, `--host=x`).

**Key → entity, by the LAST piece** (it splits on `.` `_` `-` `:` and camelCase, lowercase):

| Last piece | Entity | Pseudonym |
|---|---|---|
| `host`, `hostname`, `server`, `servername`, `servidor`, `endpoint`, `address`, `addr`, `fqdn`, `broker`, `bootstrap`, `bootstrap.servers`, `cluster`, `instance`, `warehouse` | servidor (server) | `HOST_` |
| `database`, `db`, `dbname`, `databasename`, `catalog` | database | `DB_` |
| `schema`, `schemaname`, `dataset` | schema | `SCH_` |
| `table`, `tablename`, `tabela`, `collection` | tabela (table) | `T_` |
| `user`, `username`, `usuario`, `login`, `principal` | usuario (user) | `USR_` |
| `namespace` | namespace | `NS_` |
| `service`, `servico`, `app`, `application` | servico (service) | `SVC_` |
| `bucket`, `container` | bucket | `BKT_` |
| `queue`, `topic`, `fila`, `topico`, `exchange`, `stream`, `subject` | fila (queue) | `TOP_` |
| `repo`, `repository` | repositorio (repository) | `REPO_` |
| `org`, `organization` | organizacao (organization) | `ORG_` |
| `account`, `tenant`, `subscription`, `project_id`/`projectId` | conta_nuvem (cloud account) | `ACC_` |
| `<x>_name` with x = db, database, table, schema, host, server, user, service, bucket, queue, topic | the entity of x | — |

A key ending in something else (`port`, `timeout`, `count`, `size`, `enabled`, `max`, `min`,
`version`, `type`, `mode`, `ttl`, `retries`, `id`...) is not in the table and is left out.
**Strong** when the key is exactly the name (`host:`, `database=`, `--host`, `HOST=`);
**weak** when it is compound (`db_host`, `spring.datasource.username`).

**Values that stay:** `true/false/null/none`, a number, an IP (left to the IP detector), `localhost`,
`0.0.0.0`, a path (`/`, `./`, `~`), an expression (`${...}`, `{{...}}`, `%s`, `$VAR`, `<x>`), an e-mail,
a URL (the URL readers decide), a public domain (known or reserved TLD: `.com`, `.io`,
ccTLD, `.example`, `.invalid`), a file name (`config.yaml`), a version/hash, a type word
(`str`, `string`, `int`). A name that looks like an example (`my-bucket`) is not an exception: it is masked.

**Guards against code:** `:=`, `==`, `+=`... stay; a value followed by `(`, `[` or `"` is a call, an
index or a composite literal; a value with no quotes, no hyphen and no digit (it may be a variable) only counts on
a configuration line (key at the start of the line, value up to its end, with a YAML `:`, an
UPPERCASE .env key, a dotted .properties key that does not start with `self.`/`cfg.`..., or `=`
without spaces); a dotted value with no digit/hyphen (`self.host`, `http.server`) is an attribute or
module access; `Server: nginx` (a Title-case key with `:`) is a prose label or an HTTP header.
Measured on 4 MB of the Go source code and 4 MB of the Python 3.10 library: 0 and 2 findings.

```text
BEFORE                                       AFTER
DB_HOST=db-exemplo-01                        DB_HOST=HOST_r2wq7m4k          (weak: compound key)
<database>vendas_x</database>                <database>db_k5n2b7xq</database>
pg_dump --host db-exemplo-01 --dbname=vendas_x   pg_dump --host HOST_r2wq7m4k --dbname=db_k5n2b7xq
bootstrap.servers=kafka-01:9092,kafka-02:9092    bootstrap.servers=host_...:9092,host_...:9092
user = request.user                          (stays: code expression)
```

## Kubernetes and containers

Central idea: in a manifest the **field path** says what the value is. There is no need to guess
whether `pedidos-x9` is a name: if it is in `metadata.name` of a `kind: Service`, it is a service.
The list of what is fixed vocabulary is small and comes from the API schema itself.

### 1. Detection signals

One strong signal is enough. Two weak signals together are enough too.

| Format | Strong signal | Weak signal |
|---|---|---|
| K8s manifest (YAML/JSON) | `apiVersion:` and `kind:` in the same document, with `kind` inside the public list (section 3) | `metadata:` with `name:`/`namespace:`; `---` separator between documents |
| `kubectl get` output | uppercase column header: `NAME READY STATUS RESTARTS AGE`, `NAMESPACE NAME ...`, `NAME TYPE CLUSTER-IP EXTERNAL-IP PORT(S) AGE` | `STATUS` values of the enum (`Running`, `Pending`, `CrashLoopBackOff`) |
| `kubectl describe` | lines `Name:`, `Namespace:`, `Labels:`, `Annotations:`, `Events:` aligned in a column | `Controlled By:  ReplicaSet/...` (`Kind/name` format) |
| `kubectl -o yaml/json` | like a manifest, with `status:`, `uid`, `resourceVersion`, `managedFields` | `"kind": "List"` with `items` |
| Service DNS name | suffix `.svc.cluster.local` (or `.svc.<cluster domain>`), `.pod.cluster.local` | short form `<svc>.<ns>` inside a URL/connection string |
| kubeconfig | `apiVersion: v1` + `kind: Config` with `clusters:` / `contexts:` / `users:` | `current-context:` |
| Helm | `Chart.yaml` with `apiVersion: v2` + `name` + `version`; templates with `{{ .Values.` / `{{ .Release.` / `{{ include` | `values.yaml` without `kind` |
| docker-compose | top-level key `services:`, with children containing a service key of the specification (`image:`, `build:`, `depends_on:`, `command:`... or the merge `<<:`); top-level `networks:`/`volumes:`/`secrets:` | top-level `name:` (project name) |
| Dockerfile | a line starting with an instruction: `FROM`, `RUN`, `COPY`, `ENV`, `ARG`, `WORKDIR`, `ENTRYPOINT`, `CMD`, `LABEL`, `EXPOSE` | `FROM x AS stage` |
| `docker ps` | header `CONTAINER ID IMAGE COMMAND CREATED STATUS PORTS NAMES` | 12-character hex ID |
| `docker inspect` | JSON array with `"Id"`, `"Config": {"Image", "Env", "Hostname"}`, `"NetworkSettings"` | `"Name": "/name"` (leading slash) |

### 2. Position (field path) → entity type

Paths in OpenAPI notation (`[]` = any item of the list). `PodSpec` appears in
`Pod.spec`, `*.spec.template.spec` (Deployment, StatefulSet, DaemonSet, Job, ReplicaSet) and
`CronJob.spec.jobTemplate.spec.template.spec` — treat them all with the same set of rules.

| Path | Entity | Note |
|---|---|---|
| `metadata.name` | the type depends on the `kind` (`SVC_`, `DEPLOY_`, `SECRET_`, `CM_`, `SA_`, `STS_`, `JOB_`...) | — |
| `metadata.namespace`, `kind: Namespace` → `metadata.name` | `NS_` | same pseudonym everywhere |
| `metadata.generateName` | prefix of the type of the `kind` | the server appends a random suffix |
| `metadata.labels.*`, `spec.selector.matchLabels.*`, `Service.spec.selector.*` | value: `APP_` (if the key is `app`, `app.kubernetes.io/name`, `instance`, `part-of`) | a key with an official prefix stays; a key without a prefix may be private |
| `metadata.annotations.*` | free text → goes through the other detectors (URL, host, e-mail) | — |
| `metadata.ownerReferences[].name` | type of `ownerReferences[].kind` | `kind` stays |
| `StatefulSet.spec.serviceName` | `SVC_` | must match the headless Service |
| `PodSpec.serviceAccountName`, `ServiceAccount.metadata.name`, `RoleBinding.subjects[].name` (with `kind: ServiceAccount`) | `SA_` | `subjects[].namespace` → `NS_` |
| `RoleBinding.roleRef.name` | `ROLE_` | except public roles (`cluster-admin`, `admin`, `edit`, `view`) |
| `RoleBinding.subjects[].name` with `kind: User` / `Group` | `USER_` / `GROUP_` | `system:*` groups are public |
| `containers[].name`, `initContainers[].name` | `CTR_` | — |
| `containers[].image`, `docker FROM`, compose `image` | `IMG_` (only a private registry/repository) | see the image rule in section 3 |
| `env[].name` | **stays** (a variable name is structure) | but it indicates the type of the value next to it |
| `env[].value` | free text → host/URL/DSN/user detectors; if `name` ends in `_HOST`, `_USER`, `_DB`, `_DATABASE` → `HOST_`/`USER_`/`DB_` | password/token: always mask |
| `env[].valueFrom.secretKeyRef.name`, `envFrom[].secretRef.name`, `volumes[].secret.secretName`, `imagePullSecrets[].name` | `SECRET_` | `.key` is a key name: it stays |
| `env[].valueFrom.configMapKeyRef.name`, `envFrom[].configMapRef.name`, `volumes[].configMap.name` | `CM_` | — |
| `volumes[].persistentVolumeClaim.claimName`, `volumeClaimTemplates[].metadata.name` | `PVC_` | — |
| `volumes[].name`, `volumeMounts[].name` | `VOL_` | `volumeMounts[].mountPath` = path, goes through the path detector |
| `Secret.data.*` / `stringData.*` | **value always masked** (base64 or text) | the keys may stay |
| `ConfigMap.data.*` | free text → detectors | — |
| `Ingress.spec.rules[].host`, `spec.tls[].hosts[]` | `HOST_` | — |
| `Ingress.spec.tls[].secretName` | `SECRET_` | — |
| `Ingress ...backend.service.name` | `SVC_` | `port.number` stays |
| `Ingress.spec.ingressClassName` | `CLASS_`, or stays if it is public (`nginx`) [VERIFY list] | — |
| `Service.spec.externalName`, `PodSpec.hostname`, `PodSpec.subdomain`, `hostAliases[].hostnames[]` | `HOST_` / `SVC_` (subdomain = name of the headless Service) | `hostAliases[].ip` → IP detector |
| `PodSpec.nodeName`, `nodeSelector` values | `NODE_` | `kubernetes.io/hostname` keys stay |
| `Service.spec.ports[].name` | stays (usually `http`, `grpc`) | it enters the SRV record `_http._tcp...` |
| compose `services.<key>` | `SVC_` | the **key** of the map is the name |
| compose `container_name`, `hostname`, `domainname` | `CTR_`, `HOST_`, `HOST_` | — |
| compose `networks.<key>`, `volumes.<key>`, `secrets.<key>`, `configs.<key>` (top level and references) | `NET_`, `VOL_`, `SECRET_`, `CM_` | — |
| compose `depends_on[]`, `links[]` (`SVC[:ALIAS]`), `extra_hosts` (`HOST=IP`) | `SVC_`, `HOST_` | — |
| compose `name` (top level) | `PROJ_` | — |
| Helm `Chart.yaml`: `name`, `dependencies[].name`, `dependencies[].repository` | `CHART_`, URL → detector | a public chart from a public repo may stay |
| `Ingress ...backend.serviceName` (v1beta1), `PodSpec.serviceAccount` (old name) | `SVC_`, `SA_` | — |
| `labels` / `matchLabels` / `selector`: value of `app.kubernetes.io/name`, `app.kubernetes.io/instance`, `app.kubernetes.io/part-of` | `SVC_` (weak evidence: masks in place, does not teach on its own) | the other labels stay |
| kubeconfig: `clusters[].name`, `contexts[].name`, `contexts[].context.cluster`, `current-context` | `HOST_` | a name in ARN form (`arn:aws:eks:<region>:<account>:cluster/<name>`): only the account (`ACC_`) and the final name |
| kubeconfig: `users[].name`, `contexts[].context.user` | `USR_` | — |
| kubeconfig: `contexts[].context.namespace` | `NS_` | — |
| kubeconfig: `clusters[].cluster.server` | host of the URL → `HOST_` | scheme, port and path stay |
| DNS name `<svc>.<ns>.svc.cluster.local` (in any text) | `SVC_` + `NS_` | the suffix stays; the label before the service (StatefulSet pod) stays |
| `kubectl get`: columns `NAME`, `NAMESPACE`, `NODE`, `NOMINATED NODE` | according to the requested resource | `READY`, `STATUS`, `AGE`, `TYPE` stay |
| `docker ps`: `NAMES`, `IMAGE` | `CTR_`, `IMG_` | `CONTAINER ID` is a hash: it stays or becomes `ID_` |

In the implementation the types above fall into the base entities: `metadata.name` of a `Namespace` → namespace,
of a `ServiceAccount` → user, of the other `kind`s → service; `SECRET_`, `CM_`, `PVC_`, `SA_` → service/user;
`IMG_` → the registry as a server and each piece of the path as a service (the tag and the digest stay). The
manifest is only recognized with `apiVersion` (in the form `group/vN`) and `kind` in the same object; inside
`{{ }}` nothing is touched.

### 3. Public vocabulary (what is never masked)

| Vocabulary | Examples | How to extract |
|---|---|---|
| `kind` | `Deployment`, `Service`, `Secret`, `CronJob`, `RoleBinding`... | `x-kubernetes-group-version-kind[].kind` of each definition |
| `apiVersion` | `v1`, `apps/v1`, `batch/v1`, `networking.k8s.io/v1`, `rbac.authorization.k8s.io/v1` | same field: `group` + `/` + `version` (empty group = core → only `v1`) |
| enums | `ClusterIP`, `NodePort`, `LoadBalancer`, `ExternalName`, `Always`, `IfNotPresent`, `Never`, `OnFailure`, `TCP`, `UDP`, `Opaque`, `kubernetes.io/tls`, `Prefix`, `Exact` | in OpenAPI v3, properties with `enum` (or a description "Possible enum values") |
| kubectl statuses | `Running`, `Pending`, `Succeeded`, `Failed`, `CrashLoopBackOff`, `ImagePullBackOff`, `Completed` | `PodStatus.phase` (enum) + reasons of `containerStatuses[].state.waiting.reason` [VERIFY closed list] |
| field names | every property key (`spec`, `template`, `containers`, `secretKeyRef`...) | keys of `properties` in all the definitions |
| label/annotation prefixes | `kubernetes.io/`, `k8s.io/` (reserved to the core), `app.kubernetes.io/` (recommended labels: `name`, `instance`, `version`, `component`, `part-of`, `managed-by`), `helm.sh/chart`, `com.docker.compose.*` (reserved by Compose) | docs "Well-Known Labels, Annotations and Taints" + "Recommended Labels" |
| system objects | namespaces `default`, `kube-system`, `kube-public`, `kube-node-lease`; SA `default`; ClusterRoles `cluster-admin`, `admin`, `edit`, `view`; `system:*` groups | docs on namespaces and RBAC |
| cluster domain | `cluster.local`, `svc`, `pod` | docs DNS for Services and Pods |
| Dockerfile / Compose | instructions (`FROM`, `RUN`...) and keys of the Compose spec | Dockerfile reference and compose-spec |

**Extraction from OpenAPI** (once, generating a list embedded in the Go binary):
1. Download `api/openapi-spec/v3/*.json` (one file per group/version) from the official repository.
2. For each `components.schemas.*`: read `x-kubernetes-group-version-kind` → `kinds` and `apiVersions`;
   read `properties` → field names; read `enum` → fixed values.
3. Mark as a **reference field** the properties whose name is `name`, `namespace`, `secretName`,
   `claimName`, `serviceName`, `serviceAccountName`, `host`, `hosts`, `hostname`, `subdomain`,
   `image`, `externalName`, `nodeName` — and check the full path by hand (the table in section 2).
   `name` alone is not enough: `ports[].name` and `env[].name` are structure.
4. CRDs (`kind` outside the list) → treat `metadata.*` with the same rules; the rest becomes free text.

**Public images.** Do not keep a list of images. Structural rule: the part before the first `/`
is a registry if it has `.` or `:` or is `localhost`. No registry ⇒ Docker Hub (public: `nginx:1.27`,
`postgres:16` stay). A known, small public registry (`docker.io`, `registry.k8s.io`, `ghcr.io`,
`quay.io`, `gcr.io`, `mcr.microsoft.com`, `public.ecr.aws`) ⇒ the registry stays, and the repository stays
only if it is the project's official one [VERIFY: `ghcr.io/<org>` may be private]. Any other host ⇒
private registry: mask host and repository, **keep tag and digest** (a version is not a secret and helps the diagnosis).
Reference format: `[<registry>/][<project>/]<image>[:<tag>|@<digest>]`.

### 4. Identifier rules (signal and constraint)

| Rule | Length | Characters | Where |
|---|---|---|---|
| DNS-1123 subdomain | ≤ 253 | `a-z 0-9 - .`, starts and ends alphanumeric | most `metadata.name` (Deployment, Secret, ConfigMap, SA) |
| RFC 1123 label | ≤ 63 | `a-z 0-9 -`, starts with a letter (current docs), ends alphanumeric | `Namespace`, `containers[].name`, `hostname` |
| RFC 1035 label | ≤ 63 | same, starts with a letter | `Service` (may start with a digit with `RelaxedServiceNameValidation`) |
| Label value | ≤ 63, may be empty | `A-Za-z0-9 - _ .`, starts/ends alphanumeric | `labels.*` |
| Compose project | — | `a-z 0-9 - _`, starts with a letter/digit | `name`, `-p` |
| Compose `container_name` | — | `[a-zA-Z0-9][a-zA-Z0-9_.-]+` | — |

Validation regexes of apimachinery: label `[a-z0-9]([-a-z0-9]*[a-z0-9])?`, subdomain
`label(\.label)*`, 1035 `[a-z]([-a-z0-9]*[a-z0-9])?`.

How to use them: (a) **signal** — a value in a reference field that matches DNS-1123 reinforces the detection;
a value that does not match (it has uppercase, a space) indicates that the field is something else or is a template. (b) **constraint
on the pseudonym** — the pseudonym must stay valid in the same field. That is why the form
`NS_wbk2i5of` (uppercase and `_`) **does not work inside the YAML**: use the lowercase form with a hyphen
`ns-wbk2i5of`, `svc-n3dsajwo`, `host-scsrx4tu`, keeping the type prefix. The form `NS_wbk2i5of` is left for
free text outside a validated field. Map both forms to the same original.
Pseudonym length ≤ original when the limit gets tight (63).

### 5. Before/after examples (fictitious data)

```yaml
# BEFORE
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: svc-pedidos-x9
  namespace: ns-financeiro-demo
  labels:
    app.kubernetes.io/name: svc-pedidos-x9
spec:
  serviceName: svc-pedidos-x9
  template:
    spec:
      serviceAccountName: sa-pedidos-leitor
      containers:
      - name: api
        image: registry.exemplo.interno/app-demo:1.2
        imagePullPolicy: IfNotPresent
        env:
        - name: DB_HOST
          value: pg-pedidos.ns-financeiro-demo.svc.cluster.local
        - name: DB_PASSWORD
          valueFrom:
            secretKeyRef: {name: segredo-pg-demo, key: password}
```

```yaml
# AFTER
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: svc-n3dsajwo
  namespace: ns-wbk2i5of
  labels:
    app.kubernetes.io/name: svc-n3dsajwo
spec:
  serviceName: svc-n3dsajwo
  template:
    spec:
      serviceAccountName: usr-vhkjy7s4
      containers:
      - name: api
        image: host-scsrx4tu/svc-3acw37uo:1.2
        imagePullPolicy: IfNotPresent
        env:
        - name: DB_HOST
          value: svc-vk7i3ahq.ns-wbk2i5of.svc.cluster.local
        - name: DB_PASSWORD
          valueFrom:
            secretKeyRef: {name: secret-w6n0, key: password}
```

What stayed: `apiVersion`, `kind`, keys, `api` (short generic name [VERIFY policy]), `IfNotPresent`,
`DB_HOST`, `password`, `svc.cluster.local`, the tag `1.2`. The namespace inside the FQDN got the
**same** `ns-wbk2i5of` as `metadata.namespace`.

Output of `kubectl get pods -n ns-financeiro-demo`:

```
BEFORE                                                  AFTER
NAME                              READY STATUS  AGE     NAME                     READY STATUS  AGE
svc-pedidos-x9-0                  1/1   Running 3d      svc-n3dsajwo-0               1/1   Running 3d
api-demo-7d9f8b6c5d-x2k8q         0/1   CrashLoopBackOff  svc-7eygw6sy-7d9f8b6c5d-x2k8q 0/1 CrashLoopBackOff
```

docker-compose: `services: { svc-pedidos-x9: { image: registry.exemplo.interno/app-demo:1.2, depends_on: [db-demo] } }`
→ `services: { svc-n3dsajwo: { image: host-scsrx4tu/svc-3acw37uo:1.2, depends_on: [svc-jgzcjsnq] } }`.

### 6. Hard cases and limits

- **Cross-references.** The same name appears in `metadata.name` of the Service, `spec.serviceName`,
  `backend.service.name`, `subdomain`, the FQDN `x.ns.svc.cluster.local`, `depends_on`, the `NAME` column.
  The original→pseudonym map is **derived by HMAC from the llm-dlp key, per type** (the same in every conversation and after restarts); the same text in different types
  (a Service and a Deployment both called `svc-pedidos-x9`) should get *linked* pseudonyms — suggestion:
  the same root with a different prefix, or the same string if the prefix is neutral [VERIFY decision].
- **Generated suffixes.** Deployment pod = `<deployment>-<pod-template-hash>-<5 characters>`;
  StatefulSet = `<sts>-<ordinal>`; Job/CronJob = `<cronjob>-<stamp>-<suffix>`; `generateName` = prefix +
  random; Compose v2 = `<project>-<service>-<n>`. Recognize the known root, mask only the root and
  **preserve the suffix** (it is not sensitive and keeps the correlation between log lines).
  If the root has not been seen yet, cutting suffixes structurally (`-[a-z0-9]{5}$`, `-[0-9a-f]{8,10}`, `-[0-9]+$`) is a heuristic: it may cut too much.
- **Helm templates.** `{{ .Values.x }}`, `{{ include "chart.fullname" . }}` are not values: do not mask
  what is between `{{ }}`; the template's YAML is not even valid YAML before rendering. Mask the literal
  around it (`name: {{ .Release.Name }}-pedidos-x9`) only in the span outside the braces. In `values.yaml` there
  is no `kind`: the path has no schema → use the key name (`image.repository`, `ingress.hosts[].host`,
  `serviceAccount.name`, `existingSecret`) as a weak signal and fall back to the free-text detectors.
- **Base64 in a Secret.** `data.*` comes in base64 and may contain a DSN/password; mask the whole value (do not decode it to show).
- **`kubectl describe` and logs.** Names appear in prose (`Successfully assigned ns/pod to node`,
  `Pulling image "..."`). The formats `ns/name` and `Kind/name` (`ReplicaSet/api-demo-7d9f8b6c5d`) are structural signals.
- **Short generic strings.** `api`, `web`, `db`, `worker` in `containers[].name` or a compose key:
  masking by position is coherent, but leaving them does little harm [VERIFY policy].
- **Column alignment.** A pseudonym of a different length misaligns `kubectl get`/`docker ps`; to the LLM it makes no difference; do not reformat.
- **Way back (unmasking).** The LLM's response may bring `svc-n3dsajwo` inside a new command; swap back only whole tokens that are in the map.
- **CRDs and operators** (Argo, cert-manager etc.): `kind` outside the official list; only `metadata` has a guaranteed rule.

### 7. Links used

- https://kubernetes.io/docs/concepts/overview/working-with-objects/names/
- https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/
- https://kubernetes.io/docs/concepts/overview/working-with-objects/common-labels/
- https://kubernetes.io/docs/reference/labels-annotations-taints/
- https://kubernetes.io/docs/concepts/services-networking/dns-pod-service/
- https://github.com/kubernetes/kubernetes/tree/master/api/openapi-spec (README, `swagger.json`, `v3/`)
- https://docs.docker.com/reference/compose-file/services/
- https://docs.docker.com/compose/how-tos/project-name/
- https://helm.sh/docs/topics/charts/ (Chart.yaml) and https://helm.sh/docs/chart_template_guide/ (templates) — not consulted in this round [VERIFY]
- https://docs.docker.com/reference/dockerfile/ — not consulted in this round [VERIFY]


## Infrastructure as code

IaC files already come structured. The real names (server, database, bucket, network, account) sit in
**predictable positions**: the value of certain attributes inside resource blocks. Everything else is
public vocabulary of the providers: resource types, argument names and keywords of the language.
The general rule is simple: **key and type are never masked; only the literal value of a
name attribute is masked**.

### 1. Detection signals

| Format | Structural signals (2 or more are enough) |
|---|---|
| Terraform/HCL (`.tf`, `.tfvars`) | a line starts with `resource "x" "y" {`, `data "x" "y" {`, `variable "x" {`, `module "x" {`, `output "x" {`, `locals {`, `provider "x" {`, `terraform {`; assignment `ident = value`; interpolation `${...}`; references `var.`, `local.`, `module.`, `data.` |
| Output of `terraform plan`/`apply` | `# <address> will be created` / `will be updated in-place` / `must be replaced`; lines with the prefix `+`, `-`, `~`, `-/+`, `<=`; `-> ` between the old and the new value; `(known after apply)`; `Plan: N to add, N to change, N to destroy.` |
| State/plan JSON (`terraform show -json`) | keys `format_version`, `terraform_version`, `resource_changes[].address`, `change.actions`, `before`/`after`, `values.root_module.resources[]` |
| Ansible playbook | YAML list with `hosts:`, `tasks:`, `become:`, `roles:`, `vars:`; FQCN modules `ansible.builtin.*`, `community.*`; `{{ var }}` (Jinja2) |
| Ansible INI inventory | `[group]`, `[group:vars]`, `[group:children]`; lines `host ansible_host=... ansible_user=...`; ranges `www[01:50]` |
| Ansible YAML inventory | root `all:` with `hosts:` / `children:` / `vars:`; paths `host_vars/<host>` and `group_vars/<group>` |
| CloudFormation (YAML/JSON) | `AWSTemplateFormatVersion`, `Resources:`, `Type: AWS::Service::Resource`, `Properties:`, `Parameters:`, `Outputs:`; tags `!Ref`, `!GetAtt`, `!Sub`, `!Join` or `Fn::*` |
| ARM template (JSON) | `$schema` with `deploymentTemplate.json`, `contentVersion`, `resources[]` with `type: "Microsoft.X/y"`, `apiVersion`, `name`; expressions `"[parameters('x')]"`, `"[concat(...)]"` |
| Bicep (`.bicep`) | `resource <symbol> 'Microsoft.X/y@YYYY-MM-DD' = {`, `param`, `var`, `module`, `existing`, interpolation `'${x}'` |

### 2. Position → entity type

| Format | Position | Entity | Mask? |
|---|---|---|---|
| HCL | 1st label of `resource "aws_db_instance"` | type (vocabulary) | no |
| HCL | 2nd label `"relatorios"` | local name, it only exists in the code | optional (see item 6) |
| HCL | `identifier`, `db_name`, `database_name` | DB | yes |
| HCL | `bucket` in `aws_s3_bucket` / `name` in `google_storage_bucket` | BKT | yes |
| HCL | `name` in `azurerm_mssql_server`, `server_name`, `host`, `endpoint`, `address` | HOST/SRV | yes |
| HCL | `name` in VPC/VNet/subnet, `network`, a literal `vpc_id` | NET | yes |
| HCL | `account_id`, `project`, `account`, `username`, `user` | ACC/USR | yes |
| HCL | `cluster_name`, `cluster_identifier`, `instance_name` | HOST | yes |
| HCL | `namespace`, `topic`, `queue_name`, `subscription`, `repository`, `dataset_id`, `table_id`, `organization`, `folder`, `function_name` | NS / TOP / REPO / SCH / T / ORG / DIR / SVC | yes |
| HCL | 12-digit `account_id` (AWS) | ACC | yes; other pure numbers stay |
| HCL | `provider "x" { project = ... }`, `backend "x" { bucket = ... }` | ACC / BKT | yes |
| HCL | `tags { Name = ... }` / `tags = { Name = ... }` | type of the resource | yes, weak evidence |
| HCL | value with `${...}` | type of the attribute | only the literal pieces, weak evidence |
| HCL | `default` of a `variable` whose name suggests the entity (`db_name`) | inherits the type of the usage | yes |
| plan text | value to the right of `=`, on both sides of `->` | type of the attribute | yes, both sides |
| plan/state JSON | `before.<attr>`, `after.<attr>`, `values.<attr>` | type of the attribute | yes |
| Ansible INI | 1st token of the line, under `[group]` | HOST | yes |
| Ansible INI/YAML | `ansible_host`, `ansible_user`, `delegate_to` | HOST / USR | yes |
| Ansible YAML | child keys of `hosts:` (map) in the inventory | HOST | yes |
| Ansible playbook | `hosts: <pattern>` of a play (with `tasks`/`roles`/`become`...) | HOST | yes, weak evidence (`all`, `localhost` stay) |
| Ansible | group name `[dbservers]` | internal label | optional |
| Ansible | parameters `name`/`login_host`/`login_user`/`db` of database modules (e.g. `community.mysql.mysql_db`) | DB / HOST / USR | yes |
| Ansible | file name in `host_vars/<host>.yml` | HOST | yes (the path leaks too) |
| CFN | logical key in `Resources:` (`BancoRelatorios:`) | logical name | optional |
| CFN | `DBInstanceIdentifier`, `DBName`, `DatabaseName`, `BucketName`, `TableName`, `DBClusterIdentifier`, `MasterUsername`, `UserName`, `RoleName`, `GroupName` | DB / BKT / T / USR | yes |
| CFN | `ServerName`, `ClusterName` / `QueueName`, `TopicName`, `StreamName` / `FunctionName`, `ServiceName` / `RepositoryName` | HOST / TOP / SVC / REPO | yes; `!Ref`/`!GetAtt` stay, in `!Sub` only the literal outside `${}` |
| CFN | `Default` of `Parameters` used in those properties | inherits | yes |
| ARM | `resources[].name` | according to `type` | yes |
| ARM / Bicep | child resource name `parent/child` (`Microsoft.Sql/servers/databases`) | one piece per type: HOST / DB | yes |
| Bicep | symbol after `resource` | local name | optional |
| Bicep | `name:` inside the body | according to the type | yes |

The entity comes from the **pair (resource type, attribute name)**, never from the text of the value. That is why
`name` in `aws_s3_bucket` does not exist, `name` in `google_storage_bucket` is BKT and `name` in
`azurerm_mssql_database` is DB.

### 3. Public vocabulary: source and how to extract

| Format | Vocabulary | Official source | Extraction |
|---|---|---|---|
| HCL (language) | block words (`resource`, `data`, `variable`, `locals`, `module`, `output`, `provider`, `terraform`, `moved`, `import`, `check`), `var`, `local`, `each`, `count`, `self`, `path`, built-in functions | HCL spec (github.com/hashicorp/hcl) and Terraform Language docs | fixed, small list |
| Terraform providers | resource and data source types, with the arguments of each one | schema of the providers | `terraform providers schema -json` → `provider_schemas[<p>].resource_schemas[<type>].block.attributes` (and `block_types` for nested blocks) |
| CloudFormation | `AWS::Service::Resource`, names of `Properties` and `Attributes` | Resource specification (one JSON per region) or resource provider schemas (zip per region, JSON Schema draft-07) | `ResourceTypes.<type>.Properties` (spec) or `properties` of each schema; `aws cloudformation describe-type --type RESOURCE --type-name AWS::RDS::DBInstance` |
| CFN intrinsics | `Ref`, `Fn::GetAtt`, `Fn::Sub`, `Fn::Join`, `Fn::If`… and the short `!` forms | Intrinsic function reference | fixed list |
| ARM/Bicep | `Microsoft.<Provider>/<type>`, `apiVersion`, properties | Azure resource reference (learn.microsoft.com/azure/templates) and published JSON schemas | `az provider list --query "[].{ns:namespace,t:resourceTypes[].resourceType}"` for the types; properties from the JSON schema of each `apiVersion` |
| Ansible | play/task keys (`hosts`, `tasks`, `vars`, `become`…), `ansible_*` variables, modules and parameters | docs.ansible.com (Playbook keywords, connection vars, collections) | `ansible-doc -l` (modules); `ansible-doc -j <module>` → `doc.options` (parameters) |

**How to choose the name attributes without a dictionary:** take from the schema the `string` attributes that are not
`computed` and that match a technical pattern (`name`, `*_name`, `identifier`, `*_identifier`,
`bucket`, `host`, `*_host`, `endpoint`, `server*`, `account*`, `project`, `user*`, `*Name`,
`*Identifier`). Then review that list by hand a single time. The result is a table
`(type, attribute) → entity`, which goes versioned in the repository. The schema's `description` and `sensitive`
help in the review: attributes with `sensitive: true` (passwords) go into another type, SECRET.

### 4. Identifier rules

- **HCL interpolation `${...}`**: inside a string, the literal span is masked and the expression is not.
  In `"${var.prefixo}-relatorios-demo"`, `var.prefixo` stays as it is (it is a reference). The literal
  suffix `-relatorios-demo` is only masked if the attribute is in the table; in that case the pseudonym
  replaces the whole string and the expression is preserved: `"${var.prefixo}-BKT_sfdst7tg"`.
  The escape `$${` is literal text and does not open an interpolation.
- **References `type.name.attribute`** (`aws_db_instance.relatorios.address`,
  `data.aws_vpc.principal.id`, `module.rede.subnet_ids`, `var.x`, `local.y`): it is a code
  address and does not appear as data. Only the `name` segment may be replaced, and only if the option
  "mask the local name" is on. In that case the replacement must be consistent in the whole file.
- **Ansible's Jinja2 `{{ x }}`**: same logic. The variable stays, and its value in
  `vars`/`group_vars` is what gets masked.
- **CFN**: `!Ref Param` and `!GetAtt Logico.Endpoint.Address` point to logical names. In
  `!Sub 'arn:aws:s3:::${BucketRelat}/*'`, `${BucketRelat}` is a reference; the literal text outside
  the `${}` follows the rule of the attribute.
- **ARM `[...]`**: a string that starts with `[` is an expression (`"[parameters('sqlName')]"`) and is not
  masked. Starting with `[[` is the escape for a literal. Bicep: `'${x}'` works as in HCL.
- **Name constraints per provider** (the pseudonym must obey them, otherwise `validate` or `plan`
  fail):

| Resource | Official constraint | Shape of the pseudonym |
|---|---|---|
| S3 bucket | 3–63 characters, lowercase, digits, `.` and `-` | `bkt-sfdst7tg` |
| RDS `DBInstanceIdentifier` / `identifier` | 1–63 characters, letters, digits and `-`; starts with a letter; no `--` and no `-` at the end | `db-gbzw6njk` |
| Azure Storage account | 3–24 characters, only lowercase and digits, globally unique | `bktsfdst7tgimuu` |
| Azure SQL server | lowercase, digits and `-`, no `-` at the ends, globally unique | `host-7rhd4fsp` |
| DNS hostname (Ansible) | labels of letters, digits and `-`; `_` is invalid | `host-7rhd4fsp` |

  Practical rule: the canonical pseudonym is `TYPE_suffix` (e.g. `BKT_sfdst7tg`). When the attribute has a
  constraint, the proxy emits a **derived form** (`bkt-sfdst7tg`, `bktsfdst7tgimuu`) and keeps in the reverse map
  both forms pointing to the same original.

### 5. Before/after examples

**Terraform**
```hcl
# before
resource "aws_s3_bucket" "relatorios" {
  bucket = "bkt-relatorios-demo"
}
resource "aws_db_instance" "principal" {
  identifier = "db-exemplo-01"
  db_name    = "vendas_demo"
  username   = "app_demo"
}
# after
resource "aws_s3_bucket" "relatorios" {
  bucket = "bkt-sfdst7tg"
}
resource "aws_db_instance" "principal" {
  identifier = "db-gbzw6njk"
  db_name    = "DB_wzm3mqvx"
  username   = "USR_uu7ojlpg"
}
```

**Ansible (INI inventory)**
```ini
# before                                   # after
[dbservers]                                [dbservers]
db-exemplo-01 ansible_host=10.0.0.5        host-7rhd4fsp ansible_host=IP_r9d3
```

**CloudFormation**
```yaml
# before
Resources:
  BancoRelatorios:
    Type: AWS::RDS::DBInstance
    Properties:
      DBInstanceIdentifier: db-exemplo-01
  Arquivos:
    Type: AWS::S3::Bucket
    Properties:
      BucketName: !Sub 'bkt-relatorios-demo-${AWS::Region}'
# after
      DBInstanceIdentifier: db-gbzw6njk
      BucketName: !Sub 'bkt-sfdst7tg-${AWS::Region}'
```

**Bicep**
```bicep
// before
resource sql 'Microsoft.Sql/servers@2023-08-01' = {
  name: 'sqlsrv-exemplo-01'
  location: location
}
// after
resource sql 'Microsoft.Sql/servers@2023-08-01' = {
  name: 'host-7rhd4fsp'
  location: location
}
```

**Plan output**
```text
# before
  ~ resource "aws_db_instance" "principal" {
      ~ identifier = "db-exemplo-01" -> "db-exemplo-02"
      + address    = (known after apply)
# after
  ~ resource "aws_db_instance" "principal" {
      ~ identifier = "db-gbzw6njk" -> "db-6rosk7bp"
      + address    = (known after apply)
```

### 6. Hard cases and limits

- **Local name vs real name.** `"relatorios"` (HCL), `BancoRelatorios` (CFN) and `sql` (Bicep) are
  code names; the real name of the resource is the value of `bucket`, `identifier` or `name`. The local
  name usually leaks the subject but not the resource. Default: **do not mask**, so the model can
  read and fix the code. Configurable option: mask with full consistency (declaration, every
  `type.name.*` reference, `!Ref`/`!GetAtt` and the address in the plan `aws_s3_bucket.relatorios`).
- **Interpolation that builds the name.** `"${local.amb}-${var.app}-db"` has no literal name; the real name
  only exists after evaluation. What gets masked is the origin of the value: the `default` of the variable,
  `locals`, `.tfvars`. In the isolated file, the loose literal (`-db`) is generic and is not masked.
  Limit: the full name appears again, already assembled, in the plan output and in the state, and there it is caught by the
  attribute.
- **Plan with `~`, `+`, `-/+`.** Both sides of `->` are masked, each with its own pseudonym
  (different values produce different pseudonyms). `(known after apply)`, `(sensitive value)` and
  `null` are vocabulary. The address after `#` follows the rule of the local name. In `-/+` with
  `# forces replacement`, the comment stays.
- **Generic attribute `name`.** Without the resource type the entity is unknown. Inside nested
  blocks (`tags { Name = ... }`, `sku { name = "Standard_LRS" }`) the value is often
  vocabulary (SKU, tier). The table of item 3 solves this by the full path
  `(type, block, attribute)`, not by the loose name.
- **`Name` tags.** They are free text, but in practice they repeat the real name. Treat `tags.Name` as of the
  same type as the resource. `[VERIFY]` whether it is worth extending to other tags.
- **A value that is already a reference to another resource** (`vpc_id = aws_vpc.principal.id`): do not mask.
  If it is a literal ID (`host-bakbyhxo…`, ARN), it falls into the IDs/ARN family, not this one.
- **ARN and connection strings inside values** (`arn:aws:rds:…:db:db-exemplo-01`,
  `Server=tcp:sqlsrv-exemplo-01.database.windows.net`): the final segment is masked with the **same**
  pseudonym as the resource, and the provider's public DNS suffix stays.
- **Ansible**: the host appears as a key in YAML (`hosts: { db-exemplo-01: … }`), as a token in INI
  and as a file name in `host_vars/`. All three need the same pseudonym. Ranges
  `db[01:03].exemplo` are masked as a unit (the whole pattern becomes one pseudonym). Expanding the
  range would produce independent pseudonyms and break the pattern.
- **Constraints the pseudonym does not meet.** `HOST_7rhd4fsp` has `_` and uppercase, so it is invalid in
  a bucket, a storage account and a hostname. Without the derived form of item 4, the file stays
  syntactically valid but fails the provider's `validate`. That is not a problem if the file only goes
  to the LLM, but it is a problem if the response is applied without reverting the masking.
- **Limits.** Third-party providers and private modules are outside the schema; an unknown attribute
  becomes `[VERIFY]` and is not masked blindly. HEREDOC (`<<EOT`) and `jsonencode()` with an inline
  policy require a second pass with the JSON/SQL recognizer. Jinja2 templates with logic
  (`{% for %}`) are only partially parsed.

### 7. Links used

- Terraform `providers schema -json`: https://developer.hashicorp.com/terraform/cli/commands/providers/schema
- Terraform JSON output (plan/state): https://developer.hashicorp.com/terraform/internals/json-format
- Terraform resource syntax: https://developer.hashicorp.com/terraform/language/resources/syntax
- HCL native spec: https://github.com/hashicorp/hcl/blob/main/hclsyntax/spec.md
- Ansible inventory: https://docs.ansible.com/ansible/latest/inventory_guide/intro_inventory.html
- Playbook keywords: https://docs.ansible.com/ansible/latest/reference_appendices/playbooks_keywords.html
- CloudFormation resource specification: https://docs.aws.amazon.com/AWSCloudFormation/latest/TemplateReference/cfn-resource-specification.html
- CloudFormation resource provider schemas: https://docs.aws.amazon.com/AWSCloudFormation/latest/TemplateReference/resource-type-schemas.html
- CFN intrinsic functions: https://docs.aws.amazon.com/AWSCloudFormation/latest/TemplateReference/intrinsic-function-reference.html
- S3 bucket naming rules: https://docs.aws.amazon.com/AmazonS3/latest/userguide/bucketnamingrules.html
- Bicep, resource declaration: https://learn.microsoft.com/azure/azure-resource-manager/bicep/resource-declaration
- ARM template structure: https://learn.microsoft.com/azure/azure-resource-manager/templates/syntax
- Azure resource naming rules: https://learn.microsoft.com/azure/azure-resource-manager/management/resource-name-rules
- Azure type reference: https://learn.microsoft.com/azure/templates/


## Data tools

Central idea: in these tools the object name **always occupies a fixed position** (a YAML/JSON key,
a function argument, an XML attribute). The rule is: recognize the tool → locate the position → mask
the **value**, never the key. The keys, function names and enums are public vocabulary and stay intact.
Spans of embedded SQL (`sql=`, `op.execute(...)`, the body of a Flyway file, `compiled_code`) go to the SQL masker.

Typed pseudonyms: `DB_`, `SCH_`, `T_`, `C_`, `CONN_`, `JOB_` (DAG/job/task), `INST_` (platform_instance).
Format `PREFIX_[a-z0-9]{4}` — only `[A-Za-z0-9_]`, so it is a valid unquoted identifier, a valid Python/YAML
key and a **safe literal inside a regex** (it has no metacharacter).

### 1. Detection signals

| Tool | Structural signal (1 strong or 2 weak are enough) |
|---|---|
| dbt | Jinja `{{ ref(` / `{{ source(` / `{{ config(`; YAML with the root `models:`/`sources:`/`seeds:` containing `columns:` + `data_tests:`/`tests:`; `dbt_project.yml` (`name`, `profile`, `model-paths`); `profiles.yml` (`target:` + `outputs:` + `type:`); JSON with `nodes`+`parent_map`+`child_map`; `unique_id` in the format `model.<pkg>.<name>` |
| Airflow | `DAG(` / `@dag(` with `dag_id=`; `Operator(` with `task_id=`; `conn_id=`/`*_conn_id=`; `Variable.get(`, `BaseHook.get_connection(`; env `AIRFLOW_CONN_<ID>`; URI `type://login@host:port/schema?extra`; logs with `dag_id=… task_id=… run_id=…` |
| DataHub | YAML with `source:` → `type:` + `config:` and optional `sink:`/`transformers:`; keys `*_pattern:` with `allow:`/`deny:`; the text `urn:li:` |
| OpenLineage | JSON with `eventType` ∈ {START, RUNNING, COMPLETE, ABORT, FAIL, OTHER} + `run.runId` + `job.namespace`/`job.name` + `producer`/`schemaURL` |
| Great Expectations | `import great_expectations as gx`; classes `Expect[A-Z]\w+(` or methods `expect_\w+(`; `add_table_asset(`, `add_query_asset(`, `data_sources.add_<type>(`, `batch_request` |
| Liquibase | XML `<databaseChangeLog>`/`<changeSet>`; YAML/JSON `databaseChangeLog:` → `changeSet:` → `changes:`; attributes `tableName`, `columnName` |
| Flyway | file name `^[VUR]\d*[._\d]*__\w+\.sql$` (e.g. `V001.002__X.sql`, `R__X.sql`) |
| Alembic | `from alembic import op`; `op.create_table(`, `op.add_column(`, `revision = '…'` + `down_revision` |

### 2. Position → entity type

**dbt**

| Position | Type |
|---|---|
| `ref('m')` / `ref('pkg','m')` / `ref('m', v=2)` — last string literal | T (model) |
| `source('s','t')` — 1st arg / 2nd arg | SCH (logical name of the source) / T |
| `config(database=, schema=, alias=)` | DB / SCH / T |
| YAML `sources[].name` / `.database` / `.schema` / `.tables[].name` / `.tables[].identifier` | SCH / DB / SCH / T / T |
| YAML `models[].name`, `seeds[].name`, `snapshots[].name` | T |
| `columns[].name`; `arguments.column_name`, `arguments.field` (relationships) | C |
| `arguments.to: ref('x')` | T (via Jinja) |
| `profiles.yml` → `database`, `dbname`, `schema`, `warehouse`, `role` | DB / DB / SCH / [VERIFY type: resource] |
| `manifest.json` nodes: `name`, `alias`, `database`, `schema`, `relation_name`, `columns.<k>.name`; `unique_id` keys (`model.pkg.name`) | T/T/DB/SCH/FQN/C/last segment T |
| `dbt ls` output (`pkg.folder.model`, `source:s.t`) | last segment T; in `source:` SCH.T |

**Airflow**

| Position | Type |
|---|---|
| `dag_id=`, `task_id=`, `@dag` function name | JOB |
| `conn_id=`, `*_conn_id=` (e.g. `postgres_conn_id`), suffix of `AIRFLOW_CONN_<ID>` | CONN |
| `database=`, `schema=`, `table=` of operators | DB / SCH / T |
| `sql=` | SQL → SQL masker |
| Connection URI: path `/schema` | SCH (the host is left to the network family) |
| `Variable.get("key")` | [VERIFY] — the key may be sensitive; type `VAR_` |

**DataHub**

| Position | Type |
|---|---|
| `source.config.database`, `database_pattern`, `schema_pattern`, `table_pattern`, `view_pattern` (`allow`/`deny`) | DB / DB regex / SCH regex / FQN regex / FQN regex |
| `platform_instance` | INST |
| `source.config.host_port` | host → network family |
| URN `urn:li:dataset:(urn:li:dataPlatform:<plat>,<name>,<FABRIC>)` | `<name>` = FQN; `<plat>` and `<FABRIC>` public |

**OpenLineage**

| Position | Type |
|---|---|
| `job.namespace` / `job.name` | INST / JOB |
| `inputs[]`/`outputs[]` `.namespace` | `scheme://host:port` (public scheme, host → network); `bigquery`, `file` public |
| `inputs[]`/`outputs[]` `.name` | FQN: split on `.` according to the platform (below) |
| `facets.schema.fields[].name` (recursive in `.fields`) | C |

Splitting of the `name` (naming spec): Postgres/MSSQL/Redshift/Snowflake `db.schema.table`; Trino `catalog.schema.table`;
MySQL/Hive `db.table`; BigQuery `project.dataset.table`; S3/GCS object key (path).

**Great Expectations / Liquibase / Alembic / Flyway**

| Position | Type |
|---|---|
| GX `column=`, `column_list=[…]`, `add_batch_definition_*(column=)` | C |
| GX `add_table_asset(table_name=)`, `schema_name=` [VERIFY] | T / SCH |
| GX `name=` of a data source/asset, `data_asset_name` | free label — mask if equal to a name already mapped |
| GX `add_query_asset(query=)`, connection string | SQL / network family |
| Liquibase `tableName`, `baseTableName`, `referencedTableName`, `newTableName`, `oldTableName` | T |
| Liquibase `schemaName`, `catalogName` | SCH / DB |
| Liquibase `column name=`, `columnName`, `baseColumnNames`, `referencedColumnNames` (comma-separated list) | C |
| Alembic `create_table(T, sa.Column(C, …))`, `add_column(T, sa.Column(C))`, `drop_column(T, C)`, `alter_column(T, C, new_column_name=C)` | positional |
| Alembic `create_index(name, T, [C…])`, `create_foreign_key(name, T_source, T_ref, [C], [C])`, `rename_table(T, T)`, `schema=`/`source_schema=`/`referent_schema=` | T/C/SCH |
| Alembic `op.execute("…")`; body of `V*__*.sql` | SQL |
| Flyway description in the file name (`V3__cria_tb_pedido_x9.sql`) | only if it contains a name already mapped in the body |

### 3. Public vocabulary (never mask)

| Set | Items (sample) | Source / how to extract |
|---|---|---|
| dbt generic tests | `unique`, `not_null`, `accepted_values`, `relationships` | docs.getdbt.com/docs/build/data-tests ("four generic data tests") |
| dbt keys | `models`, `sources`, `seeds`, `columns`, `data_tests`, `tests`, `arguments`, `config`, `values`, `to`, `field`, `quoting`, `identifier`, `severity`, `where` | same page + /docs/build/sources; package tests `pkg.name` (e.g. `dbt_utils.…`) are public by the prefix |
| `manifest.json` keys | `metadata`, `nodes`, `sources`, `macros`, `parent_map`, `child_map`, `unique_id`, `package_name`, `original_file_path` | /reference/artifacts/manifest-json + JSON Schema at schemas.getdbt.com (extract `properties` recursively) |
| `type:` of an adapter / DataHub source | `snowflake`, `bigquery`, `postgres`, `mysql`, `mssql`, `oracle`, `redshift`, `hive`, `kafka`, `dbt`, `iceberg`, … | index docs.datahub.com/docs/generated/ingestion/sources/ (last segment of the URLs) |
| DataHub recipe keys | `source`, `type`, `config`, `sink`, `transformers`, `host_port`, `database`, `allow`, `deny`, `ignoreCase`, `platform_instance`, `env` | recipe_overview + the page of each source (config table) |
| DataHub fabric | `PROD`, `DEV`, `QA`, `TEST`, … [VERIFY the complete list in the FabricType enum] | source code of the PDL model |
| OpenLineage | `eventType`, `eventTime`, `run`, `runId`, `job`, `inputs`, `outputs`, `namespace`, `name`, `facets`, `inputFacets`, `outputFacets`, `producer`, `schemaURL`, `fields`, `type`, `description` | JSON Schema openlineage.io/spec/2-0-2/OpenLineage.json and spec/facets/*.json (extract `properties`) |
| Airflow operators | structural rule: an identifier `\w+Operator`/`\w+Sensor` imported from `airflow.*`; parameters `task_id`, `sql`, `conn_id`, `parameters`, `split_statements` | airflow.apache.org (provider reference) |
| GX expectations | structural rule `^Expect[A-Z][A-Za-z]+$` and `^expect_[a-z_]+$` (e.g. `ExpectColumnMaxToBeBetween`) | docs.greatexpectations.io + Expectation Gallery |
| Liquibase | `databaseChangeLog`, `changeSet`, `createTable`, `addColumn`, `column`, `constraints`, `primaryKey`, `nullable`, `type`, `remarks`, `tablespace` | docs.liquibase.com/change-types/* (attribute table of each change type) |
| Alembic | `op.*` and parameters (`schema`, `nullable`, `type_`, `server_default`, `existing_type`) | alembic.sqlalchemy.org/en/latest/ops.html |

The SQL types inside `type=` (`varchar(255)`, `INT`) follow the public list of the SQL masker.

### 4. Identifier rules

**Jinja (dbt).** Only mask **string literals** in the arguments of `ref`/`source`/`config`. Expressions
(`var('x')`, `env_var('X')`, `target.schema`, `this`) are code: they are kept. Preserve the kind of quotes
(`'`/`"`) and the spaces inside `{{ }}`. `ref('pkg','m')`: the package is a project name → mask only if it is in the map.

**Quotes and letter case.** Remove the quotes to look up the map and put them back the same. In Snowflake/Postgres with
`quoting: true` or a quoted `"Nome"` the case is significant: map the exact form; unquoted, the map key is
normalized (uppercase in Snowflake, lowercase in Postgres). The manifest's `relation_name` comes already quoted
(`"db"."sch"."t"`): split into quoted parts, mask each one, quote again.

**Regex in `allow`/`deny` (DataHub).** The pattern is applied **at the start** of the name (`re.match` style), against the
qualified name (`db.schema.table` in `table_pattern`). To mask without breaking:
1. Tokenize the regex into *literals* and *metacharacters*. Metas: `^ $ . * + ? ( ) [ ] { } | \d \w \s`.
   `\.` is a **literal separator of parts** (not a meta).
2. Each literal sequence `[A-Za-z0-9_]+` between separators is a candidate; the type comes from the position
   (1st segment DB, 2nd SCH, 3rd T in `table_pattern`; a single segment in `schema_pattern` = SCH).
3. If the whole literal is in the map → replace it with the pseudonym. Since the pseudonym only has `[A-Za-z0-9_]`, it needs no escaping.
4. Keep metas, anchors, classes and escapes in place. Check: the masked regex must match the masked name
   whenever the original matched the original (automatic test with the names of the map).
5. `ignoreCase: true` (default) → case-insensitive lookup.
6. YAML escape: in double quotes `"\\."` = regex `\.`; in single quotes/unquoted `'\.'`. Do not rewrite the quoting style.

### 5. Before/after examples (fictitious)

dbt (schema.yml + model):
```yaml
# before                                  # after
sources:                                  sources:
  - name: financeiro                        - name: SCH_mvrhafae
    tables:                                   tables:
      - name: tb_pedido_x9                      - name: T_cszwa3ri
models:                                   models:
  - name: fct_pedido_x9                     - name: T_lnnfl7qr
    columns:                                  columns:
      - name: vl_total                          - name: C_xsdgeq7l
        data_tests: [not_null, unique]            data_tests: [not_null, unique]
```
```sql
{{ config(schema='financeiro', alias='fct_pedido_x9') }}   →  {{ config(schema='SCH_mvrhafae', alias='T_lnnfl7qr') }}
select vl_total from {{ source('financeiro','tb_pedido_x9') }}
→ select C_xsdgeq7l from {{ source('SCH_mvrhafae','T_cszwa3ri') }}
```

Airflow:
```python
DAG(dag_id="carga_pedido_x9")                          →  DAG(dag_id="SVC_56abpg7a")
SQLExecuteQueryOperator(task_id="soma_total",          →  SQLExecuteQueryOperator(task_id="SVC_gxv6bzrw",
    conn_id="conn_vendas_demo",                        →      conn_id="SVC_tnmdaymg",
    sql="select sum(vl_total) from financeiro.tb_pedido_x9")  →  sql="select sum(C_xsdgeq7l) from SCH_mvrhafae.T_cszwa3ri")
# AIRFLOW_CONN_CONN_VENDAS_DEMO  →  AIRFLOW_CONN_CONN_X7K2  (the env requires uppercase: case-insensitive map)
```

DataHub (recipe + URN):
```yaml
source:                                   source:
  type: postgres                            type: postgres
  config:                                   config:
    database: vendas_demo                     database: DB_egzghucd
    table_pattern:                            table_pattern:
      allow: ["^vendas_demo\\.financeiro\\..*"]   allow: ["^DB_egzghucd\\.SCH_mvrhafae\\..*"]
      deny:  [".*\\.tb_pedido_x9$"]               deny:  [".*\\.T_cszwa3ri$"]
```
`urn:li:dataset:(urn:li:dataPlatform:postgres,vendas_demo.financeiro.tb_pedido_x9,PROD)`
→ `urn:li:dataset:(urn:li:dataPlatform:postgres,DB_egzghucd.SCH_mvrhafae.T_cszwa3ri,PROD)`

OpenLineage:
```json
{"eventType":"COMPLETE","job":{"namespace":"airflow_demo","name":"carga_pedido_x9.soma_total"},
 "inputs":[{"namespace":"postgres://db.exemplo.local:5432","name":"vendas_demo.financeiro.tb_pedido_x9",
   "facets":{"schema":{"fields":[{"name":"vl_total","type":"NUMERIC"}]}}}]}
```
→ `"namespace":"HOST_7h2tokhw"`, `"name":"SVC_56abpg7a.SVC_gxv6bzrw"`, `"name":"DB_egzghucd.SCH_mvrhafae.T_cszwa3ri"`,
`"fields":[{"name":"C_xsdgeq7l","type":"NUMERIC"}]` (the host of the namespace goes to the network family; `postgres://` and `NUMERIC` stay).

Liquibase / Alembic / GX:
```xml
<createTable schemaName="financeiro" tableName="tb_pedido_x9">   →  <createTable schemaName="SCH_mvrhafae" tableName="T_cszwa3ri">
  <column name="vl_total" type="numeric(12,2)"/>                  →    <column name="C_xsdgeq7l" type="numeric(12,2)"/>
```
```python
op.add_column('tb_pedido_x9', sa.Column('vl_total', sa.Numeric()), schema='financeiro')
→ op.add_column('T_cszwa3ri', sa.Column('C_xsdgeq7l', sa.Numeric()), schema='SCH_mvrhafae')
gx.expectations.ExpectColumnValuesToNotBeNull(column="vl_total")  →  (column="C_xsdgeq7l")
```

### 6. Hard cases and limits

- **Regex with a fragment of a name.** `^tb_ped.*`, `.*_x9$`, `financeiro_(2023|2024)`: the literal is a *piece* of a name,
  not a whole name; replacing it with a pseudonym breaks the match. Policy: (a) if the fragment matches ≥1 name in the
  map, generate a **pattern** pseudonym (`T_kwvciyut`) and **do not promise equivalence** — mark `[VERIFY]` in the
  diagnostic output; (b) if it matches nothing known, leave it as it is (it is structure, e.g. `_tmp$`).
  Alternation `(a|b)` with whole literals → mask each branch. Classes `[0-9]{4}` are always kept.
- **Convention prefix/suffix** (`stg_`, `_tmp`, `_x9`): it is not an entity; mask the whole name, never the affix,
  otherwise two different names may collide or leak the convention. Accept that the LLM loses the semantic hint.
- **Concatenation in Python.** `f"{schema}.tb_{dominio}_x9"`, `"tb_" + nome`, `"%s.%s" % (s, t)`, `.format()`:
  the final name only exists at run time. Only a literal that, by itself, is a known whole name is masked.
  Fragments stay; this is a **declared limit** (partial leak possible). Mitigation: the log/CLI output,
  where the name already appears complete, is masked normally.
- **A name that coincides with the vocabulary.** A column called `name`, `type`, `schema`, `unique`: it is decided by the
  **position** (value vs. key), never by the text. `columns: - name: name` → only the value becomes `C_…`.
- **Logical name ≠ physical name.** dbt `source name` vs `schema`, GX `name=` of the asset, Liquibase `id`/`author`
  of the changeSet: they are labels; mask only if the value is already in the map (it avoids bloating the map with free text).
- **Compound `job.name`.** In the Airflow integration the pattern `dag_id.task_id` is common [VERIFY in the integration docs];
  split on `.` and mask each part with the same map as the DAG, to keep the link.
- **Tabular CLI output** (`airflow dags list`, `datahub get`, `dbt ls`): recognize it by the header line
  and mask per column; the space alignment may change — acceptable.
- **Escapes and sizes.** A fixed-length pseudonym changes aligned columns, and `varchar` in DDL is not affected
  (the size belongs to the data, not to the name). In a URN, never introduce `(`, `)` or `,` (forbidden in the tuple).
- **Connection env var.** `AIRFLOW_CONN_<ID>` is uppercase: the map needs to match `conn_vendas_demo` ↔
  `CONN_VENDAS_DEMO` so it does not generate two pseudonyms.

### 7. Links used

- https://docs.getdbt.com/docs/build/data-tests
- https://docs.getdbt.com/docs/build/sources
- https://docs.getdbt.com/reference/artifacts/manifest-json
- https://docs.getdbt.com/docs/core/connect-data-platform/connection-profiles
- https://airflow.apache.org/docs/apache-airflow-providers-common-sql/stable/operators.html
- https://airflow.apache.org/docs/apache-airflow/stable/howto/connection.html
- https://docs.datahub.com/docs/metadata-ingestion/recipe_overview
- https://docs.datahub.com/docs/what/urn
- https://docs.datahub.com/docs/generated/ingestion/sources/ (source pages: semantics of `allow`/`deny`/`ignoreCase`)
- https://openlineage.io/docs/spec/object-model
- https://openlineage.io/docs/spec/naming
- https://openlineage.io/spec/2-0-2/OpenLineage.json
- https://openlineage.io/spec/facets/1-1-1/SchemaDatasetFacet.json
- https://docs.greatexpectations.io/docs/core/define_expectations/create_an_expectation
- https://docs.greatexpectations.io/docs/core/connect_to_data/sql_data/
- https://docs.liquibase.com/change-types/create-table.html
- https://documentation.red-gate.com/fd/versioned-migrations-273973333.html
- https://documentation.red-gate.com/fd/repeatable-migrations-273973335.html
- https://alembic.sqlalchemy.org/en/latest/ops.html


## Code

Goal: find object names (host, database, schema, table, column, namespace, user) **by the position
they occupy** in a known grammar or API. No language dictionary: the vocabulary below is only
keywords and API/option names, taken from the official documentation.

### 1. Detection signals

A region becomes "code" when at least one of these signals appears:

| Signal | Example | Weight |
|---|---|---|
| Markdown fence with a language | ` ```python `, ` ```sql `, ` ```bash ` | strong |
| Import/use of a known library | `from sqlalchemy import`, `import pandas as pd`, `from pyspark.sql`, `import { Entity } from "typeorm"`, `import jakarta.persistence.*` | strong |
| ORM annotation/decorator | `@Entity`, `@Table(`, `@Column(`, `@@map(` | strong |
| The first token of the line is a CLI binary from the list (section 3) | `psql`, `sqlcmd`, `mysql`, `sqlplus`, `bq`, `snow`, `kubectl`, `ssh`, `scp`, `az`, `aws`, `gcloud` | strong if it comes with an option from the list |
| Shell prompt | `$ `, `PS> `, `# ` followed by a binary from the list | medium |
| A string literal whose content starts with an SQL verb | `"SELECT `, `"""\n  INSERT INTO`, `` `UPDATE `` | medium, confirmed by the SQL reader |

Rule: the signal only **opens** the code mode. What decides what is a name is the table in section 2. A word outside
a position of the table is never masked by this family.

### 2. Position → entity type

**(a) SQL inside a string** — find the literal, extract the text, hand it to the SQL reader (SQL family), which
returns the typed names. The code family only takes care of *cutting out* and *putting back* the text.

| Construct | Where the SQL is |
|---|---|
| Python `"..."`, `'...'`, `"""..."""`, `'''...'''`, prefixes `r`, `u`, `f`, `rf`, `fr` (case-insensitive) | the content between the quotes |
| Adjacent literals `("SELECT a " "FROM t")` | concatenate before handing over (Python joins neighbouring literals) |
| Java 15+/Kotlin text block `"""` ... `"""` | from the end of the opening line to the final `"""` |
| JS/TS template literal `` `...${x}...` `` | the content between backticks |
| Argument of `cursor.execute(...)`, `text(...)`, `spark.sql(...)`, `pd.read_sql(...)`, `@NamedQuery(query=...)`, `createQuery(...)`, `$queryRaw` | 1st positional argument (or `query=`) |

**(b) ORMs**

| Pattern | Entity |
|---|---|
| SQLAlchemy `Table("x", metadata, ...)` | 1st arg → table; `schema="s"` → schema |
| SQLAlchemy `Column("y", ...)` | 1st string arg → column |
| SQLAlchemy `__tablename__ = "x"` | table |
| SQLAlchemy `__table_args__ = {"schema": "s"}` (or a dict at the end of the tuple) | schema |
| SQLAlchemy `mapped_column("y", ...)` or `mapped_column(name="y")` | column |
| SQLAlchemy `ForeignKey("s.t.c")` | schema.table.column (split on the dot) |
| `create_engine("dialect://user:...@host:port/db")` | user, host, database (see URL in the connection family) |
| Django `class Meta: db_table = "x"` | table (it may come as `'"s"."x"'` in Postgres → schema + table) |
| Django `models.XField(db_column="y")` | column |
| Django `db_table_comment`, `db_tablespace="ts"` | tablespace (if the type exists) |
| JPA `@Table(name="x", schema="s", catalog="c")` | table / schema / database |
| JPA `@Column(name="y")`, `@JoinColumn(name="y", referencedColumnName="z")` | column |
| JPA `@JoinTable(name="x", schema="s")`, `@SecondaryTable`, `@CollectionTable` | table / schema |
| JPA `@NamedQuery(query="...")` | **JPQL**: it uses *entity* and *attribute* names, not table names; only mask if the same name was already mapped as a table/column |
| JPA `@NamedNativeQuery(query="...")` | SQL → SQL reader |
| Prisma `model X { ... @@map("x") }` | `@@map` → table; `@@schema("s")` → schema |
| Prisma field `y Int @map("y")` | column |
| Prisma `datasource db { url = "..." }` | connection URL; `env("VAR")` has no name (only the variable name) |
| TypeORM `@Entity("x")` or `@Entity({ name: "x", schema: "s", database: "d" })` | table / schema / database |
| TypeORM `@Column({ name: "y" })`, `@JoinColumn({ name: "y" })`, `@PrimaryColumn({ name: "y" })` | column |

**(c) pandas / PySpark**

| Pattern | Entity |
|---|---|
| `df["y"]`, `df[["a","b"]]`, `df.loc[:, "y"]` | column (string inside the subscript) |
| `df.y` | column **only if** `y` already appeared as a column in another position of the same text (see section 6) |
| `col("y")`, `F.col("y")`, `df.select("a","b")`, `withColumn("y", ...)`, `groupBy("y")`, `.rename(columns={"a": "b"})` | column |
| `spark.table("d.t")`, `spark.read.table("c.d.t")`, `df.write.saveAsTable("d.t")`, `insertInto("d.t")` | multi-part name: 1 part → table; 2 → database.table; 3 → catalog.database.table |
| `spark.sql("...")`, `pd.read_sql("...", con)`, `pd.read_sql_query("...")` | SQL → SQL reader |
| `pd.read_sql_table("t", con, schema="s")` | table / schema |
| `df.to_sql("t", con, schema="s")` | table / schema |
| `.option("dbtable", "s.t")`, `.option("url", "jdbc:...")`, `.option("user", "u")` | table / URL / user |

**(d) CLIs with names in options**

| Command | Option → entity |
|---|---|
| `psql` | `-h/--host` host; `-d/--dbname` database (accepts URI/conninfo); `-U/--username` user; 1st positional = database, 2nd = user; `-c` SQL |
| `sqlcmd` | `-S [tcp:]server[\instance][,port]` host (+instance); `-d` database; `-U` user; `-Q`/`-q` SQL |
| `mysql` | `-h/--host` host; `-D/--database` database; `-u/--user` user; positional = database; `-e/--execute` SQL |
| `sqlplus` | `user[/password]@identifier` → user + host/service (EZConnect `host:port/service`) |
| `bq` | `--project_id` project; `--dataset_id` dataset; reference `[project:]dataset.table` in `show/ls/mk/rm/load` |
| `snow sql` | `-c/--connection` connection name; `--account` account; `--user` user; `--database`, `--schema`, `--warehouse`, `--role`, `--host`; `-q` SQL |
| `kubectl` | `-n/--namespace` namespace; `--context`, `--cluster`, `--user`; `-s/--server` host |
| `ssh` | destination `[user@]host` or `ssh://[user@]host[:port]`; `-l` user; `-J` jump host |
| `scp` | `[user@]host:path` (the `:` separates host from path) |
| `az sql ...` | `--resource-group/-g`, `--server/-s` host, `--name/-n` database (in the `sql db` context) |
| `aws rds ...` | `--db-instance-identifier`, `--db-cluster-identifier`, `--db-name` |
| `gcloud sql ...` | `--project` project; `--instance` or the positional of the instance; `--database/-d` database; `--user/-u` user |

### 3. Public vocabulary (source and how to extract)

| Vocabulary | Source | How to extract |
|---|---|---|
| Python string prefixes and quotes | docs.python.org, *Lexical analysis* | fixed list: `r b f t u` + combinations with `r`; quotes `' " ''' """` |
| JS template literal | MDN/ECMA-262 (*Template literals*) | backtick, `${`...`}` |
| Java text block | JEP 378 / JLS §3.10.6 | `"""` + line break |
| SQLAlchemy constructors | docs.sqlalchemy.org (*Core: Table, Column*; *ORM: mapped_column, Declarative*) | names `Table Column mapped_column ForeignKey __tablename__ __table_args__ schema name` |
| Django options | docs.djangoproject.com (*Model Meta options*, *Model field reference*) | `db_table db_column db_tablespace` |
| JPA annotations | jakarta.ee/specifications/persistence (package `jakarta.persistence`) | `Table Column JoinColumn JoinTable SecondaryTable CollectionTable NamedQuery NamedNativeQuery` + attributes `name schema catalog referencedColumnName query` |
| Prisma attributes | prisma.io/docs (*Prisma schema reference*) | `model datasource @map @@map @@schema url env` |
| TypeORM decorators | typeorm.io (*Entities*, *Decorator reference*) | `Entity Column PrimaryColumn JoinColumn` + keys `name schema database` |
| pandas API | pandas.pydata.org (*read_sql*, *read_sql_table*, *DataFrame.to_sql*) | method name + position/kw of the parameter (`name`, `schema`, `table_name`) |
| PySpark API | spark.apache.org/docs (*SparkSession.table*, *DataFrameReader.table*, *DataFrameWriter.saveAsTable*, *JDBC data source*) | methods + options `dbtable url user` |
| CLI options | the official page of each one (section 7) | copy the options table (short and long) into an `option → type` map per binary |

Suggested storage format: one file per language/CLI with `{api, position, type}` (e.g.
`{"psql", "-d", DB}`, `{"Table", arg0, T}`), versioned with the URL and the date of the lookup.

### 4. Identifier rules

1. **Host string escaping first, SQL afterwards.** Decode the literal (`\"`, `\\`, `\n`) before
   reading the SQL; when putting it back, **re-escape** the pseudonym the same way. In an `r"..."` literal there is no escaping.
   Since the pseudonyms are `[A-Z0-9_]`, they need no escaping — the rule matters when *reading* the name.
2. **SQL quotes stay.** `"vl_total"`, `` `vl_total` ``, `[vl_total]` → only the inside is replaced:
   `"C_xsdgeq7l"`. A name between double quotes is case-sensitive in Postgres/Snowflake; the pseudonym is always the same
   for the same exact spelling (case-sensitive in the pseudonym table when it was between quotes).
3. **Multi-part name** (`d.t`, `project:dataset.table`, `s.t.c`): split on the API's separator (`.`; `:` only in
   `bq` and `scp`) and mask each part with its type.
4. **f-string / template / text block with interpolation**: replace each `{expr}`/`${expr}` with an opaque
   marker (`__P1__`), send the rest to the SQL reader, mask what is a literal name and restore the marker.
   The content of `{expr}` is Python/JS, not SQL: never mask inside it by this rule. `{{`/`}}` in an f-string
   become `{`/`}` only when reading; when writing they are doubled again.
5. **Concatenation** (`"SELECT * FROM " + tabela`, `"a " "b"`): neighbouring literals are joined; with `+` and
   a variable, read only the literal part and treat the hole as an opaque marker (see section 6).
6. **CLI options**: accept the three forms `-d vendas_demo`, `-dvendas_demo`, `--dbname=vendas_demo`
   (getopt). A value between shell quotes (`-d "vendas_demo"`) → strip the quotes to read, put them back when writing.
7. **Consistency**: the same real name produces the same pseudonym in the whole text (SQL, ORM and CLI together), so
   that `@Table(name="tb_pedido_x9")` and `SELECT ... FROM tb_pedido_x9` keep matching.

### 5. Before/after examples

Python with an f-string (the `{dt}` is not touched):
```python
# before
sql = f"""SELECT vl_total FROM vendas_demo.tb_pedido_x9 WHERE dt_ref = '{dt}'"""
# after
sql = f"""SELECT C_xsdgeq7l FROM DB_gbzw6njk.T_cszwa3ri WHERE C_fvp7nx2j = '{dt}'"""
```

SQLAlchemy and Django:
```python
# before
pedido = Table("tb_pedido_x9", metadata, Column("vl_total", Numeric), schema="vendas_demo")
class Pedido(models.Model):
    total = models.DecimalField(db_column="vl_total")
    class Meta:
        db_table = "tb_pedido_x9"
# after
pedido = Table("T_cszwa3ri", metadata, Column("C_xsdgeq7l", Numeric), schema="SCH_wy3wj5vm")
class Pedido(models.Model):
    total = models.DecimalField(db_column="C_xsdgeq7l")
    class Meta:
        db_table = "T_cszwa3ri"
```
(Note that `Pedido` and `total` are identifiers of the program, not of the database: they stay.)

JPA and Prisma:
```java
// before
@Table(name = "tb_pedido_x9", schema = "vendas_demo")
class Pedido { @Column(name = "vl_total") BigDecimal total; }
// after
@Table(name = "T_cszwa3ri", schema = "SCH_wy3wj5vm")
class Pedido { @Column(name = "C_xsdgeq7l") BigDecimal total; }
```
```prisma
model Pedido { total Decimal @map("C_xsdgeq7l")  @@map("T_cszwa3ri") }   // before: "vl_total", "tb_pedido_x9"
```

PySpark / pandas:
```python
df = spark.read.table("vendas_demo.tb_pedido_x9").select("vl_total")   # before
df = spark.read.table("DB_gbzw6njk.T_cszwa3ri").select("C_xsdgeq7l")               # after
df.to_sql("tb_pedido_x9", con, schema="vendas_demo")                    # before
df.to_sql("T_cszwa3ri", con, schema="SCH_wy3wj5vm")                               # after
```

Shell:
```bash
psql -h db-exemplo-01 -dvendas_demo -U usr_relatorio -c "SELECT vl_total FROM tb_pedido_x9"   # before
psql -h HOST_7rhd4fsp -dDB_gbzw6njkambe -U USR_jqndvqfs -c "SELECT C_xsdgeq7l FROM T_cszwa3ri"                          # after
ssh usr_relatorio@db-exemplo-01        →  ssh USR_jqndvqfs@HOST_7rhd4fsp
bq show projeto-demo:vendas_demo.tb_pedido_x9  →  bq show DB_7jsplxmw:DB_gbzw6njk.T_cszwa3ri
kubectl -n ns-demo get pods            →  kubectl -n NS_eorwmkh3 get pods
```
The pseudonyms are valid unquoted identifiers in SQL, Python, Java and shell, so the code stays
syntactically valid; on the way back, the pseudonym table returns the real name and the command runs as before.

### 6. Hard cases and limits

- **Name built in a variable** (`tabela = "tb_pedido_" + sufixo`; `f"FROM {tabela}"`): the literal
  `"tb_pedido_"` alone is in no position of the table → it is **not masked**. It is only masked if the variable receives
  a whole literal and is used, in the same block, in a known position (`tabela = "tb_pedido_x9"` +
  `spark.table(tabela)`): simple constant propagation, one assignment, no control flow. Outside that,
  record it as not covered.
- **`df.col` is a Python attribute**: `df.vl_total` is grammatically the same as `df.shape` or `df.head`. Rule: only
  mask if `vl_total` was already seen as a column in a strong position (`df["vl_total"]`, SQL, ORM) in the same text,
  and never if the name is a public attribute/method of pandas/PySpark (list from the API reference). When in doubt, it stays.
- **Combined options**: `-dvendas_demo` is `-d` + value; but `-it` (kubectl/docker) are grouped flags. Use the
  per-binary map: if the letter requires a value (`-d`, `-h`, `-U`, `-S`, `-n`), the rest of the token is the value. A glued
  `mysql -pSenha` is a password (secrets family), not a name. Attention: in `psql` and `mysql`, `-h` is the host, but in
  `sqlcmd` `-h` is the number of rows between headers — that is why the map is **per binary**, never global.
- **Same option, different meanings per subcommand**: `az ... -n` is the database in `az sql db`, but the server in
  `az sql server`. The map needs the key `binary + subcommand`.
- **JPQL is not SQL**: `SELECT p FROM Pedido p WHERE p.total > 0` talks about a class and an attribute; do not mask
  as table/column.
- **Interpolation in the middle of an identifier** (`FROM vendas_{amb}.tb_pedido_x9`): the marker breaks the name; only
  the whole, literal part (`tb_pedido_x9`) is masked.
- **Dynamic ORM SQL** (`session.query(Pedido)`, `objects.filter(total__gt=0)`): there is no database name written;
  nothing to do.
- **Unrecognized language** or a string the SQL reader rejects: do not mask by the grammar; it falls to the
  other families. Mark `[VERIFY]` in the log.
- **Honest limit**: the table covers the listed APIs and options; an API of the project itself (`minha_lib.ler("t")`)
  is not recognized by structure.

### 7. Links used

- Python, literals and f-strings: https://docs.python.org/3/reference/lexical_analysis.html
- SQLAlchemy Core (Table/Column): https://docs.sqlalchemy.org/en/20/core/metadata.html
- SQLAlchemy ORM (mapped_column, `__table_args__`): https://docs.sqlalchemy.org/en/20/orm/declarative_tables.html
- Django Meta: https://docs.djangoproject.com/en/stable/ref/models/options/
- Django fields (`db_column`): https://docs.djangoproject.com/en/stable/ref/models/fields/
- Jakarta Persistence: https://jakarta.ee/specifications/persistence/
- Prisma schema reference: https://www.prisma.io/docs/orm/reference/prisma-schema-reference
- TypeORM decorators: https://typeorm.io/docs/help/decorator-reference
- pandas `read_sql`: https://pandas.pydata.org/docs/reference/api/pandas.read_sql.html
- pandas `to_sql`: https://pandas.pydata.org/docs/reference/api/pandas.DataFrame.to_sql.html
- PySpark `DataFrameReader.table`: https://spark.apache.org/docs/latest/api/python/reference/pyspark.sql/api/pyspark.sql.DataFrameReader.table.html
- Spark JDBC: https://spark.apache.org/docs/latest/sql-data-sources-jdbc.html
- psql: https://www.postgresql.org/docs/current/app-psql.html
- sqlcmd: https://learn.microsoft.com/en-us/sql/tools/sqlcmd/sqlcmd-utility
- mysql: https://dev.mysql.com/doc/refman/8.4/en/mysql-command-options.html
- SQL*Plus (connection): https://docs.oracle.com/en/database/oracle/oracle-database/19/sqpug/
- bq: https://docs.cloud.google.com/bigquery/docs/reference/bq-cli-reference
- snow sql: https://docs.snowflake.com/en/developer-guide/snowflake-cli/command-reference/sql-commands/sql
- kubectl: https://kubernetes.io/docs/reference/kubectl/kubectl/
- ssh / scp: https://man.openbsd.org/ssh , https://man.openbsd.org/scp
- az sql db: https://learn.microsoft.com/en-us/cli/azure/sql/db
- aws rds: https://docs.aws.amazon.com/cli/latest/reference/rds/
- gcloud sql: https://cloud.google.com/sdk/gcloud/reference/sql

Note: psql, mysql, snow sql, kubectl, ssh and Python were checked in this research. On the pages of bq,
PySpark and sqlcmd the content came truncated; their options come from the known reference and are marked
**[VERIFY]** against the page before becoming a rule. The ORM, Azure, AWS, gcloud and SQL*Plus links were not
opened in this round: **[VERIFY]**.


## NoSQL and search

Central idea: the name is recognized by the **position it occupies in the syntax** (after `use`, between `db.` and `.find`, in the 1st segment of a URL `/x/_search`, as a key of `properties`...). The fixed vocabulary (`$` operators, Redis commands, mapping types, `_x` endpoints) comes from public lists of the official docs and is **never** masked. What is left in a name position is masked.

### 1. Detection signals

| System | Structural signals (1 strong or 2 weak are enough) |
|---|---|
| MongoDB (mongosh) | a `use <x>` line; `db.<x>.<method>(`; `db.getCollection('<x>')`; keys starting with `$` (`$match`, `$group`); `show dbs` / `show collections`; prompt `test>` or `<db>>`; `ObjectId("...")`; `"queryPlanner"`/`"winningPlan"` (explain) |
| Elasticsearch/OpenSearch | a line `GET|PUT|POST|DELETE /<x>/_<endpoint>`; JSON with `"mappings"` → `"properties"`; `"query": {"bool"|"match"|"term"|"range"...}`; header `health status index uuid pri rep docs.count ...` |
| Redis | a line starting with a known command (`GET`, `SET`, `HGET`, `SCAN`...) followed by a key; prompt `127.0.0.1:6379>`; replies `(integer) n`, `(nil)`, `1) "..."`; `SCAN <cursor> MATCH <pattern>` |
| Cassandra CQL | `CREATE KEYSPACE`, `USE <ks>;`, `<ks>.<table>` after `FROM`/`INTO`/`UPDATE`; `WITH replication = {'class': ...}`; prompt `cqlsh>` / `cqlsh:<ks>>` |
| DynamoDB | JSON with `"TableName"`, `"KeySchema"`, `"AttributeName"`, `"AttributeDefinitions"`, `"KeyType": "HASH"|"RANGE"`; descriptors `{"S": ...}`, `{"N": ...}`; `ExpressionAttributeNames` with `#x` |

A signal alone is weak (`GET x` can be HTTP or Redis): require context (prompt, `_search`, `(integer)`).

### 2. Position → entity type

| Position (pattern) | Entity | Pseudonym |
|---|---|---|
| `use <X>` (mongosh) / `db.getSiblingDB('<X>')` | database | `DB_` |
| `db.<X>.<method>(` / `db.getCollection('<X>')` | collection | `COL_` |
| `from: '<X>'` inside `$lookup` / `$graphLookup`; `$out: '<X>'`; `$merge: {into: '<X>'}` | collection | `COL_` |
| object key in a filter/projection/`$group` that does **not** start with `$` | field | `F_` |
| string `"$<X>"` as a value (field reference in an aggregation) | field | `F_` |
| `"a.b"` (dot notation) | field, each segment | `F_`.`F_` |
| lines of `show dbs` (1st column) / `show collections` | database / collection | `DB_` / `COL_` |
| `"ns": "<db>.<col>"` in explain | db + collection | `DB_`.`COL_` |
| `/<X>/_search`, `/<X>/_doc/<id>`, `PUT /<X>` | index (or alias, or list `a,b`) | `IDX_` |
| `"aliases": {"<X>": {}}`, `"alias": "<X>"`, `"index": "<X>"` | alias / index | `IDX_` |
| key under `"properties"` (recursive) and under `"fields"` (multi-field) | field | `F_` |
| `"field": "<X>"`, `"path": "<X>"`; key inside `match`/`term`/`range` | field | `F_` |
| `index` column in `_cat/indices` (position by the header) | index | `IDX_` |
| 1st argument after a Redis key command (`GET <K>`, `HSET <K> <field> v`) | key | `KEY_` per segment |
| hash field (`HGET k <F>`, `HSET k <F> v`) | field | `F_` |
| `CREATE KEYSPACE <X>` / `USE <X>` / `<X>.<t>` | keyspace | `DB_` |
| `<ks>.<X>` / `CREATE TABLE <X>` | table | `COL_` |
| `"TableName": "<X>"`, `"IndexName": "<X>"` | table / secondary index | `COL_` / `IDX_` |
| `"AttributeName": "<X>"`, `#alias → "<X>"` in `ExpressionAttributeNames` | attribute | `F_` |

### 3. Public vocabulary (do not mask)

| List | Content | Source / how to extract |
|---|---|---|
| MongoDB operators | query (`$eq $gt $in $and $or $exists $regex $elemMatch`...), update (`$set $unset $inc $push $pull`...), stages (`$match $group $lookup $project $unwind $sort $limit $out $merge`...), expressions (`$sum $avg $cond`...) | pages *Query and Projection Operators*, *Update Operators*, *Aggregation Stages*, *Expression Operators*: extract every token `^\$[a-zA-Z]+$` from the index of each page. Rule: a key that starts with `$` and is in the list = vocabulary; `$` outside the list = suspect (a field with `$`, allowed since 5.0). |
| mongosh methods | `find findOne aggregate insertOne updateMany deleteOne countDocuments createIndex explain getCollection getSiblingDB`; words `use show dbs collections` | *mongosh Methods* (reference/method). A short list, fixed per version. |
| Mongo system names | `admin local config`, prefix `system.` | *Limits and Thresholds* / Reserved databases: they are not client data, they may go through. |
| ES/OS endpoints | any URL segment starting with `_` (`_search _doc _mapping _cat _bulk _count _alias _aliases _reindex _update_by_query`) | the index rule forbids a leading `_` → a `_x` segment is never an index name. List in *REST APIs*. |
| Query DSL | `query bool must should must_not filter match match_phrase multi_match term terms range gte lte exists wildcard prefix aggs size from sort _source` | *Query DSL* (elastic.co) and *Query DSL* (opensearch.org). |
| Mapping types | `text keyword long integer short byte double float half_float scaled_float date date_nanos boolean binary object nested flattened ip geo_point geo_shape dense_vector sparse_vector completion search_as_you_type token_count alias join percolator wildcard constant_keyword semantic_text`... | *Field data types*: extract the names from each subpage. The value of `"type"` is **always** in this list; mapping keys (`properties fields analyzer index dynamic format`) likewise (*Mapping parameters*). |
| `_cat/indices` header | `health status index uuid pri rep docs.count docs.deleted store.size pri.store.size dataset.size` | *cat indices API*; read the header (`?v`) and mask only the `index` column. |
| Redis commands | `GET SET DEL EXISTS TYPE EXPIRE TTL HGET HSET HGETALL LPUSH SADD ZADD SCAN KEYS MATCH COUNT`... | *Commands* (redis.io/docs/latest/commands): official list; the public `commands.json` of the redis-doc repository also brings the arity (`key_specs`), which says **which argument is a key**. |
| CQL | keywords (Appendix A) and types (`text int uuid timestamp map list set`...) | *CQL Definitions* / *Data types*. |
| DynamoDB | API keys (`TableName KeySchema AttributeName KeyType HASH RANGE`), descriptors `S N B BOOL NULL M L SS NS BS`, reserved words | *Naming rules and data types*; *Reserved words*. |

### 4. Identifier rules (validate the pseudonym against them)

- **MongoDB database**: not empty, < 64 bytes; forbidden on Unix `/\. "$` and null (on Windows also `*<>:|?`); do not distinguish by case.
- **MongoDB collection**: start with a letter or `_`; no `$`, no null, not empty; do not start with `system.` nor contain `.system.`. Namespace `db.col` ≤ 255 bytes (235 in a sharded collection). A name with a special character or a leading digit only via `db.getCollection("...")`.
- **MongoDB field**: no null; `.` and `$` are allowed on the server (5.0+), but `.` in a query becomes a path → treat `.` as a separator.
- **ES/OS index**: lowercase only; forbidden `\ / * ? " < > | , #` and space; do not start with `-`, `_`, `+`; not `.` or `..`; ≤ 255 bytes; a leading `.` only for a hidden/system index.
- **Redis key**: binary-safe, any byte, up to 512 MB, the empty string is valid. No rule → only the **convention** `type:id:field` (separator `:`; `.`/`-` in compound words) and the cluster hashtag `{...}`.
- **Cassandra**: unquoted `[a-zA-Z_0-9]{1,48}`, case-insensitive; keyspace ≤ 48, table ≤ 222; quoted it is case-sensitive.
- **DynamoDB**: table/index 3–255 chars `[a-zA-Z0-9_.-]`, case-sensitive; attribute ≥ 1 char and < 64 KB (255 for secondary index keys); `#` and `:` have a special meaning in expressions.

Consequence: a **lowercase** pseudonym, `[a-z0-9_]`, starting with a letter: `db_gbzw6njk`, `t_cszwa3ri`, `t_kzkcrymy`, `c_xsdgeq7l`. It works in all the systems above (in the text below, uppercase only for readability; in the ES index use `t_kzkcrymy`, mandatorily lowercase).

### 5. Before → after examples

MongoDB:
```js
use vendas_demo
db.pedidos_x9.find({ "cliente.cidade": "Recife", total: { $gt: 100 } })
db.pedidos_x9.aggregate([
  { $match: { status: "pago" } },
  { $lookup: { from: "itens_x9", localField: "_id", foreignField: "pedido_id", as: "itens" } },
  { $group: { _id: "$cliente.cidade", soma: { $sum: "$total" } } }
])
```
```js
use DB_gbzw6njk
db.T_cszwa3ri.find({ "C_xsdgeq7l.C_2slmva2f": "Recife", C_gdgjt7nj: { $gt: 100 } })
db.T_cszwa3ri.aggregate([
  { $match: { C_wkpovbsa: "pago" } },
  { $lookup: { from: "T_cua5v3bz", localField: "_id", foreignField: "C_roj22pqm", as: "C_yw3muenb" } },
  { $group: { _id: "$C_xsdgeq7l.C_2slmva2f", C_mihi7y6y: { $sum: "$C_gdgjt7nj" } } }
])
```
`_id` stays (reserved name). Values (`"Recife"`, `"pago"`) are another detector's problem. `as:` creates a new field → `F_`.

Elasticsearch:
```
GET /idx-logs-demo/_search
{ "query": { "bool": { "filter": [ { "term": { "nivel": "erro" } },
  { "range": { "data_evento": { "gte": "now-1d" } } } ] } } }

PUT /idx-logs-demo
{ "mappings": { "properties": { "nivel": { "type": "keyword" },
  "usuario": { "properties": { "email": { "type": "keyword" } } } } } }
```
```
GET /t_kzkcrymy/_search
{ "query": { "bool": { "filter": [ { "term": { "C_n33epld6": "erro" } },
  { "range": { "C_wommwydi": { "gte": "now-1d" } } } ] } } }

PUT /t_kzkcrymy
{ "mappings": { "properties": { "C_n33epld6": { "type": "keyword" },
  "C_rkykwjjg": { "properties": { "C_2xgvahga": { "type": "keyword" } } } } } }
```
`_cat/indices?v`: keep the header, replace only the `index` column (`idx-logs-demo` → `t_kzkcrymy`); `uuid` is opaque (mask it if the policy treats it as an identifier).

Redis:
```
127.0.0.1:6379> HSET pedido:123:status etapa enviado
127.0.0.1:6379> SCAN 0 MATCH pedido:*:status COUNT 100
1) "0"
2) 1) "pedido:123:status"
```
```
127.0.0.1:6379> HSET T_637plzyd:123:T_sj65iems C_vkzmx2ot enviado
127.0.0.1:6379> SCAN 0 MATCH T_637plzyd:*:T_sj65iems COUNT 100
1) "0"
2) 1) "T_637plzyd:123:T_sj65iems"
```
Numeric segments/IDs go through or go to the identifier detector; the map must be the **same** inside the pattern and in the output.

Cassandra / DynamoDB:
```
SELECT * FROM vendas_demo.pedidos_x9 WHERE pedido_id = 1;   → SELECT * FROM DB_gbzw6njk.T_cszwa3ri WHERE C_roj22pqm = 1;
{"TableName":"pedidos_x9","KeySchema":[{"AttributeName":"pedido_id","KeyType":"HASH"}]}
→ {"TableName":"T_cszwa3ri","KeySchema":[{"AttributeName":"C_roj22pqm","KeyType":"HASH"}]}
```

### 6. Hard cases and limits

- **Redis key with personal data inside** (`sessao:maria@exemplo.com`, `cpf:00000000000:score`): segment on `:`; each segment **also** goes through the value detectors (e-mail, CPF, phone). A prefix segment → `KEY_`; a segment that is data → pseudonym of the data's type. A key without `:` (e.g. serialized JSON, SHA hash) → treat it as opaque and mask it whole. Hashtag `{x}`: preserve the braces `{}` (they change the cluster slot).
- **Dynamic field** (`"properties"` with generated names, e.g. dates `2026-10-01` as a key; Mongo `{ "metricas": { "sku_991": 3 } }`): there is no way to tell by the structure whether the key is schema or data. Rule: if the sibling keys follow a numeric/date/ID pattern, mask as a value; otherwise `F_`. `dynamic_templates` (`"match": "attr_*"`) brings patterns, not names.
- **Patterns with wildcards**: `GET /idx-logs-*/_search`, `SCAN MATCH pedido:*`, `"match": "attr_*"`, `index_patterns`. Mask only the literal part and keep `* ? [ ]`; the same prefix must become the same pseudonym as the concrete names, otherwise the pattern stops matching (`idx-logs-*` → `t_kzkcrymy*` only works if the replacement is by **prefix**, not by whole name — a real limit: pseudonymizing prefix and full name consistently requires tokenizing the name on `-`/`_`).
- **Lists and dates in an ES index**: `GET /a,b/_search`, `logs-2026.10.05` (date math `<logs-{now/d}>`): split on `,`; a date suffix is structure, keep it.
- **Ambiguous dot notation**: `"a.b"` can be a literal field with a dot (5.0+) or a path. Masking segment by segment covers both.
- **`$` as a reference vs. an operator**: `"$total"` (value) is a field; `$sum` (key) is an operator. Decide by the list of item 3, not by the `$`.
- **Names that coincide with the vocabulary** (a field called `status`, `type`, `index`, `match`): the position decides. A key under `properties` is a field even if it is called `type`; the value of `"type"` is a type.
- **Ambiguous `GET`/`SET`** (HTTP × Redis): without a Redis prompt or reply, do not assume.
- **Limit**: free text ("the orders collection is slow") has no structure → out of reach of this detector.
- Way back (unmasking the LLM's response): the LLM may invent new derived names (`col_a8f1_backup`); only revert exact tokens of the map.

### 7. Links used

- https://www.mongodb.com/docs/manual/reference/limits/ (names of database, collection, field, namespace)
- https://www.mongodb.com/docs/manual/reference/operator/query/ · https://www.mongodb.com/docs/manual/reference/operator/aggregation-pipeline/ · https://www.mongodb.com/docs/manual/reference/operator/update/
- https://www.mongodb.com/docs/manual/core/dot-dollar-considerations/
- https://www.elastic.co/guide/en/elasticsearch/reference/current/indices-create-index.html (index naming rules)
- https://www.elastic.co/guide/en/elasticsearch/reference/current/mapping-types.html (field types)
- https://www.elastic.co/guide/en/elasticsearch/reference/current/cat-indices.html
- https://www.elastic.co/guide/en/elasticsearch/reference/current/query-dsl.html · https://opensearch.org/docs/latest/query-dsl/
- https://redis.io/docs/latest/develop/using-commands/keyspace/ (keys, convention, SCAN/KEYS glob)
- https://redis.io/docs/latest/commands/
- https://cassandra.apache.org/doc/latest/cassandra/developing/cql/definitions.html · https://cassandra.apache.org/doc/latest/cassandra/developing/cql/ddl.html
- https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/HowItWorks.NamingRulesDataTypes.html
- https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/ReservedWords.html

[VERIFY] the exact header of `_cat/indices?v` (the page read does not show example output; the columns come from the attribute list). [VERIFY] the existence of `commands.json` with `key_specs` in the current version of the Redis docs.


## Transport formats (diff, grep, line numbering, logs, markdown, heredoc)

**How it is implemented** (`normalizacao.go`): a layer before all the readers strips the
transport and maps each position of the clean text back to the original. It strips: the line number
of the reading tool (`   12→`) and of `cat -n`/`nl`; the `grep -n` prefix (`file:12:`,
`12:`, context `file-12-`, separator `--`), only when most lines have it; diff/patch
(headers and the 1st character `+`/`-`/space), when there is a diff header; `git blame`;
markdown quotation (`> `, nested); date stamp and level at the start of a log line;
code fence; ANSI sequences; BOM; CRLF; trailing spaces. Box borders
(U+2500–U+257F) become `|` and `-`, which the table reader already understands. A JSON string with an
escaped `\n` is decoded (up to 3 levels), each one in its own block. The structure and table
readers run on the clean text; only the findings that correspond byte for byte to the original
come back (the replacement and the way back stay exact). The original is also read when the
clean-up removed something that carries a name (the grep file, the diff header) or changed the structure
(decoded JSON). In propagation, a name right after an escaped `\n`/`\t` is also
searched for without the letter of the escape.

Principle: the proxy does not mask the wrapper, it masks the **content**. Each wrapper becomes
a list of *segments* `(original_start, inner_start, length)` and a *type label*
(sql, yaml, json, unknown). The reader of the type finds the names in the inner text; the map
returns the positions in the original text. Wrappers can nest (JSON → heredoc → SQL;
markdown → diff → SQL): peel from the outside in, composing the maps.

### 1. Detection signals

Test per line, from the strongest to the weakest. Require **two signals** before assuming the wrapper.

| Wrapper | Strong signal (anchor) | Confirmation signal |
|---|---|---|
| git diff | a line `^diff --git ` | `index <hash>..<hash>`, `---`/`+++`, `@@ -` |
| diff -u (GNU) | the pair `^--- ` followed by `^\+\+\+ ` | `^@@ -\d+(,\d+)? \+\d+(,\d+)? @@` |
| loose hunk | `^@@ -\d+(,\d+)? \+\d+(,\d+)? @@` | the following lines only with the 1st character in `{' ','+','-','\'}` |
| grep/rg without heading | `^<path>:<n>:` or `^<path>-<n>-` on several lines | the same `<path>` repeated; a `^--$` line between groups |
| rg --heading | a line with only a path, followed by `^\d+:` / `^\d+-` | an empty line between files |
| Read line numbering (Claude Code) | `^\s*\d+\t` on consecutive lines (format checked in the sessions; `→` accepted too) | numbers increasing by 1 |
| cat -n | `^\s*\d+\t` (right-aligned number + TAB) | numbers increasing by 1 |
| PostgreSQL log | `LOG:  statement: `, `ERROR:  `, `STATEMENT:  `, `DETAIL:  ` after a prefix | repeated prefix (`log_line_prefix`, default `%m [%p] `) |
| SQL Server ERRORLOG | `^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\.\d+ \S+\s+` | origin `spid\d+`, `Server`, `Logon` [phase 3: logs are 0.0% of the volume] |
| JSON Lines | every non-empty line starts with `{` or `[` and is valid JSON by itself | the same keys on consecutive lines |
| Airflow/Spark log | `^\[?\d{4}-\d{2}-\d{2}[ T]\d{2}:\d{2}` + level (`INFO`, `WARN`, `ERROR`) | `{file.py:line}` in Airflow [phase 3: logs are 0.0% of the volume] |
| markdown | a line `^ {0,3}(`{3,}\|~{3,})` | a closing line with the same character and length ≥ |
| heredoc | `<<-?\s*(['"]?)(\w+)\1` on a command line | a line containing **only** the delimiter word |
| JSON string | `"command":"..."`, presence of `\n`, `\"`, `\\`, `\u` | the text between quotes does not break lines (breaks become `\n`) |

### 2. Wrapper → how to remove it → how to find the inner type

| Wrapper | How to remove it (what becomes inner text) | Inner type |
|---|---|---|
| git diff / -u | Discard the headers. In each hunk, strip **1 character** of prefix (` `, `+`, `-`). Build **two views**: old (` `+`-`) and new (` `+`+`). Ignore `\ No newline at end of file`. | extension of the path in `diff --git a/X b/X` or `+++ b/X` (`.sql`, `.yaml`, `.yml`, `.json`, `.py`); if `/dev/null`, use the other side |
| text after the 2nd `@@` | It is the "function" line chosen by git (`xfuncname`): content of the file, not wrapper. Mask with the same type as the file. | same as the file |
| grep/rg (without heading) | Remove `path` + sep + `n` + sep (+ `col`/`byte` + sep with `-b`/`--column`). Sep = `:` on a match, `-` on context. A `--` line = block break (it is not content). | extension of the `path` of the prefix |
| rg --heading | Path line = header (outside the content); on the others, remove `^\d+[:-]` | extension of the path of the header |
| line numbering (`→`, TAB, cat -n) | Remove `^\s*\d+→` or `^\s*\d+\t` (only the **first** occurrence per line) | no path hint: sniff the content (section 6) |
| PostgreSQL text log | Remove the line prefix and the fixed word up to `:  `; the rest (and continuation lines, which start with a TAB) is the statement | `statement:`, `STATEMENT:`, `execute <name>:` → sql; `DETAIL:`/`HINT:` → free text |
| PostgreSQL jsonlog / JSON Lines | Parse each line as JSON; extract the **value** of each string (undoing the escapes, section 4) | the key decides: `statement`, `internal_query`, `query` → sql; `message`/`detail` → text (it may contain a name between quotes) |
| SQL Server ERRORLOG | Remove `date time origin` from the start of the line | free text with names between `'...'` or `[...]` [phase 3: logs are 0.0% of the volume] |
| Airflow/Spark | Remove `[timestamp]`, `{file:line}`, level; the rest is the message | free text; SQL if it starts with an SQL keyword (section 6) |
| markdown | Content = the lines between the fences. If the opening fence has N spaces of indentation, remove **up to N spaces** from each line. A block without a closing goes to the end of the document. | 1st word of the info string (` ```sql `, ` ```yaml `); empty → sniff |
| heredoc | Content = the lines after the command line up to the line with only the delimiter. `<<-`: remove the leading TABs of each line. | the receiving command (`psql`, `sqlcmd`, `snowsql`, `bq query` → sql; `cat > x.yaml` → extension of the destination); otherwise sniff |
| JSON string | Undo the RFC 8259 escapes character by character, keeping the map | whatever is inside (normally a shell command → heredoc/SQL) |

### 3. Public vocabulary (fixed words — never mask)

| Origin | Fixed words | Source |
|---|---|---|
| git diff | `diff --git`, `old mode`, `new mode`, `deleted file mode`, `new file mode`, `copy from`, `copy to`, `rename from`, `rename to`, `similarity index`, `dissimilarity index`, `index`, `diff --combined`, `diff --cc`, `@@@`; prefixes `a/` `b/` (or `c/ i/ w/ o/` with `diff.mnemonicPrefix`) | git-scm diff-format, git-diff |
| GNU diff | `---`, `+++`, `@@`, `\ No newline at end of file` [not confirmed → tolerant rule] | gnu diffutils |
| grep | separators `:` (match), `-` (context), `--` (group); fixed order path → line → byte | gnu grep §2.1.4–2.1.5 |
| ripgrep | `--` (default of `--context-separator`), `:` / `-` (field separators) | ripgrep defs.rs |
| PostgreSQL levels | `DEBUG1`–`DEBUG5`, `INFO`, `NOTICE`, `WARNING`, `ERROR`, `LOG`, `FATAL`, `PANIC` | runtime-config-logging, table 19.2 |
| PostgreSQL fields | `DETAIL`, `HINT`, `QUERY`, `CONTEXT`, `STATEMENT`; jsonlog: `error_severity`, `message`, `statement`, `internal_query`, `detail`, `hint`, `context`, `dbname`, `user`, `application_name`… | same (`log_error_verbosity`, jsonlog) |
| PostgreSQL messages | `statement:`, `duration:`, `execute <name>:` — literal form [not confirmed → tolerant rule] | — |
| SQL Server | files `ERRORLOG`, `ERRORLOG.<n>`; `sp_cycle_errorlog` | learn.microsoft.com |
| Generic logs | `TRACE`, `DEBUG`, `INFO`, `WARN`, `WARNING`, `ERROR`, `CRITICAL`, `FATAL` [phase 3: logs are 0.0% of the volume] | — |
| CommonMark | fences ` ``` `, `~~~` (≥ 3); info string | spec.commonmark.org 0.31.2 |
| POSIX shell | `<<`, `<<-`, a quoted delimiter turns expansion off | POSIX 2.7.4 |

**Attention:** fields of `log_line_prefix` such as `%u` (user), `%d` (database), `%h`/`%r` (host),
`%a` (application) are **data**, not vocabulary — mask them (types `DB_`, `HOST_`, `USR_`).

### 4. Position mapping rules

1. **Segments, not a single offset.** For each kept line store
   `(original_start, inner_start, length)`. Removing a fixed-width prefix
   (diff, grep, line numbering, markdown indentation, the TAB of `<<-`) produces one segment per line.
   Position lookup = binary search on `inner_start`; `orig = original_start + (pos - inner_start)`.
2. **JSON escaping produces a per-character map.** `\n`, `\"`, `\\`, `\/`, `\t` take 2 bytes in the
   original and 1 in the inner text; `\uXXXX` takes 6 (and a surrogate pair 12) and becomes 1 character.
   Keep a vector `inner[i] → original[j]` (or segments with a break at each escape).
3. **A name never crosses a segment.** If the finding starts in one segment and ends in another
   (a name broken between lines, or with an escape in the middle), replace the **whole original
   range** `[orig(start), orig(end))` — that erases the prefix in the middle, so it is only allowed
   when the segments are contiguous in the original (the escape case). Between lines: do not mask
   as a name, record a warning.
4. **Replace from the end to the beginning** in the original text: the pseudonym has a different length
   from the name and must not invalidate the positions not yet applied.
5. **Re-escape the pseudonym** in the destination wrapper. Pseudonyms are `[A-Z]+_[a-z0-9]{4}`
   — nothing to escape in JSON, shell or markdown; keep it that way by design.
6. **A diff has two views, one table.** A context line appears in both views with the
   same original position: deduplicate findings by `original_start`. The same name → the same
   pseudonym on the `-` and `+` lines, otherwise the masked diff shows a change that does not exist.
7. **Headers do not move.** `@@ -l,n +l,n @@`, the line number of grep and of `→` stay
   intact: only the content changes in length, not in number of lines.
8. **The path in the header is content too** (section 6): it goes through the path reader, and the
   same path in `diff --git`, `---`, `+++` and in the grep prefix gets the same pseudonym.

### 5. Before/after examples

Diff (the type comes from `.sql`; the wrapper stays; `-` and `+` use the same pseudonym):

```
diff --git a/sql/relatorio_pedidos.sql b/sql/relatorio_pedidos.sql
@@ -3,2 +3,2 @@ CREATE VIEW financeiro.vw_pedidos AS
-SELECT id FROM financeiro.tb_pedido_x9
+SELECT id, valor FROM financeiro.tb_pedido_x9
```
```
diff --git a/sql/relatorio_pedidos.sql b/sql/relatorio_pedidos.sql
@@ -3,2 +3,2 @@ CREATE VIEW SCH_mvrhafae.T_nebrjj4t AS
-SELECT id FROM SCH_mvrhafae.T_cszwa3ri
+SELECT id, valor FROM SCH_mvrhafae.T_cszwa3ri
```

grep (`:` match, `-` context, `--` group):

```
sql/relatorio_pedidos.sql-11-  -- totais
sql/relatorio_pedidos.sql:12:  FROM financeiro.tb_pedido_x9 p
--
```
→ `sql/relatorio_pedidos.sql:12:  FROM SCH_mvrhafae.T_cszwa3ri p` (line 11 and `--` intact)

PostgreSQL log (prefix `%m [%p] %u@%d `; user and database are data too):

```
2026-01-10 08:00:01.120 UTC [4410] app_rel@dw_vendas LOG:  statement: DELETE FROM financeiro.tb_pedido_x9 WHERE id = 7
```
```
2026-01-10 08:00:01.120 UTC [4410] USR_n4a4mukt@DB_k3gr76nw LOG:  statement: DELETE FROM SCH_mvrhafae.T_cszwa3ri WHERE id = 7
```

JSON → heredoc → SQL (composed map; `\n` preserved):

```
{"command":"psql <<'EOF'\nSELECT * FROM \"financeiro\".tb_pedido_x9;\nEOF"}
```
```
{"command":"psql <<'EOF'\nSELECT * FROM \"SCH_mvrhafae\".T_cszwa3ri;\nEOF"}
```
Note: `\"financeiro\"` takes 14 bytes in the original; in the inner text `"financeiro"` takes 12. The
finding is `financeiro` (without quotes); the per-character map returns exactly the 10 bytes
between the `\"`, and the escaped quotes stay.

### 6. Hard cases and limits

- **A hunk that starts in the middle of an SQL statement.** The SQL reader cannot require a complete
  statement: it must find names by local patterns (`FROM|JOIN|INTO|UPDATE|TABLE <id>(.<id>)*`,
  `<id>.<id>.<id>`). A line `+  JOIN financeiro.tb_pedido_x9 x` without the `SELECT` above still
  matches. A loose column (`valor,`) with no anchor is left unmasked — acceptable. The text after the 2nd
  `@@` often brings the beginning of the statement (`CREATE VIEW ...`) and helps give context.
- **The hunk count does not add up** (a diff truncated by the agent or pasted halfway): do not
  validate the `n` of the header against the lines; keep accepting lines with the prefix
  `{' ','+','-','\'}` until a strange prefix; the rest goes back to being generic text.
- **Truncated diff line / rg with `--max-columns`.** The end of the line is missing (or a
  warning of an omitted line comes instead of the content [not confirmed → tolerant rule]). A name cut at the
  end (`financeiro.tb_ped`) is not recognizable as the original: mask the piece with its
  **own** pseudonym if it has the shape of a qualified identifier; do not try to complete it.
- **grep -o / piece of a line.** Only the matched part appears: no `FROM` on the left. Use the
  qualified name pattern (`a.b`, `a.b.c`, `[a].[b]`, `"a"."b"`) and the type of the file.
- **A path with a server name**: `config/sqlserver_srv-exemplo-01.yaml`. The path is
  data. Break it into components (`/`, `.`, `_`), keep the extension and the parts that are vocabulary of the
  product (`config`, `sqlserver`, `yaml`) and mask the span with the shape of a host
  (`srv-exemplo-01` → `HOST_vltoxf7e`): `config/sqlserver_HOST_r4t6.yaml`. The **extension keeps
  deciding the type** — so detect the type before masking the path. Apply the same
  pseudonym in `a/`, `b/`, `---`, `+++`, the grep prefix and the rg header.
- **Quoted paths in git** (`core.quotePath`): `"config/\303\241rea.yaml"`; undo the
  C/octal escape to read the name and re-escape on the way back. `--no-prefix` removes `a/` `b/`;
  `diff.mnemonicPrefix` replaces them with `c/ i/ w/ o/` — accept any `^[a-z]/` [not confirmed → tolerant rule].
- **False positive of a wrapper.** An SQL line `-- comment` looks like a grep separator or a
  removed diff line; `@@` appears in SQL Server variables (`@@ROWCOUNT`). That is why
  two signals are required (section 1) and the pattern must repeat on neighbouring lines.
- **Line numbering inside a diff** (the agent read a diff with numbers): strip the numbering first
  (outer layer), then the diff. Order = from the outside in, always.
- **Nested markdown**: a 4-backtick fence with a 3-backtick fence inside — the closing requires
  the same character and a length ≥ that of the opening; use the count, not "the first ``` that comes".
- **Unquoted heredoc** (`<<EOF`): `$VAR` and `$(...)` are expanded by the shell; names inside
  `${...}` are shell variables, not database objects. With `<<'EOF'` the text is literal.
  Several heredocs on the same line are consumed in the order they appear.
- **No type hint** (line numbering, a fence with no info string, a heredoc for an unknown command):
  sniff — 1st significant word in `SELECT|WITH|INSERT|UPDATE|DELETE|MERGE|CREATE|ALTER`
  → sql; repeated `^\s*[\w-]+:\s` / `^---$` → yaml; starts with `{`/`[` and parses → json;
  otherwise the generic reader (qualified names and hosts).
- **Multi-line log**: in PostgreSQL the statement continues on the following lines (with a TAB);
  in JSON Lines there is never a real break inside a record (it comes as an escaped `\n`).

### 7. Links used

- https://git-scm.com/docs/diff-format
- https://git-scm.com/docs/git-diff
- https://git-scm.com/docs/git-config (core.quotePath — the page came cut off; text checked from memory [not confirmed → tolerant rule])
- https://www.gnu.org/software/diffutils/manual/html_node/Detailed-Unified.html
- https://www.gnu.org/software/grep/manual/grep.html
- https://github.com/BurntSushi/ripgrep/blob/master/GUIDE.md
- https://raw.githubusercontent.com/BurntSushi/ripgrep/master/crates/core/flags/defs.rs
- https://www.postgresql.org/docs/current/runtime-config-logging.html
- https://learn.microsoft.com/en-us/sql/tools/configuration-manager/viewing-the-sql-server-error-log
- https://jsonlines.org/
- https://spec.commonmark.org/0.31.2/
- https://pubs.opengroup.org/onlinepubs/9799919799/utilities/V3_chap02.html (2.7.4 Here-Document)
- https://www.rfc-editor.org/rfc/rfc8259 (section 7, Strings)

With no official source read (marked [VERIFY]): the `N<tab>` format of Read (checked in the sessions: 605 of 640 results; none with `N→`), the width of `cat -n`
(coreutils returned 429), the line format of ERRORLOG, Airflow/Spark logs, the literal form
`LOG:  statement:` of PostgreSQL, the omitted-line text of rg.


## Loose script and shell output

Script and shell output has no fixed format. Four generic mechanisms apply, none per
tool (the tool names below are examples of where the shape appears):

**1. The command says what the output is** (`chamada_comando.go`; the proxy links the `tool_use` to the `tool_result`
of the same id in `requisicao.go`, `dicasDosComandos`). The tool input (the shell
`command`, or the strings of the input, without the description) becomes a *hint* with the type of the columns of the
output, which goes along with the text in the key of the result memory. The hint is only evidence for the
table reader (`classificarTabelaD`, in hint mode: only the columns the header does not type) and
for headerless output (`linhasDica`: every line with the same number of cells, by `|`,
TAB, `,`, `;` or space; one line outside the shape undoes everything; the footer `(N rows)` and the line that
repeats the header are left out; `uniq -c` has the count stripped). Shapes:
- SQL in the command: the last `SELECT ... FROM` gives the type of each column in order and by name
  (alias or column, by the catalog header/type word); `name` counts by the catalog of the
  FROM in the plural (`sys.tables` → table); `SELECT *` gives no position. `SHOW <types>` gives the type of the
  `name` column (`SHOW SCHEMAS` → schema); `DESCRIBE` gives column. Strong evidence.
- column extraction by position (`cut -d, -fN`, `awk -F, '{print $N}'`) over a file whose
  header already went through the conversation (the output of a command that cites the file starts with
  `a,b,c` of identifiers, including through the reading tool): column N has the type of
  header N. Strong.
- resource listing: `<cli> get|list|ls <type>`, `<cli> <type> list`, `<cli> list-<type>`
  (the verb is never the program: the shell's `ls` and `ps` do not count) give the type of the
  `NAME`/`NAMES` column; `<cli> ps` and `<cli> list` without a type, service (containers, releases). Types:
  the type words (`tables`, `topics`, `buckets`, `instances`...) and the orchestrator and container
  resources with the CLIs' abbreviations (`pods`, `deploy`, `svc`, `ns`,
  `nodes`...). Weak evidence (masks in place; teaches only with another rule) and only a value that
  looks like an identifier outside the devops vocabulary (`kube-system`, `bridge` stay).

**2. Qualified name after a type word, in any sentence** (the error reader,
`acharTipoQualificado`, extended from "type word + name between quotes" to an unquoted
name): `tabela fin.t_x: 1200 linhas`, `Loading table a.b.c`, `created sql table model
fin.t_x` (one lowercase word fits in the middle, and then the evidence is weak). Type words
in English and Portuguese (`tabela`, `esquema`, `banco`, `objeto`...). Unquoted, only a qualified
name (2 or 3 parts, the last with 3+ letters): a file (`vendas.csv`), a public domain,
a call, a path, a version, `i.e.` and a code receiver (`os.path`) do not count. A database address
without a scheme `host:port/database` (EZConnect and connection messages) in the connection reader, only on
a line that talks about a connection (`conect`, `connect`, `conex`), with `como|as <user>` right after;
without a user, a public host with a database that does not look like a name (`localhost:8080/api`) does not count.

**3. Homogeneous list** (`objetos_listas.go`, runs after the readers, because it depends on what is already
known): in a loose column (one item per line, with a `- `/`* ` marker), in count + value
(`sort | uniq -c`, `value_counts`), in a decorated loop line (`== t_x ==`, `--- t_x`, `## t_x`,
grouped by the decoration) or in a comma-separated list (with or without quotes and brackets:
`json.dumps`, `print`), with 3+ items and at least half already names of the same type (learned
before or found by the readers in this text), the other items that look like identifiers are of the
same type (mask and learn). A list between the parentheses of a call (`f(a, b, c)`) does not
count; a file and a data type do not become an item. Likewise for a qualified name with a known
part in the schema or database position: `fin.<x>` with `fin` learned as a schema makes `<x>`
a table.

**4. The rest is propagation** (vistos): a learned name is masked inside any shape
(f-string, log, `B2=t_x`, text extracted from .docx/.pdf, a `kubectl logs` line).

Limits: text from .docx/.pdf (and prose in general) is only covered by propagation: a name that
appears for the first time there, without structure, goes through. The hint only holds for the result of
the call itself; output of a script that does not say what it prints (no SQL, no header, no list
with names already known) goes through too. In the homogeneous list, a name that only appears in lists
of unknown names is not seen. When the first line of a headerless output is read
by the table reader as a header, the cell comes out masked as a column (the type of the hint does not
replace that of the header).


# 12 — Generic fallback (unknown structured format)

## Generic fallback

Role: catch what the specific readers (SQL, YAML/JSON, CSV, connection string) let
through. It does not understand the format; it only recognizes a **key position** and the **shape of an identifier**.
The minimal safe rule, in one sentence:

> The value that occupies the value position of a key whose **core** is in the vocabulary of
> "name of a thing" is masked whole. The name learned this way becomes an entry in a session
> block list and is propagated to prose **only if it looks like an identifier**
> (or if it appears in a structural context: backticks, quotes, a qualified name `a.b.c`).

### 1. Detection signals

A line (or cell) enters the fallback when it matches one of these shapes:

| Shape | Pattern (summary) | Fictitious example |
|---|---|---|
| key: value | `^\s*[-*]?\s*KEY\s*:\s*VALUE` | `host: db-exemplo-01` |
| key=value (list with `;` `,` `&` or space) | `KEY\s*=\s*VALUE` | `Initial Catalog=vendas_x9;User ID=svc_relatorio` |
| aligned key value | `^\s*KEY {2,}VALUE$` (2+ spaces or a tab) | `SERVIDOR     db-exemplo-01` |
| CLI flag | `--KEY[= ]VALUE`, `-n VALUE` only if the equivalent long flag is known | `--namespace ns-exemplo-02` |
| aligned table | header line + (a `---`/`===`/`|` line or 2+ lines with columns in the same position) | see §5 |
| list | `- KEY: VALUE` or `KEY:` followed by `- VALUE` items | `buckets:` / `- bkt-exemplo-01` |

**Core of the key** (normalization, nothing about language):
1. lowercase; remove accents; split on `_ - . space` and on a camelCase boundary
   (`proxyHost` → `proxy host`; `Initial Catalog` → `initial catalog`).
2. core = last piece; if the last one is `name`, `nome`, `id`, `ids`, use the previous one
   (`table_name` → `table`; `group.id` → `group`; `User ID` → `user`).
3. simple plural: drop the final `s` (`bootstrap.servers` → `server`, `buckets` → `bucket`).
4. **modifier lock**: if the last piece is a configuration one (`timeout`, `port`, `count`,
   `size`, `retries`, `mode`, `enabled`, `interval`, `version`, `file`, `path`, `encoding`),
   it is not the name of a thing (`tcp_user_timeout`, `proxyPort`, `keepalives_count`).

**Value**: mask it whole, splitting on `,` when the key is plural (list of hosts) and
preserving `:port`. Do **not** mask: empty, `true/false/yes/no/null/none`, a pure number,
a placeholder (`${VAR}`, `{{ env_var('X') }}`, `<...>`), and documented public defaults
(`localhost`, `(local)`, `default`, `public`, `dbo`, `PUBLIC`) — they reveal nothing and help the
model reason.

**Table**: if the header of a column has its core in the vocabulary, the whole column is masked
(it is what Google calls a hotword in the header with `windowBefore = 1`, and Macie a keyword
"in the name of the field or column").

### 2. Position → entity type

| Core of the key (EN / PT) | Entity | Pseudonym |
|---|---|---|
| host, hostaddr, server, servidor, address, addr, data source, network address, failover partner, proxyhost, nonproxyhosts, workstation/wsid | HOST | `HOST_tu6pvlhb` |
| database, db, dbname, banco, catalog, initial catalog, project (BigQuery: `database` = project) | DATABASE | `DB_xoal5ygh` |
| schema, esquema, dataset | SCHEMA | `SCH_7h7ved4b` |
| table, tabela, relation, view | TABLE | `T_3gjogzbd` |
| column, coluna, field, campo | COLUMN | `C_etwhmr4n` |
| user, usuario, username, uid, login, owner, dono, proxyuser | USER | `USR_j4ed3tyq` |
| role, papel | ROLE | `USR_xgbsyz5d` |
| namespace | NAMESPACE | `NS_xcykm4tb` |
| cluster, context, current-context | CLUSTER | `HOST_yw4kukvb` |
| bucket | BUCKET | `BKT_xztzuyuw` |
| topic, topico, queue, fila, group (`group.id`) | QUEUE_TOPIC | `TOP_7bwlqw3s` |
| service, servico, application, app, instance, instancia | SERVICE | `SVC_rmaijbls` |
| warehouse | WAREHOUSE | `SVC_mdgkpbby` |
| account, conta, tenant | ACCOUNT | `HOST_bth7wilh` |

Credentials (`password`, `token`, `private_key`…) are **not** of this family: they go to the
secrets family, which always masks, without looking at the shape.

### 3. Public vocabulary and source of each group

The list comes from the keys that repeat in the official connection and configuration specifications —
not from a client, not from a dictionary. Each key must appear in at least one spec below
(the PT ones are a closed translation of the same concept, not general vocabulary).

| Group | Keys | Where they appear (official) |
|---|---|---|
| Host | `host`, `hostaddr` | libpq 32.1.2 Parameter Key Words |
| Host (synonyms) | `Data Source`, `Server`, `Address`, `Addr`, `Network Address`, `Failover Partner`, `Workstation ID`/`WSID` | SqlConnection.ConnectionString (Microsoft Learn) |
| Host (proxy) | `proxyHost`, `nonProxyHosts` | Snowflake JDBC parameters |
| Host (broker) | `bootstrap.servers` | Apache Kafka consumer configs |
| Host (k8s) | `server` (of the cluster) | Kubernetes, kubeconfig |
| Database | `dbname` | libpq |
| Database | `Initial Catalog`, `Database` | SqlConnection |
| Database / context | `db`, `schema`, `warehouse`, `role` | Snowflake JDBC ("Default context") |
| Database / schema | `database`, `schema`, `account`, `warehouse`, `user` | dbt Snowflake setup (profiles.yml) |
| Project / dataset | `database` = GCP project, `schema` = dataset | dbt BigQuery setup |
| User | `user` | libpq, Snowflake JDBC (the only mandatory one), dbt |
| User (synonyms) | `User ID`, `UID`, `User` | SqlConnection |
| User (proxy) | `proxyUser` | Snowflake JDBC |
| Service / app | `service`, `application_name` | libpq |
| Service / app | `Application Name`, `App` | SqlConnection |
| k8s context | `cluster`, `user`, `namespace` (the 3 parameters of a context), `current-context` | Kubernetes, kubeconfig |
| Queue / topic | `group.id`, topic (`subscribe(topic)`) | Apache Kafka consumer configs |
| Bucket | `bucket` | Amazon S3 (bucket naming rules) |

Group B (only with a value that looks like an identifier, because they are too generic):
`base`, `target`, `source`, `origem`, `destino`, `stage`, `stream`, `task`, `pipe`, `name`/`nome`
alone. `[VERIFY]` with samples before promoting any of them to group A.

### 4. The "looks like an identifier" rule and exclusions

It applies to **propagating** a name already learned by a key to prose. The cost is asymmetric:
propagating too much spoils text; propagating too little leaks. So the shape decides only free
prose; in a structural context (backticks, quotes, `a.b.c`, right after "table/schema/host") the learned
name is always replaced.

It looks like an identifier if, with 3 to 255 characters, only ASCII `[A-Za-z0-9_$.-]`, it has **one** of:
- `_` between parts (`tb_pedido_x9`, `svc_relatorio`, `TB_PEDIDO_X9`);
- a letter **and** a digit in the same token (`db01`, `ns-exemplo-02`);
- an inner `$` (`SYS$X`, allowed in an unquoted Snowflake identifier);
- `.` between parts that start with a letter/`_` (`vendas_x9.tb_pedido_x9`);
- mixed case with 2+ "humps" (`PedidoItemX`, `pedidoItem`).

Structural basis: unquoted Snowflake identifier = letter/`_` + letters, digits, `_`, `$`;
k8s name = lowercase, digits, `-`, `.`; S3 bucket = lowercase, digits, `.`, `-`. None of them
accepts an accent or a space — that is why **an accent knocks the shape down** (a prose word).

A hyphen alone is **not** a signal (it only counts with a digit or together with another signal).

| Edge case | Example | Decision | Why |
|---|---|---|---|
| Version | `1.2.3`, `v2.10.0-rc1` | exclude | `^v?\d+(\.\d+)+([-+][\w.]+)?$` (SemVer) |
| Date / time | `2026-10-05`, `05/10/2026`, `20261005T120000Z` | exclude | ISO 8601 and dd/mm/yyyy regex |
| UUID | `3f2c…-…-…` (8-4-4-4-12 hex) | exclude from this family | it is an ID, not a name; IDs/secrets family |
| Hash | `9fceb02` (7–64 pure hex) | exclude | hex without a separator; IDs family |
| File | `relatorio_final.xlsx`, `main.go` | exclude by shape | known extension in the last piece; but the learned name inside it is replaced: `tb_pedido_x9.sql` → `T_3gjogzbd.sql` |
| Portuguese hyphen | `guarda-chuva`, `segunda-feira`, `pé-de-moleque`, `e-mail` | not an identifier | a hyphen with letters only does not count; an accent knocks it down |
| Acronym | `SQL`, `API`, `HTTP`, `JSON` | no | uppercase only, no `_` or digit |
| Acronym with a digit | `S3`, `EC2`, `UTF8`, `IPv4`, `OAuth2`, `K8s` | no by shape (≤ 6, no separator) | but if it was learned by a key, it is replaced in a structural context |
| Public camelCase | `getConnection`, `DataFrame`, `PostgreSQL`, `GitHub` | do not propagate if equal to a reserved word/public function | allow list = reserved words and functions of the grammar itself (official docs of SQL/of the library), not a client list |
| Qualified name vs. domain | `vendas_x9.tb_pedido_x9` vs `example.com` | qualified = yes; domain = host/URL family | known TLD at the end → host |
| Decimal number | `3.14` | no | parts start with a digit |

### 5. Before/after examples (fictitious data)

```text
BEFORE                                          AFTER
host: db-exemplo-01.interno                     host: HOST_tu6pvlhb
port: 5432                                      port: 5432                 (modifier)
Initial Catalog=vendas_x9;User ID=svc_relatorio Initial Catalog=DB_xoal5ygh;User ID=USR_j4ed3tyq
SERVIDOR     db-exemplo-01                      SERVIDOR     HOST_tu6pvlhb
--namespace ns-exemplo-02                       --namespace NS_xcykm4tb
bootstrap.servers=kfk-a1:9092,kfk-a2:9092       bootstrap.servers=HOST_3qmjbssv:9092,HOST_6vqnblkq:9092
schema: public                                  schema: public             (public default)
tcp_user_timeout=30                             tcp_user_timeout=30        (modifier lock)
```

Aligned table (header with core `table` and `column` → columns masked):

```text
table_name     column_name    data_type        TABLE_NAME   COLUMN_NAME  DATA_TYPE
tb_pedido_x9   cd_cliente_7   NUMBER     →     T_3gjogzbd    C_etwhmr4n    NUMBER
```

Propagation to prose:

```text
"a carga da tb_pedido_x9 falhou no db-exemplo-01"  → "a carga da T_3gjogzbd falhou no HOST_tu6pvlhb"
schema: cliente   (learned, but it does not look like an identifier)
"o cliente reclamou do schema `cliente`"           → "o cliente reclamou do schema `SCH_7h7ved4b`"
```

### 6. Market approaches and what is useful to us

| Approach | How it works (official) | What we adopted |
|---|---|---|
| **Context word / hotword** | Presidio: context words near the entity raise the confidence (default factor 0.35, minimum 0.4); context may come in the request, e.g. a column name. Google SDP: a hotword rule with `windowBefore/After` in characters adjusts the likelihood; `windowBefore = 1` in a table = hotword in the header. Macie: keyword in the name of the field/column (structured data) or up to N characters before (default 50, 1–300). | Our vocabulary of keys **is** the hotword, but with a **structural** window (the key of the same line / the header of the column) instead of a character window. In prose, a short Macie-like window after "table/schema/host". |
| **Block / allow list** | Presidio: `deny_list` marks fixed tokens; `allow_list` in the call ignores values. Google SDP: exclusion rules by dictionary, regex, `excludeInfoTypes` and `ExcludeByHotword`. Macie: ignore words (up to 10). | A **dynamic** block list: each name learned by a key enters the session and is propagated (§4). A **public** allow list: documented defaults, reserved words, versions/dates/hashes by regex. |
| **Likelihood / score** | Google SDP: 5 levels VERY_UNLIKELY…VERY_LIKELY, fixed or relative adjustment. Comprehend: score per entity. | 3 internal levels: CERTAIN (key position) and PROBABLE (shape + hotword in prose) mask; POSSIBLE (shape only) masks if it has `_`+digit, otherwise it is only recorded. |

Comprehend PII has no type for host, database, table or column (it has `USERNAME`, `URL`,
`IP_ADDRESS`, AWS keys) — off-the-shelf NER does not cover object names; the fallback is necessary.

### 7. What this fallback does NOT guarantee

- **Name in prose without structure**: "the northern sales database went down" — no key, no shape.
- **Plain word as a name**: schema `cliente`, table `pedidos`. Masked in the key position,
  but not propagated to free prose (only in backticks/quotes). It leaks if quoted loose.
- **Split name**: `tb_pedido` on one line and `_x9` on the other; "table pedido x9" spelled out.
- **Abbreviation / nickname**: `t1`, `p`, "the PX9", an SQL alias; a translation ("orders table").
- **Key outside the vocabulary**: `alvo: ...`, `onde: ...`, a key in another language, an obfuscated key.
- **Misleading key**: `user: true` goes through (boolean); `name: tb_pedido_x9` only if `name` is Group B
  and the value has the shape.
- **Multi-line / nested value** (`table: |`, inline object) that the generic reader does not follow.
- **Name inside an error message** without quotes, or in an image/screenshot.
- **Inference by the model**: even with a pseudonym, context (region, volume, industry) can re-identify.
- **Collision**: a learned name equal to a common word or a public API is replaced too much (or excluded
  by the allow list and then leaks in prose).

### Links used

- Presidio — context words: https://presidio.dataprivacystack.org/tutorial/06_context/
- Presidio — deny list: https://presidio.dataprivacystack.org/tutorial/01_deny_list/
- Presidio — allow list: https://presidio.dataprivacystack.org/tutorial/13_allow_list/
  (the old microsoft.github.io/presidio redirects here; project in transition to a community maintainer)
- Google SDP — hotword rules: https://docs.cloud.google.com/sensitive-data-protection/docs/creating-custom-infotypes-likelihood
- Google SDP — exclusion rules: https://docs.cloud.google.com/sensitive-data-protection/docs/creating-custom-infotypes-rules
- Google SDP — likelihood: https://docs.cloud.google.com/sensitive-data-protection/docs/likelihood
- Amazon Macie — keywords, ignore words, distance: https://docs.aws.amazon.com/macie/latest/user/cdis-options.html
- Amazon Comprehend — PII types: https://docs.aws.amazon.com/comprehend/latest/dg/how-pii.html
- PostgreSQL libpq — Parameter Key Words: https://www.postgresql.org/docs/current/libpq-connect.html
- SqlConnection.ConnectionString: https://learn.microsoft.com/en-us/dotnet/api/system.data.sqlclient.sqlconnection.connectionstring
- Snowflake JDBC parameters: https://docs.snowflake.com/en/developer-guide/jdbc/jdbc-parameters
- Snowflake identifiers: https://docs.snowflake.com/en/sql-reference/identifiers-syntax
- dbt Snowflake setup: https://docs.getdbt.com/docs/core/connect-data-platform/snowflake-setup
- dbt BigQuery setup: https://docs.getdbt.com/docs/core/connect-data-platform/bigquery-setup
- Kubernetes kubeconfig: https://kubernetes.io/docs/concepts/configuration/organize-cluster-access-kubeconfig/
- Kubernetes names: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/
- Apache Kafka consumer configs: https://kafka.apache.org/43/configuration/consumer-configs/
- Amazon S3 bucket names: https://docs.aws.amazon.com/AmazonS3/latest/userguide/bucketnamingrules.html
- SemVer (version regex): https://semver.org/ [DOC ✓] — not consulted in this round


### 8. What is implemented

The key-value fallback is the `chave-valor` reader (see the section JSON, YAML, TOML..., §8). Besides
it, two generic readers with no format:

**Internal host in a URL and loose address** (the `endereço` reader, rules `url-interna` and
`host-interno`). In `http(s)://`, `ws(s)://`, `grpc://`, `ftp://`, `redis://`... (any
scheme that is not a database, storage, queue or git one), the host is a **strong** server when it is
internal: a single label with no dot (`http://wiki-interna/`) or ending in `.local` (RFC 6762),
`.internal` (ICANN reservation, 2024), `.home.arpa` (RFC 8375), `.svc`/`.cluster.local` (Kubernetes
DNS), `.intra`, `.intranet`, `.interno`, `.corp`, `.lan`, `.localdomain`. The user before the
`@` too. A public domain stays (the client's ones go in `dominios_internos`). Outside a URL, a name
with those suffixes (`db01.corp:5432`, `redis.vendas.svc.cluster.local`) is also a server, if it has a
digit or a hyphen, or two labels before the suffix, and is not a code attribute (`threading.local()`)
nor a Java package (`org.foo.internal`).

**Registered term inside an identifier** (the `termo-embutido` reader, on when there are
one-word `termos`). The term detector replaces the whole word; this one replaces the
identifier-looking identifier that has the term as a whole piece (separated by
`_ . -` or camelCase): the term `acmex` catches `acmex_pedidos`, `dbAcmexVendas01`, `svc-acmex-carga`,
but not `acmexvendas` nor `macmex_x`. The type comes from the position (after `FROM`/`JOIN` → table,
after `DATABASE`/`USE` → database, the value of a known key → the entity of the key, the host of a URL →
server); with no position, service. Always strong.

| Rule | Catches | Strong? |
|---|---|---|
| `url-interna` | internal host in a URL; user of the URL | yes |
| `host-interno` | `name.internal-suffix` outside a URL | yes |
| `termo-embutido` | identifier with a registered term as a piece | yes |


## CI pipelines

In a pipeline almost everything is vocabulary of the tool (keys, public actions, hosted runner
labels). The proper names sit in a few stable places: the container **image**, the self-hosted
**runner/agent** and the deploy **environment**. Only those are handled; the rest (commands of
`script`/`run`, variables) goes through the other detectors.

### 1. Detection signals

| Format | Strong signal | Weak signal |
|---|---|---|
| GitHub Actions (`.github/workflows/*.yml`) | top-level `jobs:` with `runs-on:` in the jobs | top-level `on:`, `steps:` with `uses:` |
| GitLab CI (`.gitlab-ci.yml`) | a job with `script:`; top-level `stages:` | `image:`, `services:`, `tags:` in the job |
| Azure Pipelines (`azure-pipelines.yml`) | top-level `trigger:`/`pool:`/`stages:`/`jobs:`/`steps:` | `- script:`, `- task:` |
| Jenkinsfile (declarative) | `pipeline {` | `agent {`, `stages {`, `stage('x') {` |
| Jenkinsfile (scripted) | `node(` together with `stage(` | — |

### 2. Position → entity type

| Format | Position | Entity | Note |
|---|---|---|---|
| GitHub Actions | `jobs.<id>.runs-on` (text, list, `group:`, `labels:`) | HOST | weak evidence; public labels stay |
| GitHub Actions | `jobs.<id>.container` (text or `.image`), `jobs.<id>.services.<name>.image` | image | image rule (Kubernetes section, item 3) |
| GitHub Actions | `jobs.<id>.environment` (text or `.name`) | SVC | weak evidence |
| GitLab CI | `image` (text or `.name`), `services[]` (text or `.name`) | image | image rule |
| GitLab CI | `<job>.tags[]` | HOST | weak evidence; only in a job (with `script`/`stage`...) |
| GitLab CI | `<job>.environment` (text or `.name`) | SVC | weak evidence |
| Azure Pipelines | `pool` (text) or `pool.name` | HOST | weak evidence; `pool.vmImage` stays |
| Azure Pipelines | `container` (text or `.image`), `environment` | image / SVC | — |
| Jenkinsfile | `agent { label 'x' }`, `node('x')` | HOST | weak evidence |
| Jenkinsfile | `docker { image 'x' }`, `docker.image('x')` | image | image rule |

Weak evidence: the value is masked where it appears, but is only learned (and propagated to the rest of the
text) if it also appears in another rule.

### 3. Public vocabulary (never mask)

| Vocabulary | Examples | Source |
|---|---|---|
| hosted runner labels | `ubuntu-latest`, `ubuntu-24.04`, `windows-latest`, `windows-2022`, `macos-latest`, `macos-14`, `self-hosted`, `linux`, `x64`, `arm64` | docs "GitHub-hosted runners" and "self-hosted runners" (default labels) |
| public images | Docker Official Image (`node:20`, `postgres:16`), an organization of the public reference (`bitnami/redis`) on Docker Hub or a public registry, and a vendor registry (`registry.k8s.io`, `mcr.microsoft.com`) | image rule (Public only with proof) |
| generic environments | `production`, `staging`, `development`, `dev`, `prod`, `test`, `qa` | — |
| expressions | `${{ ... }}` (Actions), `$VAR` / `${VAR}` (GitLab), `$(var)` (Azure) | they are not values: they stay |
| keys and actions | `jobs`, `steps`, `uses: actions/checkout@v4`, `script`, `stage`, `pipeline`, `agent` | spec of each tool |

### 4. Identifier rules

- A masked value only has letters, digits, `_`, `.` and `-` (no space, quotes or expression). A compound
  Jenkins label (`'linux && docker'`) stays.
- Image: `[registry/]path[:tag][@digest]`; registry = first piece with `.` or `:`. A private
  registry becomes HOST, each piece of the path becomes SVC; the tag and the digest stay.
- `tags:` only counts as a runner inside a GitLab job: in an Ansible task (list at the root), `tags`
  is a task label and stays.

### 5. Before/after examples

```yaml
# before                                         # after
jobs:                                            jobs:
  build:                                           build:
    runs-on: [self-hosted, runner-financeiro-x1]     runs-on: [self-hosted, HOST_...]
    container: registry.exemplo.interno/ci/builder-x1:3    container: host_.../svc_.../svc_...:3
    environment:                                     environment:
      name: amb-pedidos-x9                             name: svc_...
    steps:                                           steps:
      - uses: actions/checkout@v4                      - uses: actions/checkout@v4
```

```groovy
// before                                          // after
pipeline {                                         pipeline {
  agent { label 'agente-build-x6' }                  agent { label 'svc_...' }   // HOST
  ...                                                ...
}                                                  }
```

### 6. Hard cases and limits

- **Matrix and expressions.** `runs-on: ${{ matrix.os }}` has no literal value: it stays.
- **Runner with a generic name** (`build`, `linux-grande`): masked in place, not propagated (it does not
  look like an identifier or it is weak evidence).
- **Third-party actions** (`uses: org/action@v1`) and GitLab's `include:` point to repositories; they are left
  to the URL/repository rules, not to these.
- **Environment variables** (`env:`, `variables:`) are ordinary key-value (generic fallback).
- **Scripted Jenkins** with arbitrary Groovy logic: only `node('x')` and `docker.image('x')` are stable.

### 7. Links used

- GitHub Actions, workflow syntax: https://docs.github.com/actions/reference/workflows-and-actions/workflow-syntax
- GitHub-hosted runners (labels): https://docs.github.com/actions/reference/runners/github-hosted-runners
- GitLab CI/CD YAML: https://docs.gitlab.com/ci/yaml/
- Azure Pipelines YAML schema: https://learn.microsoft.com/azure/devops/pipelines/yaml-schema/
- Jenkins Pipeline syntax: https://www.jenkins.io/doc/book/pipeline/syntax/
- not consulted in this round [VERIFY]: the links above were written from memory


## Repositories, packages and paths

Central idea: the name of the organization, of the repository, of the internal package and of the user who owns a
folder appears in fixed positions of public formats (git remote, `go.mod`, `pom.xml`,
`package.json`, home path). The position gives the type; public hosts and prefixes stay.
Code: `internal/mask/leitor_enderecos.go` (git, paths) and `leitor_nuvem.go` (packages).

### 1. Detection signals

| Family | Signal (cheap filter before any analysis) |
|---|---|
| Git remote (scp) | `git@host:org/repo(.git)` — only if the text has `git@` |
| Git remote (URL) | `ssh://`, `git://`, `git+ssh://` always; `https://` only if the path ends in `.git`, if it comes after `git clone`, `git remote`, `git push`, `git pull`, `git fetch` or `git submodule` on the same line, or if it is the `url =` of a `[remote "..."]`/`[submodule "..."]` section of `.git/config` |
| Go module | a line that starts with `module ` and only has the path (go.mod) |
| groupId | `<groupId>...</groupId>` (Maven) or `group = '...'` / `group "..."` at the start of the line (Gradle) |
| npm scope | `"name": "@scope/package"` (package.json) |
| User path | `/home/<u>/`, `/Users/<u>/`, `C:\Users\<u>\` (also `C:\\Users\\` escaped in JSON) |

### 2. Position → entity type

| Format | Position | Entity | Strong? |
|---|---|---|---|
| git remote | the pieces before the last one (group and subgroups; `scm`, `_git`, `v3` belong to the hosting and stay) | organizacao (`ORG_`) | yes |
| git remote | the last piece without `.git` | repositorio (`REPO_`) | yes |
| git remote | internal host (single label or internal suffix) | servidor (`HOST_`) | yes |
| git remote | user of `ssh://user@` (not `git`) | usuario (`USR_`) | yes |
| go.mod | a host outside the public list: internal → server; each piece of the path (except `vN`) | servidor / pacote (`PKG_`) | yes |
| groupId | the pieces after the reversed TLD (`br.com.`, `com.`) | pacote (package) | yes |
| package.json | `@scope` / `package` | organizacao / pacote | yes |
| home path | the piece after `home`/`Users` | usuario | yes |
| home path | the following folders that look like identifiers (not the final file) | pasta (`DIR_`) | no |

Outside those contexts, an ordinary github.com or gitlab.com URL is **not** masked (it appears in
all public documentation). Measured on 2 MB of `.md` from Go modules: 6 names in a git context.

### 3. Public vocabulary (never mask)

| Group | List | Source |
|---|---|---|
| Go module hosts | `github.com`, `gitlab.com`, `bitbucket.org`, `golang.org`, `google.golang.org`, `gopkg.in`, `go.uber.org`, `k8s.io`, `sigs.k8s.io`, `example.com/org/net` | the most common module hosts; `example.*` reserved (RFC 2606) |
| groupId prefixes | `org.apache`, `org.springframework`, `com.google`, `io.*`, `javax`, `jakarta`, `org.jetbrains`, `org.junit`, `junit`, `org.slf4j`, `ch.qos`, `com.fasterxml`, `org.hibernate`, `org.projectlombok`, `org.mockito`, `org.eclipse`, `com.amazonaws`, `software.amazon`, `com.microsoft`, `com.azure`, `org.postgresql`, `com.mysql`, `com.oracle`, `com.h2database`, `org.yaml`, `com.squareup`, `org.codehaus`, `org.gradle`, `com.android`, `androidx`, `org.jboss`, `org.testcontainers`, `org.flywaydb`, `org.liquibase`, `commons-*`, `com.github`, `org.example`, `com.example`... (complete list in `gruposPublicos`) | Maven Central coordinates (central.sonatype.org) and most used groups (mvnrepository.com/popular) |
| npm scopes | `@types`, `@angular`, `@babel`, `@vue`, `@nestjs`, `@aws-sdk`, `@google-cloud`, `@azure`, `@mui`, `@testing-library`, `@typescript-eslint`, `@storybook`, `@tanstack`, `@octokit`, `@sentry`... (list in `escoposNpmPublicos`) | most downloaded scopes of the npm registry |
| Path users | `user`, `runner` (GitHub Actions), `ubuntu`, `ec2-user`, `azureuser`, `opc`, `vagrant`, `jovyan` (Jupyter), `linuxbrew` (Homebrew), `node`, `gopher`, `Shared`, `Public`, `Default`... | default users of cloud and CI images |
| Folders | `node_modules`, `site-packages`, `dist-packages`, `__pycache__`, `AppData`, `LocalLow`, `OneDrive`, `IdeaProjects`, `PycharmProjects`, `go-build*`, tool+version (`python3.10`, `go1.22.0`), hidden folders (`.config`, `.venv`) | Windows Known Folders, macOS layout, Python, Node, Go, JetBrains |

Folders that do not look like identifiers (`Documents`, `Desktop`, `src`, `bin`, `projetos`) already stay by the general rule.

### 4. Identifier rules

- Repository and organization: `[A-Za-z0-9_.-]+`, starts with a letter or a digit (the GitHub and GitLab rule).
- Go module: import path (`[A-Za-z0-9._~/-]`); `v2`, `v3`... are a major version and stay.
- groupId: pieces `[A-Za-z_][A-Za-z0-9_$#-]*` separated by dots.
- npm scope: lowercase, digits, `-`, `_`, `.` (npm naming rules).
- Path user: `[A-Za-z0-9._-]{1,32}`, followed by a separator or the end; `$USER` and `<you>` stay.

### 5. Before/after examples

```text
git clone git@github.com:org-exemplo/repo-demo.git   →  git clone git@github.com:ORG_.../REPO_....git
git remote add origin https://gitlab.interno/org-exemplo/sub/repo-demo  →  https://host_.../org_.../org_.../repo_...
module git.interno/org-exemplo/svc-pedidos   →  module host_.../pkg_.../pkg_...
<groupId>br.com.exemplo01.vendas</groupId>   →  <groupId>br.com.pkg_....pkg_...</groupId>
"name": "@org-exemplo/pacote-demo"           →  "name": "@org_.../pkg_..."
/home/joao_x/projetos/cliente_x9/main.go     →  /home/usr_.../projetos/dir_.../main.go
https://github.com/org-exemplo/repo-demo/issues/1   (stays: outside a git context)
```

### 6. Hard cases and limits

- `git clone https://github.com/golang/go.git` in a README is masked (it is in a git context):
  the reader does not know whether the project is public. Plain names (`golang`) are not propagated.
- A path with a space (`C:\Users\Maria Souza\`) is not read (the user would have a space).
- `import "git.interno/org/x/pkg"` in Go code: there is no import reader; the name learned in
  `go.mod` only propagates if it looks like an identifier (`svc-pedidos` yes, `vendas` no).
- A company groupId under `com.github.*` or `io.*` stays (public prefix).

### 7. Links used

- Git — URLs: https://git-scm.com/docs/git-clone#_git_urls
- Go Modules Reference (go.mod, module path): https://go.dev/ref/mod
- Maven Central — coordinates: https://central.sonatype.org/publish/requirements/coordinates/
- Maven — naming conventions: https://maven.apache.org/guides/mini/guide-naming-conventions.html
- npm — scope: https://docs.npmjs.com/cli/v10/using-npm/scope ; package.json name: https://docs.npmjs.com/cli/v10/configuring-npm/package-json#name
- Windows Known Folders: https://learn.microsoft.com/windows/win32/shell/knownfolderid
- AWS — default users of the AMIs: https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/managing-users.html

## Cloud resources and network users

**Snowflake** (the same generic rules, plus the shapes of the product). Account: keys
`account`/`account_id`/`accountname` in any syntax (key-value and named argument); the
host `<account>.snowflakecomputing.com` (the account, without the region) and `app.snowflake.com/<org>/<account>/`
(`leitor_nuvem.go`); afterwards, loose in the text, by propagation. A role (`role`, `rolename`,
`SNOWFLAKE_ROLE`) is a user; in SQL, the name after `ROLE` or `USER` (`USE ROLE`, `GRANT ... TO
ROLE`, `REVOKE ... FROM ROLE`, `GRANT ROLE x TO USER y`, `CREATE/ALTER/DROP ROLE`) is a user.
A warehouse is a service: key `warehouse`, `USE WAREHOUSE x`, `WAREHOUSE = x`, `CREATE/ALTER
WAREHOUSE x`. Allow/deny lists (`allowed_roles: [..]`, `blocked_users`) have items of the type of the
key. Names of MASKING/ROW ACCESS/NETWORK... POLICY, TAG, STAGE (also `@db.sch.stage`), STREAM,
TASK, PIPE, SEQUENCE, FILE FORMAT, SECRET, ALERT, DYNAMIC TABLE are schema objects (qualified
name, last piece as a table); `COPY INTO` is a statement. The DDL parameters
(`SCHEDULE`, `URL`, `AUTO_SUSPEND`...) are in the SQL vocabulary. SHOW output as JSON: `name`
next to `schema_name` is a table (next to only `database_name`, a schema); `owner` is a user. A DataHub
URN with parts padded with spaces (`db2,ABCD    .TABELA`) is read without the spaces.

Central idea: cloud resource identifiers have a published grammar (ARN, Azure
Resource Manager ID, Google Cloud resource name); a network user has two fixed shapes
(`DOMAIN\user`, `user@host` after ssh/scp). The grammar gives the type of each piece.
Code: `internal/mask/leitor_nuvem.go` (cloud, public IP) and `leitor_enderecos.go`
(network user).

### 1. Detection signals

| Family | Signal (filter before analyzing) |
|---|---|
| ARN | text with `arn:`; `arn:aws[-cn|-us-gov|-iso...]:service:region:account:resource` |
| Azure | text with `/subscriptions/`; `/subscriptions/<guid>/resourceGroups/<rg>/providers/<Ns>/<type>/<name>...` |
| Google Cloud | text with `projects/`; `projects/<p>/<collection>/<name>` or `projects/<p>/locations/<l>/<collection>/<name>` |
| DOMAIN\user | text with `\`; NetBIOS domain (uppercase, digits, `-`; 2 to 15) before the backslash |
| ssh/scp/sftp/rsync | the command as a word, and on the same line an argument `user@host[:path]` |
| Public IP (option `ip_publico`) | IPv4 with 3 dots or IPv6 with 2+ `:` |

### 2. Position → entity type

| Format | Position | Entity | Strong? |
|---|---|---|---|
| ARN | account (12 digits) | conta_nuvem (`ACC_`) | yes |
| ARN | resource of `s3` (up to `/`) | bucket | yes |
| ARN | resource of `sqs` and of `sns` | fila (queue) | yes |
| ARN | `dynamodb` `table/<name>` | tabela (table) | yes |
| ARN | `iam` `user/<name>` | usuario (user) | yes |
| ARN | the others (`type/name`, `type:name`, `role/path/name`) | servico (service) | yes |
| ARN | partition, service, region | **never** | — |
| Azure | subscription GUID / resource group / name of each resource (`type/name`) | conta_nuvem / servico / servico | yes |
| Google Cloud | project | conta_nuvem | yes |
| Google Cloud | `topics`/`subscriptions`/`queues` → fila; `datasets` → schema; `tables` → tabela; `buckets` → bucket; the others (`instances`, `functions`, `services`...) → servico | according to the collection | yes |
| `DOMAIN\user` | user (the domain is left to the other detectors) | usuario | yes |
| `ssh user@host` | user / host outside a public domain and that is not an IP | usuario / servidor | yes |
| Public IP | public IPv4 or IPv6 | servidor (server) | yes |

### 3. Public vocabulary (never mask)

| Group | List | Source |
|---|---|---|
| AWS example accounts | `123456789012`, `111122223333`, `444455556666`, `012345678901`; managed resources (`arn:aws:iam::aws:policy/...`) | AWS documentation |
| Google Cloud / Azure examples | `my-project`, `project-id`, `my-topic`, `my-subscription`; subscription `00000000-0000-...`; `my-resource-group` | Google Cloud and Azure documentation |
| Windows domains and accounts | `NT AUTHORITY`, `BUILTIN`, `NT SERVICE`, `WORKGROUP`, Registry roots (`HKLM`, `HKCU`...); accounts `SYSTEM`, `Administrator(s)`, `Guest` | Well-known SIDs (Microsoft Learn) |
| ssh users | `root`, `git`, `ubuntu`, `ec2-user`, `admin`, `pi`... | default users of images |
| IPs | `::1`, link-local, `0.0.0.0`, broadcast, multicast, private ranges (the IP detector already handles them), documentation ranges (RFC 5737: `192.0.2.0/24`, `198.51.100.0/24`, `203.0.113.0/24`; RFC 3849: `2001:db8::/32`), public DNS `8.8.8.8`, `8.8.4.4`, `1.1.1.1`, `1.0.0.1`, `9.9.9.9`, `208.67.222.222`, `2001:4860:4860::8888`, `2606:4700:4700::1111` | IANA Special-Purpose Address Registry |

### 4. Identifier rules

- AWS account: exactly 12 digits. A resource with a wildcard or a variable (`fila-*`, `${Nome}`) stays.
- Azure GUID: 36 characters with 4 hyphens. Names `[A-Za-z0-9_.-]+`.
- Google Cloud project: 6 to 30 characters, lowercase, digits and hyphen, starts with a letter (no dot).
- `DOMAIN\user`: the user starts with a letter, has 2+ characters, and does not continue with `\` or `/`
  (`HKLM\SOFTWARE\...` and `\\SERVER\share` stay). A code escape sequence
  (`"ERRO\nfalhou"`: a user starting with `n t r b f v a x u 0` glued to another character) stays.
- An IPv4 whose first three numbers have one digit (`6.0.6.1`, `8.2.4.44`) is a version or a section
  number and stays. An IPv6 needs 3+ `:` or a group with 3+ hexadecimal digits (`x[1::2]` stays).

### 5. Before/after examples

```text
arn:aws:sqs:us-east-1:210987654321:fila-pedidos-x9     →  arn:aws:sqs:us-east-1:ACC_...:top_...
arn:aws:s3:::bkt-relatorios-demo/*                      →  arn:aws:s3:::bkt_.../*
/subscriptions/1a2b3c4d-1111-2222-3333-abcdefabcdef/resourceGroups/rg-dados-x9/providers/Microsoft.Storage/storageAccounts/contaexemplo01
   →  /subscriptions/acc_.../resourceGroups/svc_.../providers/Microsoft.Storage/storageAccounts/svc_...
projects/proj-exemplo-01/topics/fila-pedidos-x9         →  projects/acc_.../topics/top_...
entrou como CORPX\svc_relatorio                         →  entrou como CORPX\usr_...
ssh -p 2222 svc_relatorio@db-exemplo-01                  →  ssh -p 2222 usr_...@host_...
servidor em 34.120.10.5 (with ip_publico on)           →  servidor em HOST_...
```

### 6. Hard cases and limits

- Public IP comes **off**: an IP of a public service (CDN, API) would also be replaced, and the
  model loses the reference. Turn it on with `objetos.ip_publico`.
- In an IAM ARN without a path (`role/name`) the name is a service; in `user/name`, a user.
- `ssh host` without a user: the host is only masked if another reader catches it (internal suffix,
  `dominios_internos`).
- `DOMAIN\user` with a user that has a space is not read.
- A Google Cloud resource name outside `projects/...` (`gs://`, `bq://`) is left to other readers.

### 7. Links used

- ARN: https://docs.aws.amazon.com/IAM/latest/UserGuide/reference-arns.html
- Azure Resource Manager — IDs and naming rules: https://learn.microsoft.com/azure/azure-resource-manager/management/resource-name-rules
- Google Cloud — resource names: https://cloud.google.com/apis/design/resource_names
- Google Cloud — project ID: https://cloud.google.com/resource-manager/docs/creating-managing-projects
- Windows — Well-known SIDs: https://learn.microsoft.com/windows/win32/secauthz/well-known-sids
- NetBIOS — domain names: https://learn.microsoft.com/troubleshoot/windows-server/active-directory/naming-conventions-for-computer-domain-site-ou
- IANA — IPv4 and IPv6 Special-Purpose Address Registry: https://www.iana.org/assignments/iana-ipv4-special-registry/ ; https://www.iana.org/assignments/iana-ipv6-special-registry/
- RFC 5737 (documentation IPv4): https://www.rfc-editor.org/rfc/rfc5737 ; RFC 3849 (IPv6): https://www.rfc-editor.org/rfc/rfc3849

## Variations every reader accepts (section G)

`variacoes_test.go` generates, for each case of each reader, the variations below (one dimension at
a time) and checks that every name is masked and no word of the template is masked needlessly:
letter case (UPPERCASE, lowercase, Capitalized and, in SQL, aLtErNaTiNg), spaces, tabs and line breaks between
tokens, quoting (`"` `'` `` ` `` `[ ]`), comments in the middle (`--`, `/* */`, `#`, `//`, `;`),
punctuation glued at the end, names with 2, 3 and 4 parts, names with `$`, `#`, `-` and accents, several
items on the same line and the whole text inside a JSON string (escaped quotes).

What started to hold:

| Reader | It did not cover before | Now |
|---|---|---|
| SQL | a comment between the name and the clause (`UPDATE t /* x */ SET`); `create table`, `truncate table`, `exec` and `use` outside uppercase; an accented name (`relatório_01`) | the shape is read without comments; DDL, `truncate`, `exec`/`call`/`use`/`describe` in any case count with an unambiguous shape (an identifier-looking name, or followed by `(`, `;`, `@`, end of line); identifier with accented letters |
| SQL in prose | a sentence with uppercase keywords (`o SELECT pega os dados FROM da tabela...`) became columns | 3 loose words side by side (lowercase, no `_` or digit, outside the vocabulary) are prose: the statement ends there; `UPDATE` requires `SET column =` |
| Error message | an accented name; a message inside JSON on a single line (`relation \"x\"`) | accented identifier; JSON with escaped quotes is decoded even with one string |
| Connection | `databaseName=`/`currentSchema=` as the only pair after a database URI (`jdbc:sqlserver://h;databaseName=x`) | the pair right after `;` `?` `&` of a database URI on the same line counts as a connection string |
| Table, schema | accented cell and column name | same |
| Key-value | a value that is accented or has `$` in the middle (for database, schema and table); a value followed by the quote that closes the surrounding string (`"...table=x"}`) | accepted; only a prefix of 1–2 letters before the quote (`f"`, `r'`) is a literal |
| Code | a name that is accented or has `$` in the middle | same |
| Paths | a folder with `$`/`#` in the middle or an accent cut the path | accepted (`$` at the start is still a variable) |
| Command line | a value with the closing of the surrounding string glued to it (`-U x"}`) | a closing, comma and period that were not opened in the token are dropped |
| Addresses | URLs glued without a separator and a repeated `a://` were quadratic | each URL goes at most up to the scheme of the next one (and 2 KB) |
| Fields (`field: value`) | the prefix of a Python literal (`r'...'`) became the value | the prefix is skipped; a value of 1–2 letters, a regex pattern (`(?`, `\d`, `\b`) and a template (`{x}`, `%s`) are not data (the optional quasi-identifiers accept a short value) |

Non-Latin scripts: database, schema, table, column, procedure, index and user accept letters of
any script (`Продажи_хх`, `客户数据`, `판매팀`) in key-value, in code, in SQL and in the database
name of a connection URL: for a Russian, Chinese or Korean client the real name is in that
script. A dotted user (`maria.souza`, `josé.antônio`) is also a name; a code access
(`settings.db_user`, `self.user`) is not.

Known limits: the name of a host, bucket, queue and namespace remains ASCII only (it is what DNS and the
clouds accept); `$` in the middle only counts in SQL names and paths; alternating case is only tested
where the language is case-insensitive (SQL); the sequence of 3 loose words is not searched for
when there is `_`, a digit or a vocabulary word among them.

## Knowing the name: conversation memory and the naming rules

Up to here, a name was only masked where the structure said it was a name, and was only propagated if
it looked like an identifier. Two experiments showed the limit of that:

- a catalog read with `head` arrived masked, but the same content printed by a script
  as `tag | ordem | regex | plataforma | objeto | coluna | tipo`, with no header, came out almost
  entirely in the clear;
- a `kubectl get ns` with common-word namespaces (`payments`, `billing`) masked
  nothing; the command-line reader masked `-n payments` in the next command, but the output
  brought `payments` in the clear, and the pseudonym became decoration.

The principle of this round is not to chase the format of the output: the proxy needs to **know** the
name and, once it knows it, search for the exact word in any format. None of this uses a list
of internal names, a catalog, a user dictionary or a per-tool rule: the code does not know
what `kubectl`, `aws`, `psql` or `gh` is. Every decision comes from grammar, structure, provenance
or uniqueness. The only vocabulary is the closed grammar of the enumeration and query verbs
(`list`, `get`, `describe`, `show`, `ls`, `search`, `find`, `query`, `select`, `scan`, `enum`,
`dump`, `export`, `fetch`, `inspect`, `info`, in any convention: `get_x`, `listX`, `Get-X`,
`GET /x`), the types of `EntObjeto`, the public vocabularies that already existed and the public
reference derived by measurement (below).

### The pieces (decisao.go)

- **Decision:** a name decided in a text, with the name, the type and the rule that decided it
  (`Decisao{Nome, Ent, Regra, Generica}`). The object names found by the readers enter
  as decisions of the rule `leitor`; the new rules enter with their own name.
- **Decider:** a rule that looks at the whole text and at what was already found in it, and decides names
  (`TextoCtx.Decidir` masks in place and records). The deciders run after the readers, only
  on new text; the rest comes from the memo.
- The decisions stay in the memo result, in RAM, along with the masked text. **They do not go to
  `vistos.json`**: the format, the 90-day validity, the cap and the learning rules of
  `vistos.json` are the same as before.
- The command hint (what the proxy knows about the `tool_use` that produced a `tool_result`) may carry
  an extension after the separator `\x1e`; the table reader reads only the old part, the
  deciders receive the extension. The whole hint enters the memo key.

### Conversation memory

In a request, the proxy masks all the texts in parallel (1st pass) and then assembles the
body (2nd pass). Between the two, it gathers the names decided in **all** the texts of the request:
that is the conversation memory. It is applied to the texts not sent yet, as whole words,
with the case rule of each type (SQL and server names case-insensitive; the others
exact). A decision made in a new text also holds for the other new texts of the same
request. Applying the memory is a step after the memo and does not change the memo key.

- **It ends by itself.** Nothing is stored per conversation outside the memo: the memory is recomputed
  from the pieces of the request itself. In a new conversation, without the inventory in the history, the
  common word goes back to being just a word.
- **It does not rewrite the past.** What was already sent stays frozen (`memoria_enviados.go`): the
  new decisions hold only for text that has not gone out yet. An old text that brought the word
  in the clear stays the same, so the API's prompt cache stays valid.
- **It applies to** the user's messages and the command outputs. It does not apply to
  thinking blocks (untouched in both directions) nor to the assistant's text (see *Who wrote it*).
- **A generic word does not enter.** A word of the derived public reference (`default`,
  `public`, `api`...) decided in an inventory is masked there, but does not enter the memory
  (`Decisao.Generica`): propagating `default` to the whole conversation would mask half of the text.

### Who wrote it

When unmasking the response (JSON or SSE streaming), the proxy keeps, per text block of the
assistant, the **original** text the API sent (with pseudonyms only, never with the real value),
indexed by the hash of the unmasked text that Claude Code will resend in the history, and the
spans it translated from a pseudonym. When the block comes back:

- the original is used: **exactly** the words the proxy translated go back to being pseudonyms;
- the conversation memory does **not** apply to the rest of the assistant's text: Claude never saw the
  real name; if it wrote the word, it is a common word (the model writes "payments" because it is the
  word, not because it knows the namespace).

Without a record (the proxy restarted, another process answered) the previous behavior applies. The
record stays in RAM with a cap; if it is persisted, it is in the style of `memoria_enviados.go` (pseudonyms
and HMAC only).

### Provenance rule

The proxy links each `tool_result` to the `tool_use` that produced it. The words of the **program** are the
tokens of the command or of the script, broken on camelCase, snake_case, kebab-case, path,
Verb-Noun and literals. A word of the output that is not in the program and is not of the public
reference **came from the data**. Mandatory exception: the word the proxy itself put in the
`tool_use` when translating a pseudonym (the model wrote `-n ns_x`, the proxy sent
`-n payments`) is still a name, although it is in the program.

This way, a script with the tags and the regexes as literals prints those tags and regexes readable (they belong
to the program), and what it read from the file (tables, columns) comes from the data.

### Identity rule

The output is segmented into records generically: lines, after the transport
normalization (line number, grep, diff, ANSI, escaped JSON), and the JSON and list
records the readers already segment. The position of each candidate is its **order** in the record,
without depending on a separator. In a block of 3 or more parallel records (the same number of
candidates, with a slack of 1), the position in which all the values are distinct, next to
positions with repeated values, is **identity**: it is the key of the inventory. A list with one
item per line, all distinct, is also identity. Numbers, dates, durations,
versions and the public reference are left out.

### Decision for a common word, and echo

An identifier-looking word follows the earlier rules. A common word becomes a name when:

- it **came from the data** (provenance) **and** is in an **identity position**; or
- there is an **echo**: it appeared in an earlier output and comes back as an argument in a later `tool_use`.
  "Argument" is read by the grammar, not per tool: a value after a type word, an option
  `--x value`, an assignment, a REST path segment, `FROM` / `INTO` / `TO ROLE`. A word that is a
  piece of a distinctive name already masked is also a name (`payments` in
  `payments-api-7d9f`, in `registry/acme/payments:1.2`).

**Type of the pseudonym:** the noun the enumeration verb of the request points to
(`list_findings` → `finding`, `Get-ADGroup` → `group`, `GET /queues` → `queue`). If it matches
a type of `EntObjeto`, that prefix is used; otherwise, the generic prefix that already existed. The
user's message is also a request: "list the groups with access to the database" gives the type of the next
output, by the same grammar.

### Lexical rule (code and configuration)

A one-pass lexical scan, the same for any language, separates text literals
(single quotes, double quotes, backticks, triple quotes), comments (`#`, `//`, `/* */`, `--`, `<!-- -->`,
the `;` of INI) and values of `key: value` / `key = value` (YAML, TOML, INI, .env, JSON,
HCL). A common word is only a candidate if it is the **whole** literal or value, with no space
(a literal with a sentence is prose); a code identifier (variable, function, import) never is;
a comment is prose. The candidate becomes a name with one more hint:

- the key contains a type of `EntObjeto` (`namespace:`, `bucket=`, `table_name=`);
- anchor: another value of the same block is already a known name;
- echo;
- the word also appears as data in a command output of the conversation.

`namespace = "payments"` becomes a name; `payments = load()`, `# payments do dia` and
`msg = "payments failed"` do not.

### Guards

**Schema by the type, not by the indentation.** In the schema reader, a `name    type` block is a
schema when at least one type is a data-only type (`object`, `category`, `datetime64[ns]`,
`VARCHAR(n)`, `NUMBER(p,s)`, an uppercase type like `TIMESTAMP` and `DATE`), with or without indentation,
or when the pandas footer `dtype: object` comes. A type that is also a language type
(`string`, `int`, `bool`, `uint64`, `float64`...) does not decide alone, because a Go struct
writes `name    type` the same way; and a type with the shape of code (`*T`, `[]T`,
`strings.Builder`, `map[K]V`) is never a schema type. Which types are "also language types" is not
written by hand: it is measured in public Go code (`tipos_linguagem.txt`, below). Limit: an
indented block with only `int64` and `float64`, without a footer, is not read as a schema (it is the same as a
struct).

**SQL quoted in prose and in a comment.** The SQL reader marked 746 findings in 40 MB of
public code. From the samples, almost all came from three shapes, none of them SQL:
a code comment that talks about the code (`// Use x.Errors`, `# delete FROM line`,
`// Insert into hash table`), a common keyword in the middle of a sentence (`you can use x`,
`would never call f(), so`, `Call data.encode(...) but`), an uppercase word in the middle of another
uppercase sentence (`0x3B -> CUSTOMER USE THREE`) and a struct field named after a keyword
(`Use    uint32`, `call    ast.CallExpr`). The guards (`leitor_sql_freios.go`) are about shape:

- in a code comment (`#`, `//`, `/*`, `*`, and the comment at the end of the line), only
  the statement that opens the comment counts, outside uppercase only with an identifier-looking target, and
  never with a common word (`use`, `call`, `exec`...) outside uppercase;
- a common word outside uppercase right after another word on the same line is a sentence;
- `CALL` requires parentheses, and after them comes `;` or the end of the line;
- the target of a statement is not a data type;
- an uppercase statement does not start right after another uppercase word that is not of the
  SQL vocabulary (`EXPLAIN SELECT` still counts).

**Template connection.** In the real sessions, the public values that `conexão/tnsnames` and
`conexão/uri` learned all had the same shape: a type or attribute word followed by a
number or glued to another one (`host1`, `user2`, `db01`, `dbName`): an example name, not a resource
name. In the database URI and in tnsnames, a value of at most two pieces, all of the
vocabulary that already exists, with or without a number at the end, is not masked (`leitor_conexao_freios.go`). In the
other rules, `broker1` in a `bootstrap.servers` is still a name.

### Derived public reference

`ref_publica.txt` and `tipos_linguagem.txt` are **generated** by `TestGerarRefPublica`
(`vocab_gerar_test.go`) from the public material of the machine (the directories of
`LLM_DLP_CORPUS`: Go modules, Python libraries, `/usr/share/doc`), never written by hand. The
header of each file says from where, when and with which criterion it was generated:

```
LLM_DLP_GERAR_REF=1 LLM_DLP_CORPUS=~/go/pkg/mod:/usr/lib/python3.10:/usr/share/doc:... \
  go test ./internal/mask -run TestGerarRefPublica -v
```

- **ref_publica.txt:** a word in a name position in at least 8 distinct projects. Name
  position, by the grammar: the value of a type key (`entChave`) or `name` in YAML, JSON, TOML,
  INI, .env and a named argument (in code, only a text literal); a `--type` option; the name after
  `FROM`/`JOIN`/`INTO`/`UPDATE`/`TABLE`/`SCHEMA`/`DATABASE` (in lowercase, only a qualified
  name); the first label of the host and the first piece of the path of a URL. A project is the
  Go module (up to the `@`) or the first directory below the root of the corpus. A name that only one
  project uses never enters, and an internal company name does not appear in distinct public
  projects. In the current generation: 977 projects, 367 MB, 289 words.
- **tipos_linguagem.txt:** a type of the data type vocabulary that appears as a field or
  variable type (`\tname    type`) in Go code of at least 5 distinct projects (16 types).

- **imagens_oficiais.txt:** the Docker Official Images (`redis`, `postgres`, `nginx`...): the
  folders of the public repository `docker-library/docs` that have `content.md` and `metadata.json`
  (one per image). Generated along with the others, when that repository is in the corpus.
- **software_publico.txt and fornecedores.txt:** name and owner of each public GitHub repository
  with at least 3000 stars (`TestGerarSoftwarePublico`, with `LLM_DLP_GITHUB_TOP` pointing
  to the JSONL list `{"r": "owner/name", "s": stars}`). The list comes from the API search,
  sliced by star range because each query returns at most 1000
  (`gh api -X GET search/repositories -f q='stars:A..B fork:false' -f per_page=100 -f page=N`,
  30 queries per minute; about 22 thousand repositories in 15 minutes). Normalized without `-`,
  `_` and `.` (Docker Hub writes `prometheuscommunity/postgres-exporter`, GitHub
  `prometheus-community/postgres_exporter`). The two lists only count **together**, in the image
  rule: as measured, the list of names has every typical client codename (`apollo`,
  `atlas`, `hermes`, `phoenix`, `zeus`), so by itself it neither lets through nor holds back anything. None
  of 30 Brazilian companies tested owns a repository with 3000 stars; 0 of the 45
  secrets of the battery is in the list.

`ehGenerica` consults the reference. A word of the reference decided by any rule is
masked in place, with `Decisao.Generica = true`, and does not enter the conversation memory.

### Public only with proof

A rule that leaves a name in the clear has a leak as its worst case. So every rule
of this kind requires a guarantee of shape (one distinctive piece blocks it) and is measured against the
secrets of the battery and the names of the labelled set that must be masked. When in doubt, mask.

- **Value with no owner** (`semDono`, `vocab_dev.go`): a placeholder and a role name (`my_bucket`,
  `stub-user`, `source_db`, `XXXXXXXX`, `000000000000`) stay in the clear; one piece outside the
  vocabulary of role, type and entity is enough for the name to be the owner's (`stub-vendashx` is
  masked). An example account number only with digits repeated in equal blocks and up to 3 letters.
- **Container image:** the path of an image from Docker Hub or from a multi-user public registry
  (`ghcr.io`, `quay.io`, `public.ecr.aws`) stays in the clear only with proof: without an
  organization, a Docker Official Image; with an organization, the two keys: the organization is
  public (reference or `fornecedores.txt`) **and** the name is public software (whole, or each
  piece is software, a word of the reference, a role, a type or a version, with at least one piece of
  software: `clickhouse/clickhouse-server`, `quay.io/prometheus/statsd-exporter`). Outside that,
  the organization and the name are the owner's (`vendashx/api-cobranca`, `grafana/billing-vendashx`,
  `ghcr.io/vendashx/clickhouse-server` and the local image `image: api-x` built by
  `build:`). An unknown piece blocks, on purpose: `confluentinc/cp-kafka` stays
  masked because of `cp`, because a short acronym is precisely what distinguishes a client
  name. A vendor registry (`registry.k8s.io`, `mcr.microsoft.com`) stays whole.
- **Service named after the software** (`softwareDoTexto`): the service, host or namespace with the
  name of an image the text itself runs stays in the clear only if the image is official
  (`image: redis:7` → service `redis`) or from an organization equal to the name and public
  (`minio/minio`). `image: yudao-server` without an organization is a local image: the name is the owner's.
- **Command in sight:** a table right below a **listing** in sight (`$ airflow dags list`,
  `$ gcloud projects list`, or the line that is itself a listing) has the tool's
  header, not the owner's columns; the listing of a type outside the vocabulary types only the compound
  identity column (`dags` → `dag_id`, `projects` → `project_id`). Listing only: below
  `$ cat x.csv`, `$ python relatorio.py` or SQL in the command (`psql -c "select ..."`),
  the header is the owner's and is still a column.
- **Software proof** (`memoria_software.go`): DataHub's database is called `datahub`, Airflow's
  `airflow`; decided in a DSN, the default name of the software was spread through the whole
  conversation, including where it is the tool. The conversation memory does not spread the word when the
  conversation itself ties it to an external package, the way a compiler resolves a name: installed
  (`pip`/`npm`/`brew`/`helm`/`go get` ... `install`), imported (`from datahub.x import`,
  `require('x')`, `import "github.com/org/x"`), run as a public image (the two keys of the
  image rule) or executed as a program (`airflow dags list`, `python -m dagster`; the
  proxy passes the command of each shell call). Where a reader decides it (the DSN itself), it
  stays masked. Locks, all fail-closed:
  - second key: the word is the name of public software (`software_publico.txt` or a Docker
    Official Image); an old list only stops proving;
  - a real word never gets the proof (`palavras_comuns.txt` and `palavras_dicionario.txt`, the
    50000 most frequent words of pt and en): it is what a client codename usually is, and
    an internal package with that name is imported the same way. Measured: of the popular
    repositories with a typical codename (`polaris`, `kraken`, `nexus`, `pulsar`, `zephyr`…),
    none passes; `datahub`, `airflow`, `superset`, `metabase`, `minio`, `trino`, `grafana`,
    `clickhouse`, `dagster` pass; `kafka`, `snowflake`, `prefect`, `looker` (real words)
    are left out and stay masked. A pure number does not either (`2048` is a repository name);
  - shadowing (the local definition hides the global one): a name defined in the project (`name = "x"`
    in a manifest, `"name": "x"`, `module .../x` in `go.mod`, a folder `x/__init__.py` outside
    `site-packages`) is the owner's and cancels the proof; matching too much only blocks;
  - a registered term always wins.

  The proof and the shadowing stay in RAM, hold for any conversation (they are facts about the
  software and the project) and do not change text already sent (freezing). The two sets only
  store a name that is in the software list: the size is limited by the list, not by what
  goes through the conversation (measured: 60000 texts all different, 3 and 0 entries).
- **Origin by path** (`leitor_traceback.go`): the path of a file says whose
  code it is, the way a compiler's include paths separate the system header from the
  project header. Being in a dependency folder is not enough, because the client's internal
  package is installed and downloaded into the same folders. It is third-party code:

  | Where | What must be public |
  |---|---|
  | `site-packages`, `dist-packages`, `node_modules` | The package: the name of a popular repository or an Official Image (`pandas`, `express`); in scoped npm, the scope (`@types`, `@aws-sdk`). A role name (`core`, `common`, `utils`, `app`) proves nothing, and neither does a package defined in the project (shadowing) |
  | Go module cache (`pkg/mod`) | The module: an open-source-only host (`golang.org`, `k8s.io`, `gopkg.in`) or `github.com/<owner>/<repository>` with the two keys (vendor owner and popular repository) |
  | `.m2/repository`, `.gradle/caches` | The group: a public Maven group, except the whole-TLD ones (`io.<company>` belongs to the company) |
  | `.cargo/registry` | The registry: that of crates.io |
  | JVM, GOROOT, `lib/python3.11/`, `<frozen ...>`, `node:...` | Nothing: it is the platform. A loose `lib/python3/` and `internal/` in a path are project folders |

  Three uses, with two levels of strictness:
  - **traceback**: the line that points to third-party code stays; the one that points
    elsewhere is the client's, and the function, the module, the identifiers of the code line and the value
    quoted in the final error (`KeyError: 'COD_APOLICE_HX'`) are its names. Python, Java/Kotlin/
    Scala (package, class, method and file; `br.com.<company>.<system>` including in
    lowercase), JavaScript and Go (function, receiver and the package `github.com/<company>/<system>`,
    with the folder of the same name on the path line). Here the criterion of the table applies as it is:
    without the reader, the whole line already went out in the clear;
  - **path**: the path readers stop at the public package (`.venv/lib/python3.11/
    site-packages/sqlalchemy/engine/base.py` stays whole) and keep reading the folders of the
    project under a public user (`/home/ubuntu/<project>/...`: the user stays, the
    project does not);
  - **file reading**: the output of a tool that read a dependency file
    (`file_path` of the call) is masked as always, but does not teach the conversation memory:
    DataHub's internal table read in `site-packages/datahub/` does not become a client name in
    prose.

  In the path and in file reading the criterion is stricter, because there the reader already
  masked and taught: an installed package named after a dictionary word (`pandas`,
  `requests`, and also `atlas`, `apollo`) does not count, for the same reason as the software proof.
  The folders of `site-packages/pandas/...` then follow the usual path rule.

Limit: in a translation catalog, the translated label of a type key (`"url_schema":
"スキーマ"`, `"User": "Użytkownik"`) comes out masked. Separating that from `"database": "Продажи_хх"`
of a Russian client would require letting a value in a non-Latin script through, which is the leak that
non-Latin script support came to close; the over-masking stays.

Rejected rules, so they do not come back (each one has a case in `casos_publicos_test.go`):

| Rule | Why it leaks |
|---|---|
| Let the value through when the keys of the JSON are phrases (the shape of a translation catalog) | A data JSON exported from a spreadsheet has the same shape (`"Nome do Cliente": "..."`, `"Usuario": "..."`): the person's name, the user and the server went out |
| The tool's header below any command in sight | Below `$ cat apolices.csv` the header is the owner's columns |
| A dependency folder is always public (`site-packages`, `pkg/mod`, `.m2`) | The client's internal package and private module live in the same folders |
| `internal/` or `lib/python...` in the path as the standard library | They are ordinary Go and Python project folders |
| The whole `io.` or `dev.` group as public outside the package reader | A company with a `.io` domain writes `io.<company>.<system>` |
| An English article (`the`, `an`, `this`) cuts the SQL statement | They are common table aliases (`FROM anuncios an WHERE an.x`) |
| A keyword glued to the parenthesis is never SQL | `SELECT(col) FROM tabela` is SQL; only the chained call (`select(x).where(...)`) is not |
| A line `a.b.c = ...` is always code | In a `SET` list it is a qualified column (`UPDATE t SET\n  db.s.t.col = 1`) |

### Limits

- The memory lasts as long as the history: if the inventory left the context (compaction,
  new conversation), the common word goes back to being just a word until the next inventory.
- What was sent before the inventory stays as it went out (we do not rewrite the past).
- A common word that never appears in an inventory, an echo or a type key is not known: the name
  that only appears in prose, without structure, stays readable.
- The public reference depends on the public material installed on the machine that generated it. In the
  current generation, `kube-system` (no Kubernetes manifest in the corpus) and `dbo` (2
  projects) did not enter; both were already public for the readers (`vocabDev`), but they are not
  marked as generic in the decisions of the new rules.
- A generic word decided in an inventory is masked only there; in the other occurrences of the
  conversation it stays readable, on purpose.
- The mask protects identifiers, not the logic: see [Policy](policy.md).
