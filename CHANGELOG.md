# Mudanças

O que mudou em cada versão do llm-dlp. As versões seguem `MAIOR.MENOR.CORREÇÃO`; o que cada
número significa aqui está em [docs/versoes.md](docs/versoes.md). A mais nova vem primeiro.

Em cada versão: **Mascaramento** é o que passou a ser (ou deixou de ser) mascarado;
**Programa** é comando, instalação, configuração e desempenho; **Código** é o que só interessa
a quem mexe no código.

## [0.2.0] - 2026-10-10

Nome público só fica em claro com prova. Toda regra que deixa um nome passar exige agora uma
garantia de estrutura, de falha fechada: na dúvida, mascara.

### Mascaramento

- **Escrita não latina:** nome de banco, schema, tabela e usuário em qualquer escrita
  (cirílico, japonês, acentos) é mascarado. Usuário com ponto (`maria.souza`) também.
- **Traceback** (Python, Java, JavaScript, Go): a linha que aponta para o projeto tem a função,
  o módulo, o código e o valor citado no erro mascarados. A linha de dependência ou da
  biblioteca padrão (`pandas`, `net/http`) fica legível.
- **Imagem de contêiner:** só fica em claro com duas chaves, organização fornecedora **e** nome
  de software público. `acme/redis` e a imagem construída no projeto são mascaradas.
- **Software público na conversa:** o nome padrão de um software (`airflow`, `datahub`,
  `superset`) deixa de ser espalhado pela conversa quando ela mostra que é o software
  (instalado, importado, rodado como imagem pública ou executado). Palavra de dicionário, nome
  definido no projeto e termo cadastrado nunca recebem essa prova.
- **Origem pelo caminho:** os leitores de caminho param no pacote público
  (`site-packages/sqlalchemy/...` fica inteiro) e continuam lendo as pastas do projeto debaixo
  de usuário público (`/home/ubuntu/<projeto>`). Arquivo de dependência lido não ensina nomes à
  conversa. Estar numa pasta de dependências não basta: o pacote, o módulo ou o grupo tem de
  ser público, porque o pacote interno do cliente fica nas mesmas pastas.
- **docker-compose:** serviço reconhecido por qualquer chave da especificação, inclusive `<<`.
- **Listagem de linha de comando:** o comando à vista (`$ airflow dags list`) diz que o
  cabeçalho é da ferramenta; num tipo fora do vocabulário, só a coluna de identidade
  (`dag_id`) é tipada. Tabela de `kubectl get` e `docker ps` é lida pelo cabeçalho.
- **Hosts de serviço gerenciado de nuvem** (RDS, Redshift, S3, Azure, GCP): o rótulo do cliente
  é mascarado e o domínio do provedor fica.

### Programa

- Versão e mudanças organizadas: `llm-dlp versao`, este arquivo, binários prontos em cada
  release e `make release` (ver [docs/versoes.md](docs/versoes.md)).
- Estimativa de uso de CPU, memória e disco em
  [docs/como-funciona.md](docs/como-funciona.md#quanto-usa-da-máquina).

### Código

- Listas geradas em `internal/mask/dados/`; arquivos do pacote `mask` agrupados por prefixo
  (`detectores_`, `campos_`, `leitor_`, `chamada_`, `decisor_`, `memoria_`, `vocab_`). O mapa e
  a tabela do que fica guardado estão em [docs/desenvolvimento.md](docs/desenvolvimento.md).
- Listas novas, geradas: `software_publico.txt` e `fornecedores.txt` (repositórios do GitHub
  com 3000 estrelas ou mais), `imagens_oficiais.txt` (Imagens Oficiais do Docker) e
  `palavras_dicionario.txt` (50 mil palavras mais frequentes de português e inglês).

### Medido

Contra a 0.1.0, em nomes rotulados à mão de 16 repositórios públicos:

| | 0.1.0 | 0.2.0 |
|---|---|---|
| Nome do dono que vazou (5 repositórios, validação) | 6 de 22 | 1 de 22 |
| Nome do dono que vazou (11 repositórios novos, validação) | 2 de 9 | 1 de 9 |
| Nome público mascarado à toa (5 repositórios, validação) | 138 de 207 | 124 de 207 |
| Nome público mascarado à toa (11 repositórios novos, validação) | 40 de 122 | 46 de 122 |
| Palavras trocadas em prosa / código / configuração | 2,29% / 2,06% / 5,47% | 2,16% / 1,70% / 4,91% |
| Tempo para 21 MB de texto novo (2 núcleos) | 75 s | 77 s |
| Memória ao subir | 21 MB | 25 MB |

O excesso que subiu nos repositórios novos são rótulos de catálogo de tradução (`"User":
"Gebruiker"`): uma regra que os liberava também liberava dado de planilha exportada, e foi
recusada.

## [0.1.0] - 2026-10-08

Primeira versão: tudo o que existia até o commit `c7bb6cf`.

### Mascaramento

- **Dado pessoal e segredo pelo formato:** e-mail, CPF, CNPJ, telefone, cartão, documentos com
  dígito verificador, tokens e chaves (gitleaks), senhas comuns.
- **Dado pessoal pelo nome do campo:** `campo: valor`, JSON, SQL, XML, CSV, planilha, tabela de
  terminal.
- **Pessoas cadastradas** (nomes, e-mails, códigos), guardadas só como hash.
- **Nomes de recursos internos pela estrutura:** SQL e DDL, tabelas em qualquer desenho,
  esquemas, strings de conexão, YAML/JSON de Kubernetes, Helm, compose, Terraform, Ansible e
  CI, chave-valor, endereços, caminhos, pacotes, nuvem, código e linha de comando.
- **Normalização de transporte:** o conteúdo é lido como chega (número de linha, `grep`, diff,
  ANSI, JSON escapado).
- **Memória da conversa e rastreamento:** o nome decidido onde entra vale em toda a conversa;
  o que o modelo escreveu antes de qualquer dado é público; palavra comum fica como palavra na
  prosa.
- **Imagens e PDFs:** OCR e tarja preta.

### Programa

- Proxy local para o Claude Code, com falha fechada, volta exata e texto já enviado congelado
  (o cache da API continua valendo).
- `llm-dlp instalar` (programa, configuração, OCR, Claude Code, trava), `status`, `parar`,
  `emergencia`, `desinstalar`.
