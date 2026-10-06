# Estruturas: onde ficam nomes de objetos em cada formato

Este documento é a base dos **leitores de estrutura** do llm-dlp: as regras que reconhecem, pela
gramática de cada formato, onde há um nome de servidor, banco, schema, tabela, coluna,
procedure, usuário, namespace, bucket ou fila, e o que nunca deve ser mascarado (palavras
reservadas, funções e tipos nativos, chaves padronizadas).

Princípios:

- **A decisão vem da estrutura, não do nome.** Nenhum dicionário de idioma, nenhuma lista
  por cliente. As únicas listas são técnicas, pequenas e públicas, tiradas da documentação
  oficial de cada formato (a fonte está em cada seção).
- **Posição estrutural mascara e ensina.** O nome visto numa posição estrutural passa a ser
  mascarado também em texto solto, mas só se tiver cara de identificador (`_`, dígito,
  ponto, hífen entre partes ou mistura de caixa). Palavra simples fica só na posição.
- **O conteúdo continua válido.** O pseudônimo é tipado e consistente e respeita as regras de
  nome do formato (aspas, colchetes, caixa, DNS-1123, minúsculas do Elasticsearch).

Cada seção traz, nesta ordem: sinais de detecção, tabela "posição → entidade", vocabulário
público, regras de identificador, exemplos antes/depois (dados fictícios), casos difíceis e
limites, e links.

