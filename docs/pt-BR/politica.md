# Política: o que o llm-dlp protege e por quê

[← voltar ao README](README.md) · [English](../policy.md)

Este documento diz, para quem usa o llm-dlp, que tipo de informação existe numa conversa com o
Claude Code, o que o llm-dlp faz com cada tipo e em que referências legais e técnicas isso se
apoia. Também diz, com todas as letras, o que ele **não** protege.

## Os quatro níveis

A classificação em quatro níveis é a convenção mais comum nas políticas de classificação de
informação das empresas. A ISO/IEC 27001:2022 (Anexo A, controle 5.12, "Classificação da
informação") exige que a organização tenha um esquema de classificação, mas não fixa os
níveis; cada empresa define os seus. Os níveis abaixo são os que o llm-dlp usa para explicar o
próprio comportamento. Se a sua empresa tem outra escala, vale a dela: use esta tabela para
fazer a correspondência.

| Nível | O que cobre | Padrão do llm-dlp |
|---|---|---|
| **Restrito** | Credenciais e segredos (senhas, chaves e tokens de API, cabeçalhos de autenticação, chaves privadas); dados pessoais sensíveis (LGPD, art. 5º, II); dados de cartão e de conta | **Sempre mascarado**, onde quer que apareça, pelo formato e pela palavra por perto. Imagem e PDF passam por OCR e tarja. Na dúvida, mascara; com falha, recusa (falha fechada) |
| **Confidencial** | Dados pessoais (LGPD, art. 5º, I: nome, e-mail, telefone, documentos, endereço); nomes de recursos internos (servidores, bancos, schemas, tabelas, colunas, procedures, usuários e papéis, namespaces, serviços, buckets, filas, repositórios) e a topologia que eles desenham | **Mascarado** por pseudônimo tipado e consistente (`T_…`, `HOST_…`, `NS_…`), que o Claude consegue usar nos comandos e que volta ao valor real na resposta. Nomes de recurso podem ser desligados por tipo (`objetos.mascarar`) |
| **Interno** | Código-fonte, lógica e regras de negócio, estrutura das consultas, texto da conversa, nomes genéricos que todo mundo usa (`default`, `public`, `api`) | **Não mascarado.** Os identificadores que aparecem dentro do código são mascarados (nível Confidencial); a lógica, não. Ver [Limite declarado](#limite-declarado) |
| **Público** | Vocabulário de linguagens e formatos (palavras reservadas do SQL, tipos de dado, nomes de software, namespaces padrão do Kubernetes, domínios públicos), a referência pública derivada por medição | **Nunca mascarado**: mascarar o que é público só atrapalha o Claude e não protege nada |

## Por que cada nível

### Dados pessoais (Restrito e Confidencial)

A Lei Geral de Proteção de Dados (Lei nº 13.709/2018) define:

- **art. 5º, I** — dado pessoal: informação relacionada a pessoa natural identificada ou
  identificável;
- **art. 5º, II** — dado pessoal sensível: dado sobre origem racial ou étnica, convicção
  religiosa, opinião política, filiação a sindicato ou a organização de caráter religioso,
  filosófico ou político, dado referente à saúde ou à vida sexual, dado genético ou biométrico,
  quando vinculado a uma pessoa natural.

Mandar esses dados a um serviço externo é tratamento. Dois dispositivos orientam o desenho do
llm-dlp:

- **art. 6º, III** — princípio da necessidade: o tratamento se limita ao mínimo necessário
  para a finalidade, com dados pertinentes, proporcionais e não excessivos. Para escrever uma
  consulta ou corrigir um pipeline, o Claude precisa da forma do dado, não do dado: o
  pseudônimo basta.
- **art. 46, §2º** — as medidas de segurança do art. 46 devem ser observadas desde a fase de
  concepção do produto ou do serviço até a sua execução. O llm-dlp é uma dessas medidas, posta
  no caminho entre a ferramenta e a API, para não depender de cada pessoa lembrar de apagar
  dado antes de colar.

O llm-dlp não decide se um tratamento é lícito nem substitui a análise do encarregado de
dados da sua empresa: ele reduz o que sai.

### Nomes de recursos internos (Confidencial)

O nome de um servidor, banco, tabela, namespace ou bucket não é dado pessoal, mas é
informação sobre o ambiente. As referências técnicas tratam essa informação como alvo:

- **MITRE ATT&CK, tática TA0043 (Reconnaissance)** — o adversário junta informação sobre a
  vítima antes de agir. Técnicas: **T1590** (Gather Victim Network Information), **T1591**
  (Gather Victim Org Information), **T1592** (Gather Victim Host Information).
- **MITRE ATT&CK, tática TA0007 (Discovery)** — já dentro do ambiente, o adversário descobre o
  que existe. Técnicas: **T1613** (Container and Resource Discovery), **T1526** (Cloud Service
  Discovery), **T1580** (Cloud Infrastructure Discovery).

  Uma saída de `kubectl get ns`, `aws s3 ls` ou `SHOW TABLES` é exatamente o resultado dessas
  técnicas. Mandada a um serviço externo, ela vira um mapa do ambiente fora dele.
- **CWE-497 (Exposure of Sensitive System Information to an Unauthorized Control Sphere)** —
  expor informação de sistema (nomes, caminhos, configuração, topologia) a quem não deveria
  tê-la é uma fraqueza catalogada, mesmo quando nenhum segredo vaza.
- **NIST SP 800-60, Vol. 1, Rev. 1 (2008)** — o guia de categorização da informação lembra que
  a **agregação** muda o nível: um item isolado pode ter impacto baixo, e o conjunto ter
  impacto maior. Um nome de tabela sozinho diz pouco; o inventário inteiro (bancos, schemas,
  tabelas, colunas, papéis) diz como a empresa funciona. É por isso que o llm-dlp, ao ver um
  inventário, passa a conhecer cada nome dele e o mascara também no resto da conversa
  ([memória da conversa](estruturas.md#memória-da-conversa)).

### Nomes genéricos e vocabulário público (Interno e Público)

Uma palavra que aparece como nome de recurso em muitos projetos públicos (`default`,
`public`, `api`, `app`) não identifica ambiente nenhum. O llm-dlp mede essas palavras no
material público da máquina (código e documentação de terceiros) e grava o resultado num
arquivo de dados versionado, com a origem e o critério no cabeçalho; nada é escrito à mão
([referência pública derivada](estruturas.md#referência-pública-derivada)). Uma palavra dessas
dentro de um inventário é mascarada ali, mas não é propagada para a conversa.

O vocabulário dos formatos (palavras reservadas, tipos, nomes de software) vem da documentação
oficial de cada um ([estruturas.md](estruturas.md)) e nunca é mascarado.

## Limite declarado

**A máscara protege identificadores, não a lógica do código.**

O llm-dlp troca nomes e dados por pseudônimos. O código-fonte, as consultas, as regras de
cálculo, a lógica de negócio e o texto da conversa passam como estão, com os identificadores
trocados. Uma regra de precificação, um algoritmo de conciliação ou a modelagem de um
processo continuam legíveis para o serviço do outro lado.

Isso importa porque código-fonte e regras de negócio são, em geral, **segredo de negócio**. A
Lei da Propriedade Industrial (Lei nº 9.279/1996), **art. 195, XI**, define como crime de
concorrência desleal divulgar, explorar ou utilizar, sem autorização, conhecimentos,
informações ou dados confidenciais utilizáveis na indústria, no comércio ou na prestação de
serviços (excluídos os de conhecimento público ou evidentes para um técnico no assunto), a que
se teve acesso mediante relação contratual ou empregatícia, mesmo após o término do contrato.

O llm-dlp não decide o que pode ou não ser compartilhado com um serviço externo. Essa decisão
é da sua empresa: do contrato com o fornecedor do modelo, das regras de uso de IA e da
classificação do que você está escrevendo. O llm-dlp garante que, dentro do que foi decidido
compartilhar, os identificadores e os dados pessoais não saiam.

Outros limites (o que passa sem máscara, ferramentas da web, conectores) estão em
[Segurança e limites](seguranca.md).

## Referências

- Lei nº 13.709/2018 (LGPD), arts. 5º, I e II; 6º, III; 46, §2º —
  https://www.planalto.gov.br/ccivil_03/_ato2015-2018/2018/lei/l13709.htm
- Lei nº 9.279/1996 (Lei da Propriedade Industrial), art. 195, XI —
  https://www.planalto.gov.br/ccivil_03/leis/l9279.htm
- MITRE ATT&CK, TA0043 Reconnaissance — https://attack.mitre.org/tactics/TA0043/
  (T1590 https://attack.mitre.org/techniques/T1590/, T1591
  https://attack.mitre.org/techniques/T1591/, T1592 https://attack.mitre.org/techniques/T1592/)
- MITRE ATT&CK, TA0007 Discovery — https://attack.mitre.org/tactics/TA0007/
  (T1613 https://attack.mitre.org/techniques/T1613/, T1526
  https://attack.mitre.org/techniques/T1526/, T1580 https://attack.mitre.org/techniques/T1580/)
- CWE-497 — https://cwe.mitre.org/data/definitions/497.html
- NIST SP 800-60 Vol. 1 Rev. 1, *Guide for Mapping Types of Information and Information
  Systems to Security Categories* (2008) — https://csrc.nist.gov/pubs/sp/800/60/v1/r1/final
- ISO/IEC 27001:2022, Anexo A, controle 5.12 (Classificação da informação) — norma paga;
  resumo em https://www.iso.org/standard/27001