**Situação:** pesquisa; nada disto está implementado ainda. Os pontos da 1ª fase foram
conferidos (ver [Conferência](#conferência-dos-pontos-verificar-da-1ª-fase)). Os que restam marcados `[VERIFICAR]` são das fases seguintes e não
foram confirmados na página oficial e precisam ser conferidos antes de virar regra. Também
precisam de conferência, embora não estejam marcados: na seção de chave-valor, as listas de
JSON Schema, OpenAPI, Protobuf, GraphQL, TOML, YAML e XML; na seção de infraestrutura como
código, as regras de nome do identificador de instância RDS, do bucket S3 e de hostname DNS.

## Formato do pseudônimo

Um formato só, para todas as famílias:

```
<PREFIXO>_<ID>        ex.: T_x7k2m9qa   (tabela tb_pedido_x9)
```

- **ID:** 8 caracteres base32 minúsculos, o mesmo tamanho dos outros pseudônimos do llm-dlp,
  tirados de `HMAC-SHA256(chave do llm-dlp, "objeto" ‖ tipo ‖ nome normalizado)`
  (`internal/mask/chave.go`).
- **Estável:** o mesmo nome do mesmo tipo dá o mesmo pseudônimo em qualquer conversa e depois
  de reiniciar (só depende da chave). Nunca é "por sessão" e nunca é um contador.
- **Caixa:** nos tipos de SQL (banco, schema, tabela, coluna, procedure, índice), identificadores
  sem aspas são insensíveis à caixa, então `TB_PEDIDO` e `tb_pedido` são o mesmo objeto. Nos
  outros tipos (pasta, bucket, fila, serviço...) a grafia vale como está: `/dados/Relatorios` e
  `/dados/relatorios` são duas pastas, e cada uma volta com a própria grafia.
- **Prefixo:** o do tipo de entidade (tabela abaixo); em minúsculas quando o nome é todo em
  minúsculas (`t_…`). Onde o formato exige minúsculas e hífen (DNS-1123, bucket S3), vale a mesma
  ideia.
- **Volta:** o desmascaramento aceita o pseudônimo em qualquer caixa (`T_abc…`, `t_abc…`,
  `T_ABC…`) e devolve o nome real.
- **Só o que nós geramos é pulado:** um nome do texto com a forma de um pseudônimo
  (`t_customer`) é mascarado como qualquer outro; só é deixado como está o pseudônimo que o
  próprio llm-dlp gerou.

### Tipos de entidade

| Tipo (chave no `config.json`) | Prefixo | Inclui, por família |
|---|---|---|
| `servidor` | `HOST_` | servidor, host, instância, cluster, conta do Snowflake, `platform_instance` do DataHub |
| `database` | `DB_` | banco, catálogo, projeto do BigQuery/GCP |
| `schema` | `SCH_` | schema, dataset do BigQuery, keyspace do Cassandra, namespace do Avro, pacote do Protobuf |
| `tabela` | `T_` | tabela, view, coleção do MongoDB, índice do Elasticsearch, prefixo de chave do Redis, tabela do DynamoDB, padrão de tabela em allow/deny |
| `coluna` | `C_` | coluna, campo (MongoDB, Elasticsearch, Avro, Protobuf, JSON Schema), alias de coluna |
| `procedure` | `PROC_` | procedure, função, trigger, package |
| `indice` | `IDX_` | índice e constraint do SQL |
| `usuario` | `USR_` | usuário, login, role, service account |
| `namespace` | `NS_` | namespace do Kubernetes |
| `servico` | `SVC_` | serviço, deployment, imagem privada, DAG/job/task, conexão (`conn_id`), warehouse do Snowflake |
| `bucket` | `BKT_` | bucket, contêiner, caminho de armazenamento |
| `fila` | `TOP_` | fila, tópico, stream |

## Ligar e desligar por tipo

São **duas chaves diferentes**, por tipo de entidade:

- `mascarar`: o nome desse tipo é trocado por pseudônimo **onde foi reconhecido** (na posição
  estrutural). Padrão: todos os tipos ligados.
- `propagar`: o nome desse tipo, depois de aprendido, é trocado também **em qualquer outro
  lugar** (texto solto, outro arquivo). Padrão: ligado para servidor, database, schema, tabela,
  procedure, usuário, namespace, serviço, bucket e fila; **desligado para coluna**, porque nomes
  como `user_id` e `created_at` existem em todo código.

O que não estiver listado fica no padrão. Exemplo para manter as colunas legíveis (nem mascarar
nem propagar) e esconder o resto:

```json
"objetos": {
  "ligado": true,
  "mascarar": { "coluna": false },
  "propagar": { }
}
```

Onde cada chave é aplicada:

| Etapa | `mascarar` | `propagar` |
|---|---|---|
| Decisão de cada trecho devolvido pelos leitores, antes da troca | trecho de tipo desligado é descartado e o texto fica como está | — |
| Aprendizado (memória e `vistos.json`) | tipo desligado não é guardado | tipo desligado é guardado só para a posição estrutural, sem marca de propagação |
| Busca de nomes aprendidos em texto novo | — | só os nomes de tipo ligado, e só os que passaram nos freios (abaixo) |
| Registro do que já saiu (`enviados.log`) | mudar a chave recomeça o registro (o cache é regravado uma vez) | idem |

Limite: se um mesmo nome é tabela (ligada) num lugar e coluna (desligada) em outro, ele fica
legível onde aparece como coluna, e isso revela o nome da tabela.

### Freios da propagação

Um nome aprendido só é propagado se tiver **cara de identificador** (`_`, dígito, ponto, hífen
entre partes ou mistura de caixa), se o seu tipo estiver em `propagar` e se a evidência for
**forte**:

- posição inequívoca: o nome de objeto depois de `CREATE`/`ALTER`/`FROM`/`JOIN`/`INTO`/`UPDATE`
  numa instrução reconhecida como SQL por pelo menos 2 sinais (a forma mínima da gramática e o
  tipo do bloco, ou mais de uma cláusula, ou uma cláusula só com forma inequívoca, em maiúsculas
  ou minúsculas: `SELECT * FROM fin.tb_x`, `update tb_x set ...`); valor de coluna de catálogo (`table_name`,
  `TABSCHEMA`, `Key_name`…); chave de conexão (`Server=`, `host:`, `-S`, `jdbc:…//host`); URN;
- ou o mesmo nome visto em **2 regras estruturais diferentes**.

Leitura duvidosa (a regra de origem, um cabeçalho de tabela qualquer) mascara no lugar, mas não
ensina.

### Validade

um nome não visto há **90 dias** deixa de ser propagado (continua mascarado na
  posição estrutural). O `vistos.json` guarda, junto do hash, o dia da última vez que o nome
  foi visto (2 bytes a mais por nome).

## Conferência dos pontos [VERIFICAR] da 1ª fase

Conferidos na página oficial antes de virarem regra. Legenda nos capítulos: **[DOC ✓]** =
confirmado; **[não confirmado → regra tolerante]** = a documentação não traz o texto literal,
então a regra não depende dele; **[fase 2]** = saiu da 1ª fase.

| Ponto | Resultado | Regra na 1ª fase | Fonte |
|---|---|---|---|
| MySQL 1146 / 1054 / 1049 / 1045 | ✓ `Table '%s.%s' doesn't exist`, `Unknown column '%s' in '%s'`, `Unknown database '%s'`, `Access denied for user '%s'@'%s' …` | nome entre aspas simples, na posição do `%s` | [MySQL error reference](https://dev.mysql.com/doc/mysql-errors/8.0/en/server-error-reference.html) |
| SQL Server 208 | ✓ texto `Invalid object name '%.*ls'.`; ✗ a linha `Msg 208, Level 16, State 1, Server X, Line 1` não está na página | nome entre aspas; servidor após `Server ` só pela forma da linha, sem depender do texto exato | [MSSQLSERVER_208](https://learn.microsoft.com/en-us/sql/relational-databases/errors-events/mssqlserver-208-database-engine-error) |
| PostgreSQL 42P01 | ✓ `relation "x" does not exist` (exemplo do ECPG); ✗ 42703, 3F000, 3D000, 42883 sem texto na doc | nome entre aspas duplas após `relation`/`column`/`schema`/`database`; tolerante ao resto | [ECPG Error Handling](https://www.postgresql.org/docs/current/ecpg-errors.html) |
| Snowflake 002003 | ✓ `Object 'DB.SC.MYTABLE' does not exist or not authorized.` | nome qualificado entre aspas simples | [BCR-1858](https://docs.snowflake.com/en/release-notes/bcr-bundles/un-bundled/bcr-1858), [Error codes](https://docs.snowflake.com/en/user-guide/dynamic-tables/error-codes) |
| BigQuery notFound | ✓ `Not Found: Dataset myproject:foo`; ✗ forma da tabela | após `Not found:`/`Not Found:` + `Dataset`/`Table`, sem diferença de caixa; `proj:ds.tb` | [Error messages](https://docs.cloud.google.com/bigquery/docs/error-messages) |
| psql: rodapé, formato, quebra | ✓ `(n rows)`, `aligned` padrão, `.`/`+` (ascii) e `…` (unicode) na margem do `wrapped`; ✗ título `List of relations` | tabela pelo par cabeçalho + linha de traços, não pelo título | [psql](https://www.postgresql.org/docs/current/app-psql.html) |
| sqlcmd | ✓ `-s` separador, `-W` tira espaços, `-h` cabeçalhos, mensagem `<x> rows affected` | idem psql | [sqlcmd](https://learn.microsoft.com/en-us/sql/tools/sqlcmd/sqlcmd-utility) |
| SQL*Plus | ✓ `N rows selected.` (controlado por `SET FEEDBACK`); ✗ `no rows selected` | idem psql | [SQL*Plus Basics](https://docs.oracle.com/en/database/oracle/oracle-database/26/sqpug/SQL-Plus-basics.html) |
| snowsql | ✓ `N Row(s) produced. Time Elapsed: X.XXXs` | idem psql | [Using SnowSQL](https://docs.snowflake.com/en/user-guide/snowsql-use) |
| Db2 CLP | ✓ `N record(s) selected.` (com ponto no LUW) | idem psql | [Db2 CLP tutorial](https://www.ibm.com/docs/en/SSEPEK_13.0.0/comref/src/tpc/db2z_tut_clp.html) |
| mysql cliente | ✓ `1 row in set (0.09 sec)`, `\G` com `*** 1. row ***`; ✗ `Empty set` | moldura `+---+` e modo vertical `coluna: valor` | [mysql tips](https://dev.mysql.com/doc/refman/8.4/en/mysql-tips.html) |
| Colunas que indicam objeto | ✓ Oracle `OWNER`, `TABLE_NAME`; Db2 `TABSCHEMA`, `TABNAME`; MySQL `Key_name`, `Column_name` (SHOW INDEX), `table`, `possible_keys`, `key` (EXPLAIN) | entram na lista de cabeçalhos → entidade | [ALL_TABLES](https://docs.oracle.com/en/database/oracle/oracle-database/19/refrn/ALL_TABLES.html), [SYSCAT.TABLES](https://www.ibm.com/docs/en/db2-warehouse?topic=views-syscattables), [SHOW INDEX](https://dev.mysql.com/doc/refman/8.4/en/show-index.html), [EXPLAIN](https://dev.mysql.com/doc/refman/8.4/en/explain-output.html) |
| `bq ls` / `bq show`, `snow sql` | ✗ sem exemplo na doc oficial | tabela pelo cabeçalho + linha de traços; `tableId` na lista de cabeçalhos | — |
| Numeração do Read | ✗ sem doc; **conferido nas sessões**: `N<tab>` em 605 de 640 resultados, nenhum com `N→` | `^\s*\d+\t` (aceita `→` também) | contagem local |
| Linha de log do PostgreSQL (`LOG:  statement:`) | ✗ a página descreve, mas não mostra a linha | o leitor de SQL roda em cada linha de log; não depende do prefixo | [runtime-config-logging](https://www.postgresql.org/docs/current/runtime-config-logging.html) |
| diff: `\ No newline at end of file`; git `diff.mnemonicPrefix`, `core.quotePath` | ✗ páginas com erro 429 ou cortadas | linha de hunk começando com `\ ` é ignorada; prefixos `^[a-z0-9]/`; caminho entre aspas com escape C | [git-config](https://git-scm.com/docs/git-config) |
| ripgrep: linha longa omitida | ✗ o GUIDE não mostra o texto | linha sem `arquivo:linha:` dentro de saída do rg é ignorada | [ripgrep GUIDE](https://github.com/BurntSushi/ripgrep/blob/master/GUIDE.md) |
| SemVer (excluir versões da propagação) | ✓ regex oficial | a regex oficial sem `^`/`$` | [semver.org](https://semver.org/) |
| JDBC do BigQuery (`ProjectId`, `DefaultDataset`) | ✗ a doc da Google remete ao PDF do fornecedor | chave=valor com chaves de nome (`ProjectId` → DB, `DefaultDataset` → SCH) | [ODBC/JDBC drivers](https://docs.cloud.google.com/bigquery/docs/reference/odbc-jdbc-drivers) |
| Planos de execução, cabeçalhos traduzidos do psql, campos semiestruturados | — | movidos para a 2ª fase | — |

## Sumário

- [Formato do pseudônimo](#formato-do-pseudônimo) · [Ligar e desligar por tipo](#ligar-e-desligar-por-tipo) · [Conferência da 1ª fase](#conferência-dos-pontos-verificar-da-1ª-fase)

1. [SQL e DDL](#sql-e-ddl)
2. [Saídas de clientes de banco](#saídas-de-clientes-de-banco)
3. [Strings de conexão e endereços](#strings-de-conexão-e-endereços)
4. [Dados tabulares](#dados-tabulares)
5. [JSON, YAML, TOML, INI, .env, .properties, XML e linguagens de esquema](#json-yaml-toml-ini-env-properties-xml-e-linguagens-de-esquema)
6. [Kubernetes e contêineres](#kubernetes-e-contêineres)
7. [Infraestrutura como código](#infraestrutura-como-código)
8. [Ferramentas de dados](#ferramentas-de-dados)
9. [Código](#código)
10. [NoSQL e busca](#nosql-e-busca)
11. [Formatos de transporte](#formatos-de-transporte-diff-grep-numeração-logs-markdown-heredoc)
    - [Saídas soltas de script e de shell](#saídas-soltas-de-script-e-de-shell)
12. [Reserva genérica](#reserva-genérica)


## SQL e DDL

Escopo: ANSI/ISO, T-SQL, Oracle (SQL e PL/SQL), PostgreSQL, MySQL/MariaDB, Db2, Snowflake, BigQuery (GoogleSQL), Spark SQL/Databricks.
Princípio: um token vira nome de objeto **pela posição na gramática**, nunca por parecer palavra de idioma. As listas públicas só servem para dizer o que **não** é nome.

### 1. Sinais de detecção

**Pela chamada que gerou o texto** (mais forte que o conteúdo):
- Ferramenta/comando: `sqlcmd`, `bcp`, `Invoke-Sqlcmd` (T-SQL); `sqlplus`, `sqlcl` (Oracle); `psql`, `pg_dump` (PG); `mysql`, `mysqldump`, `mariadb` (MySQL); `db2`, `db2look` (Db2); `snowsql`, `snow sql`, ferramenta MCP tipo `*_execute` de Snowflake (Snowflake); `bq query`, `bq show`, API `jobs.query` (BigQuery); `spark-sql`, `spark.sql("...")`, `databricks` CLI (Spark).
- Arquivo `.sql`, `.ddl`, `.pls/.pkb/.pks` (Oracle), `.prc`; argumento `-q`/`-e`/`-c`/`--query` de um desses clientes.
- Tipo de resultado: cabeçalho com `TABLE_SCHEMA | TABLE_NAME`, saída de `SHOW TABLES`, `DESCRIBE`, `\d`.

**Pelo conteúdo** (é SQL se ≥2 sinais, no início de linha/instrução, fora de prosa):
- Início de instrução: `SELECT|WITH|INSERT|UPDATE|DELETE|MERGE|CREATE|ALTER|DROP|TRUNCATE|GRANT|REVOKE|USE|CALL|EXEC|BEGIN|DECLARE`.
- Estrutura: `SELECT … FROM`, `INSERT INTO … (… ) VALUES`, `CREATE TABLE x (col tipo, …)`, terminadores `;`, `GO` (linha sozinha), `/` (linha sozinha, Oracle).

**Qual dialeto** (pontue; o maior vence; empate → regras ANSI):

| Dialeto | Marcadores típicos |
|---|---|
| T-SQL | `[x]`, `GO`, `TOP n`, `@var`, `@@ROWCOUNT`, `N'..'`, `#tmp`, `NOLOCK`, `sp_executesql`, `IDENTITY(1,1)`, `nvarchar(max)` |
| Oracle | `DUAL`, `ROWNUM`, `NVL`, `VARCHAR2`, `NUMBER(p,s)`, `:=`, `t@dblink`, `CONNECT BY`, `CREATE OR REPLACE PACKAGE [BODY]`, `EXECUTE IMMEDIATE` |
| PostgreSQL | `::tipo`, `$$ … $$`, `RETURNING`, `ILIKE`, `SERIAL`, `LANGUAGE plpgsql`, `pg_catalog`, `\d` |
| MySQL/MariaDB | `` `x` ``, `ENGINE=InnoDB`, `AUTO_INCREMENT`, `LIMIT a,b`, `# comentário`, `DELIMITER //`, `ON DUPLICATE KEY` |
| Db2 | `FETCH FIRST n ROWS ONLY` + `SYSIBM.SYSDUMMY1`, `WITH UR`, `SYSCAT.`, `GENERATED ALWAYS AS IDENTITY` |
| Snowflake | `QUALIFY`, `VARIANT`, `FLATTEN`, `@stage`, `CREATE WAREHOUSE/STAGE/PIPE/TASK/STREAM`, `COPY INTO`, `col:campo::tipo`, `IDENTIFIER('..')` |
| BigQuery | `` `proj-x.ds.t` ``, `STRUCT<>`/`ARRAY<>`, `SAFE_CAST`, `` `region-us`.INFORMATION_SCHEMA ``, `_TABLE_SUFFIX`, `OPTIONS(...)` |
| Spark/Databricks | `USING DELTA`, `OPTIMIZE … ZORDER BY`, `LOCATION 'dbfs:/…'`, `TBLPROPERTIES`, `catalogo.schema.t` com backtick, `CACHE TABLE` |

### 2. Posição → tipo de entidade

`X` = nome (simples ou qualificado). Em nome qualificado, as partes à esquerda recebem o tipo da hierarquia do dialeto (ver seção 4).

| Posição gramatical | Tipo |
|---|---|
| `FROM X`, `JOIN X`, `INTO X`, `UPDATE X`, `MERGE INTO X`, `USING X`, `TRUNCATE [TABLE] X`, `DELETE FROM X` | tabela/view (T_) |
| `CREATE/ALTER/DROP [OR REPLACE] [TEMP] TABLE|VIEW|MATERIALIZED VIEW X` | tabela / view (T_ / V_) |
| `CREATE/ALTER/DROP DATABASE|CATALOG X`, `USE [DATABASE] X` | banco/catálogo (DB_) |
| `CREATE/ALTER/DROP SCHEMA X`, `USE SCHEMA X`, `SET search_path TO X`, `ALTER SESSION SET CURRENT_SCHEMA = X` | schema (SCH_) |
| `CREATE PROCEDURE|PROC X`, `EXEC[UTE] X`, `CALL X` | procedure (PROC_) |
| `CREATE FUNCTION X`, `X(` qualificado (`sch.X(`) ou não nativo | função (FN_) |
| `CREATE TRIGGER X … ON Y` | trigger (TRG_), Y tabela |
| `CREATE [UNIQUE] INDEX X ON Y (c1, c2)` | índice (IX_), Y tabela, c colunas |
| `CONSTRAINT X PRIMARY KEY|FOREIGN KEY|CHECK|UNIQUE` | constraint (CK_) |
| `REFERENCES Y (c)` | tabela + coluna |
| `CREATE [PUBLIC] SYNONYM X FOR Y`, `CREATE ALIAS X FOR Y` (Db2) | sinônimo (SYN_) + alvo |
| `CREATE SEQUENCE X`, `X.NEXTVAL`, `NEXT VALUE FOR X`, `nextval('X')` | sequence (SEQ_) |
| `CREATE USER|ROLE|LOGIN X`, `GRANT … TO X`, `REVOKE … FROM X`, `AUTHORIZATION X`, `OWNER TO X`, `EXECUTE AS USER = 'X'` | usuário/role (U_ / R_) |
| `GRANT priv ON [tipo] Y TO X` | Y objeto do tipo dito; X role |
| `CREATE WAREHOUSE X`, `USE WAREHOUSE X`, `WAREHOUSE = X` | warehouse (WH_) |
| `CREATE STAGE X`, `@X`, `@sch.X/caminho` | stage (STG_) |
| `CREATE TASK|PIPE|STREAM X`, `AFTER X`, `ON TABLE Y` (stream) | task/pipe/stream (TSK_/PIPE_/STR_) |
| `CREATE SCHEMA X` em BigQuery, `proj.X.t` | dataset (DS_) |
| `CREATE DATABASE LINK X`, `t@X` | dblink / servidor (SRV_) |
| `srv.db.sch.t` (T-SQL 4 partes), `OPENQUERY(X, …)`, `sp_addlinkedserver 'X'` | servidor (SRV_) |
| `SELECT a, b`, `INSERT INTO t (a, b)`, `CREATE TABLE t (a tipo, …)`, `ALTER TABLE t ADD|DROP|RENAME COLUMN a`, `WHERE a =`, `GROUP BY a`, `ORDER BY a`, `ON t1.a = t2.b`, `SET a = ` | coluna (C_) |
| `AS x` depois de tabela/subconsulta; `t x` (alias implícito) | alias de tabela (ver 6) |
| `AS x` depois de expressão no SELECT | alias de coluna (C_) |
| `WITH x AS (` , `, x AS (` | CTE (CTE_) |
| Literal comparado a `table_name`, `table_schema`, `column_name`, `OBJECT_ID('..')`, `IDENTIFIER('..')`, `'..'::regclass` | objeto dentro de string (tipo pela coluna/função) |

### 3. Vocabulário público (NUNCA mascarar)

Regra: um token que bate com a lista **e** está em posição de palavra-chave, tipo ou chamada de função nativa fica. Em posição de nome (ex.: depois de `.`), a lista não protege.

| Lista | Fonte oficial | Como extrair | Tamanho aprox. |
|---|---|---|---|
| ANSI/ISO reservadas | ISO 9075 é pago; usar a coluna SQL:2023 do Apêndice C do PostgreSQL e a lista ODBC/ISO da Microsoft | raspar tabela C.1, filtrar coluna SQL:2023 = reserved | ~340 |
| T-SQL reservadas + ODBC + futuras | learn.microsoft.com, Reserved Keywords | copiar as 3 tabelas da página (ou o .md no repositório público MicrosoftDocs/sql-docs) | ~185 + ~235 + ~250 |
| T-SQL tipos/funções | `sys.types` (~34); página de funções do T-SQL | `SELECT name FROM sys.types WHERE is_user_defined=0` | ~34 tipos, ~300 funções |
| Oracle | SQL Reserved Words + PL/SQL Reserved Words and Keywords | `SELECT keyword, reserved FROM V$RESERVED_WORDS` (ficar só com reserved='Y' para a regra dura) | ~100 reservadas; view ~2.400 |
| PostgreSQL | Apêndice C | `SELECT word, catcode FROM pg_get_keywords()`; funções/tipos: `pg_proc`/`pg_type` com `pronamespace='pg_catalog'::regnamespace` | ~500 palavras; ~3.000 funções |
| MySQL 8.4 | Keywords and Reserved Words; Built-In Function Reference | `SELECT WORD, RESERVED FROM INFORMATION_SCHEMA.KEYWORDS` | ~730 (≈260 reservadas); ~450 funções |
| Db2 11.5 | Reserved schema names and reserved words | copiar as duas listas da página | ~407 + ~118 (SQL2003) |
| Snowflake | Reserved & limited keywords; All functions (alphabetical); Data types | página (92 linhas) + `SHOW FUNCTIONS` filtrando `is_builtin='Y'` | 92; ~1.000 funções |
| BigQuery | Lexical structure (reserved keywords); All functions | tabela da página Lexical; índice de funções | ~95; ~600 funções |
| Spark | ANSI Compliance (tabela de keywords); Built-in Functions | tabela com colunas Spark-ANSI/Default/SQL-2016; `SHOW SYSTEM FUNCTIONS` | ~400 keywords; ~500 funções |

**Schemas/catálogos de sistema (não mascarar):** `INFORMATION_SCHEMA` (todos); `sys`, `guest`, `db_owner`… (roles fixas), bancos `master`, `msdb`, `tempdb`, `model` (T-SQL); `SYS`, `SYSTEM`, `PUBLIC`, `DUAL`, prefixos `DBA_`/`ALL_`/`USER_`/`V$`/`GV$` (Oracle); `pg_catalog`, `pg_toast`, `pg_temp_*`, `public` (PG); `mysql`, `performance_schema`, `sys` (MySQL); `SYSCAT`, `SYSIBM`, `SYSIBMADM`, `SYSFUN`, `SYSPROC`, `SYSPUBLIC`, `SYSSTAT`, `SYSTOOLS`, `SESSION` (Db2); `SNOWFLAKE`, `SNOWFLAKE_SAMPLE_DATA`, `ACCOUNT_USAGE`, `PUBLIC`, roles `ACCOUNTADMIN`/`SYSADMIN`/`SECURITYADMIN`/`USERADMIN`/`ORGADMIN`/`PUBLIC` (Snowflake); `` `region-xx` `` (BigQuery); `system`, `builtin`, `session`, `default`, `spark_catalog`, `hive_metastore` (Spark/Databricks).

**Decisão sobre `dbo` (e `public`, `default`): não mascarar.** É o schema que o próprio produto cria em todo banco, idêntico em qualquer instalação: não identifica ninguém e trocá-lo só tira do modelo a pista de que é o schema padrão. A regra é "nome que o produto fixa em todas as instalações fica". Já um schema criado pelo usuário (no Oracle, o schema **é** o usuário) mascara sempre.

### 4. Regras de identificador por dialeto

| Dialeto | Citação | Escape dentro | Sem aspas | Com aspas | Qualificação (máx.) |
|---|---|---|---|---|---|
| ANSI | `"x"` | `""` | dobra p/ MAIÚSCULA | exato | `catálogo.schema.objeto` |
| T-SQL | `[x]`; `"x"` se `QUOTED_IDENTIFIER ON` | `]]` / `""` | depende da collation (em geral não diferencia) | idem collation | `servidor.banco.schema.objeto`; partes vazias `srv..t`, `db..t`; `#t`/`##t` temp; 128 chars |
| Oracle | `"x"` | não há (aspas proibidas) | MAIÚSCULA; `_ $ #` | exato | `schema.objeto[.parte]@dblink`; 128 bytes/parte; nomes de banco e dblink sempre maiúsculos |
| PostgreSQL | `"x"`, `U&"x"` | `""` | minúscula | exato | `banco.schema.objeto` (banco só pode ser o atual); 63 bytes |
| MySQL/MariaDB | `` `x` ``; `"x"` com `ANSI_QUOTES` | ` `` ` | colunas não diferenciam; tabelas/bancos conforme `lower_case_table_names` / SO | idem | `banco.tabela.coluna` (sem schema: banco = schema); palavra após `.` não precisa de citação; 64 chars |
| Db2 | `"x"` | `""` | MAIÚSCULA | exato | `local.schema.objeto`; 128 bytes |
| Snowflake | `"x"` | `""` | MAIÚSCULA; `$` permitido | exato (salvo `QUOTED_IDENTIFIERS_IGNORE_CASE`) | `banco.schema.objeto`; cada parte citada separada; `IDENTIFIER('…')`; 255 chars |
| BigQuery | `` `x` `` (pode cobrir o caminho todo: `` `p.d.t` ``) | escapes de string (`\``) | colunas não diferenciam; dataset/tabela **diferenciam** (salvo `is_case_insensitive`) | idem | `projeto.dataset.tabela`; hífen só na 1ª parte em FROM; `ds.prefixo_*` |
| Spark/Databricks | `` `x` `` | ` `` ` | não diferencia (citado também não) | idem | `catálogo.schema.tabela` |

Consequência para o mascarador: a chave do mapa é o nome **normalizado** conforme o dialeto (dobrar caixa onde o dialeto dobra; manter exato onde é citado e sensível). Assim `tb_pedido_x9`, `TB_PEDIDO_X9` e `"TB_PEDIDO_X9"` (Snowflake/Oracle/Db2) recebem o mesmo pseudônimo; `"Tb_Pedido"` citado recebe outro.

### 5. Exemplos antes/depois

Pseudônimo: no formato único do llm-dlp (ver [Formato do pseudônimo](#formato-do-pseudônimo)): prefixo do tipo + 8 caracteres derivados por HMAC da chave. É sempre um identificador válido sem aspas em todos os dialetos. A saída copia a **forma da caixa** do original (tudo maiúsculo → `T_A8F1`) e mantém aspas/colchetes/backticks. Na volta, a busca ignora caixa nos dialetos que dobram.

T-SQL:
```sql
-- antes
SELECT p.[Valor Total], c.nome FROM [srv-exemplo-01].vendas_demo.financeiro.tb_pedido_x9 AS p
JOIN dbo.tb_cliente c ON c.id = p.id_cliente; EXEC financeiro.usp_fecha_mes @ano = 2024;
-- depois
SELECT p.[C_xsdgeq7l], c.C_rr476ahy FROM [HOST_zvfrq7zb].DB_2vmfnowr.SCH_mvrhafae.T_cszwa3ri AS p
JOIN dbo.T_3ms3mjt3 c ON c.C_x6w4tokb = p.C_c5w3hetu; EXEC SCH_mvrhafae.PROC_zx55ztp5 @ano = 2024;
```
(`dbo`, `SELECT`, `JOIN`, `EXEC` ficam; `@ano` é parâmetro local: fica, ver 6.)

Snowflake:
```sql
-- antes
CREATE OR REPLACE TASK VENDAS_DEMO.FINANCEIRO.TSK_CARGA WAREHOUSE = WH_ETL_DEMO
AS COPY INTO "Tb_Pedido" FROM @FINANCEIRO.STG_ENTRADA/2024/ ;
-- depois
CREATE OR REPLACE TASK DB_X7K2.SCH_Q3M9.TSK_W2C4 WAREHOUSE = WH_N8L0
AS COPY INTO "T_qwea6mav" FROM @SCH_Q3M9.STG_F1Y6/2024/ ;
```
(`"Tb_Pedido"` citado e de caixa mista é outro objeto que `TB_PEDIDO` e recebe pseudônimo próprio. O caminho `/2024/` dentro do stage fica com o detector de caminhos.)

BigQuery:
```sql
-- antes
SELECT id_pedido FROM `projeto-exemplo-01.vendas_demo.tb_pedido_x9`
WHERE _TABLE_SUFFIX > '2024' AND status = 'ok';
-- depois
SELECT C_yxptlwin FROM `DB_2t2xlad4.SCH_iardtg5m.T_cszwa3ri`
WHERE _TABLE_SUFFIX > '2024' AND C_hpawygue = 'ok';
```

PostgreSQL / Oracle:
```sql
-- antes
CREATE INDEX ix_pedido_data ON financeiro.tb_pedido_x9 (dt_emissao);
GRANT SELECT ON financeiro.vw_resumo TO analista_ro;
SELECT * FROM financeiro.tb_pedido_x9@lk_srv_exemplo;
-- depois
CREATE INDEX IX_c8r1 ON SCH_mvrhafae.T_cszwa3ri (C_zh6xlzrr);
GRANT SELECT ON SCH_mvrhafae.T_me3bezjy TO USR_f3kpe7dg;
SELECT * FROM SCH_mvrhafae.T_cszwa3ri@HOST_z2l74vie;
```

### 6. Casos difíceis e limites

- **SQL dinâmico em string:** `EXEC('…')`, `sp_executesql N'…'`, `EXECUTE IMMEDIATE '…'` (Oracle, Snowflake, BigQuery, Db2), `$$ … $$` em corpo de procedure (PG/Snowflake), `spark.sql("…")`. Se a string aparece no argumento dessas construções, reanalisar o conteúdo como SQL do mesmo dialeto. Concatenação (`'SELECT * FROM ' + @tab`) fica só parcialmente coberta: os pedaços literais são mascarados e a variável não é resolvida.
- **Nome dentro de literal:** `OBJECT_ID('dbo.tb_x')`, `IDENTIFIER('db.sch.t')`, `'sch.t'::regclass`, `nextval('seq')`, `WHERE table_name = 'tb_x'` sobre `INFORMATION_SCHEMA`. Mascarar o conteúdo do literal pelo tipo implícito e manter as aspas.
- **Comentários** (`--`, `/* */`, `#` em MySQL/BigQuery, `REM` em SQL*Plus): não há gramática dentro deles. Só substituir nomes **já no mapa** da sessão, comparando por palavra inteira. Nunca inferir nome novo a partir de comentário.
- **Alias de tabela:** é local à consulta, mas `AS clientes_inadimplentes` revela significado. Regra: alias de até 3 caracteres (`p`, `t1`) fica; alias maior é mascarado (prefixo `A_`). Alias de coluna sempre vira `C_`, com o mesmo pseudônimo se coincidir com coluna real.
- **CTE:** `WITH base_vendas AS (…)` vira `CTE_`, e cada referência posterior em `FROM base_vendas` usa o mesmo pseudônimo (resolver o escopo antes de tratar como tabela).
- **Palavra não reservada usada como nome** (`status`, `name`, `date`, `comment`, `user`, `INDEX` no PG, `VALUE`): quem decide é a posição. Em lista de colunas, `t.status` ou `SET status =`, é coluna; em posição de tipo (`CREATE TABLE t (dt date)`), `date` é tipo e fica. Reservada só aparece como nome se estiver citada (`"select"`, `[order]`, `` `group` ``) ou depois de `.` (MySQL, BigQuery). Nesses casos mascarar mantendo a citação.
- **Variáveis e parâmetros** (`@x`, `:x`, `$1`, `?`, `v_total` em PL/SQL): não são objetos de banco. Ficam, a não ser que o nome já esteja no mapa.
- **Campos semiestruturados** (`col:cliente.cpf` em Snowflake, `STRUCT.campo` em BigQuery, `col->>'k'` no PG): a 1ª parte é coluna e o resto são chaves de dado. As chaves ficam com o detector de JSON [fase 2].
- **Ambiguidade de qualificação:** `a.b` pode ser `schema.tabela`, `tabela.coluna` ou `alias.coluna`. Resolver pela posição (FROM → objeto; SELECT/WHERE → `qualificador.coluna`) e pelos aliases já declarados na instrução.
- **SQL truncado** (saída cortada, `LIMIT` de log): o lexer tolera string, aspa ou comentário sem fechamento e trata o resto como literal até o fim do bloco. O que não for classificado ainda passa pela substituição por mapa conhecido. Falha segura: na dúvida sobre um token em posição de nome, mascarar.
- **Resultado tabular:** valores sob cabeçalhos `TABLE_NAME`, `TABLE_SCHEMA`, `COLUMN_NAME`, `name` (de `SHOW …`) são nomes de objeto. O nome das colunas no cabeçalho de um resultado também é coluna.
- **Limites:** sem catálogo não dá para separar função de usuário sem qualificação de função nativa desconhecida (`fn_calc(x)` fora da lista nativa vira `FN_`, aceitando falso positivo). Extensões (PostGIS, UDFs de pacote) aumentam a lista de nativos. Quando a regra de caixa do MySQL depende de `lower_case_table_names`, tratar como sensível.

### 7. Links usados

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

Conferidos nesta pesquisa: as páginas de T-SQL reserved, Snowflake reserved e identifiers, PostgreSQL Apêndice C, MySQL keywords, Oracle object names, Db2 reserved, Spark identifiers e BigQuery lexical (esta por busca no domínio oficial). Os demais links e as contagens marcadas "aprox." (funções, `V$RESERVED_WORDS`, `pg_get_keywords`) vêm do conhecimento da documentação e não foram abertos agora `[VERIFICAR]`.


## Saídas de clientes de banco

Objetivo: reconhecer, pela forma do texto, a saída de um cliente de banco e saber em que
**coluna** ou **trecho** está um nome de objeto, para trocar só esse nome por um pseudônimo tipado.
Legenda: **[DOC]** = confirmado na documentação oficial nesta pesquisa; **[VERIFICAR]** = formato
conhecido mas não confirmado na doc (a página não carregou ou não traz o texto literal). Esses itens
precisam de uma amostra real antes de virar regra.

### 1. Sinais de detecção

Regra geral: um sinal sozinho não basta. Exigir **moldura + cabeçalho conhecido**, ou **moldura + rodapé**,
ou **prefixo de erro**. Avaliar o bloco inteiro (linhas consecutivas), não a linha solta.

| Cliente / formato | Moldura | Rodapé / marcador |
|---|---|---|
| psql `aligned` (padrão) [DOC] | cabeçalho, linha `----+----`, colunas separadas por ` \| ` (border 1) | `(N rows)` / `(1 row)`; some com `\pset footer off` ou `-t` |
| psql `border 2` [DOC] | moldura `+----+` em cima e embaixo, `\|` nas bordas | igual |
| psql `\d…` [não confirmado → regra tolerante] | título centralizado `List of relations`, `List of schemas`, `List of databases`, `List of roles`; `\d tb` → `Table "sch.tb"` | `Indexes:`, `Foreign-key constraints:`, `Referenced by:` |
| psql `unaligned` (`-A`) [DOC] | separador padrão `\|`, sem espaços de preenchimento; `-F` muda | `(N rows)` continua, salvo `-t` |
| psql `csv` (`--csv`) [DOC] | RFC 4180, vírgula, cabeçalho na 1ª linha | **sem** título nem rodapé |
| psql `wrapped` [DOC] | igual a aligned, mas valor longo quebra em várias linhas | `(N rows)` |
| sqlcmd [DOC ✓] | cabeçalho, linha de traços `-----` por coluna separadas por espaço (ou pelo `-s`) | `(N rows affected)`; `-h -1` tira cabeçalho, `-W` tira espaços à direita |
| sqlplus [DOC ✓] | cabeçalho, linha `-----` por coluna; cabeçalho se repete a cada `PAGESIZE` | `N rows selected.` / `no rows selected` |
| sqlplus `DESCRIBE` | cabeçalho fixo `Name  Null?  Type` | — |
| snowsql (`output_format=psql` padrão) [DOC] | moldura `+----+` estilo psql border 2 | linha sob a tabela com nº de linhas e tempo (`timing=True`); texto literal `N Row(s) produced. Time Elapsed:` [DOC ✓] |
| snow sql (Snowflake CLI) [não confirmado → regra tolerante] | tabela com moldura (rich/box), ou `--format json` | — |
| bq `--format=pretty` [DOC formatos] | moldura `+---+` | sem rodapé de contagem [não confirmado → regra tolerante] |
| bq `sparse` / `csv` / `json` / `prettyjson` [DOC] | sparse: cabeçalho + traços; csv com cabeçalho; json | — |
| bq `ls` / `show` (padrão) [não confirmado → regra tolerante] | `tableId  Type  Labels  Time Partitioning`; `show`: `Last modified  Schema  Total Rows ...` | — |
| db2 CLP [DOC ✓] | cabeçalho + traços por coluna | `N record(s) selected.` |
| mysql cliente (tabela) [DOC ✓] | moldura `+------+` | `N rows in set (0.00 sec)`, `Empty set`; `Query OK, N rows affected` |
| mysql `\G` [DOC ✓] | `*************************** 1. row ***************************` e linhas `coluna: valor` alinhadas à direita | `N rows in set` |
| Mensagem de erro | prefixo: `ERROR:`, `Msg NNN, Level`, `ORA-NNNNN:`, `ERROR NNNN (SSSSS):`, `SQL compilation error`, `Not found:` / `notFound` | — |

### 2. Posição → tipo de entidade

**Cabeçalhos de coluna** (comparar sem diferenciar caixa; o **valor** da coluna é o nome a mascarar):

| Cabeçalho | Entidade | Origem |
|---|---|---|
| `table_catalog`, `catalog_name`, `routine_catalog`, `database_name` | DB | information_schema [DOC]; Snowflake SHOW [DOC] |
| `table_schema`, `schema_name`, `routine_schema`, `specific_schema`, `Schema`, `OWNER` (Oracle), `TABSCHEMA` (Db2) | SCH | idem; Oracle/Db2 [DOC ✓] |
| `table_name`, `Name` (com `Type` = table/view), `TABLE_NAME`, `TABNAME`, `tableId`, `Tables_in_<db>` | T | idem |
| `view_name` (Oracle `ALL_VIEWS`) [DOC exemplo] | T (view) | ORA-00942 |
| `column_name`, `COLNAME`, `Field` (mysql SHOW COLUMNS), `Name` no DESCRIBE sqlplus | C | [DOC] p/ column_name |
| `routine_name`, `specific_name` | PROC | information_schema [DOC] |
| `domain_name`, `udt_name` quando **não** for tipo nativo | TIPO definido pelo usuário | [DOC] |
| `Key_name`, `key`, `possible_keys` (mysql), `INDEX_NAME` | IDX | [DOC ✓] |
| `owner`, `schema_owner`, `Owner` (psql), `Role name`, `GRANTEE`, `User`, `Host` | USR / ROLE / HOST | Snowflake `owner` [DOC] |

Pegadinhas: (a) em MySQL `SHOW TABLES` o **nome do database está dentro do cabeçalho**
(`Tables_in_vendas_demo`) — mascarar o sufixo e manter `Tables_in_`. (b) `Name` é genérico: em `\dt`
é tabela, em `\dn` é schema, em `\l` é database, em `DESCRIBE` sqlplus é coluna — decidir pelo **título**
ou pelo conjunto de cabeçalhos vizinhos. (c) O valor de `data_type`, `Type`, `kind`, `table_type` **nunca** é nome.

**Trechos de mensagem de erro** (o grupo entre aspas é o nome):

| Mensagem | Onde está o nome | Entidade |
|---|---|---|
| PostgreSQL 42P01 [DOC código] — `relation "X" does not exist` [DOC ✓] | `"X"` (pode vir `sch.tb`) | T |
| PostgreSQL 42703 — `column "X" does not exist`; 3F000 `schema "X" does not exist`; 3D000 `database "X" does not exist`; 42883 `function X(...) does not exist` [não confirmado → regra tolerante] | entre aspas / antes de `(` | C / SCH / DB / PROC |
| SQL Server 208 [DOC] — `Invalid object name '%.*ls'.` | entre aspas simples, 1 a 4 partes | T |
| SQL Server cabeçalho `Msg 208, Level 16, State 1, Server srv-exemplo-01, Line 1` [não confirmado → regra tolerante] | após `Server ` até `,` | SRV |
| Oracle ORA-00942 [DOC] — 26ai: `table or view SCHEMA.OBJECT_NAME does not exist`; 19c/21c: sem nome | entre `view ` e ` does not exist` | SCH.T |
| Oracle ORA-00904 [DOC template] — `identifier: invalid identifier`, na prática `"COL"` ou `"ALIAS"."COL"` | antes de `: invalid identifier` | C |
| MySQL 1146 — `Table 'db.tb' doesn't exist`; 1054 `Unknown column 'c' in 'field list'`; 1049 `Unknown database 'db'` [DOC ✓] | entre aspas simples | DB.T / C / DB |
| MySQL 1045 — `Access denied for user 'u'@'h'` [DOC ✓] | as duas aspas | USR / HOST |
| Snowflake 002003 — `Object 'DB.SCH.TB' does not exist or not authorized.` [DOC ✓] | entre aspas simples | DB.SCH.T |
| BigQuery `notFound` [DOC] — `Not Found: Dataset myproject:foo`; tabela: `Not found: Table proj:ds.tb` [não confirmado → regra tolerante] | após `Dataset `/`Table ` | PROJ:DS(.T) |

**Planos de execução** [fase 2]: PostgreSQL `Seq Scan on tb`, `Index Scan using idx on tb t`
(nome após `on`/`using`, alias depois); SQL Server showplan `OBJECT:([db].[sch].[tb].[idx])`;
MySQL `EXPLAIN` colunas `table`, `key`, `possible_keys`; Oracle plano com coluna `Name` (objeto)
ao lado de `Operation` (`TABLE ACCESS FULL`, vocabulário).

### 3. Vocabulário público (nunca mascarar)

| Conjunto | Exemplos | Fonte e como extrair |
|---|---|---|
| Nomes de coluna do information_schema | `table_catalog`, `table_schema`, `table_name`, `column_name`, `ordinal_position`, `is_nullable`, `data_type`, `routine_name`... | postgresql.org cap. "The Information Schema": raspar os nomes em `<code>` das tabelas de cada view; ou rodar `SELECT table_name, column_name FROM information_schema.columns WHERE table_schema='information_schema'` num banco vazio |
| Colunas de SHOW do Snowflake | `created_on`, `name`, `database_name`, `schema_name`, `kind`, `owner`, `rows`, `bytes`, `retention_time`... | docs.snowflake.com, seção "Output" de cada `SHOW <objeto>` (minúsculas) |
| Palavras de rodapé e título | `rows`, `row`, `rows affected`, `rows selected`, `record(s) selected`, `rows in set`, `Empty set`, `Row(s) produced`, `Time Elapsed`, `List of relations`, `Indexes`, `Referenced by` | texto fixo do cliente; extrair das páginas de cada cliente |
| Tipos de dado | `integer`, `varchar`, `NUMBER`, `VARCHAR2`, `nvarchar`, `TIMESTAMP_NTZ`, `STRING`, `INT64` | páginas "Data types" de cada SGBD |
| Valores de "tipo de objeto" | `table`, `view`, `sequence`, `BASE TABLE`, `VIEW`, `PROCEDURE`, `FUNCTION`, `T`/`V` (Db2) | doc da coluna `table_type`/`kind` |
| Palavras reservadas e operações de plano | `SELECT`, `Seq Scan`, `Hash Join`, `TABLE ACCESS FULL`, `Clustered Index Scan` | listas de palavras-chave oficiais (PostgreSQL apêndice C, T-SQL Reserved Keywords, Oracle V$RESERVED_WORDS, MySQL Keywords) |
| Schemas e bancos de sistema | `information_schema`, `pg_catalog`, `public`, `dbo`, `sys`, `SYSIBM`, `SYSCAT`, `mysql`, `performance_schema`, `INFORMATION_SCHEMA` (BQ/Snowflake), `SNOWFLAKE` | doc de cada SGBD; não identificam o cliente |

Extração: gerar as listas uma vez, com versão e URL, num arquivo de dados do proxy; nunca juntar
dicionário de idioma. Comparação sem caixa.

### 4. Regras de identificador

- **Citação na mensagem**: PostgreSQL usa aspas duplas `"x"`; SQL Server e MySQL, aspas simples `'x'`;
  Oracle, aspas duplas por parte (`"E"."SALARY"`); Snowflake, aspas simples em volta do nome inteiro;
  BigQuery, sem aspas, com `projeto:dataset.tabela` (ou `projeto.dataset.tabela` no SQL). SQL Server em
  plano usa colchetes `[x]`; MySQL em SQL usa crase `` `x` ``.
- **Nome qualificado**: separar por `.` (e `:` no BigQuery) e mascarar **cada parte** com seu tipo
  pela posição: 4 partes SQL Server = SRV.DB.SCH.T; 3 partes Snowflake = DB.SCH.T; 2 partes = SCH.T.
  Partes de sistema (item 3) ficam: `dbo.tb_pedido_x9` → `dbo.T_cszwa3ri`.
- **Caixa**: Oracle e Snowflake guardam sem aspas em MAIÚSCULAS; PostgreSQL em minúsculas; SQL Server
  depende da collation (doc do erro 208: `CS` diferencia caixa) [DOC]. O mapa de pseudônimos deve ser
  **sem caixa** para o mesmo objeto (`TB_PEDIDO_X9` e `tb_pedido_x9` → mesmo `T_cszwa3ri`) e devolver o
  pseudônimo na caixa do original (`T_A8F1` em texto maiúsculo) para não quebrar a leitura.
- **Caracteres válidos** sem aspas: letra inicial, depois alfanumérico, `_`, `$`, `#` (Oracle [DOC]);
  com aspas, qualquer coisa — o delimitador decide o fim, não a classe de caractere.
- **Consistência**: o mesmo nome visto num cabeçalho de tabela, num erro e num plano recebe o mesmo
  pseudônimo na sessão inteira.

### 5. Exemplos antes/depois

Pseudônimos: `DB_2vmfnowr` (vendas_demo), `SCH_mvrhafae` (financeiro), `T_cszwa3ri` (tb_pedido_x9),
`C_xsdgeq7l` (vl_total), `HOST_kdekbkuz` (srv-exemplo-01).

psql `aligned` (`\dt financeiro.*`) — **realinhar é necessário** (larguras mudam):
```
antes                                   depois
         List of relations                       List of relations
   Schema   |     Name     | Type  | Own    Schema  |  Name  | Type  | Own
------------+--------------+-------+----   ----------+--------+-------+----
 financeiro | tb_pedido_x9 | table | ...    SCH_mvrhafae | T_cszwa3ri | table | ...
(1 row)                                   (1 row)
```
Estratégia: parsear as células pela posição dos `+` da linha de traços, trocar, recalcular a largura
de cada coluna (máx. entre cabeçalho e valores) e redesenhar. Alternativa mais simples: preencher o
pseudônimo com espaços até a largura original (só funciona se pseudônimo ≤ original; se for maior,
redesenhar).

psql `csv` / `unaligned` — **sem realinhamento**:
```
table_schema,table_name,column_name     ->  table_schema,table_name,column_name
financeiro,tb_pedido_x9,vl_total        ->  SCH_mvrhafae,T_cszwa3ri,C_xsdgeq7l
```

MySQL `SHOW TABLES` — nome no cabeçalho, realinhar a moldura:
```
antes                            depois (moldura redesenhada)
+-----------------------+        +-------------------+
| Tables_in_vendas_demo |        | Tables_in_DB_x7k2 |
+-----------------------+        +-------------------+
| tb_pedido_x9          |        | T_cszwa3ri            |
+-----------------------+        +-------------------+
1 row in set (0.00 sec)          1 row in set (0.00 sec)
```

Erros — sem alinhamento a preservar:
```
ERROR:  relation "financeiro.tb_pedido_x9" does not exist   -> relation "SCH_mvrhafae.T_cszwa3ri" ...
Msg 208, Level 16, State 1, Server srv-exemplo-01, Line 1   -> Server HOST_kdekbkuz, Line 1
Invalid object name 'financeiro.tb_pedido_x9'.              -> 'SCH_mvrhafae.T_cszwa3ri'.
ORA-00904: "VL_TOTAL": invalid identifier                   -> "C_P2V6": invalid identifier
SQL compilation error: Object 'VENDAS_DEMO.FINANCEIRO.TB_PEDIDO_X9' does not exist or not authorized.
                                                            -> 'DB_X7K2.SCH_Q3M9.T_A8F1'
```

### 6. Casos difíceis e limites

- **Truncamento**: sqlplus corta valor ao tamanho de `COLUMN ... FORMAT A10` ou `LINESIZE`; mysql e
  snowsql podem abreviar com `...`. Um prefixo truncado (`tb_pedid`) não casa com o mapa. Regra: se a
  célula termina na borda da coluna sem espaço ou com `…`/`...`, mascarar a célula inteira com
  pseudônimo novo marcado como truncado e **nunca** devolver texto parcial original.
- **Quebra de linha**: psql `wrapped` quebra o valor em várias linhas com `+`/`.` no fim [DOC ✓]
  marcador]; sqlplus com `WRAP ON` continua na linha seguinte da mesma coluna; sqlcmd e sqlplus repetem
  cabeçalho a cada página. Juntar fragmentos por coluna antes de mascarar.
- **Largura fixa sem moldura** (sqlcmd, sqlplus, db2, bq sparse): colunas são definidas pela linha de
  traços; usar as posições dos grupos de `-` como limites. Sem a linha de traços (`-h -1`, `HEADING OFF`),
  não há cabeçalho → não dá para tipar pela coluna; cair só nos sinais de erro/qualificação ou **não
  mascarar por estrutura** e marcar como baixa confiança.
- **Saída cortada** (o usuário colou só um pedaço): sem cabeçalho e sem rodapé não há como saber o tipo.
  Exigir pelo menos o cabeçalho; linhas órfãs só são tratadas se a sessão já viu o cabeçalho antes.
- **`-W` e `-s` no sqlcmd**: tiram o alinhamento e o separador vira qualquer caractere; detectar o
  separador pela linha de cabeçalho (caractere repetido entre nomes conhecidos).
- **Valor que contém o separador**: no psql `unaligned` o `|` dentro do valor não é escapado [DOC];
  preferir CSV quando possível; no unaligned, conferir o número de campos com o cabeçalho.
- **Coluna `Name` ambígua** e cabeçalhos traduzidos (clientes com locale; ex.: psql em português mostra
  `Esquema | Nome | Tipo | Dono`) [fase 2]: a lista técnica precisa incluir os textos traduzidos das
  mensagens do próprio cliente (arquivos `.po` públicos do PostgreSQL), não dicionário de idioma.
- **ORA-00942 em 19c/21c** não traz o nome — nada a mascarar; não tentar adivinhar pela query anterior.
- **JSON** (`bq --format=json`, `snow sql --format json`): tratar pelas **chaves** (`tableId`,
  `datasetId`, `projectId`, `name`, `schema_name`) e não pela moldura.
- **Valores de dados** em colunas que não são de objeto (ex.: `comment`, `view_definition`,
  `routine_definition`) contêm SQL livre: entregar ao detector de SQL da outra família, não a este.

### 7. Links usados

- psql (formatos, border, footer, fieldsep): https://www.postgresql.org/docs/current/app-psql.html
- SQLSTATE: https://www.postgresql.org/docs/current/errcodes-appendix.html
- information_schema.columns: https://www.postgresql.org/docs/current/infoschema-columns.html
- SQL Server erro 208: https://learn.microsoft.com/en-us/sql/relational-databases/errors-events/mssqlserver-208-database-engine-error
- sqlcmd: https://learn.microsoft.com/en-us/sql/tools/sqlcmd/sqlcmd-utility (página carregada, opções `-s -W -h` não extraídas → [DOC ✓])
- ORA-00942: https://docs.oracle.com/en/error-help/db/ora-00942/
- ORA-00904: https://docs.oracle.com/en/error-help/db/ora-00904/
- Snowflake SHOW TABLES: https://docs.snowflake.com/en/sql-reference/sql/show-tables
- SnowSQL config (`output_format`, `header`, `timing`): https://docs.snowflake.com/en/user-guide/snowsql-config
- BigQuery erros (`notFound`): https://docs.cloud.google.com/bigquery/docs/error-messages
- bq CLI (`--format`): https://docs.cloud.google.com/bigquery/docs/reference/bq-cli-reference
- Pendentes de leitura (não carregaram nesta pesquisa): MySQL server error reference
  (https://dev.mysql.com/doc/mysql-errors/8.4/en/server-error-reference.html), Db2 LIST TABLES
  (ibm.com/docs), sqlplus DESCRIBE/SET (docs.oracle.com, SQL*Plus User's Guide), Snowflake CLI `snow sql`.


## Strings de conexão e endereços

Ideia central: string de conexão é texto **com gramática**. O detector não adivinha se uma
palavra "parece nome"; ele reconhece o formato (esquema `xxx://`, prefixo `jdbc:`, pares
`chave=valor;`, parênteses do TNS, tupla de URN), localiza a **posição** e mascara o valor
pelo **tipo da posição**. As chaves (vocabulário público) nunca são mascaradas.

### 1. Sinais de detecção

Dois grandes formatos, mais dois especiais:

| Família | Sinal estrutural (regex de entrada, simplificado) |
|---|---|
| URI (RFC 3986) | `\b[a-z][a-z0-9+.-]*://` com esquema da lista: `postgresql`, `postgres`, `mysql`, `mongodb`, `mongodb+srv`, `s3`, `s3a`, `s3n`, `gs`, `abfs`, `abfss`, `wasb`, `wasbs`, `hdfs`, e `dialeto+driver://` (SQLAlchemy) |
| JDBC | `\bjdbc:(sqlserver|oracle:thin|postgresql|mysql(\+srv)?(:loadbalance|:replication)?|db2|snowflake|bigquery):` |
| Pares chave=valor | ≥2 pares `chave=valor` separados por `;` (ODBC/ADO.NET) ou por espaço (libpq), **e** pelo menos uma chave do vocabulário (seção 3). Sem chave conhecida, não dispara. |
| TNS (Oracle Net) | `(DESCRIPTION=` ou `(ADDRESS=` com `(HOST=`; `nome = (DESCRIPTION=...)` em tnsnames.ora |
| URN DataHub | `urn:li:(dataset|corpuser|corpGroup|dataJob|dataFlow|schemaField|dataPlatform):` |

Fronteira da string: termina em espaço, aspas, `` ` ``, `)` não balanceado, `<`, `>` ou fim
de linha. Para ODBC/ADO.NET, valor entre `{}` (ODBC) ou aspas (ADO.NET) pode conter `;`.

### 2. Posição → tipo de entidade

Pseudônimos: `HOST_` servidor/host/conta, `DB_` database/catalog/service/SID, `SCH_` schema,
`USR_` usuário/papel, `BKT_` bucket/container/conta de storage, `PTH_` caminho/objeto,
`SVC_` serviço/DSN/aplicação/cluster/warehouse, `PRJ_` projeto GCP.

| Formato | Posição | Entidade |
|---|---|---|
| URI genérica | `userinfo` antes do `@` (parte antes de `:`) | USR |
| URI genérica | `host` (reg-name; IP fica com detector de IP) | HOST |
| URI genérica | `port` | **nunca** |
| URI genérica | 1º segmento do path | DB (postgres, mysql, mongo, SQLAlchemy) |
| SQLAlchemy | `dialeto+driver` | **nunca** (vocabulário) |
| libpq k=v / query | `host`, `hostaddr` / `dbname` / `user` / `service` / `application_name` | HOST / DB / USR / SVC / SVC |
| MongoDB | lista `h1:p,h2:p`; `/defaultauthdb`; `authSource`; `replicaSet`; `appName` | HOST cada; DB; DB; SVC; SVC |
| JDBC sqlserver | `//servidor\instancia:porta`; `databaseName`/`database`; `user`; `serverName`; `instanceName`; `applicationName`; `hostNameInCertificate` | HOST\SVC; DB; USR; HOST; SVC; SVC; HOST |
| JDBC oracle | `@host:porta:SID`, `@//host:porta/servico`, `@tcp[s]:h1,h2:porta/servico`, `@alias_tns`, `usuario/senha@` | HOST, DB, SVC (alias), USR |
| TNS | `HOST=`; `SERVICE_NAME=`/`SID=`/`INSTANCE_NAME=`/`GLOBAL_NAME=`; nome à esquerda do `=` | HOST; DB; SVC |
| JDBC mysql | hosts (também `address=(host=..)` e `(host=..,port=..)`); `/database`; `user` | HOST; DB; USR |
| JDBC db2 | `//servidor:porta/DATABASE:user=..;` | HOST; DB; USR |
| JDBC snowflake | `//<conta>.snowflakecomputing.com`; `db`; `schema`; `warehouse`; `role`; `user` | HOST (só a conta); DB; SCH; SVC; USR; USR |
| JDBC bigquery | `ProjectId`, `AdditionalProjects`; `DefaultDataset`; `OAuthServiceAcctEmail`; `OAuthPvtKeyPath` | PRJ; DB; USR; PTH |
| ODBC | `DSN`, `FILEDSN`, `SAVEFILE`; `Server`; `Database`; `UID` | SVC, PTH, PTH; HOST; DB; USR |
| ADO.NET | `Data Source`/`Server`/`Address`/`Addr`/`Network Address`; `Initial Catalog`/`Database`; `User ID`/`UID`/`User`; `Failover Partner`; `Application Name`/`App`; `Workstation ID`/`WSID`; `AttachDBFilename` | HOST(\SVC); DB; USR; HOST; SVC; HOST; PTH |
| s3/s3a/gs | `s3://<bucket>/<chave>` | BKT; PTH por segmento |
| abfs[s]/wasb[s] | `abfss://<container>@<conta>.dfs.core.windows.net/<path>` (wasbs: `.blob.`) | BKT (container); BKT (conta, só o rótulo); PTH |
| hdfs | `hdfs://<namenode>[:porta]/<path>` (ou nameservice lógico) | HOST; PTH |
| URN dataset | `(urn:li:dataPlatform:<p>,<nome>,<ENV>)` | p **nunca**; nome = `db.schema.tabela` → DB.SCH.TAB por ponto; ENV **nunca** |
| URN corpuser / corpGroup | `urn:li:corpuser:<id>` | USR |
| URN dataFlow / dataJob | `(orquestrador,flow_id,cluster)` / `(<flowUrn>,job_id)` | nunca; SVC; SVC (cluster é texto livre, ex.: prod); SVC |
| URN schemaField | `(<datasetUrn>,<field_path>)` | recursivo no dataset; coluna (tipo do detector de coluna) |

### 3. Vocabulário público (nunca mascarar)

Usar só listas oficiais e pequenas; tudo fica num arquivo de dados versionado, com a URL.

- **Esquemas/prefixos**: os da seção 1. SQLAlchemy: `postgresql+psycopg2`, `+pg8000`,
  `mysql+mysqldb`, `+pymysql`, `oracle+oracledb`, `+cx_oracle`, `mssql+pyodbc`, `+pymssql`,
  `sqlite` (docs.sqlalchemy.org). Regra: `dialeto` e `driver` são `[a-z0-9_]+` antes de `://`.
- **Chaves ODBC (gramática)**: `DSN`, `FILEDSN`, `DRIVER`, `UID`, `PWD`, `SAVEFILE`; demais
  são "driver-defined" (`Server`, `Database`, `Encrypt`, `TrustServerCertificate`...).
- **Chaves ADO.NET**: a tabela do `SqlConnection.ConnectionString` (Data Source, Server,
  Initial Catalog, Database, User ID, Integrated Security, Trusted_Connection, Encrypt,
  TrustServerCertificate, ApplicationIntent, MultiSubnetFailover, Persist Security Info,
  Pooling, Min/Max Pool Size, Connect Timeout, Authentication, Network Library...).
- **Chaves libpq**: tabela de "Parameter Key Words" (host, hostaddr, port, dbname, user,
  passfile, sslmode, application_name, options, service, connect_timeout,
  target_session_attrs...). Na URI, o nome do parâmetro **tem** que ser chave válida.
- **JDBC SQL Server**: "Setting the connection properties" (databaseName, user,
  integratedSecurity, authenticationScheme, encrypt, applicationName, instanceName...).
- **MySQL Connector/J** "Configuration Properties"; **Snowflake** JDBC (user, db, schema,
  warehouse, role, authenticator, privateKey..., mais qualquer parâmetro de sessão);
  **BigQuery** JDBC (ProjectId, OAuthType, OAuthServiceAcctEmail, OAuthPvtKeyPath,
  DefaultDataset, Location, EnableSession...); **MongoDB** "Connection String Options".
- **TNS**: DESCRIPTION_LIST, DESCRIPTION, ADDRESS_LIST, ADDRESS, PROTOCOL, HOST, PORT,
  CONNECT_DATA, SERVICE_NAME, SID, INSTANCE_NAME, SERVER, FAILOVER, LOAD_BALANCE, SDU...
- **Valores enumerados (nunca)**: `true/false/yes/no/on/off/0/1/sspi`; `sslmode`
  (disable, allow, prefer, require, verify-ca, verify-full); `target_session_attrs`
  (any, read-write, read-only, primary, standby, prefer-standby); `ApplicationIntent`
  (ReadOnly, ReadWrite); `PROTOCOL` (tcp, tcps, ipc); `SERVER` (dedicated, shared, pooled);
  `readPreference` (primary, primaryPreferred, secondary, secondaryPreferred, nearest);
  `authMechanism` (SCRAM-SHA-256, SCRAM-SHA-1, MONGODB-X509, MONGODB-AWS, GSSAPI, PLAIN,
  MONGODB-OIDC); `authSource=admin`/`$external`; `OAuthType` 0–4; `(local)`, `localhost`, `.`.
- **ENV do DataHub (FabricType, 17 valores)**: DEV, TEST, QA, UAT, EI, PRE, STG, NON_PROD,
  PROD, CORP, RVW, PRD, TST, SIT, SBX, SANDBOX, CERT. **Plataforma**: o identificador
  depois de `dataPlatform:` (snowflake, bigquery, postgres, mssql, kafka, airflow...).
- **Valor de `DRIVER=`**: nunca mascarar (ex.: `{ODBC Driver 18 for SQL Server}`); é nome de
  produto, e a gramática ODBC o separa como caso especial.
- **Sufixos de nuvem e regiões**: ver seção 6.

Como extrair: parse por formato → lista de `(chave, valor, offset)`; normalizar a chave
(caixa e espaços, ver seção 4); se a chave está no mapa `chave → tipo`, mascara o valor com o
tipo; se a chave é conhecida mas "não-entidade" (Encrypt, timeout), deixa; se a chave é
desconhecida, deixa o valor (preferir não mascarar a quebrar).

### 4. Regras de identificador

- **RFC 3986**: `userinfo` e `reg-name` aceitam `unreserved / pct-encoded / sub-delims`;
  `%XX` deve ser **decodificado antes** de comparar (mesmo host = mesmo pseudônimo) e o
  pseudônimo é reemitido **sem** caracteres que exijam encoding (`[A-Z0-9_]`), logo a URI
  continua válida. Esquema e host não diferenciam caixa (RFC 3986 §3.1, §3.2.2); path sim.
- **Mongo/MySQL/SQLAlchemy**: `: / ? # [ ] @` no usuário/senha vêm em `%XX`. MySQL exige
  encoding de `/ : @ ( ) [ ] & # = ?` e espaço em qualquer parte.
- **libpq k=v**: espaço em volta de `=` é opcional; valor com espaço vem entre `'...'`, com
  `\'` e `\\` dentro. Host começando com `/` é diretório de socket Unix (tipo PTH, não HOST).
  `host`, `hostaddr` e `port` aceitam listas por vírgula: mascarar item a item.
- **ODBC**: chave **não** diferencia caixa; valor pode diferenciar. Valor entre `{}` é passado
  intacto (pode ter `;`); `}` literal dentro de chaves é `}}` (JDBC SQL Server ≥ 8.4 idem;
  `{;}` escapa `;`). Primeira ocorrência de chave repetida vence: mascarar todas igual.
- **ADO.NET**: chave sem caixa; valor com `;`, `'` ou `"` vai entre aspas duplas (ou simples,
  se contiver `"`); aspa igual à delimitadora é dobrada. `tcp:host,porta`, `host\instancia`,
  `np:\\host\pipe\nome` e `(localdb)\inst`: separar prefixo, host, instância e porta.
- **JDBC SQL Server**: propriedades sem caixa; `\` separa instância.
- **MySQL**: chaves de propriedade **diferenciam caixa** (doc oficial); `[h1,h2]` é sublista.
- **TNS**: palavras-chave sem caixa; valores podem ter caixa (preservar a original no lado
  real, pseudônimo sempre em maiúsculas).
- **Snowflake**: conta `org-conta` (até 63 caracteres; `_` e `-` equivalentes na URL) ou
  locator `xy12345[.regiao[.nuvem]]`. Valores na query vêm URL-encoded.
- **Buckets**: S3 `[a-z0-9.-]{3,63}`, sem `_` e sem maiúsculas; GCS `[a-z0-9._-]`, 3–63
  (até 222 com pontos). Validar formato antes de mascarar evita falso positivo em `s3://${VAR}`.
- **URN DataHub**: `(`, `)` e `␟` (U+241F) proibidos; `,` proibida dentro de campo de tupla;
  caracteres especiais vêm em URL-encoding (`first%20name`). Nome do dataset: dividir por
  `.` e mascarar cada parte com o tipo da posição (db/schema/tabela), mantendo os pontos.

### 5. Exemplos antes/depois

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

Mesmo valor real → mesmo pseudônimo em todas as famílias (o `vendas_demo` do JDBC é o do
URN), senão o modelo perde a ligação entre os textos.

### 6. Casos difíceis e limites

- **Senha** (`PWD`, `Password`, `user:senha@`, `oauthClientSecret`, `OAuthPvtKey`,
  `private_key_file_pwd`): é do detector de segredo. Este detector só precisa **delimitar** a
  senha para não engolir `@`/`;` errado e não mascarar o host junto. Atenção: nos textos
  coletados para esta pesquisa, o detector de segredo atual pseudonimizou a **própria
  palavra-chave** de senha (em inglês) e até o verbo inglês de 4 letras que a começa, inclusive
  em prosa da documentação. A chave é vocabulário público; só o valor é segredo.
- **Sufixos oficiais de nuvem** (não mascarar o sufixo, mascarar o rótulo à esquerda):
  `.snowflakecomputing.com` / `.snowflakecomputing.cn` (conta = HOST; região/nuvem do
  locator, ex. `.us-east-2.aws`, não); `.database.windows.net`, `.database.chinacloudapi.cn`,
  `.database.usgovcloudapi.net` (servidor Azure SQL); `.dfs.core.windows.net`,
  `.blob.core.windows.net` (conta de storage); `.mongodb.net` (cluster); `www.googleapis.com`
  (endpoint BigQuery, nunca). Regiões oficiais (`us-east-1`, `southamerica-east1`,
  `aws_us_west_2`) ficam em claro.
- **Host que é FQDN interno**: mascarar o FQDN inteiro como um HOST (não pedaço a pedaço),
  exceto quando bater sufixo oficial da lista acima.
- **Valor genérico em posição de entidade**: `Database=master`, `dbname=postgres`,
  `authSource=admin`, `schema=public`, `SID=ORCL`... são nomes padrão do produto. Lista curta
  por produto (da doc), em claro; o resto, mascarar.
- **Variáveis/placeholder**: `${DB_HOST}`, `{{ var }}`, `<host>`, `%(host)s`, `$PGHOST`:
  não são valor real, não mascarar (e não confundir `{...}` de template com chaves ODBC).
- **Strings quebradas no meio** (concatenação `"Server=" + host + ";"`, YAML multilinha,
  TNS em várias linhas): o TNS precisa de balanceamento de parênteses multilinha; as demais
  ficam no detector de chave isolada (`Server=x` sozinho vale se a chave é do vocabulário).
- **IPv6 e IP**: `[2001:db8::1]` entre colchetes; IP fica com o detector de IP.
- **`hdfs://nameservice1/`**: nameservice lógico também vira HOST.
- **Ambiguidade com URL web**: `https://` não é desta família, salvo dentro do JDBC BigQuery.
- **Reversão**: o proxy precisa devolver o original na resposta; pseudônimo com caixa e
  formato fixos (`[A-Z]{2,4}_[a-z0-9]{4}`) torna a busca reversa segura dentro de URIs.
- Sem evidência na doc: lista completa de esquemas Hadoop de terceiros (`oss://`, `cos://`)
  `[VERIFICAR]`; doc do Simba BigQuery está só em PDF no bucket do fornecedor `[não confirmado → regra tolerante]`.

### 7. Links usados

- RFC 3986: https://www.rfc-editor.org/rfc/rfc3986
- ODBC SQLDriverConnect (gramática): https://learn.microsoft.com/en-us/sql/odbc/reference/syntax/sqldriverconnect-function
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
- MongoDB: https://www.mongodb.com/docs/manual/reference/connection-string/ e .../connection-string-options/
- S3 bucket naming: https://docs.aws.amazon.com/AmazonS3/latest/userguide/bucketnamingrules.html
- GCS buckets: https://docs.cloud.google.com/storage/docs/buckets
- ABFS URI: https://learn.microsoft.com/en-us/azure/storage/blobs/data-lake-storage-introduction-abfs-uri
- DataHub URN: https://docs.datahub.com/docs/what/urn/
- DataHub Dataset (FabricType): https://docs.datahub.com/docs/generated/metamodel/entities/dataset
- DataHub SchemaField / DataJob: https://docs.datahub.com/docs/generated/metamodel/entities/schemafield , .../datajob


### 8. Armazenamento e filas: o que está implementado (leitor `endereço`)

Código: `internal/mask/leitor_enderecos.go` (`urlEm`). As URIs de banco continuam com o leitor
`conexão`; estas são as de armazenamento e de fila:

| Esquema | Posição | Entidade | Forte? |
|---|---|---|---|
| `s3://`, `s3a://`, `s3n://`, `gs://`, `gcs://`, `az://`, `oss://` | autoridade | bucket | sim |
| `abfs[s]://<container>@<conta>.dfs.core.windows.net`, `wasb[s]://...blob...` | container / 1º rótulo do host | bucket / conta_nuvem | sim |
| `hdfs://`, `webhdfs://`, `viewfs://` | namenode (fora domínio público) | servidor | sim |
| todos acima | pedaço do caminho com cara de identificador (não o arquivo final, nem `chave=valor`, curinga ou variável) | pasta | não |
| `amqp[s]://usuario@host/vhost`, `mqtt[s]://`, `stomp://` | usuário / host(s) fora de domínio público / 1º pedaço do caminho | usuario / servidor / fila | sim |
| `kafka://broker:9092/topico`, `pulsar[+ssl]://`, `nats://`, `tls+nats://` | idem | usuario / servidor / fila | sim |

Ficam: `vhost` padrão (`/`, `%2F`) e variáveis (`s3://$BKT/`). Nome com cara de exemplo
(`my-bucket`, `example-bucket`) é mascarado como qualquer outro: pode ser real.

```text
s3://bkt-relatorios-demo/carga_diaria/2026/arquivo.csv  →  s3://BKT_.../DIR_.../2026/arquivo.csv
amqp://svc_relatorio:***@rabbit-01.interno:5672/vhost_pedidos  →  amqp://usr_...:***@host_...:5672/top_...
kafka://broker-01:9092/fila-pedidos-x9  →  kafka://host_...:9092/top_...
```

## Dados tabulares

**Como está implementado** (`leitor_tabela.go`): a tabela é reconhecida pela forma. Separador
`,` `;` TAB ou `|` com a mesma contagem fora de aspas no cabeçalho e na linha seguinte (CSV
conforme a RFC 4180: a URN com vírgulas entre aspas é uma célula só); linha de traços (com ou
sem bordas, `+---+`, `|===|`, `||` duplo); colunas alinhadas por espaços sem linha de traços
(cada palavra do cabeçalho é uma coluna; cada valor vai para a coluna com que mais se sobrepõe,
alinhado à direita ou à esquerda; o que fica antes da primeira coluna é índice; exige um vão de
2+ espaços, TAB ou recuo, para prosa não virar tabela); bordas de caixa chegam como `|`/`-` pela
normalização. Linha de tipos (`str`, `<chr>`, `varchar`), linhas truncadas (`...`) e rodapés
(`[N rows x M columns]`, `(2 rows)`) não são valores. Também: linhas que são tuplas ou listas de
literais (por linha, lista de tuplas, lista de listas), com a primeira como cabeçalho quando diz
algum tipo; `<table><tr><th>/<td>`; registro vertical `-[ RECORD n ]-` + `chave | valor`; e
linha TAB com um rótulo de tipo em maiúsculas na frente (`BUCKETS<TAB>data<TAB>nome`).

O tipo de cada coluna vem do cabeçalho: os nomes de catálogo (`table_schema`, `relname`...) ou
uma palavra de tipo em qualquer grafia (a mesma regra do leitor de chave-valor, `entChave`).
`name` é tabela quando há coluna de schema, é do tipo do título da seção quando a linha acima é
uma palavra de tipo (`Buckets`), e é coluna quando ao lado há `type` com tipos de dado
(DESCRIBE). Célula de cabeçalho com mais de uma palavra só em tabela de `|`, TAB ou traços, e
toda em minúsculas ou toda em maiúsculas (`primary key`, `APP VERSION`); `Data Type` não. Fora
de CSV/TSV, o cabeçalho só vira coluna com `_` ou dígito (`CreationDate` é rótulo de ferramenta).

Escopo: texto que é uma tabela impressa (CSV, TSV, saídas de pandas/Polars/DuckDB/pyarrow,
tabela markdown). Ideia central: **a posição dentro da grade diz o que é a célula**. O cabeçalho
pode trazer nomes de colunas reais. Uma célula de dado só vira nome de objeto quando o cabeçalho
da coluna dela diz que é (table_name, column_name...). Nenhuma regra depende de dicionário de idioma.

### 1. Sinais de detecção

Primeiro detecte o bloco (linhas seguidas que formam uma grade), depois o dialeto:

| Dialeto | Sinal estrutural (todos medidos no próprio bloco) |
|---|---|
| CSV (RFC 4180) | >= 2 linhas com o **mesmo número** de campos separados por `,` (ou `;`), contando as aspas: `"` abre e fecha o campo, `""` é aspa literal. Nenhuma linha termina em vírgula |
| TSV (IANA) | >= 2 linhas com o mesmo número de `\t`. Não há aspas, porque o campo não pode conter TAB. Pela IANA a **1ª linha é sempre o cabeçalho** |
| Markdown (GFM) | uma linha de cabeçalho com `\|`, seguida de uma linha delimitadora só de `-`, `:` e `\|`, com o **mesmo número de células**. Termina na primeira linha em branco |
| Polars | `shape: (N, M)`, depois uma borda `┌─┬─┐`, separador `┆` entre colunas, linha `---` e uma linha de dtypes (`i64`, `str`, `f64`) antes de `╞═╪═╡` |
| DuckDB duckbox/box | borda `┌─┐`/`│`, linha do nome, linha do tipo (`varchar`, `double`, `int64`...), depois `├─┤` |
| pandas `print(df)`/`to_string` | 1ª linha só com rótulos; as linhas seguintes começam pelo rótulo do índice (`0`, `1`... no RangeIndex). Colunas alinhadas por espaço, com os valores **à direita** (justify padrão: right). Pode ter rodapé `[N rows x M columns]` |
| pandas `df.info()` | `<class 'pandas.DataFrame'>`, `RangeIndex: N entries`, `Data columns (total N columns):`, um cabeçalho ` #  Column  Non-Null Count  Dtype` e a linha `---  ------` |
| pandas `describe()` / `dtypes` / Series | rótulos fixos no início da linha (`count`, `mean`...) ou rodapé `dtype: <tipo>` / `Name: x, dtype: ...` |
| pyarrow `Table` | linha `pyarrow.Table`, depois linhas `nome: tipo`, depois `----`, depois `nome: [[v1,v2]]` |

Detecção genérica para tabelas alinhadas por espaço: há uma **fronteira de coluna** quando a mesma
posição de caractere fica em branco em todas as linhas do bloco (>= 2 linhas, >= 2 colunas).

**Cabeçalho ou dado?** Em ordem de força:
1. **Marcador explícito**: linha delimitadora (GFM `---`, Polars `---`/`╞═╡`, DuckDB `├─┤`,
   `df.info()` `---  ------`). O que vem antes dela é cabeçalho, sempre.
2. **Formato que define**: TSV IANA (1ª linha = cabeçalho). Num CSV o MIME `header=present|absent`
   decide, se existir.
3. **Índice do pandas**: a linha de cabeçalho não tem rótulo de índice (começa com espaços) e as
   de dado têm. No MultiIndex existe uma 2ª linha só com os nomes dos níveis (`first second`).
4. **Heurística de tipo (CSV sem marcador)**: o mesmo critério do `csv.Sniffer.has_header` do Python.
   Uma coluna "vota cabeçalho" se as linhas 2..n são numéricas e a 1ª não é, ou se o comprimento
   difere do candidato. É cabeçalho quando mais da metade das colunas vota. Reforço próprio: as
   células da 1ª linha têm forma de identificador (seção 4) e são **únicas** entre si.
   A doc oficial avisa que isso gera falso positivo e falso negativo. Se não fechar, trate o bloco
   como sem cabeçalho (seção 6).

### 2. Posição → tipo de entidade

| Posição | Entidade | Observação |
|---|---|---|
| célula do cabeçalho (CSV/TSV/MD/Polars/DuckDB/pandas) | **COLUNA** (`C_`) | só se tiver forma de identificador (seção 4) e não for vocabulário (seção 3) |
| linha `nome: tipo` do pyarrow; linha ` N  nome  ... Dtype` do `df.info()`; 1º campo de `df.dtypes` | **COLUNA** | o nome fica entre o número e a contagem, ou antes do `:` |
| `Name: x` no rodapé de uma Series | **COLUNA** | `x` é o nome da coluna de origem |
| nome de nível de índice (linha `first second`) | **COLUNA** | nível de índice costuma vir de coluna (set_index) |
| valor numa coluna com cabeçalho `table_name`/`TABLE_NAME`/`table` | **TABELA** (`T_`) | os nomes de cabeçalho-gatilho vêm do INFORMATION_SCHEMA (lista pública) |
| valor sob `table_schema`/`schema_name`/`schema` | **SCHEMA** (`S_`) | idem |
| valor sob `table_catalog`/`catalog_name`/`database`/`database_name` | **DATABASE** (`D_`) | idem |
| valor sob `column_name`/`column` | **COLUNA** | e ligue-o ao `T_` da mesma linha |
| valor com forma `a.b.c` em qualquer coluna-gatilho | DATABASE.SCHEMA.TABELA | parte a parte |
| demais valores de dado | nenhum aqui | ficam para as outras famílias (e-mail, CPF...) |
| rótulo do índice (`0`, `1`, datas) | nenhum | só vira COLUNA/valor se o índice tiver nome-gatilho |

Cabeçalho-gatilho em português (`tabela`, `coluna`, `objeto`) **não** fica embutido no código,
porque seria lista de idioma: entra por configuração do usuário. Por padrão vêm só os nomes do
INFORMATION_SCHEMA (padrão SQL), comparados sem diferenciar maiúscula de minúscula.

### 3. Vocabulário público (nunca mascarar)

São palavras fixas que a ferramenta imprime. Sem esta lista, `count` e `Dtype` virariam colunas.

| Origem | Palavras | Fonte oficial |
|---|---|---|
| `df.info()` | `RangeIndex`, `entries`, `Data`, `columns`, `total`, `#`, `Column`, `Non-Null Count`, `non-null`, `Dtype`, `dtypes:`, `memory usage:` | pandas DataFrame.info |
| `describe()` | `count`, `mean`, `std`, `min`, `25%`, `50%`, `75%`, `max`, `unique`, `top`, `freq` (e percentis `NN%` gerados) | pandas DataFrame.describe |
| dtypes pandas/NumPy | `int8..int64`, `uint*`, `float32/64`, `bool`, `object`, `str`, `string`, `category`, `datetime64[ns|us, tz]`, `timedelta64[..]`, `period[..]`, `interval[..]`, `Int64`, `Float64`, `boolean`, `Sparse[..]`, `Index`, `RangeIndex`, `MultiIndex`, `<NA>`, `NaN`, `NaT` | pandas basics + arrays |
| rodapés pandas | `[N rows x M columns]`, `dtype:`, `Name:`, `Length:` | pandas options / basics |
| Polars | `shape:`, `---`, `str`, `i8..i64`, `u8..u64`, `f32`, `f64`, `bool`, `date`, `datetime[..]`, `list[..]`, `struct[..]`, `null` | docs.pola.rs |
| DuckDB | nomes de tipo (`varchar`, `integer`, `bigint`, `double`, `boolean`, `date`, `timestamp`, `int64`...) | duckdb.org (Data Types) |
| pyarrow | `pyarrow.Table`, `----`, `int8..int64`, `string`, `large_string`, `double`, `timestamp[..]`, `chunk` | arrow.apache.org |
| pandas sem cabeçalho | `Unnamed: N` (coluna sem nome na leitura) [VERIFICAR na doc do read_csv] | pandas read_csv |

**Como extrair** (gerado no build e versionado, nunca à mão):
- pandas/NumPy: `pandas.api.types` e `np.sctypeDict.keys()` para os aliases de dtype. Os rótulos
  de `describe`/`info` saem de rodar as funções sobre um DataFrame sintético e tokenizar a saída.
- Polars: `[str(t) for t in polars.datatypes]` e a impressão de um DataFrame sintético.
- DuckDB: `SELECT DISTINCT type_name FROM duckdb_types()`.
- pyarrow: os construtores de `pyarrow.types` impressos com `str()`.

Fixe a versão de cada lib no gerador, porque o pandas 3 trocou `object` por `str` em texto.

### 4. Regras de identificador

- **Forma de identificador** (célula de cabeçalho candidata): `^[A-Za-z_][A-Za-z0-9_$]*$`,
  com >= 3 caracteres **ou** contendo `_` ou dígito. `A`, `B`, `x` e `0..N` (RangeIndex) ficam
  como estão: são rótulos genéricos e mascará-los só atrapalha.
- **Cabeçalho com espaço** (`Valor Total`, `Non-Null Count`): **não** mascarar por padrão. Num
  banco isso só existe como identificador entre aspas (`"Valor Total"`), e numa planilha é
  rótulo humano. Exceção: se o mesmo texto aparece entre aspas de identificador (`"..."`, `` `...` ``,
  `[...]`) em SQL no mesmo prompt, a família SQL já o registrou e aqui só se reaplica o mesmo pseudônimo.
- **Regra do cabeçalho: mascarar todos os que têm forma de identificador e não estão no
  vocabulário.** Não use "só os que parecem nome de banco": isso exigiria adivinhar pelo idioma.
  Justificativa do risco: a leitura não quebra, porque o pseudônimo é tipado (`C_`), estável no
  prompt inteiro e desfeito na resposta. Quebraria se mascarássemos rótulos genéricos ou o
  vocabulário, e por isso essas duas listas existem.
- **Aspas no CSV**: tire as aspas de fora, desfaça `""` → `"`, avalie o conteúdo. Ao gravar, aplique
  aspas de novo só se o pseudônimo exigir (nunca exige, porque `T_cszwa3ri` é `[A-Za-z0-9_]`). Se o
  original estava entre aspas, **mantenha as aspas**: a diferença fica mínima e o CSV continua válido.
- **Identificador composto na célula** (`financeiro.tb_pedido_x9`): separe por `.` fora de aspas e
  mascare cada parte com o seu tipo.
- **Alinhamento**: em tabelas por espaço/box, os limites de coluna vêm da linha de cabeçalho e
  das bordas. Nunca procure o token por regex solto no texto, sempre pela célula.
- **Consistência**: um nome igual a um já visto (em SQL, YAML...) recebe o mesmo pseudônimo, e
  ganha o tipo mais específico (se já é `T_`, não vira `C_`).

### 5. Exemplos antes/depois

CSV (realinhar não se aplica, o CSV continua válido e com o mesmo número de campos):
```
antes:  id_pedido,vl_total,"dt_emissao",obs
        1,"10,50",2026-01-02,"diz ""ok"""
depois: C_4bszwltr,C_xsdgeq7l,"C_4uhd5lap",obs
        1,"10,50",2026-01-02,"diz ""ok"""
```
`obs` ficou porque tem 3 caracteres sem `_` nem dígito, e cai na regra do rótulo genérico. Ajuste
o mínimo se preferir. O valor `"10,50"` não é identificador: nada muda.

Inventário (valores mascarados por causa do cabeçalho-gatilho):
```
antes:  table_schema	table_name	column_name
        financeiro	tb_pedido_x9	vl_total
depois: table_schema	table_name	column_name
        SCH_tkrm6wg5	T_cszwa3ri	C_xsdgeq7l
```

pandas `df.info()` (realinha: a largura da coluna `Column` é recalculada):
```
antes:   #   Column      Non-Null Count  Dtype
        ---  ------      --------------  -----
         0   id_pedido   5 non-null      int64
         1   vl_total    5 non-null      float64
depois:  #   Column  Non-Null Count  Dtype
        ---  ------  --------------  -----
         0   C_4bszwltr  5 non-null      int64
         1   C_xsdgeq7l  5 non-null      float64
```

Polars / markdown (realinha, redesenhando bordas e `---` na largura nova):
```
antes:  │ id_pedido ┆ vl_total │        | id_pedido | vl_total |
        │ ---       ┆ ---      │        |----------:|---------:|
        │ i64       ┆ f64      │
depois: │ C_4bszwltr ┆ C_xsdgeq7l │              | C_4bszwltr | C_xsdgeq7l |
        │ ---    ┆ ---    │              |-------:|-------:|
        │ i64    ┆ f64    │
```
**Realinhar? Sim, sempre que o dialeto for posicional** (espaço, box, Polars, DuckDB, info). O
pseudônimo tem tamanho diferente do original, e trocar só o texto desloca as colunas para
a direita, o que deixa o modelo sem saber qual valor é de qual coluna. Procedimento: parsear as
células → trocar → recalcular a largura de cada coluna (o máximo entre as células) → redesenhar,
mantendo a justificação de cada coluna (pandas: direita; GFM: os `:` da delimitadora). CSV, TSV
e markdown sem alinhamento visual não precisam ser realinhados (só as células mudam). Na volta
(desmascarar a resposta) não se realinha nada.

### 6. Casos difíceis e limites

- **Cabeçalho ausente** (CSV puro de dados, `header=None` do pandas, que vira `0 1 2`): só valem
  as regras de valor que não dependem de cabeçalho (`a.b.c` em forma de identificador). A primeira
  linha não é mascarada como coluna. Falso negativo aceito.
- **Truncamento `...`/`..`** (pandas `max_rows`/`max_columns`, Polars `…`): a coluna `...` e a linha
  `..` são preenchimento, não dado. Vale o rodapé `[N rows x M columns]`. Parte das colunas nunca
  aparece, o que não é problema porque não há o que mascarar. Polars `…` [VERIFICAR: a doc de
  Config não descreve o marcador].
- **Quebra de largura do pandas** (`expand_frame_repr=True` e a largura passa de `display.width`):
  a tabela sai em vários blocos de colunas, e a linha de cabeçalho de cada bloco que continua
  termina em `\` [VERIFICAR: comportamento conhecido, mas não mostrado na página oficial de options].
  Trate como um bloco só: o rótulo do índice se repete nas partes, e esse é o sinal para juntar.
  Realinhe cada parte em separado e preserve o `\`.
- **Célula com vírgula/quebra de linha** (CSV): só um parser com aspas resolve. Uma `"` desbalanceada
  dentro do bloco invalida a detecção, e aí o bloco fica sem máscara tabular (cai nas outras famílias).
- **Delimitador ambíguo** (`,` vs `;` vs espaço): escolha o que dá a mesma contagem em todas as
  linhas. Empate segue a ordem `, \t ; espaço :` (o `preferred` do Sniffer).
- **Valor com espaço numa tabela alinhada** (`São Paulo`): separar por um espaço quebra a célula.
  Por isso use as fronteiras verticais (posição em branco em todas as linhas), nunca `split()`.
- **MultiIndex esparso**: a célula em branco repete o rótulo de cima, então não é célula vazia.
  Linhas de cabeçalho múltiplas (`first`/`second` nas colunas) são todas cabeçalho.
- **Excel/Parquet impressos**: não existe "texto de Excel". Chega como DataFrame (`read_excel`/
  `read_parquet` e depois o print), como `pyarrow.Table`, como DuckDB box ou como lista de tuplas do
  openpyxl (`ws.iter_rows(values_only=True)`), que já é outra família (literal Python). Detecte
  pelo dialeto de saída, não pela origem. O nome da aba (`sheet_name`) é rótulo humano: não mascarar.
- **Limite honesto**: sem marcador explícito, a decisão cabeçalho/dado é heurística. Prefira não
  mascarar a mascarar errado na grade, porque o identificador ainda pode ser pego por outra família.

### 7. Links usados

- RFC 4180: https://www.rfc-editor.org/rfc/rfc4180
- IANA text/tab-separated-values: https://www.iana.org/assignments/media-types/text/tab-separated-values
- GFM, tabelas (§4.10): https://github.github.com/gfm/
- pandas DataFrame.info: https://pandas.pydata.org/docs/reference/api/pandas.DataFrame.info.html
- pandas DataFrame.describe: https://pandas.pydata.org/docs/reference/api/pandas.DataFrame.describe.html
- pandas DataFrame.to_string: https://pandas.pydata.org/docs/reference/api/pandas.DataFrame.to_string.html
- pandas options (truncamento, width, rodapé): https://pandas.pydata.org/docs/user_guide/options.html
- pandas MultiIndex: https://pandas.pydata.org/docs/user_guide/advanced.html
- pandas dtypes: https://pandas.pydata.org/docs/user_guide/basics.html e https://pandas.pydata.org/docs/reference/arrays.html
- Polars, formato impresso: https://docs.pola.rs/user-guide/getting-started/
- Polars Config: https://docs.pola.rs/api/python/stable/reference/config.html
- DuckDB CLI output formats: https://duckdb.org/docs/current/clients/cli/output_formats.html
- Arrow (pyarrow.Table): https://arrow.apache.org/docs/python/getstarted.html
- Python csv.Sniffer: https://docs.python.org/3/library/csv.html


## JSON, YAML, TOML, INI, .env, .properties, XML e linguagens de esquema

**Esquemas: nome + tipo de dado** (`leitor_esquema.go`, leitor novo: nenhum outro lê essa
forma). Onde um nome vem com um tipo de dado, o nome é coluna: `nome    int64` por linha (2+
linhas, ou 1 com o rodapé `dtype: object`), `Index(['a', 'b'], dtype='object')`,
` |-- nome: string` (printSchema), `nome: int64` no começo da linha (2+ linhas: anotação de
código é recuada e não conta), `string nome = 1;` (protobuf) e tabela `name | type` com tipos
de dado na coluna `type`. O vocabulário é só o de tipos de dado (SQL, pandas/numpy, Arrow,
Polars, Spark, Avro, Protobuf, R: `vocab_tipos.go`). Em YAML/JSON, `name` vale pelo contêiner
(o motor de YAML/JSON, `acharNomePorContexto`): sob `columns`/`fields` é coluna (dbt
`schema.yml`, Avro), sob `tables`/`models`/`seeds`/`views` é tabela, sob `sources`/`datasets`
é schema, e sob uma chave que é palavra de tipo (`table`, `dataset`, `database`...) é desse
tipo; `owner` é usuário; valor qualificado (`db.schema.tabela`) é dividido nas partes.

**Chave → valor em qualquer sintaxe** (leitor de chave-valor, `leitor_chave_valor.go`, e de
código, estendidos). A palavra de tipo é reconhecida em qualquer grafia: o nome da chave é
dividido em pedaços (camelCase, snake_case, kebab, pontos) e também colado a `name`/`path`
(`dbname`, `rolename`, `warehousename`, `accountname`, `fieldPath`); palavras de tipo novas:
`column`/`coluna`/`field` (coluna), `role` (usuário), `view` (tabela), `procedure`/`routine`;
`serviceAccountName` é usuário. Formas novas: vários `k=v` na mesma linha (2+ pares separados
por espaço, sem `(` nem `, ` na linha, que seriam argumentos nomeados); valor em lista na
mesma linha (`"tables": ["a", "b"]`, `tabelas: [a, b]`) e lista YAML logo abaixo da chave
(cada item é do tipo da chave, no singular ou no plural, inclusive em português); `name=`
dentro de uma tag XML vale pela tag (`<column name="x">`); chave com `__` em volta
(`__tablename__`); `Chave Valor` por espaço num bloco de 2+ linhas assim (ssh config);
linha `IP nome [nome...]` (/etc/hosts): os nomes são servidores.

Ideia central: nesses formatos, quem diz o que um token é não é o token, e sim a **posição**
(o valor de qual chave, ou o fato de ser chave de um mapa sob um pai conhecido). O detector
analisa a estrutura e usa o vocabulário da especificação para separar o que é fixo e público
(`type`, `properties`) do que é nome escolhido por alguém (`tb_pedido_x9`).

### 1. Sinais de detecção

| Formato | Sinal estrutural (barato, sem parser completo) |
|---|---|
| JSON | começa com `{` ou `[` (depois de espaço); pares `"k":`; parse RFC 8259 bem-sucedido confirma |
| YAML | `---` no início do documento; linhas `chave: valor` com indentação consistente; itens `- `; `&ancora` / `*alias` |
| TOML | cabeçalhos `[tabela]` / `[[array]]`; `chave = valor` com string entre aspas; chaves pontuadas `a.b = ` |
| INI | cabeçalhos `[secao]` + `chave=valor` sem aspas obrigatórias; comentário `;` ou `#` (sem spec: é convenção) |
| .env | linhas `NOME=valor`, nomes MAIÚSCULOS com `_`, `export ` opcional, sem seções |
| .properties | `chave=valor`, `chave: valor` ou `chave valor`; comentário `#`/`!`; chaves pontuadas `spring.datasource.url` |
| XML | `<?xml`, `<raiz ...>`, tags balanceadas, `xmlns=` |
| JSON Schema | chave `$schema` com URL `json-schema.org`, ou `"type":"object"` + `"properties"` |
| OpenAPI 3 | chave `openapi: 3.x.y` na raiz, com `info` e `paths` |
| Avro (.avsc) | objeto com `"type":"record"`, `"name"`, `"fields":[...]` |
| Protobuf | `syntax = "proto3";`/`edition = "2023";`, `package x;`, `message X {` |
| GraphQL SDL | `type X {`, `input X {`, `enum`, `schema {`, `extend type` |

Regra de decisão: só ativar a família quando o parse do trecho funciona (ou um prefixo dele, ver §6).
Sem parse, cair para o detector de texto livre, não "adivinhar" chaves.

### 2. Posição → tipo de entidade

**(a) Nome no VALOR.** A chave é casada por forma normalizada: minúsculas, sem `_`/`-`/`.`, e
olhando o último segmento (`spring.datasource.username` → `username`; `DB_HOST` → `host`).

| Último segmento da chave (normalizado) | Entidade | Pseudônimo |
|---|---|---|
| `host`, `hostname`, `server`, `servername`, `address`, `endpoint` | servidor/host | `HOST_` |
| `account` (Snowflake), `instance`, `cluster` | servidor/conta | `HOST_` |
| `database`, `db`, `dbname`, `catalog`, `project` (GCP) | database | `DB_` |
| `schema`, `dataset`, `keyspace`, `namespace` (fora de Avro/k8s ver §6) | schema | `S_` |
| `table`, `tablename`, `view`, `collection`, `relation` | tabela | `T_` |
| `column`, `columnname`, `field`, `partitionby`, `clusterby`, `primarykey` | coluna | `C_` |
| `user`, `username`, `uid`, `login`, `role`, `owner`, `serviceaccount` | usuário | `U_` |
| `service`, `servicename`, `app`, `application` | serviço | `SVC_` |
| `bucket`, `container`, `stage`, `location` | bucket | `B_` |
| `topic`, `queue`, `subscription`, `stream`, `channel`, `exchange` | fila/tópico | `Q_` |
| `url`, `uri`, `jdbcurl`, `dsn`, `connectionstring` | composto: decompor (host, porta, db, user) | vários |
| chaves de credencial (`pwd`, `senha`, `token`, `apikey`, `privatekey`) | **credencial** (outra família: mascarar valor inteiro) | `CRED_` |

**(b) Nome na CHAVE.** A chave é nome quando o pai é um "mapa de nomes livres":

| Pai / contexto | As chaves filhas são | Entidade |
|---|---|---|
| JSON Schema `properties`, `patternProperties`(não: são regex), `$defs`, `definitions`, `dependentRequired` | nomes de campo / de tipo | `C_` / `T_` |
| OpenAPI `components.schemas`, `components.parameters`, `components.responses` ... | nomes de tipo/componente | `T_` |
| OpenAPI `paths` | caminho: só os segmentos literais são candidatos (`/pedidos/{id}`) | `SVC_`/`T_` |
| Mapa de colunas (`columns:`, `fields:` como objeto, `mapping:`) | nome de coluna; valor é tipo | `C_` |
| Seção INI/TOML `[db_exemplo]`, `[tables.tb_pedido_x9]` | segmento livre do cabeçalho | conforme pai |
| Elemento XML sob raiz de dados (`<tb_pedido_x9>`) | nome de tabela/coluna (só se não for vocabulário do XSD/namespace) | `T_`/`C_` |

Contraprova: chave filha cujo valor é escalar de vocabulário de tipo (`"decimal"`, `"string"`,
`{"type": ...}`) reforça que a chave é nome de coluna.

### 3. Vocabulário público por formato

Lista fixa, versionada, extraída da spec. Tudo que estiver na lista nunca é mascarado na posição de chave.

| Formato | Vocabulário (palavras da própria spec) | Fonte / como extrair |
|---|---|---|
| JSON | só literais `true`, `false`, `null` | RFC 8259 §3; não há chaves reservadas |
| YAML 1.2 | `true/false/null/~`, tags `!!str !!int !!map !!seq`, `<<` (merge; é tipo de 1.1, mas muitos parsers aceitam) | spec 1.2.2 cap. 10 (schemas Failsafe/JSON/Core) |
| TOML | `true`, `false`, `inf`, `nan`; datas RFC 3339 | toml.io/en/v1.0.0, seções "Boolean", "Float" |
| INI | nenhum (sem spec) | — |
| .env | `export` | README python-dotenv; doc Docker Compose |
| .properties | nenhum (só sintaxe) | Javadoc `Properties.load(Reader)` |
| XML | `xml`, `xmlns`, `xmlns:*`, `xml:lang`, `xml:space`, `xml:base`; prefixo `xml` reservado | W3C XML 1.0 §2.3, §2.12; Namespaces in XML |
| JSON Schema 2020-12 | `$schema $id $ref $anchor $dynamicRef $dynamicAnchor $defs $vocabulary $comment`, `type properties patternProperties additionalProperties required items prefixItems contains enum const format allOf anyOf oneOf not if then else dependentRequired dependentSchemas propertyNames unevaluatedProperties unevaluatedItems minimum maximum exclusiveMinimum exclusiveMaximum multipleOf minLength maxLength pattern minItems maxItems uniqueItems minProperties maxProperties title description default examples deprecated readOnly writeOnly contentEncoding contentMediaType`; tipos `object array string number integer boolean null` | json-schema.org/draft/2020-12 (Core + Validation): copiar o índice de keywords de cada vocabulário |
| OpenAPI 3.1 | campos fixos dos objetos: `openapi info servers paths components security tags externalDocs webhooks`, `get put post delete options head patch trace`, `parameters requestBody responses operationId summary description`, `in name required schema content`, `in`: `query header path cookie`, `schemas responses parameters examples requestBodies headers securitySchemes links callbacks pathItems`, `discriminator propertyName mapping xml`; chaves `x-*` são extensão | spec.openapis.org/oas/v3.1.0: tabelas "Fixed Fields" de cada objeto. **Atenção:** `operationId` e `servers[].url` são VALORES a mascarar |
| Avro | atributos `type name namespace doc aliases fields order default symbols items values size logicalType precision scale`; primitivos `null boolean int long float double bytes string`; complexos `record enum array map fixed`; lógicos `decimal uuid date time-millis time-micros timestamp-millis timestamp-micros timestamp-nanos local-timestamp-* duration big-decimal` | avro.apache.org/docs/1.12.0/specification |
| Protobuf | `syntax edition package import option message enum service rpc returns stream oneof map reserved extensions extend optional repeated required weak public`; escalares `double float int32 int64 uint32 uint64 sint32 sint64 fixed32 fixed64 sfixed32 sfixed64 bool string bytes`; `google.protobuf.*` | protobuf.dev/reference/protobuf/proto3-spec (gramática EBNF: listar os terminais) |
| GraphQL SDL | `type interface union enum input scalar schema directive extend implements repeatable on query mutation subscription fragment`; escalares `Int Float String Boolean ID`; diretivas `@deprecated @skip @include @specifiedBy @oneOf`; nomes `__*` (introspecção) | spec.graphql.org (October 2021 / draft): seção "Type System" e "Names" |

Onde ficam os nomes internos (o que mascarar):
- **JSON Schema:** chaves de `properties`/`$defs`; itens de `required` (são nomes de propriedade:
  precisam do mesmo pseudônimo da chave); fragmento de `$ref` (`#/$defs/tb_pedido_x9`);
  `title`/`description` são texto livre (vão para o detector de prosa); `$id`/`$schema` são URLs (host).
- **OpenAPI:** chaves de `components/*`; segmentos literais de `paths`; `parameters[].name`;
  `operationId`; `servers[].url`; `tags[].name`; `$ref` (`#/components/schemas/X`); `discriminator.mapping`.
- **Avro:** `namespace` (pontos = segmentos), `name` de record/enum/fixed e de `fields[]`, `aliases[]`,
  `symbols[]` (podem ser códigos internos), `type` quando é nome de tipo nomeado já definido.
- **Protobuf:** `package` (pontuado), nome após `message`/`enum`/`service`/`rpc`, nome de campo
  (`tipo nome = N;`), tipos referenciados (`tb.Pedido`), valores de enum. Número de campo não.
- **GraphQL:** nome após `type`/`input`/`enum`/`interface`/`union`/`scalar`; nomes de campo e de
  argumento; valores de enum; tipos referenciados em `campo: Tipo!`.

### 4. Regras de identificador

- **JSON string (RFC 8259 §7):** comparar e substituir sobre o valor **decodificado**
  (`"tb_pedido"` = `tb_pedido`). Ao reescrever, emitir string JSON válida (escapar `"` `\` e
  controles). Pseudônimos só com `[A-Za-z0-9_]` evitam o problema.
- **YAML:** escalar pode ser plano, `'simples'` (escape `''`) ou `"duplo"` (escapes `\`). Preservar o
  estilo original. Escalar plano que vire `true`, `null`, `1e3` ou comece com `&*!|>%@` muda de
  tipo: pseudônimo deve começar por letra e não colidir com o schema Core (`T_cszwa3ri` é seguro).
  **Âncoras/aliases** (`&base`, `*base`): o nome da âncora é rótulo do documento, normalmente não
  sensível; o alias reaproveita o nó, então mascarar no nó resolve todas as ocorrências. Se a âncora
  tiver nome interno, renomear `&x` e todos os `*x` juntos. Chaves complexas (`? `) raras: tratar como §6.
- **TOML:** chave nua só `A-Za-z0-9_-`; com outro caractere precisa de aspas. `a.b.c = 1` é chave
  **pontuada** (três níveis), mas `"a.b.c" = 1` é uma chave só. Cabeçalho `[tables.tb_pedido_x9]`
  idem. Mascarar segmento a segmento.
- **INI:** sem spec; tratar `[secao]` e `chave=valor`, aspas opcionais e não removidas.
- **.env:** python-dotenv: chave nua ou entre `'`; valor nu, `'...'` (escapes só `\\`, `\'`) ou
  `"..."` (mais `\n \t \"`...); `#` após valor é comentário; aspas permitem multilinha; expande
  só `${VAR}`. Docker Compose: aceita `=` ou `:`, expande `$VAR` e `${VAR:-x}`, e `#` só é comentário
  com espaço antes. Não mascarar `${VAR}` (é referência, não valor); mascarar o valor final.
- **.properties:** a chave vai até o primeiro `=`, `:` ou espaço **não escapado**; `\` no fim da
  linha continua o valor; `\uXXXX` precisa ser decodificado antes de comparar; ponto na chave é só
  convenção (não há hierarquia na spec). Ao reescrever, escapar `= : # !` e espaço na chave.
- **XML:** nome de elemento/atributo segue `Name` (§2.3), pode ter prefixo `ns:local`: mascarar só
  `local`. Valores em atributo ou texto: decodificar entidades (`&amp;`, `&#95;`). CDATA: texto livre.
- **Nomes compostos dentro do valor:** dividir por `.` respeitando aspas do SQL
  (`"Meu Schema".tb`, `` `proj.ds.tb` ``) e pseudonimizar cada parte com o tipo da posição.

### 5. Exemplos antes/depois

Mesmo nome → mesmo pseudônimo em todo o arquivo (e na sessão). Estrutura e tipos preservados.

```json
// antes
{"host": "db-exemplo-01", "database": "vendas_x", "table": "tb_pedido_x9",
 "columns": {"vl_total": "decimal(12,2)", "dt_pedido": "date"}}
// depois
{"host": "HOST_7rhd4fsp", "database": "DB_dqrsyg6b", "table": "T_cszwa3ri",
 "columns": {"C_xsdgeq7l": "decimal(12,2)", "C_6kntohaz": "date"}}
```

```yaml
# antes                                   # depois
fonte: &origem                            fonte: &origem
  host: db-exemplo-01                       host: HOST_7rhd4fsp
  table: "tb_pedido_x9"                     table: "T_cszwa3ri"
destino: *origem                          destino: *origem
```

```toml
# antes                                   # depois
[tables.tb_pedido_x9]                     [tables.T_cszwa3ri]
cluster_by = ["dt_pedido"]                cluster_by = ["C_6kntohaz"]
```

```properties
# antes
spring.datasource.url=jdbc:postgresql://db-exemplo-01:5432/vendas_x
spring.datasource.username=usr_etl_x
# depois
spring.datasource.url=jdbc:postgresql://HOST_7rhd4fsp:5432/DB_dqrsyg6b
spring.datasource.username=USR_m5og72t3
```

```json
// JSON Schema: chave, required e $ref recebem o mesmo pseudônimo
{"$defs": {"tb_pedido_x9": {"type": "object",
   "properties": {"vl_total": {"type": "number"}}, "required": ["vl_total"]}},
 "$ref": "#/$defs/tb_pedido_x9"}
// depois
{"$defs": {"T_cszwa3ri": {"type": "object",
   "properties": {"C_xsdgeq7l": {"type": "number"}}, "required": ["C_xsdgeq7l"]}},
 "$ref": "#/$defs/T_cszwa3ri"}
```

```proto
// antes                                  // depois
package vendas_x.pedidos;                 package DB_dqrsyg6b.SCH_3wl5qkpz;
message TbPedidoX9 { double vl_total = 1; }  message T_c3wyfawv { double C_xsdgeq7l = 1; }
```

Avro: `"namespace": "vendas_x.pedidos"` → `"DB_dqrsyg6b.SCH_3wl5qkpz"`, `"name": "tb_pedido_x9"` → `"T_cszwa3ri"`
(o pseudônimo respeita `[A-Za-z_][A-Za-z0-9_]*`, então o .avsc continua válido).

### 6. Casos difíceis e limites

- **JSON truncado** (log cortado, streaming): usar parser tolerante/tokenizador que anda até onde
  dá e mantém a pilha de chaves; mascarar o que tem posição conhecida e mandar o resto para o
  detector de texto livre. Nunca "fechar" o JSON ao reescrever: substituir só dentro dos tokens.
- **YAML multi-documento** (`---` ... `---`, `...`): cada documento tem contexto próprio, mas o
  mapa de pseudônimos é da sessão inteira. Âncora não atravessa documentos (spec 1.2.2 §9.2).
- **Valor composto `db.schema.tabela`:** dividir e tipar pela quantidade de partes e pela chave:
  3 partes sob `table` → `DB_.S_.T_`; 2 partes → `S_.T_`. Em BigQuery `proj.ds.tb` (ou
  `proj:ds.tb` legado) a primeira parte é projeto (`DB_`). Se a contagem não fecha, `[VERIFICAR]`
  e mascarar todas as partes com tipo genérico.
- **URL/DSN/JDBC no valor:** parsear como URI (RFC 3986): host, user, path (db), query (`?schema=`).
- **Listas allow/deny com regex/glob** (`include: ["tb_pedido_.*"]`, `deny: ["stg_*"]`): o padrão
  contém nome parcial. Opções: (1) mascarar só os literais máximos do padrão (`tb_pedido_` → um
  pseudônimo de prefixo) mantendo metacaracteres; (2) mascarar o padrão inteiro como opaco
  (`T_kwvciyut`). A (1) quebra se o LLM gerar regex nova; a (2) é segura e é o padrão sugerido.
  `patternProperties` do JSON Schema cai aqui.
- **Mesma palavra, papéis diferentes:** `name` é vocabulário em Avro/OpenAPI (chave), mas o valor é
  nome interno; `namespace` em Kubernetes é ambiente, em Avro é pacote. Decidir pelo formato detectado.
- **Chave genérica com valor genérico** (`type: table`, `kind: view`): valor está no vocabulário →
  não mascarar.
- **Colisão com vocabulário:** coluna chamada `type` ou `items` dentro de `properties` é nome
  (posição manda); fora de mapa de nomes livres, é vocabulário.
- **Comentários** (YAML/TOML/INI/.properties/.env `#`, XML `<!-- -->`): texto livre, outra família.
- **Reversão:** a resposta do LLM pode reformatar (trocar aspas, reordenar). Desmascarar por token
  `PREFIXO_xxxx`, não por posição.
- **Limite:** sem dicionário de idioma, chave desconhecida com valor desconhecido
  (`"origem": "tb_pedido_x9"`) não é detectada por esta família; depende do detector de forma de
  identificador (snake_case com prefixo `tb_`, `vl_`) de outra seção.

### 7. Links usados

- JSON: https://www.rfc-editor.org/rfc/rfc8259
- YAML 1.2.2: https://yaml.org/spec/1.2.2/
- TOML 1.0: https://toml.io/en/v1.0.0
- XML 1.0: https://www.w3.org/TR/xml/ ; Namespaces: https://www.w3.org/TR/xml-names/
- Java Properties: https://docs.oracle.com/javase/8/docs/api/java/util/Properties.html (load(Reader))
- .env (python-dotenv): https://github.com/theskumar/python-dotenv (README, seção "File format")
- .env (Docker Compose): https://docs.docker.com/compose/how-tos/environment-variables/variable-interpolation/
- JSON Schema 2020-12: https://json-schema.org/draft/2020-12/json-schema-core ; https://json-schema.org/draft/2020-12/json-schema-validation
- OpenAPI 3.1.0: https://spec.openapis.org/oas/v3.1.0
- Avro 1.12.0: https://avro.apache.org/docs/1.12.0/specification/
- Protobuf: https://protobuf.dev/reference/protobuf/proto3-spec/ ; https://protobuf.dev/reference/protobuf/edition-2023-spec/
- GraphQL: https://spec.graphql.org/October2021/
- URI (DSN/JDBC): https://www.rfc-editor.org/rfc/rfc3986


### 8. O que está implementado (leitor `chave-valor`)

Código: `internal/mask/leitor_chave_valor.go`. Um leitor só, sem parser de cada formato: acha o
separador (`:` ou `=`), a chave antes dele e o valor depois. Vale para YAML, JSON, TOML, INI,
.env, .properties, atributo XML (`host="x"`), elemento XML sem atributos (`<host>x</host>`) e
opção longa de linha de comando (`--host x`, `--host=x`).

**Chave → entidade, pelo ÚLTIMO pedaço** (divide em `.` `_` `-` `:` e camelCase, minúsculas):

| Último pedaço | Entidade | Pseudônimo |
|---|---|---|
| `host`, `hostname`, `server`, `servername`, `servidor`, `endpoint`, `address`, `addr`, `fqdn`, `broker`, `bootstrap`, `bootstrap.servers`, `cluster`, `instance`, `warehouse` | servidor | `HOST_` |
| `database`, `db`, `dbname`, `databasename`, `catalog` | database | `DB_` |
| `schema`, `schemaname`, `dataset` | schema | `SCH_` |
| `table`, `tablename`, `tabela`, `collection` | tabela | `T_` |
| `user`, `username`, `usuario`, `login`, `principal` | usuario | `USR_` |
| `namespace` | namespace | `NS_` |
| `service`, `servico`, `app`, `application` | servico | `SVC_` |
| `bucket`, `container` | bucket | `BKT_` |
| `queue`, `topic`, `fila`, `topico`, `exchange`, `stream`, `subject` | fila | `TOP_` |
| `repo`, `repository` | repositorio | `REPO_` |
| `org`, `organization` | organizacao | `ORG_` |
| `account`, `tenant`, `subscription`, `project_id`/`projectId` | conta_nuvem | `ACC_` |
| `<x>_name` com x = db, database, table, schema, host, server, user, service, bucket, queue, topic | a entidade de x | — |

Chave terminada em outra coisa (`port`, `timeout`, `count`, `size`, `enabled`, `max`, `min`,
`version`, `type`, `mode`, `ttl`, `retries`, `id`...) não está na tabela e fica de fora.
**Forte** quando a chave é exatamente o nome (`host:`, `database=`, `--host`, `HOST=`);
**fraco** quando é composta (`db_host`, `spring.datasource.username`).

**Valor que fica:** `true/false/null/none`, número, IP (fica com o detector de IP), `localhost`,
`0.0.0.0`, caminho (`/`, `./`, `~`), expressão (`${...}`, `{{...}}`, `%s`, `$VAR`, `<x>`), e-mail,
URL (os leitores de URL decidem), domínio público (TLD conhecido ou reservado: `.com`, `.io`,
ccTLD, `.example`, `.invalid`), nome de arquivo (`config.yaml`), versão/hash, palavra de tipo
(`str`, `string`, `int`). Nome com cara de exemplo (`my-bucket`) não é exceção: é mascarado.

**Freios contra código:** `:=`, `==`, `+=`... ficam; valor seguido de `(`, `[` ou `"` é chamada,
índice ou literal composto; valor sem aspas, sem hífen e sem dígito (pode ser variável) só vale em
linha de configuração (chave no começo da linha, valor até o fim dela, com `:` de YAML, chave de
.env em MAIÚSCULAS, chave pontuada de .properties que não começa por `self.`/`cfg.`..., ou `=`
sem espaços); valor pontuado sem dígito/hífen (`self.host`, `http.server`) é acesso a atributo
ou módulo; `Server: nginx` (chave em Título com `:`) é rótulo de prosa ou cabeçalho HTTP.
Medido em 4 MB do código-fonte do Go e 4 MB da biblioteca do Python 3.10: 0 e 2 achados.

```text
ANTES                                        DEPOIS
DB_HOST=db-exemplo-01                        DB_HOST=HOST_r2wq7m4k          (fraco: chave composta)
<database>vendas_x</database>                <database>db_k5n2b7xq</database>
pg_dump --host db-exemplo-01 --dbname=vendas_x   pg_dump --host HOST_r2wq7m4k --dbname=db_k5n2b7xq
bootstrap.servers=kafka-01:9092,kafka-02:9092    bootstrap.servers=host_...:9092,host_...:9092
user = request.user                          (fica: expressão de código)
```

## Kubernetes e contêineres

Ideia central: num manifesto o **caminho do campo** diz o que o valor é. Não é preciso adivinhar
se `pedidos-x9` é um nome: se ele está em `metadata.name` de um `kind: Service`, é um serviço.
A lista do que é vocabulário fixo é pequena e vem do próprio esquema da API.

### 1. Sinais de detecção

Um sinal forte basta. Dois sinais fracos juntos também bastam.

| Formato | Sinal forte | Sinal fraco |
|---|---|---|
| Manifesto K8s (YAML/JSON) | `apiVersion:` e `kind:` no mesmo documento, com `kind` dentro da lista pública (seção 3) | `metadata:` com `name:`/`namespace:`; separador `---` entre documentos |
| Saída `kubectl get` | cabeçalho de colunas em maiúsculas: `NAME READY STATUS RESTARTS AGE`, `NAMESPACE NAME ...`, `NAME TYPE CLUSTER-IP EXTERNAL-IP PORT(S) AGE` | valores de `STATUS` do enum (`Running`, `Pending`, `CrashLoopBackOff`) |
| `kubectl describe` | linhas `Name:`, `Namespace:`, `Labels:`, `Annotations:`, `Events:` alinhadas em coluna | `Controlled By:  ReplicaSet/...` (formato `Kind/nome`) |
| `kubectl -o yaml/json` | como manifesto, com `status:`, `uid`, `resourceVersion`, `managedFields` | `"kind": "List"` com `items` |
| Nome DNS de serviço | sufixo `.svc.cluster.local` (ou `.svc.<domínio do cluster>`), `.pod.cluster.local` | forma curta `<svc>.<ns>` dentro de URL/connection string |
| kubeconfig | `apiVersion: v1` + `kind: Config` com `clusters:` / `contexts:` / `users:` | `current-context:` |
| Helm | `Chart.yaml` com `apiVersion: v2` + `name` + `version`; templates com `{{ .Values.` / `{{ .Release.` / `{{ include` | `values.yaml` sem `kind` |
| docker-compose | chave `services:` no topo, com filhos contendo `image:`/`build:`; `networks:`/`volumes:`/`secrets:` no topo | `name:` no topo (nome do projeto) |
| Dockerfile | linha começando com instrução: `FROM`, `RUN`, `COPY`, `ENV`, `ARG`, `WORKDIR`, `ENTRYPOINT`, `CMD`, `LABEL`, `EXPOSE` | `FROM x AS estagio` |
| `docker ps` | cabeçalho `CONTAINER ID IMAGE COMMAND CREATED STATUS PORTS NAMES` | ID hex de 12 caracteres |
| `docker inspect` | array JSON com `"Id"`, `"Config": {"Image", "Env", "Hostname"}`, `"NetworkSettings"` | `"Name": "/nome"` (barra inicial) |

### 2. Posição (caminho do campo) → tipo de entidade

Caminhos em notação do OpenAPI (`[]` = qualquer item da lista). `PodSpec` aparece em
`Pod.spec`, `*.spec.template.spec` (Deployment, StatefulSet, DaemonSet, Job, ReplicaSet) e
`CronJob.spec.jobTemplate.spec.template.spec` — trate todos com o mesmo conjunto de regras.

| Caminho | Entidade | Observação |
|---|---|---|
| `metadata.name` | tipo depende do `kind` (`SVC_`, `DEPLOY_`, `SECRET_`, `CM_`, `SA_`, `STS_`, `JOB_`...) | — |
| `metadata.namespace`, `kind: Namespace` → `metadata.name` | `NS_` | mesmo pseudônimo em todo lugar |
| `metadata.generateName` | prefixo do tipo do `kind` | servidor acrescenta sufixo aleatório |
| `metadata.labels.*`, `spec.selector.matchLabels.*`, `Service.spec.selector.*` | valor: `APP_` (se a chave for `app`, `app.kubernetes.io/name`, `instance`, `part-of`) | a chave com prefixo oficial fica; a chave sem prefixo pode ser privada |
| `metadata.annotations.*` | texto livre → passa pelos outros detectores (URL, host, e-mail) | — |
| `metadata.ownerReferences[].name` | tipo de `ownerReferences[].kind` | `kind` fica |
| `StatefulSet.spec.serviceName` | `SVC_` | deve bater com o Service headless |
| `PodSpec.serviceAccountName`, `ServiceAccount.metadata.name`, `RoleBinding.subjects[].name` (com `kind: ServiceAccount`) | `SA_` | `subjects[].namespace` → `NS_` |
| `RoleBinding.roleRef.name` | `ROLE_` | exceto roles públicas (`cluster-admin`, `admin`, `edit`, `view`) |
| `RoleBinding.subjects[].name` com `kind: User` / `Group` | `USER_` / `GROUP_` | grupos `system:*` são públicos |
| `containers[].name`, `initContainers[].name` | `CTR_` | — |
| `containers[].image`, `docker FROM`, compose `image` | `IMG_` (só registry/repositório privado) | ver regra de imagem na seção 3 |
| `env[].name` | **fica** (nome de variável é estrutura) | mas indica o tipo do valor ao lado |
| `env[].value` | texto livre → detectores de host/URL/DSN/usuário; se `name` termina em `_HOST`, `_USER`, `_DB`, `_DATABASE` → `HOST_`/`USER_`/`DB_` | senha/token: mascarar sempre |
| `env[].valueFrom.secretKeyRef.name`, `envFrom[].secretRef.name`, `volumes[].secret.secretName`, `imagePullSecrets[].name` | `SECRET_` | `.key` é nome de chave: fica |
| `env[].valueFrom.configMapKeyRef.name`, `envFrom[].configMapRef.name`, `volumes[].configMap.name` | `CM_` | — |
| `volumes[].persistentVolumeClaim.claimName`, `volumeClaimTemplates[].metadata.name` | `PVC_` | — |
| `volumes[].name`, `volumeMounts[].name` | `VOL_` | `volumeMounts[].mountPath` = caminho, passa por detector de caminho |
| `Secret.data.*` / `stringData.*` | **valor sempre mascarado** (base64 ou texto) | as chaves podem ficar |
| `ConfigMap.data.*` | texto livre → detectores | — |
| `Ingress.spec.rules[].host`, `spec.tls[].hosts[]` | `HOST_` | — |
| `Ingress.spec.tls[].secretName` | `SECRET_` | — |
| `Ingress ...backend.service.name` | `SVC_` | `port.number` fica |
| `Ingress.spec.ingressClassName` | `CLASS_` ou fica se for público (`nginx`) [VERIFICAR lista] | — |
| `Service.spec.externalName`, `PodSpec.hostname`, `PodSpec.subdomain`, `hostAliases[].hostnames[]` | `HOST_` / `SVC_` (subdomain = nome do Service headless) | `hostAliases[].ip` → detector de IP |
| `PodSpec.nodeName`, `nodeSelector` valores | `NODE_` | chaves `kubernetes.io/hostname` ficam |
| `Service.spec.ports[].name` | fica (costuma ser `http`, `grpc`) | entra no SRV `_http._tcp...` |
| compose `services.<chave>` | `SVC_` | a **chave** do mapa é o nome |
| compose `container_name`, `hostname`, `domainname` | `CTR_`, `HOST_`, `HOST_` | — |
| compose `networks.<chave>`, `volumes.<chave>`, `secrets.<chave>`, `configs.<chave>` (topo e referências) | `NET_`, `VOL_`, `SECRET_`, `CM_` | — |
| compose `depends_on[]`, `links[]` (`SVC[:ALIAS]`), `extra_hosts` (`HOST=IP`) | `SVC_`, `HOST_` | — |
| compose `name` (topo) | `PROJ_` | — |
| Helm `Chart.yaml`: `name`, `dependencies[].name`, `dependencies[].repository` | `CHART_`, URL → detector | chart público de repo público pode ficar |
| `Ingress ...backend.serviceName` (v1beta1), `PodSpec.serviceAccount` (nome antigo) | `SVC_`, `SA_` | — |
| `labels` / `matchLabels` / `selector`: valor de `app.kubernetes.io/name`, `app.kubernetes.io/instance`, `app.kubernetes.io/part-of` | `SVC_` (evidência fraca: mascara no lugar, não ensina sozinho) | as outras labels ficam |
| kubeconfig: `clusters[].name`, `contexts[].name`, `contexts[].context.cluster`, `current-context` | `HOST_` | nome em forma de ARN (`arn:aws:eks:<região>:<conta>:cluster/<nome>`): só a conta (`ACC_`) e o nome final |
| kubeconfig: `users[].name`, `contexts[].context.user` | `USR_` | — |
| kubeconfig: `contexts[].context.namespace` | `NS_` | — |
| kubeconfig: `clusters[].cluster.server` | host da URL → `HOST_` | esquema, porta e caminho ficam |
| nome DNS `<svc>.<ns>.svc.cluster.local` (em qualquer texto) | `SVC_` + `NS_` | o sufixo fica; o rótulo antes do serviço (pod de StatefulSet) fica |

Na implementação os tipos acima caem nas entidades da base: `metadata.name` de `Namespace` → namespace,
de `ServiceAccount` → usuário, dos outros `kind` → serviço; `SECRET_`, `CM_`, `PVC_`, `SA_` → serviço/usuário;
`IMG_` → registro como servidor e cada pedaço do caminho como serviço (a tag e o digest ficam). O
manifesto só é reconhecido com `apiVersion` (na forma `grupo/vN`) e `kind` no mesmo objeto; dentro de
`{{ }}` nada é tocado.
| `kubectl get`: colunas `NAME`, `NAMESPACE`, `NODE`, `NOMINATED NODE` | conforme o recurso pedido | `READY`, `STATUS`, `AGE`, `TYPE` ficam |
| `docker ps`: `NAMES`, `IMAGE` | `CTR_`, `IMG_` | `CONTAINER ID` é hash: fica ou vira `ID_` |

### 3. Vocabulário público (o que nunca se mascara)

| Vocabulário | Exemplos | Como extrair |
|---|---|---|
| `kind` | `Deployment`, `Service`, `Secret`, `CronJob`, `RoleBinding`... | `x-kubernetes-group-version-kind[].kind` de cada definição |
| `apiVersion` | `v1`, `apps/v1`, `batch/v1`, `networking.k8s.io/v1`, `rbac.authorization.k8s.io/v1` | mesmo campo: `group` + `/` + `version` (grupo vazio = core → só `v1`) |
| enums | `ClusterIP`, `NodePort`, `LoadBalancer`, `ExternalName`, `Always`, `IfNotPresent`, `Never`, `OnFailure`, `TCP`, `UDP`, `Opaque`, `kubernetes.io/tls`, `Prefix`, `Exact` | no OpenAPI v3, propriedades com `enum` (ou descrição "Possible enum values") |
| status de kubectl | `Running`, `Pending`, `Succeeded`, `Failed`, `CrashLoopBackOff`, `ImagePullBackOff`, `Completed` | `PodStatus.phase` (enum) + motivos de `containerStatuses[].state.waiting.reason` [VERIFICAR lista fechada] |
| nomes de campo | toda chave de propriedade (`spec`, `template`, `containers`, `secretKeyRef`...) | chaves de `properties` em todas as definições |
| prefixos de label/annotation | `kubernetes.io/`, `k8s.io/` (reservados ao core), `app.kubernetes.io/` (labels recomendadas: `name`, `instance`, `version`, `component`, `part-of`, `managed-by`), `helm.sh/chart`, `com.docker.compose.*` (reservado pelo Compose) | doc "Well-Known Labels, Annotations and Taints" + "Recommended Labels" |
| objetos do sistema | namespaces `default`, `kube-system`, `kube-public`, `kube-node-lease`; SA `default`; ClusterRoles `cluster-admin`, `admin`, `edit`, `view`; grupos `system:*` | doc de namespaces e de RBAC |
| domínio do cluster | `cluster.local`, `svc`, `pod` | doc DNS for Services and Pods |
| Dockerfile / Compose | instruções (`FROM`, `RUN`...) e chaves da spec Compose | referência do Dockerfile e compose-spec |

**Extração do OpenAPI** (uma vez, gerar lista embutida no binário Go):
1. Baixar `api/openapi-spec/v3/*.json` (um arquivo por grupo/versão) do repositório oficial.
2. Para cada `components.schemas.*`: ler `x-kubernetes-group-version-kind` → `kinds` e `apiVersions`;
   ler `properties` → nomes de campo; ler `enum` → valores fixos.
3. Marcar como **campo de referência** as propriedades cujo nome é `name`, `namespace`, `secretName`,
   `claimName`, `serviceName`, `serviceAccountName`, `host`, `hosts`, `hostname`, `subdomain`,
   `image`, `externalName`, `nodeName` — e conferir à mão o caminho completo (a tabela da seção 2).
   `name` sozinho não basta: `ports[].name` e `env[].name` são estrutura.
4. CRDs (`kind` fora da lista) → tratar `metadata.*` com as mesmas regras; o resto vira texto livre.

**Imagens públicas.** Não manter lista de imagens. Regra estrutural: a parte antes da primeira `/`
é registry se tiver `.` ou `:` ou for `localhost`. Sem registry ⇒ Docker Hub (público: `nginx:1.27`,
`postgres:16` ficam). Registry público conhecido e pequeno (`docker.io`, `registry.k8s.io`, `ghcr.io`,
`quay.io`, `gcr.io`, `mcr.microsoft.com`, `public.ecr.aws`) ⇒ fica o registry, e o repositório fica
só se for oficial do projeto [VERIFICAR: `ghcr.io/<org>` pode ser privado]. Qualquer outro host ⇒
registry privado: mascarar host e repositório, **manter tag e digest** (versão não é segredo e ajuda o diagnóstico).
Formato de referência: `[<registry>/][<project>/]<image>[:<tag>|@<digest>]`.

### 4. Regras de identificador (sinal e restrição)

| Regra | Tamanho | Caracteres | Onde |
|---|---|---|---|
| DNS-1123 subdomain | ≤ 253 | `a-z 0-9 - .`, começa e termina alfanumérico | maioria dos `metadata.name` (Deployment, Secret, ConfigMap, SA) |
| RFC 1123 label | ≤ 63 | `a-z 0-9 -`, começa com letra (doc atual), termina alfanumérico | `Namespace`, `containers[].name`, `hostname` |
| RFC 1035 label | ≤ 63 | idem, começa com letra | `Service` (pode começar com dígito com `RelaxedServiceNameValidation`) |
| Valor de label | ≤ 63, pode ser vazio | `A-Za-z0-9 - _ .`, começa/termina alfanumérico | `labels.*` |
| Compose projeto | — | `a-z 0-9 - _`, começa com letra/dígito | `name`, `-p` |
| Compose `container_name` | — | `[a-zA-Z0-9][a-zA-Z0-9_.-]+` | — |

Regexes de validação do apimachinery: label `[a-z0-9]([-a-z0-9]*[a-z0-9])?`, subdomain
`label(\.label)*`, 1035 `[a-z]([-a-z0-9]*[a-z0-9])?`.

Como usar: (a) **sinal** — valor em campo de referência que casa com DNS-1123 reforça a detecção;
valor que não casa (tem maiúscula, espaço) indica que o campo é outro ou é template. (b) **restrição
do pseudônimo** — o pseudônimo precisa continuar válido no mesmo campo. Por isso a forma
`NS_wbk2i5of` (maiúscula e `_`) **não serve dentro do YAML**: use a forma minúscula com hífen
`ns-wbk2i5of`, `svc-n3dsajwo`, `host-scsrx4tu`, mantendo o prefixo de tipo. A forma `NS_wbk2i5of` fica para
texto livre fora de campo validado. Mapear as duas formas para o mesmo original.
Comprimento do pseudônimo ≤ original quando o limite apertar (63).

### 5. Exemplos antes/depois (dados fictícios)

```yaml
# ANTES
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
# DEPOIS
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

Ficou: `apiVersion`, `kind`, chaves, `api` (nome genérico curto [VERIFICAR política]), `IfNotPresent`,
`DB_HOST`, `password`, `svc.cluster.local`, a tag `1.2`. O namespace dentro do FQDN recebeu o
**mesmo** `ns-wbk2i5of` do `metadata.namespace`.

Saída de `kubectl get pods -n ns-financeiro-demo`:

```
ANTES                                                   DEPOIS
NAME                              READY STATUS  AGE     NAME                     READY STATUS  AGE
svc-pedidos-x9-0                  1/1   Running 3d      svc-n3dsajwo-0               1/1   Running 3d
api-demo-7d9f8b6c5d-x2k8q         0/1   CrashLoopBackOff  svc-7eygw6sy-7d9f8b6c5d-x2k8q 0/1 CrashLoopBackOff
```

docker-compose: `services: { svc-pedidos-x9: { image: registry.exemplo.interno/app-demo:1.2, depends_on: [db-demo] } }`
→ `services: { svc-n3dsajwo: { image: host-scsrx4tu/svc-3acw37uo:1.2, depends_on: [svc-jgzcjsnq] } }`.

### 6. Casos difíceis e limites

- **Referências cruzadas.** O mesmo nome aparece em `metadata.name` do Service, `spec.serviceName`,
  `backend.service.name`, `subdomain`, FQDN `x.ns.svc.cluster.local`, `depends_on`, coluna `NAME`.
  O mapa original→pseudônimo é **derivado por HMAC da chave do llm-dlp, por tipo** (o mesmo em todas as conversas e depois de reinícios); um mesmo texto em tipos diferentes
  (Service e Deployment chamados `svc-pedidos-x9`) deve receber pseudônimos *ligados* — sugestão:
  mesma raiz com prefixo diferente, ou mesma string se o prefixo for neutro [VERIFICAR decisão].
- **Sufixos gerados.** Pod de Deployment = `<deployment>-<pod-template-hash>-<5 caracteres>`;
  StatefulSet = `<sts>-<ordinal>`; Job/CronJob = `<cronjob>-<carimbo>-<sufixo>`; `generateName` = prefixo +
  aleatório; Compose v2 = `<projeto>-<serviço>-<n>`. Reconhecer a raiz conhecida, mascarar só a raiz e
  **preservar o sufixo** (não é sensível e mantém a correlação entre linhas de log).
  Se a raiz ainda não foi vista, cortar sufixos de forma estrutural (`-[a-z0-9]{5}$`, `-[0-9a-f]{8,10}`, `-[0-9]+$`) é heurística: pode cortar demais.
- **Templates Helm.** `{{ .Values.x }}`, `{{ include "chart.fullname" . }}` não são valores: não mascarar
  o que está entre `{{ }}`; o YAML do template nem é YAML válido antes do render. Mascarar o literal
  ao redor (`name: {{ .Release.Name }}-pedidos-x9`) só no trecho fora da chave. Em `values.yaml` não
  há `kind`: o caminho não tem esquema → usar o nome da chave (`image.repository`, `ingress.hosts[].host`,
  `serviceAccount.name`, `existingSecret`) como sinal fraco e cair nos detectores de texto livre.
- **Base64 em Secret.** `data.*` vem em base64 e pode conter DSN/senha; mascarar o valor inteiro (não decodificar para mostrar).
- **`kubectl describe` e logs.** Nomes aparecem em prosa (`Successfully assigned ns/pod to node`,
  `Pulling image "..."`). Formato `ns/nome` e `Kind/nome` (`ReplicaSet/api-demo-7d9f8b6c5d`) são sinais estruturais.
- **Strings curtas genéricas.** `api`, `web`, `db`, `worker` em `containers[].name` ou chave de compose:
  mascarar por posição é coerente, mas atrapalha pouco deixá-las [VERIFICAR política].
- **Alinhamento de colunas.** Pseudônimo de tamanho diferente desalinha `kubectl get`/`docker ps`; para o LLM tanto faz; não reformatar.
- **Volta (desmascarar).** Resposta do LLM pode trazer `svc-n3dsajwo` dentro de comando novo; trocar de volta só tokens inteiros que estão no mapa.
- **CRDs e operadores** (Argo, cert-manager etc.): `kind` fora da lista oficial; só `metadata` tem regra garantida.

### 7. Links usados

- https://kubernetes.io/docs/concepts/overview/working-with-objects/names/
- https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/
- https://kubernetes.io/docs/concepts/overview/working-with-objects/common-labels/
- https://kubernetes.io/docs/reference/labels-annotations-taints/
- https://kubernetes.io/docs/concepts/services-networking/dns-pod-service/
- https://github.com/kubernetes/kubernetes/tree/master/api/openapi-spec (README, `swagger.json`, `v3/`)
- https://docs.docker.com/reference/compose-file/services/
- https://docs.docker.com/compose/how-tos/project-name/
- https://helm.sh/docs/topics/charts/ (Chart.yaml) e https://helm.sh/docs/chart_template_guide/ (templates) — não consultados nesta rodada [VERIFICAR]
- https://docs.docker.com/reference/dockerfile/ — não consultado nesta rodada [VERIFICAR]


## Infraestrutura como código

Os arquivos de IaC já vêm estruturados. Os nomes reais (servidor, banco, bucket, rede, conta) ficam em
**posições previsíveis**: o valor de certos atributos dentro de blocos de recurso. Todo o resto é
vocabulário público dos providers: tipos de recurso, nomes de argumentos e palavras-chave da linguagem.
A regra geral é simples: **chave e tipo nunca se mascaram; só se mascara o valor literal de um
atributo-nome**.

### 1. Sinais de detecção

| Formato | Sinais estruturais (bastam 2 ou mais) |
|---|---|
| Terraform/HCL (`.tf`, `.tfvars`) | linha começa com `resource "x" "y" {`, `data "x" "y" {`, `variable "x" {`, `module "x" {`, `output "x" {`, `locals {`, `provider "x" {`, `terraform {`; atribuição `ident = valor`; interpolação `${...}`; referências `var.`, `local.`, `module.`, `data.` |
| Saída de `terraform plan`/`apply` | `# <endereço> will be created` / `will be updated in-place` / `must be replaced`; linhas com prefixo `+`, `-`, `~`, `-/+`, `<=`; `-> ` entre valor antigo e novo; `(known after apply)`; `Plan: N to add, N to change, N to destroy.` |
| State/plan JSON (`terraform show -json`) | chaves `format_version`, `terraform_version`, `resource_changes[].address`, `change.actions`, `before`/`after`, `values.root_module.resources[]` |
| Ansible playbook | lista YAML com `hosts:`, `tasks:`, `become:`, `roles:`, `vars:`; módulos FQCN `ansible.builtin.*`, `community.*`; `{{ var }}` (Jinja2) |
| Ansible inventário INI | `[grupo]`, `[grupo:vars]`, `[grupo:children]`; linhas `host ansible_host=... ansible_user=...`; faixas `www[01:50]` |
| Ansible inventário YAML | raiz `all:` com `hosts:` / `children:` / `vars:`; caminhos `host_vars/<host>` e `group_vars/<grupo>` |
| CloudFormation (YAML/JSON) | `AWSTemplateFormatVersion`, `Resources:`, `Type: AWS::Serviço::Recurso`, `Properties:`, `Parameters:`, `Outputs:`; tags `!Ref`, `!GetAtt`, `!Sub`, `!Join` ou `Fn::*` |
| ARM template (JSON) | `$schema` com `deploymentTemplate.json`, `contentVersion`, `resources[]` com `type: "Microsoft.X/y"`, `apiVersion`, `name`; expressões `"[parameters('x')]"`, `"[concat(...)]"` |
| Bicep (`.bicep`) | `resource <símbolo> 'Microsoft.X/y@AAAA-MM-DD' = {`, `param`, `var`, `module`, `existing`, interpolação `'${x}'` |

### 2. Posição → tipo de entidade

| Formato | Posição | Entidade | Mascarar? |
|---|---|---|---|
| HCL | 1º rótulo de `resource "aws_db_instance"` | tipo (vocabulário) | não |
| HCL | 2º rótulo `"relatorios"` | nome local, só existe no código | opcional (ver item 6) |
| HCL | `identifier`, `db_name`, `database_name` | DB | sim |
| HCL | `bucket` em `aws_s3_bucket` / `name` em `google_storage_bucket` | BKT | sim |
| HCL | `name` em `azurerm_mssql_server`, `server_name`, `host`, `endpoint`, `address` | HOST/SRV | sim |
| HCL | `name` em VPC/VNet/subnet, `network`, `vpc_id` literal | NET | sim |
| HCL | `account_id`, `project`, `account`, `username`, `user` | ACC/USR | sim |
| HCL | `cluster_name`, `cluster_identifier`, `instance_name` | HOST | sim |
| HCL | `namespace`, `topic`, `queue_name`, `subscription`, `repository`, `dataset_id`, `table_id`, `organization`, `folder`, `function_name` | NS / TOP / REPO / SCH / T / ORG / DIR / SVC | sim |
| HCL | `account_id` de 12 dígitos (AWS) | ACC | sim; outros números puros ficam |
| HCL | `provider "x" { project = ... }`, `backend "x" { bucket = ... }` | ACC / BKT | sim |
| HCL | `tags { Name = ... }` / `tags = { Name = ... }` | tipo do recurso | sim, evidência fraca |
| HCL | valor com `${...}` | tipo do atributo | só os pedaços literais, evidência fraca |
| HCL | `default` de `variable` cujo nome sugere a entidade (`db_name`) | herda o tipo do uso | sim |
| plan texto | valor à direita de `=`, nos dois lados de `->` | tipo do atributo | sim, os dois lados |
| plan/state JSON | `before.<attr>`, `after.<attr>`, `values.<attr>` | tipo do atributo | sim |
| Ansible INI | 1º token da linha, sob `[grupo]` | HOST | sim |
| Ansible INI/YAML | `ansible_host`, `ansible_user`, `delegate_to` | HOST / USR | sim |
| Ansible YAML | chaves filhas de `hosts:` (mapa) no inventário | HOST | sim |
| Ansible playbook | `hosts: <padrão>` de um play (com `tasks`/`roles`/`become`...) | HOST | sim, evidência fraca (`all`, `localhost` ficam) |
| Ansible | nome do grupo `[dbservers]` | rótulo interno | opcional |
| Ansible | parâmetros `name`/`login_host`/`login_user`/`db` de módulos de banco (ex.: `community.mysql.mysql_db`) | DB / HOST / USR | sim |
| Ansible | nome de arquivo em `host_vars/<host>.yml` | HOST | sim (o caminho também vaza) |
| CFN | chave lógica em `Resources:` (`BancoRelatorios:`) | nome lógico | opcional |
| CFN | `DBInstanceIdentifier`, `DBName`, `DatabaseName`, `BucketName`, `TableName`, `DBClusterIdentifier`, `MasterUsername`, `UserName`, `RoleName`, `GroupName` | DB / BKT / T / USR | sim |
| CFN | `ServerName`, `ClusterName` / `QueueName`, `TopicName`, `StreamName` / `FunctionName`, `ServiceName` / `RepositoryName` | HOST / TOP / SVC / REPO | sim; `!Ref`/`!GetAtt` ficam, em `!Sub` só o literal fora de `${}` |
| CFN | `Default` de `Parameters` usado nessas propriedades | herda | sim |
| ARM | `resources[].name` | conforme `type` | sim |
| ARM / Bicep | nome de recurso filho `pai/filho` (`Microsoft.Sql/servers/databases`) | um pedaço por tipo: HOST / DB | sim |
| Bicep | símbolo depois de `resource` | nome local | opcional |
| Bicep | `name:` dentro do corpo | conforme o tipo | sim |

A entidade sai do **par (tipo de recurso, nome do atributo)**, nunca do texto do valor. Por isso
`name` em `aws_s3_bucket` não existe, `name` em `google_storage_bucket` é BKT e `name` em
`azurerm_mssql_database` é DB.

### 3. Vocabulário público: fonte e como extrair

| Formato | Vocabulário | Fonte oficial | Extração |
|---|---|---|---|
| HCL (linguagem) | palavras de bloco (`resource`, `data`, `variable`, `locals`, `module`, `output`, `provider`, `terraform`, `moved`, `import`, `check`), `var`, `local`, `each`, `count`, `self`, `path`, funções nativas | spec HCL (github.com/hashicorp/hcl) e Terraform Language docs | lista fixa e pequena |
| Terraform providers | tipos de recurso e data source, com os argumentos de cada um | schema dos providers | `terraform providers schema -json` → `provider_schemas[<p>].resource_schemas[<tipo>].block.attributes` (e `block_types` para blocos aninhados) |
| CloudFormation | `AWS::Serviço::Recurso`, nomes de `Properties` e `Attributes` | Resource specification (um JSON por região) ou resource provider schemas (zip por região, JSON Schema draft-07) | `ResourceTypes.<tipo>.Properties` (spec) ou `properties` de cada schema; `aws cloudformation describe-type --type RESOURCE --type-name AWS::RDS::DBInstance` |
| CFN intrínsecas | `Ref`, `Fn::GetAtt`, `Fn::Sub`, `Fn::Join`, `Fn::If`… e as formas curtas `!` | Intrinsic function reference | lista fixa |
| ARM/Bicep | `Microsoft.<Provider>/<tipo>`, `apiVersion`, propriedades | Azure resource reference (learn.microsoft.com/azure/templates) e schemas JSON publicados | `az provider list --query "[].{ns:namespace,t:resourceTypes[].resourceType}"` para os tipos; propriedades pelo schema JSON de cada `apiVersion` |
| Ansible | chaves de play/task (`hosts`, `tasks`, `vars`, `become`…), variáveis `ansible_*`, módulos e parâmetros | docs.ansible.com (Playbook keywords, connection vars, coleções) | `ansible-doc -l` (módulos); `ansible-doc -j <módulo>` → `doc.options` (parâmetros) |

**Como escolher os atributos-nome sem dicionário:** pegue do schema os atributos `string` que não são
`computed` e que batem com um padrão técnico (`name`, `*_name`, `identifier`, `*_identifier`,
`bucket`, `host`, `*_host`, `endpoint`, `server*`, `account*`, `project`, `user*`, `*Name`,
`*Identifier`). Depois revise essa lista à mão uma única vez. O resultado é uma tabela
`(tipo, atributo) → entidade`, que vai versionada no repositório. `description` e `sensitive` do schema
ajudam na revisão: atributos com `sensitive: true` (senhas) entram em outro tipo, SEGREDO.

### 4. Regras de identificador

- **Interpolação HCL `${...}`**: dentro de uma string, o trecho literal se mascara e a expressão não.
  Em `"${var.prefixo}-relatorios-demo"`, `var.prefixo` fica como está (é referência). O sufixo
  literal `-relatorios-demo` só se mascara se o atributo estiver na tabela; nesse caso o pseudônimo
  substitui a string inteira e a expressão fica preservada: `"${var.prefixo}-BKT_sfdst7tg"`.
  O escape `$${` é texto literal e não abre interpolação.
- **Referências `tipo.nome.atributo`** (`aws_db_instance.relatorios.address`,
  `data.aws_vpc.principal.id`, `module.rede.subnet_ids`, `var.x`, `local.y`): é um endereço de
  código e não aparece como dado. Só o segmento `nome` pode ser trocado, e só se a opção
  "mascarar nome local" estiver ligada. Nesse caso a troca tem que ser consistente em todo o arquivo.
- **Jinja2 do Ansible `{{ x }}`**: mesma lógica. A variável fica, e o valor dela em
  `vars`/`group_vars` é que se mascara.
- **CFN**: `!Ref Param` e `!GetAtt Logico.Endpoint.Address` apontam para nomes lógicos. Em
  `!Sub 'arn:aws:s3:::${BucketRelat}/*'`, `${BucketRelat}` é referência; o texto literal fora
  do `${}` segue a regra do atributo.
- **ARM `[...]`**: string que começa com `[` é expressão (`"[parameters('sqlName')]"`) e não se
  mascara. Começar com `[[` é escape para literal. Bicep: `'${x}'` funciona como no HCL.
- **Restrições de nome por provedor** (o pseudônimo tem que obedecer, senão `validate` ou `plan`
  falham):

| Recurso | Restrição oficial | Forma do pseudônimo |
|---|---|---|
| Bucket S3 | 3–63 caracteres, minúsculas, dígitos, `.` e `-` | `bkt-sfdst7tg` |
| RDS `DBInstanceIdentifier` / `identifier` | 1–63 caracteres, letras, dígitos e `-`; começa com letra; sem `--` nem `-` no fim | `db-gbzw6njk` |
| Azure Storage account | 3–24 caracteres, só minúsculas e dígitos, único global | `bktsfdst7tgimuu` |
| Azure SQL server | minúsculas, dígitos e `-`, sem `-` nas pontas, único global | `host-7rhd4fsp` |
| Hostname DNS (Ansible) | rótulos de letras, dígitos e `-`; `_` é inválido | `host-7rhd4fsp` |

  Regra prática: o pseudônimo canônico é `TIPO_sufixo` (ex.: `BKT_sfdst7tg`). Quando o atributo tem
  restrição, o proxy emite uma **forma derivada** (`bkt-sfdst7tg`, `bktsfdst7tgimuu`) e guarda no mapa reverso
  as duas formas apontando para o mesmo original.

### 5. Exemplos antes/depois

**Terraform**
```hcl
# antes
resource "aws_s3_bucket" "relatorios" {
  bucket = "bkt-relatorios-demo"
}
resource "aws_db_instance" "principal" {
  identifier = "db-exemplo-01"
  db_name    = "vendas_demo"
  username   = "app_demo"
}
# depois
resource "aws_s3_bucket" "relatorios" {
  bucket = "bkt-sfdst7tg"
}
resource "aws_db_instance" "principal" {
  identifier = "db-gbzw6njk"
  db_name    = "DB_wzm3mqvx"
  username   = "USR_uu7ojlpg"
}
```

**Ansible (inventário INI)**
```ini
# antes                                    # depois
[dbservers]                                [dbservers]
db-exemplo-01 ansible_host=10.0.0.5        host-7rhd4fsp ansible_host=IP_r9d3
```

**CloudFormation**
```yaml
# antes
Resources:
  BancoRelatorios:
    Type: AWS::RDS::DBInstance
    Properties:
      DBInstanceIdentifier: db-exemplo-01
  Arquivos:
    Type: AWS::S3::Bucket
    Properties:
      BucketName: !Sub 'bkt-relatorios-demo-${AWS::Region}'
# depois
      DBInstanceIdentifier: db-gbzw6njk
      BucketName: !Sub 'bkt-sfdst7tg-${AWS::Region}'
```

**Bicep**
```bicep
// antes
resource sql 'Microsoft.Sql/servers@2023-08-01' = {
  name: 'sqlsrv-exemplo-01'
  location: location
}
// depois
resource sql 'Microsoft.Sql/servers@2023-08-01' = {
  name: 'host-7rhd4fsp'
  location: location
}
```

**Saída de plan**
```text
# antes
  ~ resource "aws_db_instance" "principal" {
      ~ identifier = "db-exemplo-01" -> "db-exemplo-02"
      + address    = (known after apply)
# depois
  ~ resource "aws_db_instance" "principal" {
      ~ identifier = "db-gbzw6njk" -> "db-6rosk7bp"
      + address    = (known after apply)
```

### 6. Casos difíceis e limites

- **Nome local vs nome real.** `"relatorios"` (HCL), `BancoRelatorios` (CFN) e `sql` (Bicep) são
  nomes de código; o nome real do recurso é o valor de `bucket`, `identifier` ou `name`. O nome
  local costuma vazar o assunto mas não o recurso. Padrão: **não mascarar**, para o modelo conseguir
  ler e corrigir o código. Opção configurável: mascarar com consistência total (declaração, todas as
  referências `tipo.nome.*`, `!Ref`/`!GetAtt` e o endereço no plan `aws_s3_bucket.relatorios`).
- **Interpolação que monta o nome.** `"${local.amb}-${var.app}-db"` não tem nome literal; o nome real
  só existe depois da avaliação. O que se mascara é a origem do valor: `default` da variable,
  `locals`, `.tfvars`. No arquivo isolado, o literal solto (`-db`) é genérico e não se mascara.
  Limite: o nome completo aparece de novo, já montado, na saída de plan e no state, e lá é pego pelo
  atributo.
- **Plan com `~`, `+`, `-/+`.** Os dois lados de `->` são mascarados, cada um com seu pseudônimo
  (valores diferentes geram pseudônimos diferentes). `(known after apply)`, `(sensitive value)` e
  `null` são vocabulário. O endereço depois de `#` segue a regra do nome local. Em `-/+` com
  `# forces replacement`, o comentário fica.
- **Atributo genérico `name`.** Sem o tipo do recurso não se sabe a entidade. Dentro de blocos
  aninhados (`tags { Name = ... }`, `sku { name = "Standard_LRS" }`) muitas vezes o valor é
  vocabulário (SKU, tier). A tabela do item 3 resolve isso pelo caminho completo
  `(tipo, bloco, atributo)`, não pelo nome solto.
- **Tags `Name`.** São texto livre, mas na prática repetem o nome real. Tratar `tags.Name` como do
  mesmo tipo do recurso. `[VERIFICAR]` se vale estender para outras tags.
- **Valor que já é referência a outro recurso** (`vpc_id = aws_vpc.principal.id`): não mascarar.
  Se for um ID literal (`host-bakbyhxo…`, ARN), ele cai na família de IDs/ARN, não nesta.
- **ARN e connection strings dentro de valores** (`arn:aws:rds:…:db:db-exemplo-01`,
  `Server=tcp:sqlsrv-exemplo-01.database.windows.net`): o segmento final se mascara com o **mesmo**
  pseudônimo do recurso, e o sufixo DNS público do provedor fica.
- **Ansible**: o host aparece como chave em YAML (`hosts: { db-exemplo-01: … }`), como token em INI
  e como nome de arquivo em `host_vars/`. Os três precisam do mesmo pseudônimo. Faixas
  `db[01:03].exemplo` se mascaram como unidade (o padrão inteiro vira um pseudônimo). Expandir a
  faixa geraria pseudônimos independentes e quebraria o padrão.
- **Restrições que o pseudônimo não cumpre.** `HOST_7rhd4fsp` tem `_` e maiúscula, então é inválido em
  bucket, storage account e hostname. Sem a forma derivada do item 4, o arquivo continua
  sintaticamente válido mas falha no `validate` do provider. Isso não é problema se o arquivo só vai
  para o LLM, mas é problema se a resposta for aplicada sem reverter o mascaramento.
- **Limites.** Providers de terceiros e módulos privados ficam fora do schema; atributo desconhecido
  vira `[VERIFICAR]` e não é mascarado às cegas. HEREDOC (`<<EOT`) e `jsonencode()` com política
  inline exigem uma segunda passada com o reconhecedor de JSON/SQL. Templates Jinja2 com lógica
  (`{% for %}`) só são parseados parcialmente.

### 7. Links usados

- Terraform `providers schema -json`: https://developer.hashicorp.com/terraform/cli/commands/providers/schema
- Terraform JSON output (plan/state): https://developer.hashicorp.com/terraform/internals/json-format
- Sintaxe de recursos Terraform: https://developer.hashicorp.com/terraform/language/resources/syntax
- Spec nativa do HCL: https://github.com/hashicorp/hcl/blob/main/hclsyntax/spec.md
- Inventário Ansible: https://docs.ansible.com/ansible/latest/inventory_guide/intro_inventory.html
- Playbook keywords: https://docs.ansible.com/ansible/latest/reference_appendices/playbooks_keywords.html
- CloudFormation resource specification: https://docs.aws.amazon.com/AWSCloudFormation/latest/TemplateReference/cfn-resource-specification.html
- CloudFormation resource provider schemas: https://docs.aws.amazon.com/AWSCloudFormation/latest/TemplateReference/resource-type-schemas.html
- Funções intrínsecas CFN: https://docs.aws.amazon.com/AWSCloudFormation/latest/TemplateReference/intrinsic-function-reference.html
- Regras de nome de bucket S3: https://docs.aws.amazon.com/AmazonS3/latest/userguide/bucketnamingrules.html
- Bicep, declaração de recurso: https://learn.microsoft.com/azure/azure-resource-manager/bicep/resource-declaration
- Estrutura de ARM template: https://learn.microsoft.com/azure/azure-resource-manager/templates/syntax
- Regras de nome de recursos Azure: https://learn.microsoft.com/azure/azure-resource-manager/management/resource-name-rules
- Referência de tipos Azure: https://learn.microsoft.com/azure/templates/


## Ferramentas de dados

Ideia central: nestas ferramentas o nome de objeto **sempre ocupa uma posição fixa** (uma chave YAML/JSON,
um argumento de função, um atributo XML). A regra é: reconhecer a ferramenta → localizar a posição → mascarar
o **valor**, nunca a chave. As chaves, nomes de função e enums são vocabulário público e ficam intactos.
Trechos de SQL embutido (`sql=`, `op.execute(...)`, corpo do Flyway, `compiled_code`) vão para o mascarador de SQL.

Pseudônimos tipados: `DB_`, `SCH_`, `T_`, `C_`, `CONN_`, `JOB_` (DAG/job/task), `INST_` (platform_instance).
Formato `PREFIXO_[a-z0-9]{4}` — só `[A-Za-z0-9_]`, então é identificador válido sem aspas, chave Python/YAML
válida e **literal seguro dentro de regex** (não tem metacaractere).

### 1. Sinais de detecção

| Ferramenta | Sinal estrutural (basta 1 forte ou 2 fracos) |
|---|---|
| dbt | Jinja `{{ ref(` / `{{ source(` / `{{ config(`; YAML com raiz `models:`/`sources:`/`seeds:` contendo `columns:` + `data_tests:`/`tests:`; `dbt_project.yml` (`name`, `profile`, `model-paths`); `profiles.yml` (`target:` + `outputs:` + `type:`); JSON com `nodes`+`parent_map`+`child_map`; `unique_id` no formato `model.<pkg>.<nome>` |
| Airflow | `DAG(` / `@dag(` com `dag_id=`; `Operator(` com `task_id=`; `conn_id=`/`*_conn_id=`; `Variable.get(`, `BaseHook.get_connection(`; env `AIRFLOW_CONN_<ID>`; URI `tipo://login@host:porta/schema?extra`; logs com `dag_id=… task_id=… run_id=…` |
| DataHub | YAML com `source:` → `type:` + `config:` e opcional `sink:`/`transformers:`; chaves `*_pattern:` com `allow:`/`deny:`; texto `urn:li:` |
| OpenLineage | JSON com `eventType` ∈ {START, RUNNING, COMPLETE, ABORT, FAIL, OTHER} + `run.runId` + `job.namespace`/`job.name` + `producer`/`schemaURL` |
| Great Expectations | `import great_expectations as gx`; classes `Expect[A-Z]\w+(` ou métodos `expect_\w+(`; `add_table_asset(`, `add_query_asset(`, `data_sources.add_<tipo>(`, `batch_request` |
| Liquibase | XML `<databaseChangeLog>`/`<changeSet>`; YAML/JSON `databaseChangeLog:` → `changeSet:` → `changes:`; atributos `tableName`, `columnName` |
| Flyway | nome de arquivo `^[VUR]\d*[._\d]*__\w+\.sql$` (ex.: `V001.002__X.sql`, `R__X.sql`) |
| Alembic | `from alembic import op`; `op.create_table(`, `op.add_column(`, `revision = '…'` + `down_revision` |

### 2. Posição → tipo de entidade

**dbt**

| Posição | Tipo |
|---|---|
| `ref('m')` / `ref('pkg','m')` / `ref('m', v=2)` — último literal string | T (modelo) |
| `source('s','t')` — 1º arg / 2º arg | SCH (nome lógico da source) / T |
| `config(database=, schema=, alias=)` | DB / SCH / T |
| YAML `sources[].name` / `.database` / `.schema` / `.tables[].name` / `.tables[].identifier` | SCH / DB / SCH / T / T |
| YAML `models[].name`, `seeds[].name`, `snapshots[].name` | T |
| `columns[].name`; `arguments.column_name`, `arguments.field` (relationships) | C |
| `arguments.to: ref('x')` | T (via Jinja) |
| `profiles.yml` → `database`, `dbname`, `schema`, `warehouse`, `role` | DB / DB / SCH / [VERIFICAR tipo: recurso] |
| `manifest.json` nós: `name`, `alias`, `database`, `schema`, `relation_name`, `columns.<k>.name`; chaves `unique_id` (`model.pkg.nome`) | T/T/DB/SCH/FQN/C/último segmento T |
| saída `dbt ls` (`pkg.pasta.modelo`, `source:s.t`) | último segmento T; em `source:` SCH.T |

**Airflow**

| Posição | Tipo |
|---|---|
| `dag_id=`, `task_id=`, `@dag` nome da função | JOB |
| `conn_id=`, `*_conn_id=` (ex.: `postgres_conn_id`), sufixo de `AIRFLOW_CONN_<ID>` | CONN |
| `database=`, `schema=`, `table=` de operadores | DB / SCH / T |
| `sql=` | SQL → mascarador SQL |
| URI de conexão: caminho `/schema` | SCH (host fica com a família de rede) |
| `Variable.get("chave")` | [VERIFICAR] — chave pode ser sensível; tipo `VAR_` |

**DataHub**

| Posição | Tipo |
|---|---|
| `source.config.database`, `database_pattern`, `schema_pattern`, `table_pattern`, `view_pattern` (`allow`/`deny`) | DB / regex DB / regex SCH / regex FQN / regex FQN |
| `platform_instance` | INST |
| `source.config.host_port` | host → família de rede |
| URN `urn:li:dataset:(urn:li:dataPlatform:<plat>,<nome>,<FABRIC>)` | `<nome>` = FQN; `<plat>` e `<FABRIC>` públicos |

**OpenLineage**

| Posição | Tipo |
|---|---|
| `job.namespace` / `job.name` | INST / JOB |
| `inputs[]`/`outputs[]` `.namespace` | `esquema://host:porta` (esquema público, host → rede); `bigquery`, `file` públicos |
| `inputs[]`/`outputs[]` `.name` | FQN: dividir por `.` conforme a plataforma (abaixo) |
| `facets.schema.fields[].name` (recursivo em `.fields`) | C |

Divisão do `name` (spec de nomes): Postgres/MSSQL/Redshift/Snowflake `db.schema.tabela`; Trino `catalog.schema.tabela`;
MySQL/Hive `db.tabela`; BigQuery `projeto.dataset.tabela`; S3/GCS chave de objeto (caminho).

**Great Expectations / Liquibase / Alembic / Flyway**

| Posição | Tipo |
|---|---|
| GX `column=`, `column_list=[…]`, `add_batch_definition_*(column=)` | C |
| GX `add_table_asset(table_name=)`, `schema_name=` [VERIFICAR] | T / SCH |
| GX `name=` de data source/asset, `data_asset_name` | rótulo livre — mascarar se igual a nome já mapeado |
| GX `add_query_asset(query=)`, connection string | SQL / família de rede |
| Liquibase `tableName`, `baseTableName`, `referencedTableName`, `newTableName`, `oldTableName` | T |
| Liquibase `schemaName`, `catalogName` | SCH / DB |
| Liquibase `column name=`, `columnName`, `baseColumnNames`, `referencedColumnNames` (lista com vírgula) | C |
| Alembic `create_table(T, sa.Column(C, …))`, `add_column(T, sa.Column(C))`, `drop_column(T, C)`, `alter_column(T, C, new_column_name=C)` | posicional |
| Alembic `create_index(nome, T, [C…])`, `create_foreign_key(nome, T_origem, T_ref, [C], [C])`, `rename_table(T, T)`, `schema=`/`source_schema=`/`referent_schema=` | T/C/SCH |
| Alembic `op.execute("…")`; corpo de `V*__*.sql` | SQL |
| Flyway descrição no nome do arquivo (`V3__cria_tb_pedido_x9.sql`) | só se contiver nome já mapeado no corpo |

### 3. Vocabulário público (nunca mascarar)

| Conjunto | Itens (amostra) | Fonte / como extrair |
|---|---|---|
| Testes genéricos dbt | `unique`, `not_null`, `accepted_values`, `relationships` | docs.getdbt.com/docs/build/data-tests ("four generic data tests") |
| Chaves dbt | `models`, `sources`, `seeds`, `columns`, `data_tests`, `tests`, `arguments`, `config`, `values`, `to`, `field`, `quoting`, `identifier`, `severity`, `where` | mesma página + /docs/build/sources; testes de pacote `pkg.nome` (ex.: `dbt_utils.…`) são públicos pelo prefixo |
| Chaves `manifest.json` | `metadata`, `nodes`, `sources`, `macros`, `parent_map`, `child_map`, `unique_id`, `package_name`, `original_file_path` | /reference/artifacts/manifest-json + JSON Schema em schemas.getdbt.com (extrair `properties` recursivamente) |
| `type:` de adapter / source DataHub | `snowflake`, `bigquery`, `postgres`, `mysql`, `mssql`, `oracle`, `redshift`, `hive`, `kafka`, `dbt`, `iceberg`, … | índice docs.datahub.com/docs/generated/ingestion/sources/ (último segmento das URLs) |
| Chaves de receita DataHub | `source`, `type`, `config`, `sink`, `transformers`, `host_port`, `database`, `allow`, `deny`, `ignoreCase`, `platform_instance`, `env` | recipe_overview + página de cada source (tabela de config) |
| Fabric DataHub | `PROD`, `DEV`, `QA`, `TEST`, … [VERIFICAR lista completa no enum FabricType] | código-fonte do modelo PDL |
| OpenLineage | `eventType`, `eventTime`, `run`, `runId`, `job`, `inputs`, `outputs`, `namespace`, `name`, `facets`, `inputFacets`, `outputFacets`, `producer`, `schemaURL`, `fields`, `type`, `description` | JSON Schema openlineage.io/spec/2-0-2/OpenLineage.json e spec/facets/*.json (extrair `properties`) |
| Operadores Airflow | regra estrutural: identificador `\w+Operator`/`\w+Sensor` importado de `airflow.*`; parâmetros `task_id`, `sql`, `conn_id`, `parameters`, `split_statements` | airflow.apache.org (referência de provider) |
| Expectations GX | regra estrutural `^Expect[A-Z][A-Za-z]+$` e `^expect_[a-z_]+$` (ex.: `ExpectColumnMaxToBeBetween`) | docs.greatexpectations.io + Expectation Gallery |
| Liquibase | `databaseChangeLog`, `changeSet`, `createTable`, `addColumn`, `column`, `constraints`, `primaryKey`, `nullable`, `type`, `remarks`, `tablespace` | docs.liquibase.com/change-types/* (tabela de atributos de cada change type) |
| Alembic | `op.*` e parâmetros (`schema`, `nullable`, `type_`, `server_default`, `existing_type`) | alembic.sqlalchemy.org/en/latest/ops.html |

Os tipos SQL dentro de `type=` (`varchar(255)`, `INT`) seguem a lista pública do mascarador SQL.

### 4. Regras de identificador

**Jinja (dbt).** Só mascarar **literais de string** nos argumentos de `ref`/`source`/`config`. Expressões
(`var('x')`, `env_var('X')`, `target.schema`, `this`) são código: mantêm-se. Preservar o tipo de aspas
(`'`/`"`) e os espaços dentro de `{{ }}`. `ref('pkg','m')`: o pacote é nome de projeto → mascarar só se estiver no mapa.

**Aspas e caixa.** Remover aspas para buscar no mapa e devolvê-las iguais. Em Snowflake/Postgres com
`quoting: true` ou `"Nome"` entre aspas a caixa é significativa: mapear a forma exata; sem aspas, chave do mapa
normalizada (maiúscula em Snowflake, minúscula em Postgres). `relation_name` do manifest vem já citado
(`"db"."sch"."t"`): dividir em partes citadas, mascarar cada uma, recitar.

**Regex em `allow`/`deny` (DataHub).** O padrão é aplicado **ao início** do nome (estilo `re.match`), contra o nome
qualificado (`db.schema.tabela` em `table_pattern`). Para mascarar sem quebrar:
1. Tokenizar o regex em *literais* e *metacaracteres*. Metas: `^ $ . * + ? ( ) [ ] { } | \d \w \s`.
   `\.` é **separador literal de partes** (não meta).
2. Cada sequência literal `[A-Za-z0-9_]+` entre separadores é um candidato; o tipo vem da posição
   (1º segmento DB, 2º SCH, 3º T em `table_pattern`; segmento único em `schema_pattern` = SCH).
3. Se o literal inteiro está no mapa → trocar pelo pseudônimo. Como o pseudônimo só tem `[A-Za-z0-9_]`, não precisa escapar.
4. Manter metas, âncoras, classes e escapes no lugar. Conferir: o regex mascarado deve casar com o nome mascarado
   sempre que o original casava com o original (teste automático com os nomes do mapa).
5. `ignoreCase: true` (padrão) → lookup sem caixa.
6. Escape YAML: em aspas duplas `"\\."` = regex `\.`; em aspas simples/sem aspas `'\.'`. Não reescrever o estilo de aspas.

### 5. Exemplos antes/depois (fictícios)

dbt (schema.yml + modelo):
```yaml
# antes                                   # depois
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
# AIRFLOW_CONN_CONN_VENDAS_DEMO  →  AIRFLOW_CONN_CONN_X7K2  (env exige maiúsculas: mapa sem caixa)
```

DataHub (receita + URN):
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
`"fields":[{"name":"C_xsdgeq7l","type":"NUMERIC"}]` (host do namespace vai para a família de rede; `postgres://` e `NUMERIC` ficam).

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

### 6. Casos difíceis e limites

- **Regex com fragmento de nome.** `^tb_ped.*`, `.*_x9$`, `financeiro_(2023|2024)`: o literal é *pedaço* de nome,
  não nome inteiro; trocar por pseudônimo quebra o casamento. Política: (a) se o fragmento casa com ≥1 nome do
  mapa, gerar pseudônimo de **padrão** (`T_kwvciyut`) e **não prometer equivalência** — marcar `[VERIFICAR]` na
  saída de diagnóstico; (b) se não casa com nada conhecido, deixar como está (é estrutura, ex.: `_tmp$`).
  Alternância `(a|b)` com literais inteiros → mascarar cada ramo. Classes `[0-9]{4}` sempre mantidas.
- **Prefixo/sufixo de convenção** (`stg_`, `_tmp`, `_x9`): não é entidade; mascarar o nome inteiro, nunca o afixo,
  senão dois nomes diferentes podem colidir ou vazar a convenção. Aceitar que o LLM perde a pista semântica.
- **Concatenação em Python.** `f"{schema}.tb_{dominio}_x9"`, `"tb_" + nome`, `"%s.%s" % (s, t)`, `.format()`:
  o nome final só existe em tempo de execução. Só se mascara literal que, sozinho, é nome inteiro conhecido.
  Fragmentos ficam; isso é **limite declarado** (vazamento parcial possível). Mitigação: a saída de logs/CLI,
  onde o nome já aparece completo, é mascarada normalmente.
- **Nome que coincide com vocabulário.** Coluna chamada `name`, `type`, `schema`, `unique`: decide-se pela
  **posição** (valor vs. chave), nunca pelo texto. `columns: - name: name` → só o valor vira `C_…`.
- **Nome lógico ≠ nome físico.** dbt `source name` vs `schema`, GX `name=` do asset, Liquibase `id`/`author`
  do changeSet: são rótulos; mascarar só se o valor já estiver no mapa (evita inflar o mapa com texto livre).
- **`job.name` composto.** Na integração Airflow o padrão `dag_id.task_id` é comum [VERIFICAR na doc da integração];
  dividir por `.` e mascarar cada parte com o mesmo mapa do DAG, para manter o vínculo.
- **Saída de CLI tabular** (`airflow dags list`, `datahub get`, `dbt ls`): reconhecer pela linha de cabeçalho
  e mascarar por coluna; alinhamento de espaços pode mudar — aceitável.
- **Escapes e tamanhos.** Pseudônimo de tamanho fixo altera colunas alinhadas e `varchar` em DDL não é afetado
  (tamanho é do dado, não do nome). Em URN, nunca introduzir `(`, `)` ou `,` (proibidos no tuple).
- **Env var de conexão.** `AIRFLOW_CONN_<ID>` é maiúsculo: o mapa precisa casar `conn_vendas_demo` ↔
  `CONN_VENDAS_DEMO` para não gerar dois pseudônimos.

### 7. Links usados

- https://docs.getdbt.com/docs/build/data-tests
- https://docs.getdbt.com/docs/build/sources
- https://docs.getdbt.com/reference/artifacts/manifest-json
- https://docs.getdbt.com/docs/core/connect-data-platform/connection-profiles
- https://airflow.apache.org/docs/apache-airflow-providers-common-sql/stable/operators.html
- https://airflow.apache.org/docs/apache-airflow/stable/howto/connection.html
- https://docs.datahub.com/docs/metadata-ingestion/recipe_overview
- https://docs.datahub.com/docs/what/urn
- https://docs.datahub.com/docs/generated/ingestion/sources/ (páginas de source: semântica de `allow`/`deny`/`ignoreCase`)
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


## Código

Objetivo: achar nomes de objetos (host, database, schema, tabela, coluna, namespace, usuário) **pela posição
que ocupam** numa gramática ou numa API conhecida. Nada de dicionário de idioma: o vocabulário abaixo é só de
palavras-chave e nomes de API/opção, tirados da documentação oficial.

### 1. Sinais de detecção

Uma região vira "código" quando aparece pelo menos um destes sinais:

| Sinal | Exemplo | Peso |
|---|---|---|
| Cerca de Markdown com linguagem | ` ```python `, ` ```sql `, ` ```bash ` | forte |
| Import/uso de biblioteca conhecida | `from sqlalchemy import`, `import pandas as pd`, `from pyspark.sql`, `import { Entity } from "typeorm"`, `import jakarta.persistence.*` | forte |
| Anotação/decorador de ORM | `@Entity`, `@Table(`, `@Column(`, `@@map(` | forte |
| Primeiro token de linha é um binário de CLI da lista (seção 3) | `psql`, `sqlcmd`, `mysql`, `sqlplus`, `bq`, `snow`, `kubectl`, `ssh`, `scp`, `az`, `aws`, `gcloud` | forte se vier com opção da lista |
| Prompt de shell | `$ `, `PS> `, `# ` seguido de binário da lista | médio |
| Literal de string cujo conteúdo começa por verbo SQL | `"SELECT `, `"""\n  INSERT INTO`, `` `UPDATE `` | médio, confirmado pelo leitor de SQL |

Regra: o sinal só **abre** o modo código. Quem decide o que é nome é a tabela da seção 2. Uma palavra fora de
uma posição da tabela nunca é mascarada por esta família.

### 2. Posição → tipo de entidade

**(a) SQL dentro de string** — achar o literal, extrair o texto, entregar ao leitor de SQL (família SQL), que
devolve os nomes tipados. A família código só cuida de *recortar* e *recolocar* o texto.

| Construção | Onde está o SQL |
|---|---|
| Python `"..."`, `'...'`, `"""..."""`, `'''...'''`, prefixos `r`, `u`, `f`, `rf`, `fr` (sem diferença de caixa) | conteúdo entre as aspas |
| Literais adjacentes `("SELECT a " "FROM t")` | concatenar antes de entregar (Python junta literais vizinhos) |
| Java 15+/Kotlin text block `"""` ... `"""` | do fim da linha de abertura até o `"""` final |
| JS/TS template literal `` `...${x}...` `` | conteúdo entre crases |
| Argumento de `cursor.execute(...)`, `text(...)`, `spark.sql(...)`, `pd.read_sql(...)`, `@NamedQuery(query=...)`, `createQuery(...)`, `$queryRaw` | 1º argumento posicional (ou `query=`) |

**(b) ORMs**

| Padrão | Entidade |
|---|---|
| SQLAlchemy `Table("x", metadata, ...)` | 1º arg → tabela; `schema="s"` → schema |
| SQLAlchemy `Column("y", ...)` | 1º arg string → coluna |
| SQLAlchemy `__tablename__ = "x"` | tabela |
| SQLAlchemy `__table_args__ = {"schema": "s"}` (ou dict no fim da tupla) | schema |
| SQLAlchemy `mapped_column("y", ...)` ou `mapped_column(name="y")` | coluna |
| SQLAlchemy `ForeignKey("s.t.c")` | schema.tabela.coluna (partir no ponto) |
| `create_engine("dialeto://user:...@host:porta/db")` | usuário, host, database (ver URL na família de conexão) |
| Django `class Meta: db_table = "x"` | tabela (pode vir `'"s"."x"'` no Postgres → schema + tabela) |
| Django `models.XField(db_column="y")` | coluna |
| Django `db_table_comment`, `db_tablespace="ts"` | tablespace (se o tipo existir) |
| JPA `@Table(name="x", schema="s", catalog="c")` | tabela / schema / database |
| JPA `@Column(name="y")`, `@JoinColumn(name="y", referencedColumnName="z")` | coluna |
| JPA `@JoinTable(name="x", schema="s")`, `@SecondaryTable`, `@CollectionTable` | tabela / schema |
| JPA `@NamedQuery(query="...")` | **JPQL**: usa nomes de *entidade* e *atributo*, não de tabela; só mascarar se o mesmo nome já foi mapeado como tabela/coluna |
| JPA `@NamedNativeQuery(query="...")` | SQL → leitor de SQL |
| Prisma `model X { ... @@map("x") }` | `@@map` → tabela; `@@schema("s")` → schema |
| Prisma campo `y Int @map("y")` | coluna |
| Prisma `datasource db { url = "..." }` | URL de conexão; `env("VAR")` não tem nome (só o nome da variável) |
| TypeORM `@Entity("x")` ou `@Entity({ name: "x", schema: "s", database: "d" })` | tabela / schema / database |
| TypeORM `@Column({ name: "y" })`, `@JoinColumn({ name: "y" })`, `@PrimaryColumn({ name: "y" })` | coluna |

**(c) pandas / PySpark**

| Padrão | Entidade |
|---|---|
| `df["y"]`, `df[["a","b"]]`, `df.loc[:, "y"]` | coluna (string dentro do subscrito) |
| `df.y` | coluna **só se** `y` já apareceu como coluna em outra posição do mesmo texto (ver seção 6) |
| `col("y")`, `F.col("y")`, `df.select("a","b")`, `withColumn("y", ...)`, `groupBy("y")`, `.rename(columns={"a": "b"})` | coluna |
| `spark.table("d.t")`, `spark.read.table("c.d.t")`, `df.write.saveAsTable("d.t")`, `insertInto("d.t")` | nome multiparte: 1 parte → tabela; 2 → database.tabela; 3 → catálogo.database.tabela |
| `spark.sql("...")`, `pd.read_sql("...", con)`, `pd.read_sql_query("...")` | SQL → leitor de SQL |
| `pd.read_sql_table("t", con, schema="s")` | tabela / schema |
| `df.to_sql("t", con, schema="s")` | tabela / schema |
| `.option("dbtable", "s.t")`, `.option("url", "jdbc:...")`, `.option("user", "u")` | tabela / URL / usuário |

**(d) CLIs com nomes em opções**

| Comando | Opção → entidade |
|---|---|
| `psql` | `-h/--host` host; `-d/--dbname` database (aceita URI/conninfo); `-U/--username` usuário; 1º posicional = database, 2º = usuário; `-c` SQL |
| `sqlcmd` | `-S [tcp:]servidor[\instância][,porta]` host (+instância); `-d` database; `-U` usuário; `-Q`/`-q` SQL |
| `mysql` | `-h/--host` host; `-D/--database` database; `-u/--user` usuário; posicional = database; `-e/--execute` SQL |
| `sqlplus` | `usuário[/senha]@identificador` → usuário + host/serviço (EZConnect `host:porta/serviço`) |
| `bq` | `--project_id` projeto; `--dataset_id` dataset; referência `[projeto:]dataset.tabela` em `show/ls/mk/rm/load` |
| `snow sql` | `-c/--connection` nome de conexão; `--account` conta; `--user` usuário; `--database`, `--schema`, `--warehouse`, `--role`, `--host`; `-q` SQL |
| `kubectl` | `-n/--namespace` namespace; `--context`, `--cluster`, `--user`; `-s/--server` host |
| `ssh` | destino `[usuário@]host` ou `ssh://[usuário@]host[:porta]`; `-l` usuário; `-J` host de salto |
| `scp` | `[usuário@]host:caminho` (o `:` separa host de caminho) |
| `az sql ...` | `--resource-group/-g`, `--server/-s` host, `--name/-n` database (no contexto `sql db`) |
| `aws rds ...` | `--db-instance-identifier`, `--db-cluster-identifier`, `--db-name` |
| `gcloud sql ...` | `--project` projeto; `--instance` ou posicional da instância; `--database/-d` database; `--user/-u` usuário |

### 3. Vocabulário público (fonte e como extrair)

| Vocabulário | Fonte | Como extrair |
|---|---|---|
| Prefixos e aspas de string Python | docs.python.org, *Lexical analysis* | lista fixa: `r b f t u` + combinações com `r`; aspas `' " ''' """` |
| Template literal JS | MDN/ECMA-262 (*Template literals*) | crase, `${`...`}` |
| Text block Java | JEP 378 / JLS §3.10.6 | `"""` + quebra de linha |
| Construtores SQLAlchemy | docs.sqlalchemy.org (*Core: Table, Column*; *ORM: mapped_column, Declarative*) | nomes `Table Column mapped_column ForeignKey __tablename__ __table_args__ schema name` |
| Opções Django | docs.djangoproject.com (*Model Meta options*, *Model field reference*) | `db_table db_column db_tablespace` |
| Anotações JPA | jakarta.ee/specifications/persistence (pacote `jakarta.persistence`) | `Table Column JoinColumn JoinTable SecondaryTable CollectionTable NamedQuery NamedNativeQuery` + atributos `name schema catalog referencedColumnName query` |
| Atributos Prisma | prisma.io/docs (*Prisma schema reference*) | `model datasource @map @@map @@schema url env` |
| Decoradores TypeORM | typeorm.io (*Entities*, *Decorator reference*) | `Entity Column PrimaryColumn JoinColumn` + chaves `name schema database` |
| API pandas | pandas.pydata.org (*read_sql*, *read_sql_table*, *DataFrame.to_sql*) | nome do método + posição/kw do parâmetro (`name`, `schema`, `table_name`) |
| API PySpark | spark.apache.org/docs (*SparkSession.table*, *DataFrameReader.table*, *DataFrameWriter.saveAsTable*, *JDBC data source*) | métodos + opções `dbtable url user` |
| Opções de CLI | página oficial de cada uma (seção 7) | copiar a tabela de opções (curta e longa) para um mapa `opção → tipo` por binário |

Formato sugerido de armazenamento: um arquivo por linguagem/CLI com `{api, posição, tipo}` (ex.:
`{"psql", "-d", DB}`, `{"Table", arg0, T}`), versionado com a URL e a data da consulta.

### 4. Regras de identificador

1. **Escape da string hospedeira primeiro, SQL depois.** Decodificar o literal (`\"`, `\\`, `\n`) antes de
   ler o SQL; ao recolocar, **re-escapar** o pseudônimo do mesmo jeito. Em literal `r"..."` não há escape.
   Como os pseudônimos são `[A-Z0-9_]`, não exigem escape — a regra importa ao *ler* o nome.
2. **Aspas do SQL ficam.** `"vl_total"`, `` `vl_total` ``, `[vl_total]` → troca-se só o miolo:
   `"C_xsdgeq7l"`. Nome entre aspas duplas é sensível a caixa em Postgres/Snowflake; o pseudônimo é sempre o mesmo
   para a mesma grafia exata (case-sensitive na tabela de pseudônimos quando estava entre aspas).
3. **Nome multiparte** (`d.t`, `projeto:dataset.tabela`, `s.t.c`): partir no separador da API (`.`; `:` só no
   `bq` e no `scp`) e mascarar cada parte com seu tipo.
4. **f-string / template / text block com interpolação**: trocar cada `{expr}`/`${expr}` por um marcador
   opaco (`__P1__`), mandar o resto ao leitor de SQL, mascarar o que for nome literal e restaurar o marcador.
   O conteúdo de `{expr}` é Python/JS, não SQL: nunca mascarar ali dentro por esta regra. `{{`/`}}` em f-string
   viram `{`/`}` só na leitura; na escrita voltam a ser dobrados.
5. **Concatenação** (`"SELECT * FROM " + tabela`, `"a " "b"`): literais vizinhos se juntam; com `+` e
   variável, ler só a parte literal e tratar o buraco como marcador opaco (ver seção 6).
6. **Opções de CLI**: aceitar as três formas `-d vendas_demo`, `-dvendas_demo`, `--dbname=vendas_demo`
   (getopt). Valor entre aspas de shell (`-d "vendas_demo"`) → tirar as aspas para ler, recolocar ao escrever.
7. **Consistência**: o mesmo nome real gera o mesmo pseudônimo em todo o texto (SQL, ORM e CLI juntos), para
   que `@Table(name="tb_pedido_x9")` e `SELECT ... FROM tb_pedido_x9` continuem casando.

### 5. Exemplos antes/depois

Python com f-string (o `{dt}` não é tocado):
```python
# antes
sql = f"""SELECT vl_total FROM vendas_demo.tb_pedido_x9 WHERE dt_ref = '{dt}'"""
# depois
sql = f"""SELECT C_xsdgeq7l FROM DB_gbzw6njk.T_cszwa3ri WHERE C_fvp7nx2j = '{dt}'"""
```

SQLAlchemy e Django:
```python
# antes
pedido = Table("tb_pedido_x9", metadata, Column("vl_total", Numeric), schema="vendas_demo")
class Pedido(models.Model):
    total = models.DecimalField(db_column="vl_total")
    class Meta:
        db_table = "tb_pedido_x9"
# depois
pedido = Table("T_cszwa3ri", metadata, Column("C_xsdgeq7l", Numeric), schema="SCH_wy3wj5vm")
class Pedido(models.Model):
    total = models.DecimalField(db_column="C_xsdgeq7l")
    class Meta:
        db_table = "T_cszwa3ri"
```
(Note que `Pedido` e `total` são identificadores do programa, não do banco: ficam.)

JPA e Prisma:
```java
// antes
@Table(name = "tb_pedido_x9", schema = "vendas_demo")
class Pedido { @Column(name = "vl_total") BigDecimal total; }
// depois
@Table(name = "T_cszwa3ri", schema = "SCH_wy3wj5vm")
class Pedido { @Column(name = "C_xsdgeq7l") BigDecimal total; }
```
```prisma
model Pedido { total Decimal @map("C_xsdgeq7l")  @@map("T_cszwa3ri") }   // antes: "vl_total", "tb_pedido_x9"
```

PySpark / pandas:
```python
df = spark.read.table("vendas_demo.tb_pedido_x9").select("vl_total")   # antes
df = spark.read.table("DB_gbzw6njk.T_cszwa3ri").select("C_xsdgeq7l")               # depois
df.to_sql("tb_pedido_x9", con, schema="vendas_demo")                    # antes
df.to_sql("T_cszwa3ri", con, schema="SCH_wy3wj5vm")                               # depois
```

Shell:
```bash
psql -h db-exemplo-01 -dvendas_demo -U usr_relatorio -c "SELECT vl_total FROM tb_pedido_x9"   # antes
psql -h HOST_7rhd4fsp -dDB_gbzw6njkambe -U USR_jqndvqfs -c "SELECT C_xsdgeq7l FROM T_cszwa3ri"                          # depois
ssh usr_relatorio@db-exemplo-01        →  ssh USR_jqndvqfs@HOST_7rhd4fsp
bq show projeto-demo:vendas_demo.tb_pedido_x9  →  bq show DB_7jsplxmw:DB_gbzw6njk.T_cszwa3ri
kubectl -n ns-demo get pods            →  kubectl -n NS_eorwmkh3 get pods
```
Os pseudônimos são identificadores válidos sem aspas em SQL, Python, Java e shell, então o código continua
sintaticamente válido; na volta, a tabela de pseudônimos devolve o nome real e o comando roda como antes.

### 6. Casos difíceis e limites

- **Nome montado em variável** (`tabela = "tb_pedido_" + sufixo`; `f"FROM {tabela}"`): o literal
  `"tb_pedido_"` sozinho não está em posição nenhuma da tabela → **não mascara**. Só mascara se a variável receber
  um literal inteiro e for usada, no mesmo bloco, numa posição conhecida (`tabela = "tb_pedido_x9"` +
  `spark.table(tabela)`): propagação simples de constante, uma atribuição, sem fluxo de controle. Fora disso,
  registrar como não coberto.
- **`df.col` é atributo Python**: `df.vl_total` é gramaticalmente igual a `df.shape` ou `df.head`. Regra: só
  mascarar se `vl_total` já foi visto como coluna em posição forte (`df["vl_total"]`, SQL, ORM) no mesmo texto,
  e nunca se o nome for atributo/método público do pandas/PySpark (lista da referência da API). Na dúvida, fica.
- **Opções combinadas**: `-dvendas_demo` é `-d` + valor; mas `-it` (kubectl/docker) são flags agrupadas. Usar o
  mapa por binário: se a letra exige valor (`-d`, `-h`, `-U`, `-S`, `-n`), o resto do token é o valor. `mysql
  -pSenha` grudado é senha (família segredos), não nome. Atenção: no `psql` e no `mysql`, `-h` é host, mas no
  `sqlcmd` `-h` é o número de linhas entre cabeçalhos — por isso o mapa é **por binário**, nunca global.
- **Mesma opção, sentidos diferentes por subcomando**: `az ... -n` é database em `az sql db`, mas servidor em
  `az sql server`. O mapa precisa da chave `binário + subcomando`.
- **JPQL não é SQL**: `SELECT p FROM Pedido p WHERE p.total > 0` fala de classe e atributo; não mascarar
  como tabela/coluna.
- **Interpolação no meio de um identificador** (`FROM vendas_{amb}.tb_pedido_x9`): o marcador quebra o nome; só
  a parte inteira e literal (`tb_pedido_x9`) é mascarada.
- **SQL dinâmico de ORM** (`session.query(Pedido)`, `objects.filter(total__gt=0)`): não há nome de banco escrito;
  nada a fazer.
- **Linguagem não reconhecida** ou string que o leitor de SQL rejeita: não mascarar pela gramática; cai nas
  outras famílias. Marcar `[VERIFICAR]` no log.
- **Limite honesto**: a tabela cobre APIs e opções listadas; uma API própria do projeto (`minha_lib.ler("t")`)
  não é reconhecida por estrutura.

### 7. Links usados

- Python, literais e f-strings: https://docs.python.org/3/reference/lexical_analysis.html
- SQLAlchemy Core (Table/Column): https://docs.sqlalchemy.org/en/20/core/metadata.html
- SQLAlchemy ORM (mapped_column, `__table_args__`): https://docs.sqlalchemy.org/en/20/orm/declarative_tables.html
- Django Meta: https://docs.djangoproject.com/en/stable/ref/models/options/
- Django campos (`db_column`): https://docs.djangoproject.com/en/stable/ref/models/fields/
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
- SQL*Plus (conexão): https://docs.oracle.com/en/database/oracle/oracle-database/19/sqpug/
- bq: https://docs.cloud.google.com/bigquery/docs/reference/bq-cli-reference
- snow sql: https://docs.snowflake.com/en/developer-guide/snowflake-cli/command-reference/sql-commands/sql
- kubectl: https://kubernetes.io/docs/reference/kubectl/kubectl/
- ssh / scp: https://man.openbsd.org/ssh , https://man.openbsd.org/scp
- az sql db: https://learn.microsoft.com/en-us/cli/azure/sql/db
- aws rds: https://docs.aws.amazon.com/cli/latest/reference/rds/
- gcloud sql: https://cloud.google.com/sdk/gcloud/reference/sql

Observação: psql, mysql, snow sql, kubectl, ssh e Python foram conferidos nesta pesquisa. Nas páginas de bq,
PySpark e sqlcmd o conteúdo veio truncado; as opções delas vêm da referência conhecida e estão marcadas
**[VERIFICAR]** contra a página antes de virar regra. Os links de ORM, Azure, AWS, gcloud e SQL*Plus não foram
abertos nesta rodada: **[VERIFICAR]**.


## NoSQL e busca

Ideia central: o nome é reconhecido pela **posição que ocupa na sintaxe** (depois de `use`, entre `db.` e `.find`, no 1º segmento de uma URL `/x/_search`, como chave de `properties`...). O vocabulário fixo (operadores `$`, comandos Redis, tipos de mapeamento, endpoints `_x`) vem de listas públicas da doc oficial e **nunca** é mascarado. O que sobra em posição de nome é mascarado.

### 1. Sinais de detecção

| Sistema | Sinais estruturais (bastam 1 forte ou 2 fracos) |
|---|---|
| MongoDB (mongosh) | linha `use <x>`; `db.<x>.<método>(`; `db.getCollection('<x>')`; chaves começando com `$` (`$match`, `$group`); `show dbs` / `show collections`; prompt `test>` ou `<db>>`; `ObjectId("...")`; `"queryPlanner"`/`"winningPlan"` (explain) |
| Elasticsearch/OpenSearch | linha `GET|PUT|POST|DELETE /<x>/_<endpoint>`; JSON com `"mappings"` → `"properties"`; `"query": {"bool"|"match"|"term"|"range"...}`; cabeçalho `health status index uuid pri rep docs.count ...` |
| Redis | linha começando por comando conhecido (`GET`, `SET`, `HGET`, `SCAN`...) seguido de chave; prompt `127.0.0.1:6379>`; respostas `(integer) n`, `(nil)`, `1) "..."`; `SCAN <cursor> MATCH <padrão>` |
| Cassandra CQL | `CREATE KEYSPACE`, `USE <ks>;`, `<ks>.<tabela>` após `FROM`/`INTO`/`UPDATE`; `WITH replication = {'class': ...}`; prompt `cqlsh>` / `cqlsh:<ks>>` |
| DynamoDB | JSON com `"TableName"`, `"KeySchema"`, `"AttributeName"`, `"AttributeDefinitions"`, `"KeyType": "HASH"|"RANGE"`; descritores `{"S": ...}`, `{"N": ...}`; `ExpressionAttributeNames` com `#x` |

Sinal sozinho fraco (`GET x` pode ser HTTP ou Redis): exigir contexto (prompt, `_search`, `(integer)`).

### 2. Posição → tipo de entidade

| Posição (padrão) | Entidade | Pseudônimo |
|---|---|---|
| `use <X>` (mongosh) / `db.getSiblingDB('<X>')` | database | `DB_` |
| `db.<X>.<método>(` / `db.getCollection('<X>')` | coleção | `COL_` |
| `from: '<X>'` dentro de `$lookup` / `$graphLookup`; `$out: '<X>'`; `$merge: {into: '<X>'}` | coleção | `COL_` |
| chave de objeto em filtro/projeção/`$group` que **não** começa com `$` | campo | `F_` |
| string `"$<X>"` como valor (referência de campo em agregação) | campo | `F_` |
| `"a.b"` (dot notation) | campo, cada segmento | `F_`.`F_` |
| linhas de `show dbs` (1ª coluna) / `show collections` | database / coleção | `DB_` / `COL_` |
| `"ns": "<db>.<col>"` no explain | db + coleção | `DB_`.`COL_` |
| `/<X>/_search`, `/<X>/_doc/<id>`, `PUT /<X>` | índice (ou alias, ou lista `a,b`) | `IDX_` |
| `"aliases": {"<X>": {}}`, `"alias": "<X>"`, `"index": "<X>"` | alias / índice | `IDX_` |
| chave sob `"properties"` (recursivo) e sob `"fields"` (multi-field) | campo | `F_` |
| `"field": "<X>"`, `"path": "<X>"`; chave dentro de `match`/`term`/`range` | campo | `F_` |
| coluna `index` em `_cat/indices` (posição pelo cabeçalho) | índice | `IDX_` |
| 1º argumento após comando Redis de chave (`GET <K>`, `HSET <K> <campo> v`) | chave | `KEY_` por segmento |
| campo de hash (`HGET k <F>`, `HSET k <F> v`) | campo | `F_` |
| `CREATE KEYSPACE <X>` / `USE <X>` / `<X>.<t>` | keyspace | `DB_` |
| `<ks>.<X>` / `CREATE TABLE <X>` | tabela | `COL_` |
| `"TableName": "<X>"`, `"IndexName": "<X>"` | tabela / índice secundário | `COL_` / `IDX_` |
| `"AttributeName": "<X>"`, `#alias → "<X>"` em `ExpressionAttributeNames` | atributo | `F_` |

### 3. Vocabulário público (não mascarar)

| Lista | Conteúdo | Fonte / como extrair |
|---|---|---|
| Operadores MongoDB | query (`$eq $gt $in $and $or $exists $regex $elemMatch`...), update (`$set $unset $inc $push $pull`...), estágios (`$match $group $lookup $project $unwind $sort $limit $out $merge`...), expressões (`$sum $avg $cond`...) | páginas *Query and Projection Operators*, *Update Operators*, *Aggregation Stages*, *Expression Operators*: extrair todo token `^\$[a-zA-Z]+$` do índice de cada página. Regra: chave que começa com `$` e está na lista = vocabulário; `$` fora da lista = suspeito (campo com `$`, permitido desde 5.0). |
| Métodos mongosh | `find findOne aggregate insertOne updateMany deleteOne countDocuments createIndex explain getCollection getSiblingDB`; palavras `use show dbs collections` | *mongosh Methods* (reference/method). Lista curta, fixa por versão. |
| Nomes de sistema Mongo | `admin local config`, prefixo `system.` | *Limits and Thresholds* / Reserved databases: não são dado do cliente, podem passar. |
| Endpoints ES/OS | qualquer segmento de URL começando por `_` (`_search _doc _mapping _cat _bulk _count _alias _aliases _reindex _update_by_query`) | regra de índice proíbe `_` inicial → segmento `_x` nunca é nome de índice. Lista na *REST APIs*. |
| Query DSL | `query bool must should must_not filter match match_phrase multi_match term terms range gte lte exists wildcard prefix aggs size from sort _source` | *Query DSL* (elastic.co) e *Query DSL* (opensearch.org). |
| Tipos de mapeamento | `text keyword long integer short byte double float half_float scaled_float date date_nanos boolean binary object nested flattened ip geo_point geo_shape dense_vector sparse_vector completion search_as_you_type token_count alias join percolator wildcard constant_keyword semantic_text`... | *Field data types*: extrair os nomes de cada subpágina. Valor de `"type"` está **sempre** nesta lista; chaves de mapeamento (`properties fields analyzer index dynamic format`) idem (*Mapping parameters*). |
| Cabeçalho `_cat/indices` | `health status index uuid pri rep docs.count docs.deleted store.size pri.store.size dataset.size` | *cat indices API*; ler o cabeçalho (`?v`) e mascarar só a coluna `index`. |
| Comandos Redis | `GET SET DEL EXISTS TYPE EXPIRE TTL HGET HSET HGETALL LPUSH SADD ZADD SCAN KEYS MATCH COUNT`... | *Commands* (redis.io/docs/latest/commands): lista oficial; o JSON público `commands.json` do repositório redis-doc também traz a aridade (`key_specs`), que diz **qual argumento é chave**. |
| CQL | palavras-chave (Appendix A) e tipos (`text int uuid timestamp map list set`...) | *CQL Definitions* / *Data types*. |
| DynamoDB | chaves da API (`TableName KeySchema AttributeName KeyType HASH RANGE`), descritores `S N B BOOL NULL M L SS NS BS`, palavras reservadas | *Naming rules and data types*; *Reserved words*. |

### 4. Regras de identificador (validar o pseudônimo contra elas)

- **MongoDB database**: não vazio, < 64 bytes; proibidos no Unix `/\. "$` e nulo (Windows também `*<>:|?`); não distinguir por caixa.
- **MongoDB coleção**: começar por letra ou `_`; sem `$`, sem nulo, não vazio; não começar com `system.` nem conter `.system.`. Namespace `db.col` ≤ 255 bytes (235 em coleção shardada). Nome com caractere especial ou dígito inicial só via `db.getCollection("...")`.
- **MongoDB campo**: sem nulo; `.` e `$` são permitidos no servidor (5.0+), mas `.` em query vira caminho → tratar `.` como separador.
- **Índice ES/OS**: só minúsculas; proibidos `\ / * ? " < > | , #` e espaço; não começar com `-`, `_`, `+`; não ser `.` ou `..`; ≤ 255 bytes; `.` inicial só para índice oculto/sistema.
- **Chave Redis**: binary-safe, qualquer byte, até 512 MB, string vazia válida. Sem regra → só a **convenção** `tipo:id:campo` (separador `:`; `.`/`-` em palavras compostas) e hashtag `{...}` de cluster.
- **Cassandra**: sem aspas `[a-zA-Z_0-9]{1,48}`, case-insensitive; keyspace ≤ 48, tabela ≤ 222; com aspas é case-sensitive.
- **DynamoDB**: tabela/índice 3–255 chars `[a-zA-Z0-9_.-]`, case-sensitive; atributo ≥ 1 char e < 64 KB (255 para chaves de índice secundário); `#` e `:` têm sentido especial em expressões.

Consequência: pseudônimo **minúsculo**, `[a-z0-9_]`, começando com letra: `db_gbzw6njk`, `t_cszwa3ri`, `t_kzkcrymy`, `c_xsdgeq7l`. Vale em todos os sistemas acima (no texto abaixo, maiúsculo só para leitura; no índice ES usar `t_kzkcrymy` obrigatoriamente minúsculo).

### 5. Exemplos antes → depois

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
`_id` fica (nome reservado). Valores (`"Recife"`, `"pago"`) são problema de outro detector. `as:` cria campo novo → `F_`.

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
`_cat/indices?v`: manter o cabeçalho, trocar só a coluna `index` (`idx-logs-demo` → `t_kzkcrymy`), `uuid` é opaco (mascarar se a política tratar como identificador).

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
Segmentos numéricos/IDs passam ou vão para o detector de identificador; o mapa precisa ser o **mesmo** dentro do padrão e na saída.

Cassandra / DynamoDB:
```
SELECT * FROM vendas_demo.pedidos_x9 WHERE pedido_id = 1;   → SELECT * FROM DB_gbzw6njk.T_cszwa3ri WHERE C_roj22pqm = 1;
{"TableName":"pedidos_x9","KeySchema":[{"AttributeName":"pedido_id","KeyType":"HASH"}]}
→ {"TableName":"T_cszwa3ri","KeySchema":[{"AttributeName":"C_roj22pqm","KeyType":"HASH"}]}
```

### 6. Casos difíceis e limites

- **Chave Redis com dado pessoal dentro** (`sessao:maria@exemplo.com`, `cpf:00000000000:score`): segmentar por `:`; cada segmento vai **também** pelos detectores de valor (e-mail, CPF, telefone). Segmento de prefixo → `KEY_`; segmento que é dado → pseudônimo do tipo do dado. Chave sem `:` (ex. JSON serializado, hash SHA) → tratar como opaca e mascarar inteira. Hashtag `{x}` preservar as chaves `{}` (mudam o slot do cluster).
- **Campo dinâmico** (`"properties"` com nomes gerados, ex. datas `2026-10-01` como chave; Mongo `{ "metricas": { "sku_991": 3 } }`): não há como saber pela estrutura se a chave é esquema ou dado. Regra: se as chaves irmãs seguem um padrão numérico/data/ID, mascarar como valor; senão `F_`. `dynamic_templates` (`"match": "attr_*"`) traz padrões, não nomes.
- **Padrões com curinga**: `GET /idx-logs-*/_search`, `SCAN MATCH pedido:*`, `"match": "attr_*"`, `index_patterns`. Mascarar só a parte literal e manter `* ? [ ]`; o mesmo prefixo precisa virar o mesmo pseudônimo dos nomes concretos, senão o padrão deixa de casar (`idx-logs-*` → `t_kzkcrymy*` só funciona se a troca for por **prefixo**, não por nome inteiro — limite real: pseudonimizar prefixo e nome completo de forma consistente exige tokenizar o nome por `-`/`_`).
- **Listas e datas em índice ES**: `GET /a,b/_search`, `logs-2026.10.05` (data math `<logs-{now/d}>`): separar por `,`; sufixo de data é estrutura, manter.
- **Dot notation ambígua**: `"a.b"` pode ser campo literal com ponto (5.0+) ou caminho. Mascarar segmento a segmento cobre os dois.
- **`$` como referência vs. operador**: `"$total"` (valor) é campo; `$sum` (chave) é operador. Decidir pela lista do item 3, não pelo `$`.
- **Nomes que coincidem com vocabulário** (campo chamado `status`, `type`, `index`, `match`): a posição decide. Chave sob `properties` é campo mesmo que se chame `type`; valor de `"type"` é tipo.
- **`GET`/`SET` ambíguo** (HTTP × Redis): sem prompt ou resposta Redis, não assumir.
- **Limite**: texto livre ("a coleção de pedidos está lenta") não tem estrutura → fora do alcance deste detector.
- Volta (des-mascarar a resposta do LLM): o LLM pode inventar nomes novos derivados (`col_a8f1_backup`); só reverter tokens exatos do mapa.

### 7. Links usados

- https://www.mongodb.com/docs/manual/reference/limits/ (nomes de database, coleção, campo, namespace)
- https://www.mongodb.com/docs/manual/reference/operator/query/ · https://www.mongodb.com/docs/manual/reference/operator/aggregation-pipeline/ · https://www.mongodb.com/docs/manual/reference/operator/update/
- https://www.mongodb.com/docs/manual/core/dot-dollar-considerations/
- https://www.elastic.co/guide/en/elasticsearch/reference/current/indices-create-index.html (regras de nome de índice)
- https://www.elastic.co/guide/en/elasticsearch/reference/current/mapping-types.html (tipos de campo)
- https://www.elastic.co/guide/en/elasticsearch/reference/current/cat-indices.html
- https://www.elastic.co/guide/en/elasticsearch/reference/current/query-dsl.html · https://opensearch.org/docs/latest/query-dsl/
- https://redis.io/docs/latest/develop/using-commands/keyspace/ (chaves, convenção, glob do SCAN/KEYS)
- https://redis.io/docs/latest/commands/
- https://cassandra.apache.org/doc/latest/cassandra/developing/cql/definitions.html · https://cassandra.apache.org/doc/latest/cassandra/developing/cql/ddl.html
- https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/HowItWorks.NamingRulesDataTypes.html
- https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/ReservedWords.html

[VERIFICAR] cabeçalho exato de `_cat/indices?v` (a página lida não mostra saída de exemplo; colunas vêm da lista de atributos). [VERIFICAR] existência do `commands.json` com `key_specs` na versão atual da doc Redis.


## Formatos de transporte (diff, grep, numeração, logs, markdown, heredoc)

**Como está implementado** (`normalizacao.go`): uma camada antes de todos os leitores tira o
transporte e mapeia cada posição do texto limpo de volta ao original. Tira: número de linha
da ferramenta de leitura (`   12→`) e do `cat -n`/`nl`; prefixo do `grep -n` (`arq:12:`,
`12:`, contexto `arq-12-`, separador `--`), só quando a maioria das linhas o tem; diff/patch
(cabeçalhos e o 1º caractere `+`/`-`/espaço), quando há cabeçalho de diff; `git blame`;
citação de markdown (`> `, aninhada); carimbo de data e nível no começo da linha de log;
cerca de código; sequências ANSI; BOM; CRLF; espaços no fim da linha. Bordas de caixa
(U+2500–U+257F) viram `|` e `-`, que o leitor de tabela já entende. String de JSON com
`\n` escapado é decodificada (até 3 níveis), cada uma num bloco próprio. Os leitores de
estrutura e de tabela rodam no texto limpo; só voltam os achados que correspondem byte a
byte ao original (a troca e a volta ficam exatas). O original também é lido quando a
limpeza tirou algo que traz nome (arquivo do grep, cabeçalho do diff) ou mudou a estrutura
(JSON decodificado). Na propagação, um nome logo depois de `\n`/`\t` escapado também é
procurado sem a letra do escape.

Princípio: o proxy não mascara o invólucro, mascara o **conteúdo**. Cada invólucro vira
uma lista de *segmentos* `(ini_original, ini_interno, comprimento)` e um *rótulo de tipo*
(sql, yaml, json, desconhecido). O leitor do tipo acha os nomes no texto interno; o mapa
devolve as posições no texto original. Invólucros podem se aninhar (JSON → heredoc → SQL;
markdown → diff → SQL): tira-se de fora para dentro, compondo os mapas.

### 1. Sinais de detecção

Testar por linha, do mais forte para o mais fraco. Exigir **dois sinais** antes de assumir o invólucro.

| Invólucro | Sinal forte (âncora) | Sinal de confirmação |
|---|---|---|
| diff git | linha `^diff --git ` | `index <hash>..<hash>`, `---`/`+++`, `@@ -` |
| diff -u (GNU) | par `^--- ` seguido de `^\+\+\+ ` | `^@@ -\d+(,\d+)? \+\d+(,\d+)? @@` |
| hunk solto | `^@@ -\d+(,\d+)? \+\d+(,\d+)? @@` | linhas seguintes só com 1º caractere em `{' ','+','-','\'}` |
| grep/rg sem heading | `^<caminho>:<n>:` ou `^<caminho>-<n>-` em várias linhas | mesmo `<caminho>` repetido; linha `^--$` entre grupos |
| rg --heading | linha só com caminho, seguida de `^\d+:` / `^\d+-` | linha vazia entre arquivos |
| numeração do Read (Claude Code) | `^\s*\d+\t` em linhas consecutivas (formato conferido nas sessões; `→` aceito também) | números crescentes de 1 em 1 |
| cat -n | `^\s*\d+\t` (número alinhado à direita + TAB) | números crescentes de 1 em 1 |
| log PostgreSQL | `LOG:  statement: `, `ERROR:  `, `STATEMENT:  `, `DETAIL:  ` após um prefixo | prefixo repetido (`log_line_prefix`, padrão `%m [%p] `) |
| SQL Server ERRORLOG | `^\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\.\d+ \S+\s+` | origem `spid\d+`, `Server`, `Logon` [fase 3: log é 0,0% do volume] |
| JSON Lines | toda linha não vazia começa em `{` ou `[` e é JSON válido sozinha | mesmas chaves em linhas seguidas |
| log Airflow/Spark | `^\[?\d{4}-\d{2}-\d{2}[ T]\d{2}:\d{2}` + nível (`INFO`, `WARN`, `ERROR`) | `{arquivo.py:linha}` no Airflow [fase 3: log é 0,0% do volume] |
| markdown | linha `^ {0,3}(`{3,}\|~{3,})` | linha de fechamento com o mesmo caractere e comprimento ≥ |
| heredoc | `<<-?\s*(['"]?)(\w+)\1` em linha de comando | linha que contém **só** a palavra delimitadora |
| string JSON | `"command":"..."`, presença de `\n`, `\"`, `\\`, `\u` | texto entre aspas não quebra linha (quebras viram `\n`) |

### 2. Invólucro → como remover → como achar o tipo interno

| Invólucro | Como remover (o que vira texto interno) | Tipo interno |
|---|---|---|
| diff git / -u | Descartar cabeçalhos. Em cada hunk, tirar **1 caractere** de prefixo (` `, `+`, `-`). Montar **duas vistas**: antiga (` `+`-`) e nova (` `+`+`). Ignorar `\ No newline at end of file`. | extensão do caminho em `diff --git a/X b/X` ou `+++ b/X` (`.sql`, `.yaml`, `.yml`, `.json`, `.py`); se `/dev/null`, usar o outro lado |
| texto após o 2º `@@` | É a linha de "função" escolhida pelo git (`xfuncname`): conteúdo do arquivo, não invólucro. Mascarar com o mesmo tipo do arquivo. | igual ao arquivo |
| grep/rg (sem heading) | Remover `caminho` + sep + `n` + sep (+ `col`/`byte` + sep se `-b`/`--column`). Sep = `:` em casamento, `-` em contexto. Linha `--` = quebra de bloco (não é conteúdo). | extensão do `caminho` do prefixo |
| rg --heading | Linha de caminho = cabeçalho (fora do conteúdo); nas outras, remover `^\d+[:-]` | extensão do caminho do cabeçalho |
| numeração (`→`, TAB, cat -n) | Remover `^\s*\d+→` ou `^\s*\d+\t` (só a **primeira** ocorrência por linha) | sem pista de caminho: farejar o conteúdo (seção 6) |
| log PostgreSQL texto | Remover prefixo da linha e a palavra fixa até `:  `; o resto (e linhas de continuação, que começam por TAB) é a instrução | `statement:`, `STATEMENT:`, `execute <nome>:` → sql; `DETAIL:`/`HINT:` → texto livre |
| log PostgreSQL jsonlog / JSON Lines | Parsear cada linha como JSON; extrair o **valor** de cada string (desfazendo escapes, seção 4) | chave decide: `statement`, `internal_query`, `query` → sql; `message`/`detail` → texto (pode conter nome entre aspas) |
| SQL Server ERRORLOG | Remover `data hora origem` do começo da linha | texto livre com nomes entre `'...'` ou `[...]` [fase 3: log é 0,0% do volume] |
| Airflow/Spark | Remover `[timestamp]`, `{arquivo:linha}`, nível; o resto é mensagem | texto livre; SQL se começa por palavra-chave SQL (seção 6) |
| markdown | Conteúdo = linhas entre as cercas. Se a cerca de abertura tem N espaços de recuo, remover **até N espaços** de cada linha. Bloco sem fechamento vai até o fim do documento. | 1ª palavra da info string (` ```sql `, ` ```yaml `); vazia → farejar |
| heredoc | Conteúdo = linhas após a linha do comando até a linha só com o delimitador. `<<-`: remover TABs iniciais de cada linha. | comando que recebe (`psql`, `sqlcmd`, `snowsql`, `bq query` → sql; `cat > x.yaml` → extensão do destino); senão farejar |
| string JSON | Desfazer escapes de RFC 8259 caractere a caractere, guardando o mapa | o que estiver dentro (normalmente comando shell → heredoc/SQL) |

### 3. Vocabulário público (palavras fixas — nunca mascarar)

| Origem | Palavras fixas | Fonte |
|---|---|---|
| git diff | `diff --git`, `old mode`, `new mode`, `deleted file mode`, `new file mode`, `copy from`, `copy to`, `rename from`, `rename to`, `similarity index`, `dissimilarity index`, `index`, `diff --combined`, `diff --cc`, `@@@`; prefixos `a/` `b/` (ou `c/ i/ w/ o/` com `diff.mnemonicPrefix`) | git-scm diff-format, git-diff |
| diff GNU | `---`, `+++`, `@@`, `\ No newline at end of file` [não confirmado → regra tolerante] | gnu diffutils |
| grep | separadores `:` (casamento), `-` (contexto), `--` (grupo); ordem fixa caminho → linha → byte | gnu grep §2.1.4–2.1.5 |
| ripgrep | `--` (padrão de `--context-separator`), `:` / `-` (field separators) | ripgrep defs.rs |
| PostgreSQL níveis | `DEBUG1`–`DEBUG5`, `INFO`, `NOTICE`, `WARNING`, `ERROR`, `LOG`, `FATAL`, `PANIC` | runtime-config-logging, tab. 19.2 |
| PostgreSQL campos | `DETAIL`, `HINT`, `QUERY`, `CONTEXT`, `STATEMENT`; jsonlog: `error_severity`, `message`, `statement`, `internal_query`, `detail`, `hint`, `context`, `dbname`, `user`, `application_name`… | idem (`log_error_verbosity`, jsonlog) |
| PostgreSQL mensagens | `statement:`, `duration:`, `execute <nome>:` — forma literal [não confirmado → regra tolerante] | — |
| SQL Server | arquivos `ERRORLOG`, `ERRORLOG.<n>`; `sp_cycle_errorlog` | learn.microsoft.com |
| Logs genéricos | `TRACE`, `DEBUG`, `INFO`, `WARN`, `WARNING`, `ERROR`, `CRITICAL`, `FATAL` [fase 3: log é 0,0% do volume] | — |
| CommonMark | cercas ` ``` `, `~~~` (≥ 3); info string | spec.commonmark.org 0.31.2 |
| POSIX shell | `<<`, `<<-`, delimitador entre aspas desliga expansão | POSIX 2.7.4 |

**Atenção:** campos do `log_line_prefix` como `%u` (usuário), `%d` (banco), `%h`/`%r` (host),
`%a` (aplicação) são **dados**, não vocabulário — mascarar (tipos `DB_`, `HOST_`, `USR_`).

### 4. Regras de mapeamento de posição

1. **Segmentos, não deslocamento único.** Para cada linha mantida guardar
   `(ini_original, ini_interno, comprimento)`. Remoção de prefixo de largura fixa
   (diff, grep, numeração, recuo markdown, TAB de `<<-`) gera um segmento por linha.
   Busca de posição = busca binária no `ini_interno`; `orig = ini_original + (pos - ini_interno)`.
2. **Escape JSON gera mapa por caractere.** `\n`, `\"`, `\\`, `\/`, `\t` ocupam 2 bytes no
   original e 1 no interno; `\uXXXX` ocupa 6 (e um par substituto 12) e vira 1 caractere.
   Guardar um vetor `interno[i] → original[j]` (ou segmentos com quebra em cada escape).
3. **Um nome nunca atravessa segmento.** Se o achado começa num segmento e termina noutro
   (nome quebrado entre linhas, ou com escape no meio), substituir o **intervalo original
   inteiro** `[orig(ini), orig(fim))` — isso apaga o prefixo do meio, então só é permitido
   quando os segmentos são contíguos no original (caso escape). Entre linhas: não mascarar
   como nome, registrar alerta.
4. **Substituir de trás para frente** no texto original: pseudônimo tem tamanho diferente
   do nome e não pode invalidar as posições ainda não aplicadas.
5. **Reescapar o pseudônimo** no invólucro de destino. Pseudônimos são `[A-Z]+_[a-z0-9]{4}`
   — nada a escapar em JSON, shell ou markdown; manter assim por desenho.
6. **Diff tem duas vistas, uma tabela.** A linha de contexto aparece nas duas vistas com a
   mesma posição original: deduplicar achados por `ini_original`. Mesmo nome → mesmo
   pseudônimo nas linhas `-` e `+`, senão o diff mascarado mostra mudança que não existe.
7. **Cabeçalhos não andam.** `@@ -l,n +l,n @@`, número de linha do grep e do `→` ficam
   intactos: só o conteúdo muda de tamanho, não de número de linhas.
8. **Caminho no cabeçalho é conteúdo também** (seção 6): passa pelo leitor de caminhos, e o
   mesmo caminho em `diff --git`, `---`, `+++` e no prefixo do grep recebe o mesmo pseudônimo.

### 5. Exemplos antes/depois

Diff (tipo vem de `.sql`; o invólucro fica; `-` e `+` usam o mesmo pseudônimo):

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

grep (`:` casamento, `-` contexto, `--` grupo):

```
sql/relatorio_pedidos.sql-11-  -- totais
sql/relatorio_pedidos.sql:12:  FROM financeiro.tb_pedido_x9 p
--
```
→ `sql/relatorio_pedidos.sql:12:  FROM SCH_mvrhafae.T_cszwa3ri p` (linha 11 e `--` intactos)

Log PostgreSQL (prefixo `%m [%p] %u@%d `; usuário e banco também são dados):

```
2026-01-10 08:00:01.120 UTC [4410] app_rel@dw_vendas LOG:  statement: DELETE FROM financeiro.tb_pedido_x9 WHERE id = 7
```
```
2026-01-10 08:00:01.120 UTC [4410] USR_n4a4mukt@DB_k3gr76nw LOG:  statement: DELETE FROM SCH_mvrhafae.T_cszwa3ri WHERE id = 7
```

JSON → heredoc → SQL (mapa composto; `\n` preservado):

```
{"command":"psql <<'EOF'\nSELECT * FROM \"financeiro\".tb_pedido_x9;\nEOF"}
```
```
{"command":"psql <<'EOF'\nSELECT * FROM \"SCH_mvrhafae\".T_cszwa3ri;\nEOF"}
```
Note: `\"financeiro\"` no original ocupa 14 bytes; no interno `"financeiro"` ocupa 12. O
achado é `financeiro` (sem aspas); o mapa por caractere devolve exatamente os 10 bytes
entre os `\"`, e as aspas escapadas ficam.

### 6. Casos difíceis e limites

- **Hunk que começa no meio de uma instrução SQL.** O leitor SQL não pode exigir instrução
  completa: deve achar nomes por padrões locais (`FROM|JOIN|INTO|UPDATE|TABLE <id>(.<id>)*`,
  `<id>.<id>.<id>`). Uma linha `+  JOIN financeiro.tb_pedido_x9 x` sem o `SELECT` acima ainda
  casa. Coluna solta (`valor,`) sem âncora fica sem mascarar — aceitável. O texto após o 2º
  `@@` muitas vezes traz o começo da instrução (`CREATE VIEW ...`) e ajuda a dar contexto.
- **Contagem do hunk não fecha** (diff truncado pelo agente ou colado pela metade): não
  validar `n` do cabeçalho contra as linhas; seguir aceitando linhas com prefixo
  `{' ','+','-','\'}` até um prefixo estranho; o resto volta a ser texto genérico.
- **Linha de diff truncada / rg com `--max-columns`.** O final da linha falta (ou vem um
  aviso de linha omitida em vez do conteúdo [não confirmado → regra tolerante]). Um nome cortado no
  fim (`financeiro.tb_ped`) não é reconhecível como o original: mascarar o pedaço com
  pseudônimo **próprio** se tiver forma de identificador qualificado; não tentar completar.
- **grep -o / pedaço de linha.** Só a parte casada aparece: sem `FROM` à esquerda. Usar o
  padrão de nome qualificado (`a.b`, `a.b.c`, `[a].[b]`, `"a"."b"`) e o tipo do arquivo.
- **Caminho com nome de servidor**: `config/sqlserver_srv-exemplo-01.yaml`. O caminho é
  dado. Quebrar em componentes (`/`, `.`, `_`), manter extensão e partes de vocabulário do
  produto (`config`, `sqlserver`, `yaml`) e mascarar o trecho com forma de host
  (`srv-exemplo-01` → `HOST_vltoxf7e`): `config/sqlserver_HOST_r4t6.yaml`. A **extensão continua
  decidindo o tipo** — por isso detectar o tipo antes de mascarar o caminho. Aplicar o mesmo
  pseudônimo em `a/`, `b/`, `---`, `+++`, prefixo do grep e cabeçalho do rg.
- **Caminhos com aspas no git** (`core.quotePath`): `"config/\303\241rea.yaml"`; desfazer o
  escape C/octal para ler o nome e reescapar na volta. `--no-prefix` tira `a/` `b/`;
  `diff.mnemonicPrefix` troca por `c/ i/ w/ o/` — aceitar qualquer `^[a-z]/` [não confirmado → regra tolerante].
- **Falso positivo de invólucro.** Linha SQL `-- comentário` parece separador de grep ou
  linha removida de diff; `@@` aparece em variáveis do SQL Server (`@@ROWCOUNT`). Por isso
  exigir dois sinais (seção 1) e que o padrão se repita em linhas vizinhas.
- **Numeração dentro de diff** (agente leu um diff com números): tirar a numeração primeiro
  (camada externa), depois o diff. Ordem = de fora para dentro, sempre.
- **Markdown aninhado**: cerca de 4 crases dentro da qual há cerca de 3 — o fechamento exige
  mesmo caractere e comprimento ≥ ao da abertura; usar contagem, não "primeira ``` que vier".
- **Heredoc sem aspas** (`<<EOF`): `$VAR` e `$(...)` são expandidos pelo shell; nomes dentro
  de `${...}` são variáveis de shell, não objetos de banco. Com `<<'EOF'` o texto é literal.
  Vários heredocs na mesma linha são consumidos na ordem em que aparecem.
- **Sem pista de tipo** (numeração, cerca sem info string, heredoc para comando desconhecido):
  farejar — 1ª palavra significativa em `SELECT|WITH|INSERT|UPDATE|DELETE|MERGE|CREATE|ALTER`
  → sql; `^\s*[\w-]+:\s` repetido / `^---$` → yaml; começa por `{`/`[` e parseia → json;
  senão leitor genérico (nomes qualificados e hosts).
- **Log multilinha**: no PostgreSQL a instrução continua nas linhas seguintes (com TAB);
  em JSON Lines nunca há quebra real dentro de um registro (vem `\n` escapado).

### 7. Links usados

- https://git-scm.com/docs/diff-format
- https://git-scm.com/docs/git-diff
- https://git-scm.com/docs/git-config (core.quotePath — página veio cortada; texto conferido de memória [não confirmado → regra tolerante])
- https://www.gnu.org/software/diffutils/manual/html_node/Detailed-Unified.html
- https://www.gnu.org/software/grep/manual/grep.html
- https://github.com/BurntSushi/ripgrep/blob/master/GUIDE.md
- https://raw.githubusercontent.com/BurntSushi/ripgrep/master/crates/core/flags/defs.rs
- https://www.postgresql.org/docs/current/runtime-config-logging.html
- https://learn.microsoft.com/en-us/sql/tools/configuration-manager/viewing-the-sql-server-error-log
- https://jsonlines.org/
- https://spec.commonmark.org/0.31.2/
- https://pubs.opengroup.org/onlinepubs/9799919799/utilities/V3_chap02.html (2.7.4 Here-Document)
- https://www.rfc-editor.org/rfc/rfc8259 (seção 7, Strings)

Sem fonte oficial lida (marcados [VERIFICAR]): formato `N<tab>` do Read (conferido nas sessões: 605 de 640 resultados; nenhum com `N→`), largura do `cat -n`
(coreutils deu 429), formato de linha do ERRORLOG, logs Airflow/Spark, forma literal
`LOG:  statement:` do PostgreSQL, texto de linha omitida do rg.


## Saídas soltas de script e de shell

Saída de script e de shell não tem formato fixo. Valem quatro mecanismos genéricos, nenhum por
ferramenta (os nomes de ferramenta abaixo são exemplos de onde a forma aparece):

**1. O comando diz o que a saída é** (`comando.go`; o proxy liga o `tool_use` ao `tool_result`
do mesmo id em `requisicao.go`, `dicasDosComandos`). A entrada da ferramenta (o `command` do
shell, ou as strings da entrada, sem a descrição) vira uma *dica* com o tipo das colunas da
saída, que vai junto do texto na chave da memória de resultados. A dica só é evidência para o
leitor de tabela (`classificarTabelaD`, em modo dica: só as colunas que o cabeçalho não tipa) e
para a saída sem cabeçalho (`linhasDica`: toda linha com o mesmo número de células, por `|`,
TAB, `,`, `;` ou espaço; uma linha fora da forma desfaz tudo; o rodapé `(N rows)` e a linha que
repete o cabeçalho ficam de fora; `uniq -c` tem a contagem tirada). Formas:
- SQL no comando: a última `SELECT ... FROM` dá o tipo de cada coluna em ordem e pelo nome
  (alias ou coluna, pelo cabeçalho de catálogo/palavra de tipo); `name` vale pelo catálogo do
  FROM no plural (`sys.tables` → tabela); `SELECT *` não dá posição. `SHOW <tipos>` dá o tipo da
  coluna `name` (`SHOW SCHEMAS` → schema); `DESCRIBE` dá coluna. Evidência forte.
- extração de coluna por posição (`cut -d, -fN`, `awk -F, '{print $N}'`) sobre um arquivo cujo
  cabeçalho já passou pela conversa (a saída de um comando que cita o arquivo começa com
  `a,b,c` de identificadores, inclusive pela ferramenta de leitura): a coluna N tem o tipo do
  cabeçalho N. Forte.
- listagem de recurso: `<cli> get|list|ls <tipo>`, `<cli> <tipo> list`, `<cli> list-<tipo>`
  (o verbo nunca é o programa: `ls` e `ps` do shell não contam) dão o tipo da coluna
  `NAME`/`NAMES`; `<cli> ps` e `<cli> list` sem tipo, serviço (contêineres, releases). Tipos:
  as palavras de tipo (`tables`, `topics`, `buckets`, `instances`...) e os recursos de
  orquestrador e contêiner com as abreviações das CLIs (`pods`, `deploy`, `svc`, `ns`,
  `nodes`...). Evidência fraca (mascara no lugar; ensina só com outra regra) e só valor com
  cara de identificador fora do vocabulário de devops (`kube-system`, `bridge` ficam).

**2. Nome qualificado depois de palavra de tipo, em qualquer frase** (leitor de erro,
`acharTipoQualificado`, estendido de "palavra de tipo + nome entre aspas" para nome sem
aspas): `tabela fin.t_x: 1200 linhas`, `Loading table a.b.c`, `created sql table model
fin.t_x` (uma palavra em minúsculas cabe no meio, e então a evidência é fraca). Palavras de
tipo em inglês e português (`tabela`, `esquema`, `banco`, `objeto`...). Sem aspas, só nome
qualificado (2 ou 3 partes, a última com 3+ letras): não vale arquivo (`vendas.csv`), domínio
público, chamada, caminho, versão, `i.e.`, nem receptor de código (`os.path`). Endereço de banco
sem esquema `host:porta/banco` (EZConnect e mensagens de conexão) no leitor de conexão, só em
linha que fala de conexão (`conect`, `connect`, `conex`), com `como|as <usuário>` logo depois;
sem usuário, host público com banco sem cara de nome (`localhost:8080/api`) não vale.

**3. Lista homogênea** (`listas.go`, roda depois dos leitores, porque depende do que já se
sabe): numa coluna solta (um item por linha, com marcador `- `/`* `), em contagem + valor
(`sort | uniq -c`, `value_counts`), em linha decorada de laço (`== t_x ==`, `--- t_x`, `## t_x`,
agrupadas pela decoração) ou numa lista entre vírgulas (com ou sem aspas e colchetes:
`json.dumps`, `print`), com 3+ itens e pelo menos metade já nomes de um mesmo tipo (aprendidos
antes ou achados pelos leitores neste texto), os outros itens com cara de identificador são do
mesmo tipo (mascara e aprende). Lista entre os parênteses de uma chamada (`f(a, b, c)`) não
conta; arquivo e tipo de dado não viram item. Idem para nome qualificado com uma parte
conhecida na posição de schema ou banco: `fin.<x>` com `fin` aprendido como schema faz de `<x>`
uma tabela.

**4. O resto é propagação** (vistos): um nome aprendido é mascarado dentro de qualquer forma
(f-string, log, `B2=t_x`, texto extraído de .docx/.pdf, linha de `kubectl logs`).

Limites: texto de .docx/.pdf (e prosa em geral) só é coberto pela propagação: um nome que
aparece pela primeira vez ali, sem estrutura, passa. A dica vale só para o resultado da
própria chamada; saída de script que não diz o que imprime (sem SQL, sem cabeçalho, sem lista
com nomes já conhecidos) também passa. Na lista homogênea, um nome que só aparece em listas
de nomes desconhecidos não é visto. Quando a primeira linha de uma saída sem cabeçalho é lida
pelo leitor de tabela como cabeçalho, a célula sai mascarada como coluna (o tipo da dica não
substitui o do cabeçalho).


# 12 — Reserva genérica (formato estruturado desconhecido)

## Reserva genérica

Papel: pegar o que os leitores específicos (SQL, YAML/JSON, CSV, connection string) deixaram
passar. Não entende o formato; só reconhece **posição de chave** e **forma de identificador**.
Regra mínima segura, em uma frase:

> O valor que ocupa a posição de valor de uma chave cujo **núcleo** está no vocabulário de
> "nome de coisa" é mascarado inteiro. O nome aprendido assim vira entrada de uma lista de
> bloqueio da sessão e é propagado para a prosa **somente se tiver cara de identificador**
> (ou se aparecer em contexto estrutural: crase, aspas, nome qualificado `a.b.c`).

### 1. Sinais de detecção

Uma linha (ou célula) entra na reserva quando casa com uma destas formas:

| Forma | Padrão (resumo) | Exemplo fictício |
|---|---|---|
| chave: valor | `^\s*[-*]?\s*CHAVE\s*:\s*VALOR` | `host: db-exemplo-01` |
| chave=valor (lista com `;` `,` `&` ou espaço) | `CHAVE\s*=\s*VALOR` | `Initial Catalog=vendas_x9;User ID=svc_relatorio` |
| chave valor alinhado | `^\s*CHAVE {2,}VALOR$` (2+ espaços ou tab) | `SERVIDOR     db-exemplo-01` |
| flag de CLI | `--CHAVE[= ]VALOR`, `-n VALOR` só se a flag longa equivalente for conhecida | `--namespace ns-exemplo-02` |
| tabela alinhada | linha de cabeçalho + (linha `---`/`===`/`|` ou 2+ linhas com colunas na mesma posição) | ver §5 |
| lista | `- CHAVE: VALOR` ou `CHAVE:` seguido de itens `- VALOR` | `buckets:` / `- bkt-exemplo-01` |

**Núcleo da chave** (normalização, nada de idioma):
1. minúsculas; remover acentos; partir em `_ - . espaço` e em fronteira camelCase
   (`proxyHost` → `proxy host`; `Initial Catalog` → `initial catalog`).
2. núcleo = último pedaço; se o último for `name`, `nome`, `id`, `ids`, usa o anterior
   (`table_name` → `table`; `group.id` → `group`; `User ID` → `user`).
3. plural simples: tirar `s` final (`bootstrap.servers` → `server`, `buckets` → `bucket`).
4. **trava de modificador**: se o último pedaço for de configuração (`timeout`, `port`, `count`,
   `size`, `retries`, `mode`, `enabled`, `interval`, `version`, `file`, `path`, `encoding`),
   não é nome de coisa (`tcp_user_timeout`, `proxyPort`, `keepalives_count`).

**Valor**: mascarar inteiro, separando por `,` quando a chave é plural (lista de hosts) e
preservando `:porta`. **Não** mascarar: vazio, `true/false/yes/no/null/none`, número puro,
placeholder (`${VAR}`, `{{ env_var('X') }}`, `<...>`), e defaults públicos documentados
(`localhost`, `(local)`, `default`, `public`, `dbo`, `PUBLIC`) — eles não revelam nada e ajudam o
modelo a raciocinar.

**Tabela**: se o cabeçalho de uma coluna tem núcleo no vocabulário, a coluna inteira é mascarada
(é o que o Google chama de hotword no cabeçalho com `windowBefore = 1`, e o Macie de keyword
"no nome do campo ou coluna").

### 2. Posição → tipo de entidade

| Núcleo da chave (EN / PT) | Entidade | Pseudônimo |
|---|---|---|
| host, hostaddr, server, servidor, address, addr, data source, network address, failover partner, proxyhost, nonproxyhosts, workstation/wsid | HOST | `HOST_tu6pvlhb` |
| database, db, dbname, banco, catalog, initial catalog, project (BigQuery: `database` = projeto) | DATABASE | `DB_xoal5ygh` |
| schema, esquema, dataset | SCHEMA | `SCH_7h7ved4b` |
| table, tabela, relation, view | TABLE | `T_3gjogzbd` |
| column, coluna, field, campo | COLUMN | `C_etwhmr4n` |
| user, usuario, username, uid, login, owner, dono, proxyuser | USER | `USR_j4ed3tyq` |
| role, papel | ROLE | `USR_xgbsyz5d` |
| namespace | NAMESPACE | `NS_xcykm4tb` |
| cluster, context, current-context | CLUSTER | `HOST_yw4kukvb` |
| bucket | BUCKET | `BKT_xztzuyuw` |
| topic, topico, queue, fila, group (`group.id`) | FILA_TOPICO | `TOP_7bwlqw3s` |
| service, servico, application, app, instance, instancia | SERVICO | `SVC_rmaijbls` |
| warehouse | WAREHOUSE | `SVC_mdgkpbby` |
| account, conta, tenant | CONTA | `HOST_bth7wilh` |

Credenciais (`password`, `token`, `private_key`…) **não** são desta família: vão para a família de
segredos, que mascara sempre, sem olhar forma.

### 3. Vocabulário público e fonte de cada grupo

A lista sai das chaves que se repetem nas especificações oficiais de conexão e de configuração —
não de cliente, não de dicionário. Cada chave tem que aparecer em pelo menos uma spec abaixo
(as PT são tradução fechada do mesmo conceito, não vocabulário geral).

| Grupo | Chaves | Onde aparecem (oficial) |
|---|---|---|
| Host | `host`, `hostaddr` | libpq 32.1.2 Parameter Key Words |
| Host (sinônimos) | `Data Source`, `Server`, `Address`, `Addr`, `Network Address`, `Failover Partner`, `Workstation ID`/`WSID` | SqlConnection.ConnectionString (Microsoft Learn) |
| Host (proxy) | `proxyHost`, `nonProxyHosts` | Snowflake JDBC parameters |
| Host (broker) | `bootstrap.servers` | Apache Kafka consumer configs |
| Host (k8s) | `server` (do cluster) | Kubernetes, kubeconfig |
| Database | `dbname` | libpq |
| Database | `Initial Catalog`, `Database` | SqlConnection |
| Database / contexto | `db`, `schema`, `warehouse`, `role` | Snowflake JDBC ("Default context") |
| Database / schema | `database`, `schema`, `account`, `warehouse`, `user` | dbt Snowflake setup (profiles.yml) |
| Projeto / dataset | `database` = projeto GCP, `schema` = dataset | dbt BigQuery setup |
| Usuário | `user` | libpq, Snowflake JDBC (único obrigatório), dbt |
| Usuário (sinônimos) | `User ID`, `UID`, `User` | SqlConnection |
| Usuário (proxy) | `proxyUser` | Snowflake JDBC |
| Serviço / app | `service`, `application_name` | libpq |
| Serviço / app | `Application Name`, `App` | SqlConnection |
| Contexto k8s | `cluster`, `user`, `namespace` (os 3 parâmetros de um context), `current-context` | Kubernetes, kubeconfig |
| Fila / tópico | `group.id`, tópico (`subscribe(topic)`) | Apache Kafka consumer configs |
| Bucket | `bucket` | Amazon S3 (regras de nome de bucket) |

Grupo B (só com valor que tenha cara de identificador, por serem genéricas demais):
`base`, `target`, `source`, `origem`, `destino`, `stage`, `stream`, `task`, `pipe`, `name`/`nome`
sozinho. `[VERIFICAR]` com amostras antes de promover qualquer uma ao grupo A.

### 4. Regra de "cara de identificador" e exclusões

Vale para **propagar** um nome já aprendido por chave para a prosa. O custo é assimétrico:
propagar demais estraga texto; propagar de menos vaza. Por isso a forma decide só a prosa
livre; em contexto estrutural (crase, aspas, `a.b.c`, logo após "tabela/schema/host") o nome
aprendido é sempre trocado.

Tem cara de identificador se, com 3 a 255 caracteres, só ASCII `[A-Za-z0-9_$.-]`, tiver **um** de:
- `_` entre partes (`tb_pedido_x9`, `svc_relatorio`, `TB_PEDIDO_X9`);
- letra **e** dígito no mesmo token (`db01`, `ns-exemplo-02`);
- `$` interno (`SYS$X`, permitido em identificador Snowflake sem aspas);
- `.` entre partes que começam por letra/`_` (`vendas_x9.tb_pedido_x9`);
- caixa mista com 2+ "corcovas" (`PedidoItemX`, `pedidoItem`).

Base estrutural: identificador Snowflake sem aspas = letra/`_` + letras, dígitos, `_`, `$`;
nome k8s = minúsculas, dígitos, `-`, `.`; bucket S3 = minúsculas, dígitos, `.`, `-`. Nenhum
aceita acento ou espaço — por isso **acento derruba a forma** (palavra de prosa).

Hífen sozinho **não** é sinal (só conta com dígito ou junto de outro sinal).

| Caso de borda | Exemplo | Decisão | Por quê |
|---|---|---|---|
| Versão | `1.2.3`, `v2.10.0-rc1` | excluir | `^v?\d+(\.\d+)+([-+][\w.]+)?$` (SemVer) |
| Data / hora | `2026-10-05`, `05/10/2026`, `20261005T120000Z` | excluir | regex ISO 8601 e dd/mm/aaaa |
| UUID | `3f2c…-…-…` (8-4-4-4-12 hex) | excluir desta família | é ID, não nome; família de IDs/segredos |
| Hash | `9fceb02` (7–64 hex puros) | excluir | hex sem separador; família de IDs |
| Arquivo | `relatorio_final.xlsx`, `main.go` | excluir pela forma | extensão conhecida no último pedaço; mas o nome aprendido dentro dele é trocado: `tb_pedido_x9.sql` → `T_3gjogzbd.sql` |
| Hífen PT | `guarda-chuva`, `segunda-feira`, `pé-de-moleque`, `e-mail` | não é identificador | hífen só com letras não conta; acento derruba |
| Sigla | `SQL`, `API`, `HTTP`, `JSON` | não | só maiúsculas, sem `_` nem dígito |
| Sigla com dígito | `S3`, `EC2`, `UTF8`, `IPv4`, `OAuth2`, `K8s` | não por forma (≤ 6, sem separador) | mas se foi aprendido por chave, troca em contexto estrutural |
| camelCase público | `getConnection`, `DataFrame`, `PostgreSQL`, `GitHub` | não propagar se igual a palavra reservada/função pública | lista de permissão = palavras reservadas e funções da própria gramática (doc oficial do SQL/da lib), não lista de cliente |
| Nome qualificado vs. domínio | `vendas_x9.tb_pedido_x9` vs `example.com` | qualificado = sim; domínio = família de host/URL | TLD conhecido no fim → host |
| Número decimal | `3.14` | não | partes começam por dígito |

### 5. Exemplos antes/depois (dados fictícios)

```text
ANTES                                           DEPOIS
host: db-exemplo-01.interno                     host: HOST_tu6pvlhb
port: 5432                                      port: 5432                 (modificador)
Initial Catalog=vendas_x9;User ID=svc_relatorio Initial Catalog=DB_xoal5ygh;User ID=USR_j4ed3tyq
SERVIDOR     db-exemplo-01                      SERVIDOR     HOST_tu6pvlhb
--namespace ns-exemplo-02                       --namespace NS_xcykm4tb
bootstrap.servers=kfk-a1:9092,kfk-a2:9092       bootstrap.servers=HOST_3qmjbssv:9092,HOST_6vqnblkq:9092
schema: public                                  schema: public             (default público)
tcp_user_timeout=30                             tcp_user_timeout=30        (trava de modificador)
```

Tabela alinhada (cabeçalho com núcleo `table` e `column` → colunas mascaradas):

```text
table_name     column_name    data_type        TABLE_NAME   COLUMN_NAME  DATA_TYPE
tb_pedido_x9   cd_cliente_7   NUMBER     →     T_3gjogzbd    C_etwhmr4n    NUMBER
```

Propagação para a prosa:

```text
"a carga da tb_pedido_x9 falhou no db-exemplo-01"  → "a carga da T_3gjogzbd falhou no HOST_tu6pvlhb"
schema: cliente   (aprendido, mas sem cara de identificador)
"o cliente reclamou do schema `cliente`"           → "o cliente reclamou do schema `SCH_7h7ved4b`"
```

### 6. Abordagens do mercado e o que serve para nós

| Abordagem | Como funciona (oficial) | O que adotamos |
|---|---|---|
| **Palavra de contexto / hotword** | Presidio: palavras de contexto perto da entidade sobem a confiança (fator padrão 0,35, mínimo 0,4); contexto pode vir na requisição, ex. nome de coluna. Google SDP: hotword rule com `windowBefore/After` em caracteres ajusta a likelihood; `windowBefore = 1` em tabela = hotword no cabeçalho. Macie: keyword no nome do campo/coluna (dado estruturado) ou até N caracteres antes (padrão 50, 1–300). | Nosso vocabulário de chaves **é** a hotword, mas com janela **estrutural** (a chave da mesma linha / o cabeçalho da coluna) em vez de janela de caracteres. Na prosa, janela curta tipo Macie após "tabela/schema/host". |
| **Lista de bloqueio / permissão** | Presidio: `deny_list` marca tokens fixos; `allow_list` na chamada ignora valores. Google SDP: exclusion rules por dicionário, regex, `excludeInfoTypes` e `ExcludeByHotword`. Macie: ignore words (até 10). | Lista de bloqueio **dinâmica**: cada nome aprendido por chave entra na sessão e é propagado (§4). Lista de permissão **pública**: defaults documentados, palavras reservadas, versões/datas/hashes por regex. |
| **Likelihood / score** | Google SDP: 5 níveis VERY_UNLIKELY…VERY_LIKELY, ajuste fixo ou relativo. Comprehend: score por entidade. | 3 níveis internos: CERTO (posição de chave) e PROVÁVEL (forma + hotword na prosa) mascaram; POSSÍVEL (só forma) mascara se tiver `_`+dígito, senão só registra. |

Comprehend PII não tem tipo para host, database, tabela ou coluna (tem `USERNAME`, `URL`,
`IP_ADDRESS`, chaves AWS) — NER de mercado não cobre nome de objeto; a reserva é necessária.

### 7. O que esta reserva NÃO garante

- **Nome em prosa sem estrutura**: "a base de vendas do norte caiu" — sem chave, sem forma.
- **Palavra simples como nome**: schema `cliente`, tabela `pedidos`. Mascarada na posição de chave,
  mas não propagada para a prosa livre (só em crase/aspas). Vaza se citada solta.
- **Nome partido**: `tb_pedido` numa linha e `_x9` na outra; "tabela pedido x9" por extenso.
- **Abreviação / apelido**: `t1`, `p`, "a PX9", alias de SQL; tradução ("tabela de pedidos").
- **Chave fora do vocabulário**: `alvo: ...`, `onde: ...`, chave em outro idioma, chave ofuscada.
- **Chave enganosa**: `user: true` passa (booleano); `name: tb_pedido_x9` só se `name` for Grupo B
  e o valor tiver forma.
- **Valor multilinha / aninhado** (`table: |`, objeto inline) que o leitor genérico não segue.
- **Nome dentro de mensagem de erro** sem aspas, ou em imagem/print.
- **Inferência pelo modelo**: mesmo com pseudônimo, contexto (região, volume, ramo) pode reidentificar.
- **Colisão**: nome aprendido igual a palavra comum ou API pública é trocado demais (ou excluído
  pela lista de permissão e aí vaza na prosa).

### Links usados

- Presidio — palavras de contexto: https://presidio.dataprivacystack.org/tutorial/06_context/
- Presidio — deny list: https://presidio.dataprivacystack.org/tutorial/01_deny_list/
- Presidio — allow list: https://presidio.dataprivacystack.org/tutorial/13_allow_list/
  (o antigo microsoft.github.io/presidio redireciona para cá; projeto em transição para mantenedor comunitário)
- Google SDP — hotword rules: https://docs.cloud.google.com/sensitive-data-protection/docs/creating-custom-infotypes-likelihood
- Google SDP — exclusion rules: https://docs.cloud.google.com/sensitive-data-protection/docs/creating-custom-infotypes-rules
- Google SDP — likelihood: https://docs.cloud.google.com/sensitive-data-protection/docs/likelihood
- Amazon Macie — keywords, ignore words, distância: https://docs.aws.amazon.com/macie/latest/user/cdis-options.html
- Amazon Comprehend — tipos de PII: https://docs.aws.amazon.com/comprehend/latest/dg/how-pii.html
- PostgreSQL libpq — Parameter Key Words: https://www.postgresql.org/docs/current/libpq-connect.html
- SqlConnection.ConnectionString: https://learn.microsoft.com/en-us/dotnet/api/system.data.sqlclient.sqlconnection.connectionstring
- Snowflake JDBC parameters: https://docs.snowflake.com/en/developer-guide/jdbc/jdbc-parameters
- Snowflake identificadores: https://docs.snowflake.com/en/sql-reference/identifiers-syntax
- dbt Snowflake setup: https://docs.getdbt.com/docs/core/connect-data-platform/snowflake-setup
- dbt BigQuery setup: https://docs.getdbt.com/docs/core/connect-data-platform/bigquery-setup
- Kubernetes kubeconfig: https://kubernetes.io/docs/concepts/configuration/organize-cluster-access-kubeconfig/
- Kubernetes nomes: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/
- Apache Kafka consumer configs: https://kafka.apache.org/43/configuration/consumer-configs/
- Amazon S3 nomes de bucket: https://docs.aws.amazon.com/AmazonS3/latest/userguide/bucketnamingrules.html
- SemVer (regex de versão): https://semver.org/ [DOC ✓] — não consultado nesta rodada


## Pipelines de CI

Num pipeline quase tudo é vocabulário da ferramenta (chaves, ações públicas, rótulos de runner
hospedado). Os nomes próprios ficam em poucos lugares estáveis: a **imagem** de contêiner, o
**runner/agente** próprio e o **ambiente** de deploy. Só esses são tratados; o resto (comandos de
`script`/`run`, variáveis) passa pelos outros detectores.

### 1. Sinais de detecção

| Formato | Sinal forte | Sinal fraco |
|---|---|---|
| GitHub Actions (`.github/workflows/*.yml`) | `jobs:` no topo com `runs-on:` nos jobs | `on:` no topo, `steps:` com `uses:` |
| GitLab CI (`.gitlab-ci.yml`) | job com `script:`; `stages:` no topo | `image:`, `services:`, `tags:` no job |
| Azure Pipelines (`azure-pipelines.yml`) | `trigger:`/`pool:`/`stages:`/`jobs:`/`steps:` no topo | `- script:`, `- task:` |
| Jenkinsfile (declarativo) | `pipeline {` | `agent {`, `stages {`, `stage('x') {` |
| Jenkinsfile (scripted) | `node(` junto com `stage(` | — |

### 2. Posição → tipo de entidade

| Formato | Posição | Entidade | Observação |
|---|---|---|---|
| GitHub Actions | `jobs.<id>.runs-on` (texto, lista, `group:`, `labels:`) | HOST | evidência fraca; rótulos públicos ficam |
| GitHub Actions | `jobs.<id>.container` (texto ou `.image`), `jobs.<id>.services.<nome>.image` | imagem | regra de imagem (seção Kubernetes, item 3) |
| GitHub Actions | `jobs.<id>.environment` (texto ou `.name`) | SVC | evidência fraca |
| GitLab CI | `image` (texto ou `.name`), `services[]` (texto ou `.name`) | imagem | regra de imagem |
| GitLab CI | `<job>.tags[]` | HOST | evidência fraca; só em job (com `script`/`stage`...) |
| GitLab CI | `<job>.environment` (texto ou `.name`) | SVC | evidência fraca |
| Azure Pipelines | `pool` (texto) ou `pool.name` | HOST | evidência fraca; `pool.vmImage` fica |
| Azure Pipelines | `container` (texto ou `.image`), `environment` | imagem / SVC | — |
| Jenkinsfile | `agent { label 'x' }`, `node('x')` | HOST | evidência fraca |
| Jenkinsfile | `docker { image 'x' }`, `docker.image('x')` | imagem | regra de imagem |

Evidência fraca: o valor é mascarado onde aparece, mas só é aprendido (e propagado para o resto do
texto) se aparecer também em outra regra.

### 3. Vocabulário público (nunca mascarar)

| Vocabulário | Exemplos | Fonte |
|---|---|---|
| rótulos de runner hospedado | `ubuntu-latest`, `ubuntu-24.04`, `windows-latest`, `windows-2022`, `macos-latest`, `macos-14`, `self-hosted`, `linux`, `x64`, `arm64` | doc "GitHub-hosted runners" e "self-hosted runners" (rótulos padrão) |
| imagens públicas | sem registro (`node:20`, `postgres:16`) ou registro público oficial (`docker.io`, `ghcr.io`, `quay.io`, `gcr.io`, `registry.k8s.io`, `mcr.microsoft.com`, `public.ecr.aws`) | regra de imagem |
| ambientes genéricos | `production`, `staging`, `development`, `dev`, `prod`, `test`, `qa` | — |
| expressões | `${{ ... }}` (Actions), `$VAR` / `${VAR}` (GitLab), `$(var)` (Azure) | não são valores: ficam |
| chaves e ações | `jobs`, `steps`, `uses: actions/checkout@v4`, `script`, `stage`, `pipeline`, `agent` | spec de cada ferramenta |

### 4. Regras de identificador

- Valor mascarado só tem letras, dígitos, `_`, `.` e `-` (sem espaço, aspas ou expressão). Rótulo
  composto do Jenkins (`'linux && docker'`) fica.
- Imagem: `[registro/]caminho[:tag][@digest]`; registro = primeiro pedaço com `.` ou `:`. Registro
  privado vira HOST, cada pedaço do caminho vira SVC; a tag e o digest ficam.
- `tags:` só conta como runner dentro de um job do GitLab: em tarefa do Ansible (lista na raiz), `tags`
  é rótulo de tarefa e fica.

### 5. Exemplos antes/depois

```yaml
# antes                                          # depois
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
// antes                                           // depois
pipeline {                                         pipeline {
  agent { label 'agente-build-x6' }                  agent { label 'svc_...' }   // HOST
  ...                                                ...

### 8. O que está implementado

A reserva de chave-valor é o leitor `chave-valor` (ver a seção JSON, YAML, TOML..., §8). Além
dele, dois leitores genéricos sem formato:

**Host interno em URL e endereço solto** (leitor `endereço`, regras `url-interna` e
`host-interno`). Em `http(s)://`, `ws(s)://`, `grpc://`, `ftp://`, `redis://`... (qualquer
esquema que não seja de banco, armazenamento, fila ou git), o host é servidor **forte** quando é
interno: rótulo único sem ponto (`http://wiki-interna/`) ou terminado em `.local` (RFC 6762),
`.internal` (reserva da ICANN, 2024), `.home.arpa` (RFC 8375), `.svc`/`.cluster.local` (DNS do
Kubernetes), `.intra`, `.intranet`, `.interno`, `.corp`, `.lan`, `.localdomain`. O usuário antes do
`@` também. Domínio público fica (os do cliente vão em `dominios_internos`). Fora de URL, um nome
com esses sufixos (`db01.corp:5432`, `redis.vendas.svc.cluster.local`) também é servidor, se tiver
dígito ou hífen, ou dois rótulos antes do sufixo, e não for atributo de código (`threading.local()`)
nem pacote Java (`org.foo.internal`).

**Termo cadastrado dentro de um identificador** (leitor `termo-embutido`, ligado quando há
`termos` de uma palavra só). O detector de termos troca a palavra inteira; este troca o
identificador com cara de identificador que tem o termo como pedaço inteiro (separado por
`_ . -` ou camelCase): termo `acmex` pega `acmex_pedidos`, `dbAcmexVendas01`, `svc-acmex-carga`,
mas não `acmexvendas` nem `macmex_x`. O tipo vem da posição (depois de `FROM`/`JOIN` → tabela,
de `DATABASE`/`USE` → database, valor de chave conhecida → a entidade da chave, host de URL →
servidor); sem posição, servico. Sempre forte.

| Regra | Pega | Forte? |
|---|---|---|
| `url-interna` | host interno em URL; usuário da URL | sim |
| `host-interno` | `nome.sufixo-interno` fora de URL | sim |
| `termo-embutido` | identificador com um termo cadastrado como pedaço | sim |

## Repositórios, pacotes e caminhos

Ideia central: o nome da organização, do repositório, do pacote interno e do usuário dono de uma
pasta aparece em posições fixas de formatos públicos (remoto do git, `go.mod`, `pom.xml`,
`package.json`, caminho de home). A posição diz o tipo; os hosts e prefixos públicos ficam.
Código: `internal/mask/leitor_enderecos.go` (git, caminhos) e `leitor_nuvem_pacotes.go` (pacotes).

### 1. Sinais de detecção

| Família | Sinal (filtro barato antes de qualquer análise) |
|---|---|
| Remoto do git (scp) | `git@host:org/repo(.git)` — só se o texto tiver `git@` |
| Remoto do git (URL) | `ssh://`, `git://`, `git+ssh://` sempre; `https://` só se o caminho terminar em `.git`, se vier depois de `git clone`, `git remote`, `git push`, `git pull`, `git fetch` ou `git submodule` na mesma linha, ou se for o `url =` de uma seção `[remote "..."]`/`[submodule "..."]` do `.git/config` |
| Módulo Go | linha que começa com `module ` e só tem o caminho (go.mod) |
| groupId | `<groupId>...</groupId>` (Maven) ou `group = '...'` / `group "..."` no começo da linha (Gradle) |
| Escopo npm | `"name": "@escopo/pacote"` (package.json) |
| Caminho de usuário | `/home/<u>/`, `/Users/<u>/`, `C:\Users\<u>\` (também `C:\\Users\\` escapado em JSON) |

### 2. Posição → tipo de entidade

| Formato | Posição | Entidade | Forte? |
|---|---|---|---|
| remoto do git | pedaços antes do último (grupo e subgrupos; `scm`, `_git`, `v3` são da hospedagem e ficam) | organizacao (`ORG_`) | sim |
| remoto do git | último pedaço sem `.git` | repositorio (`REPO_`) | sim |
| remoto do git | host interno (rótulo único ou sufixo interno) | servidor (`HOST_`) | sim |
| remoto do git | usuário de `ssh://usuario@` (não `git`) | usuario (`USR_`) | sim |
| go.mod | host fora da lista pública: interno → servidor; cada pedaço do caminho (menos `vN`) | servidor / pacote (`PKG_`) | sim |
| groupId | pedaços depois do TLD invertido (`br.com.`, `com.`) | pacote | sim |
| package.json | `@escopo` / `pacote` | organizacao / pacote | sim |
| caminho de home | o pedaço depois de `home`/`Users` | usuario | sim |
| caminho de home | pastas seguintes com cara de identificador (não o arquivo final) | pasta (`DIR_`) | não |

Fora desses contextos, uma URL comum do github.com ou gitlab.com **não** é mascarada (aparece em
toda documentação pública). Medido em 2 MB de `.md` de módulos Go: 6 nomes em contexto de git.

### 3. Vocabulário público (nunca mascarar)

| Grupo | Lista | Fonte |
|---|---|---|
| Hosts de módulo Go | `github.com`, `gitlab.com`, `bitbucket.org`, `golang.org`, `google.golang.org`, `gopkg.in`, `go.uber.org`, `k8s.io`, `sigs.k8s.io`, `example.com/org/net` | os hosts de módulo mais comuns; `example.*` reservado (RFC 2606) |
| Prefixos de groupId | `org.apache`, `org.springframework`, `com.google`, `io.*`, `javax`, `jakarta`, `org.jetbrains`, `org.junit`, `junit`, `org.slf4j`, `ch.qos`, `com.fasterxml`, `org.hibernate`, `org.projectlombok`, `org.mockito`, `org.eclipse`, `com.amazonaws`, `software.amazon`, `com.microsoft`, `com.azure`, `org.postgresql`, `com.mysql`, `com.oracle`, `com.h2database`, `org.yaml`, `com.squareup`, `org.codehaus`, `org.gradle`, `com.android`, `androidx`, `org.jboss`, `org.testcontainers`, `org.flywaydb`, `org.liquibase`, `commons-*`, `com.github`, `org.example`, `com.example`... (lista completa em `gruposPublicos`) | coordenadas do Maven Central (central.sonatype.org) e grupos mais usados (mvnrepository.com/popular) |
| Escopos npm | `@types`, `@angular`, `@babel`, `@vue`, `@nestjs`, `@aws-sdk`, `@google-cloud`, `@azure`, `@mui`, `@testing-library`, `@typescript-eslint`, `@storybook`, `@tanstack`, `@octokit`, `@sentry`... (lista em `escoposNpmPublicos`) | escopos mais baixados do registro npm |
| Usuários de caminho | `user`, `runner` (GitHub Actions), `ubuntu`, `ec2-user`, `azureuser`, `opc`, `vagrant`, `jovyan` (Jupyter), `linuxbrew` (Homebrew), `node`, `gopher`, `Shared`, `Public`, `Default`... | usuários padrão das imagens de nuvem e de CI |
| Pastas | `node_modules`, `site-packages`, `dist-packages`, `__pycache__`, `AppData`, `LocalLow`, `OneDrive`, `IdeaProjects`, `PycharmProjects`, `go-build*`, ferramenta+versão (`python3.10`, `go1.22.0`), pastas ocultas (`.config`, `.venv`) | Known Folders do Windows, layout do macOS, Python, Node, Go, JetBrains |

As pastas sem cara de identificador (`Documents`, `Desktop`, `src`, `bin`, `projetos`) já ficam pela regra geral.

### 4. Regras de identificador

- Repositório e organização: `[A-Za-z0-9_.-]+`, começa por letra ou dígito (regra do GitHub e do GitLab).
- Módulo Go: caminho de importação (`[A-Za-z0-9._~/-]`); `v2`, `v3`... são versão maior e ficam.
- groupId: pedaços `[A-Za-z_][A-Za-z0-9_$#-]*` separados por ponto.
- Escopo npm: minúsculas, dígitos, `-`, `_`, `.` (regras de nome do npm).
- Usuário de caminho: `[A-Za-z0-9._-]{1,32}`, seguido de separador ou fim; `$USER` e `<voce>` ficam.

### 5. Exemplos antes/depois

```text
git clone git@github.com:org-exemplo/repo-demo.git   →  git clone git@github.com:ORG_.../REPO_....git
git remote add origin https://gitlab.interno/org-exemplo/sub/repo-demo  →  https://host_.../org_.../org_.../repo_...
module git.interno/org-exemplo/svc-pedidos   →  module host_.../pkg_.../pkg_...
<groupId>br.com.exemplo01.vendas</groupId>   →  <groupId>br.com.pkg_....pkg_...</groupId>
"name": "@org-exemplo/pacote-demo"           →  "name": "@org_.../pkg_..."
/home/joao_x/projetos/cliente_x9/main.go     →  /home/usr_.../projetos/dir_.../main.go
https://github.com/org-exemplo/repo-demo/issues/1   (fica: fora de contexto de git)
```

### 6. Casos difíceis e limites

- **Matriz e expressões.** `runs-on: ${{ matrix.os }}` não tem valor literal: fica.
- **Runner com nome genérico** (`build`, `linux-grande`): mascarado no lugar, não propaga (não tem
  cara de identificador ou é evidência fraca).
- **Ações de terceiros** (`uses: org/acao@v1`) e `include:` do GitLab apontam para repositórios; ficam
  com as regras de URL/repositório, não com estas.
- **Variáveis de ambiente** (`env:`, `variables:`) são chave-valor comum (reserva genérica).
- **Jenkins scripted** com lógica Groovy arbitrária: só `node('x')` e `docker.image('x')` são estáveis.

### 7. Links usados

- GitHub Actions, sintaxe de workflow: https://docs.github.com/actions/reference/workflows-and-actions/workflow-syntax
- GitHub-hosted runners (rótulos): https://docs.github.com/actions/reference/runners/github-hosted-runners
- GitLab CI/CD YAML: https://docs.gitlab.com/ci/yaml/
- Azure Pipelines YAML schema: https://learn.microsoft.com/azure/devops/pipelines/yaml-schema/
- Jenkins Pipeline syntax: https://www.jenkins.io/doc/book/pipeline/syntax/
- não consultados nesta rodada [VERIFICAR]: os links acima foram escritos de memória

- `git clone https://github.com/golang/go.git` num README é mascarado (está em contexto de git):
  o leitor não sabe se o projeto é público. Nomes simples (`golang`) não propagam.
- Caminho com espaço (`C:\Users\Maria Souza\`) não é lido (o usuário teria espaço).
- `import "git.interno/org/x/pkg"` em código Go: não há leitor de import; o nome aprendido no
  `go.mod` só propaga se tiver cara de identificador (`svc-pedidos` sim, `vendas` não).
- groupId de empresa sob `com.github.*` ou `io.*` fica (prefixo público).

### 7. Links usados

- Git — URLs: https://git-scm.com/docs/git-clone#_git_urls
- Go Modules Reference (go.mod, module path): https://go.dev/ref/mod
- Maven Central — coordenadas: https://central.sonatype.org/publish/requirements/coordinates/
- Maven — convenção de nomes: https://maven.apache.org/guides/mini/guide-naming-conventions.html
- npm — scope: https://docs.npmjs.com/cli/v10/using-npm/scope ; package.json name: https://docs.npmjs.com/cli/v10/configuring-npm/package-json#name
- Windows Known Folders: https://learn.microsoft.com/windows/win32/shell/knownfolderid
- AWS — usuários padrão das AMIs: https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/managing-users.html

## Recursos de nuvem e usuários de rede

**Snowflake** (as mesmas regras genéricas, mais as formas do produto). Conta: chaves
`account`/`account_id`/`accountname` em qualquer sintaxe (chave-valor e argumento nomeado); o
host `<conta>.snowflakecomputing.com` (a conta, sem a região) e `app.snowflake.com/<org>/<conta>/`
(`leitor_nuvem_pacotes.go`); depois, solta no texto, pela propagação. Papel (`role`, `rolename`,
`SNOWFLAKE_ROLE`) é usuário; no SQL, o nome depois de `ROLE` ou `USER` (`USE ROLE`, `GRANT ... TO
ROLE`, `REVOKE ... FROM ROLE`, `GRANT ROLE x TO USER y`, `CREATE/ALTER/DROP ROLE`) é usuário.
Warehouse é serviço: chave `warehouse`, `USE WAREHOUSE x`, `WAREHOUSE = x`, `CREATE/ALTER
WAREHOUSE x`. Listas allow/deny (`allowed_roles: [..]`, `blocked_users`) têm itens do tipo da
chave. Nomes de MASKING/ROW ACCESS/NETWORK... POLICY, TAG, STAGE (também `@db.sch.stage`), STREAM,
TASK, PIPE, SEQUENCE, FILE FORMAT, SECRET, ALERT, DYNAMIC TABLE são objetos de schema (nome
qualificado, último pedaço como tabela); `COPY INTO` é instrução. Os parâmetros de DDL
(`SCHEDULE`, `URL`, `AUTO_SUSPEND`...) estão no vocabulário do SQL. Saída de SHOW em JSON: `name`
ao lado de `schema_name` é tabela (ao lado só de `database_name`, schema); `owner` é usuário. URN
do DataHub com partes preenchidas com espaços (`db2,ABCD    .TABELA`) é lida sem os espaços.

Ideia central: identificadores de recurso de nuvem têm gramática publicada (ARN, ID do Azure
Resource Manager, nome de recurso do Google Cloud); usuário de rede tem duas formas fixas
(`DOMINIO\usuario`, `usuario@host` depois de ssh/scp). A gramática diz o tipo de cada pedaço.
Código: `internal/mask/leitor_nuvem_pacotes.go` (nuvem, IP público) e `leitor_enderecos.go`
(usuário de rede).

### 1. Sinais de detecção

| Família | Sinal (filtro antes de analisar) |
|---|---|
| ARN | texto com `arn:`; `arn:aws[-cn|-us-gov|-iso...]:serviço:região:conta:recurso` |
| Azure | texto com `/subscriptions/`; `/subscriptions/<guid>/resourceGroups/<rg>/providers/<Ns>/<tipo>/<nome>...` |
| Google Cloud | texto com `projects/`; `projects/<p>/<coleção>/<nome>` ou `projects/<p>/locations/<l>/<coleção>/<nome>` |
| DOMINIO\usuario | texto com `\`; domínio NetBIOS (maiúsculas, dígitos, `-`; 2 a 15) antes da barra |
| ssh/scp/sftp/rsync | o comando como palavra, e na mesma linha um argumento `usuario@host[:caminho]` |
| IP público (opção `ip_publico`) | IPv4 com 3 pontos ou IPv6 com 2+ `:` |

### 2. Posição → tipo de entidade

| Formato | Posição | Entidade | Forte? |
|---|---|---|---|
| ARN | conta (12 dígitos) | conta_nuvem (`ACC_`) | sim |
| ARN | recurso do `s3` (até `/`) | bucket | sim |
| ARN | recurso do `sqs` e do `sns` | fila | sim |
| ARN | `dynamodb` `table/<nome>` | tabela | sim |
| ARN | `iam` `user/<nome>` | usuario | sim |
| ARN | demais (`tipo/nome`, `tipo:nome`, `role/caminho/nome`) | servico | sim |
| ARN | partição, serviço, região | **nunca** | — |
| Azure | GUID da assinatura / resource group / nome de cada recurso (`tipo/nome`) | conta_nuvem / servico / servico | sim |
| Google Cloud | projeto | conta_nuvem | sim |
| Google Cloud | `topics`/`subscriptions`/`queues` → fila; `datasets` → schema; `tables` → tabela; `buckets` → bucket; demais (`instances`, `functions`, `services`...) → servico | conforme coleção | sim |
| `DOMINIO\usuario` | usuário (o domínio fica com os outros detectores) | usuario | sim |
| `ssh usuario@host` | usuário / host fora de domínio público e que não seja IP | usuario / servidor | sim |
| IP público | IPv4 público ou IPv6 | servidor | sim |

### 3. Vocabulário público (nunca mascarar)

| Grupo | Lista | Fonte |
|---|---|---|
| Contas de exemplo da AWS | `123456789012`, `111122223333`, `444455556666`, `012345678901`; recursos gerenciados (`arn:aws:iam::aws:policy/...`) | documentação da AWS |
| Exemplos do Google Cloud / Azure | `my-project`, `project-id`, `my-topic`, `my-subscription`; assinatura `00000000-0000-...`; `my-resource-group` | documentação do Google Cloud e do Azure |
| Domínios e contas do Windows | `NT AUTHORITY`, `BUILTIN`, `NT SERVICE`, `WORKGROUP`, raízes do Registro (`HKLM`, `HKCU`...); contas `SYSTEM`, `Administrator(s)`, `Guest` | Well-known SIDs (Microsoft Learn) |
| Usuários de ssh | `root`, `git`, `ubuntu`, `ec2-user`, `admin`, `pi`... | usuários padrão de imagens |
| IPs | `::1`, link-local, `0.0.0.0`, broadcast, multicast, faixas privadas (o detector de IP já cuida), faixas de documentação (RFC 5737: `192.0.2.0/24`, `198.51.100.0/24`, `203.0.113.0/24`; RFC 3849: `2001:db8::/32`), DNS públicos `8.8.8.8`, `8.8.4.4`, `1.1.1.1`, `1.0.0.1`, `9.9.9.9`, `208.67.222.222`, `2001:4860:4860::8888`, `2606:4700:4700::1111` | IANA Special-Purpose Address Registry |

### 4. Regras de identificador

- Conta AWS: exatamente 12 dígitos. Recurso com curinga ou variável (`fila-*`, `${Nome}`) fica.
- GUID do Azure: 36 caracteres com 4 hífens. Nomes `[A-Za-z0-9_.-]+`.
- Projeto do Google Cloud: 6 a 30 caracteres, minúsculas, dígitos e hífen, começa por letra (sem ponto).
- `DOMINIO\usuario`: o usuário começa por letra, tem 2+ caracteres, e não continua com `\` ou `/`
  (`HKLM\SOFTWARE\...` e `\\SERVIDOR\compartilhamento` ficam). Sequência de escape de código
  (`"ERRO\nfalhou"`: usuário começando por `n t r b f v a x u 0` colado a outro caractere) fica.
- IPv4 com os três primeiros números de um dígito (`6.0.6.1`, `8.2.4.44`) é versão ou número de
  seção e fica. IPv6 precisa de 3+ `:` ou de um grupo com 3+ dígitos hexadecimais (`x[1::2]` fica).

### 5. Exemplos antes/depois

```text
arn:aws:sqs:us-east-1:210987654321:fila-pedidos-x9     →  arn:aws:sqs:us-east-1:ACC_...:top_...
arn:aws:s3:::bkt-relatorios-demo/*                      →  arn:aws:s3:::bkt_.../*
/subscriptions/1a2b3c4d-1111-2222-3333-abcdefabcdef/resourceGroups/rg-dados-x9/providers/Microsoft.Storage/storageAccounts/contaexemplo01
   →  /subscriptions/acc_.../resourceGroups/svc_.../providers/Microsoft.Storage/storageAccounts/svc_...
projects/proj-exemplo-01/topics/fila-pedidos-x9         →  projects/acc_.../topics/top_...
entrou como CORPX\svc_relatorio                         →  entrou como CORPX\usr_...
ssh -p 2222 svc_relatorio@db-exemplo-01                  →  ssh -p 2222 usr_...@host_...
servidor em 34.120.10.5 (com ip_publico ligado)        →  servidor em HOST_...
```

### 6. Casos difíceis e limites

- IP público vem **desligado**: um IP de serviço público (CDN, API) também seria trocado, e o
  modelo perde a referência. Ligue com `objetos.ip_publico`.
- Em ARN de IAM sem caminho (`role/nome`) o nome é servico; em `user/nome`, usuario.
- `ssh host` sem usuário: o host só é mascarado se outro leitor o pegar (sufixo interno,
  `dominios_internos`).
- `DOMINIO\usuario` com usuário que tem espaço não é lido.
- Nome de recurso do Google Cloud fora de `projects/...` (`gs://`, `bq://`) fica com outros leitores.

### 7. Links usados

- ARN: https://docs.aws.amazon.com/IAM/latest/UserGuide/reference-arns.html
- Azure Resource Manager — IDs e regras de nome: https://learn.microsoft.com/azure/azure-resource-manager/management/resource-name-rules
- Google Cloud — nomes de recurso: https://cloud.google.com/apis/design/resource_names
- Google Cloud — ID de projeto: https://cloud.google.com/resource-manager/docs/creating-managing-projects
- Windows — Well-known SIDs: https://learn.microsoft.com/windows/win32/secauthz/well-known-sids
- NetBIOS — nomes de domínio: https://learn.microsoft.com/troubleshoot/windows-server/active-directory/naming-conventions-for-computer-domain-site-ou
- IANA — IPv4 e IPv6 Special-Purpose Address Registry: https://www.iana.org/assignments/iana-ipv4-special-registry/ ; https://www.iana.org/assignments/iana-ipv6-special-registry/
- RFC 5737 (IPv4 de documentação): https://www.rfc-editor.org/rfc/rfc5737 ; RFC 3849 (IPv6): https://www.rfc-editor.org/rfc/rfc3849
