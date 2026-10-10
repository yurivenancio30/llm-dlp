# Policy: what llm-dlp protects and why

[← back to the README](../README.md) · [Português](pt-BR/politica.md)

This document tells whoever uses llm-dlp what kinds of information exist in a conversation
with Claude Code, what llm-dlp does with each kind and which legal and technical references
that rests on. It also says, in so many words, what it does **not** protect.

The legal references are Brazilian (LGPD, the Brazilian data protection law, and the
Industrial Property Law); the technical ones are international.

## The four levels

Classification in four levels is the most common convention in companies' information
classification policies. ISO/IEC 27001:2022 (Annex A, control 5.12, "Classification of
information") requires the organization to have a classification scheme, but does not fix the
levels; each company defines its own. The levels below are the ones llm-dlp uses to explain
its own behavior. If your company has another scale, that one prevails: use this table to
map between them.

| Level | What it covers | llm-dlp default |
|---|---|---|
| **Restricted** | Credentials and secrets (passwords, API keys and tokens, authentication headers, private keys); sensitive personal data (LGPD, art. 5, II); card and account data | **Always masked**, wherever it appears, by format and by the word nearby. Images and PDFs go through OCR and black bars. When in doubt, mask; on failure, refuse (fail-closed) |
| **Confidential** | Personal data (LGPD, art. 5, I: name, e-mail, phone, documents, address); internal resource names (servers, databases, schemas, tables, columns, procedures, users and roles, namespaces, services, buckets, queues, repositories) and the topology they draw | **Masked** with a typed, consistent pseudonym (`T_…`, `HOST_…`, `NS_…`), which Claude can use in commands and which goes back to the real value in the response. Resource names can be turned off per type (`objetos.mascarar`) |
| **Internal** | Source code, business logic and rules, the structure of queries, the conversation text, generic names everybody uses (`default`, `public`, `api`) | **Not masked.** The identifiers that appear inside the code are masked (Confidential level); the logic is not. See [Declared limit](#declared-limit) |
| **Public** | Vocabulary of languages and formats (SQL reserved words, data types, software names, standard Kubernetes namespaces, public domains), the public reference derived by measurement | **Never masked**: masking what is public only gets in Claude's way and protects nothing |

## Why each level

### Personal data (Restricted and Confidential)

The Brazilian General Data Protection Law (Lei nº 13.709/2018, LGPD) defines:

- **art. 5, I** — personal data: information related to an identified or identifiable natural
  person;
- **art. 5, II** — sensitive personal data: data on racial or ethnic origin, religious
  belief, political opinion, membership of a trade union or of a religious, philosophical or
  political organization, data concerning health or sex life, genetic or biometric data,
  when linked to a natural person.

Sending this data to an external service is processing. Two provisions guide the design of
llm-dlp:

- **art. 6, III** — the necessity principle: processing is limited to the minimum necessary
  for its purpose, with data that is relevant, proportional and not excessive. To write a
  query or fix a pipeline, Claude needs the shape of the data, not the data: the pseudonym
  is enough.
- **art. 46, §2** — the security measures of art. 46 must be observed from the design phase
  of the product or service through to its execution. llm-dlp is one of those measures,
  placed on the path between the tool and the API, so that it does not depend on each person
  remembering to delete data before pasting.

llm-dlp does not decide whether a processing operation is lawful, nor does it replace the
analysis of your company's data protection officer: it reduces what goes out.

### Internal resource names (Confidential)

The name of a server, database, table, namespace or bucket is not personal data, but it is
information about the environment. The technical references treat that information as a
target:

- **MITRE ATT&CK, tactic TA0043 (Reconnaissance)** — the adversary gathers information about
  the victim before acting. Techniques: **T1590** (Gather Victim Network Information),
  **T1591** (Gather Victim Org Information), **T1592** (Gather Victim Host Information).
- **MITRE ATT&CK, tactic TA0007 (Discovery)** — once inside the environment, the adversary
  discovers what exists. Techniques: **T1613** (Container and Resource Discovery), **T1526**
  (Cloud Service Discovery), **T1580** (Cloud Infrastructure Discovery).

  An output of `kubectl get ns`, `aws s3 ls` or `SHOW TABLES` is exactly the result of those
  techniques. Sent to an external service, it becomes a map of the environment outside it.
- **CWE-497 (Exposure of Sensitive System Information to an Unauthorized Control Sphere)** —
  exposing system information (names, paths, configuration, topology) to someone who should
  not have it is a catalogued weakness, even when no secret leaks.
- **NIST SP 800-60, Vol. 1, Rev. 1 (2008)** — the information categorization guide points out
  that **aggregation** changes the level: an isolated item may have low impact, and the set
  a higher one. A table name alone says little; the whole inventory (databases, schemas,
  tables, columns, roles) says how the company works. That is why llm-dlp, on seeing an
  inventory, comes to know each name in it and masks it in the rest of the conversation too
  ([conversation memory](structures.md#conversation-memory)).

### Generic names and public vocabulary (Internal and Public)

A word that appears as a resource name in many public projects (`default`, `public`, `api`,
`app`) identifies no environment at all. llm-dlp measures these words in the public material
of the machine (third-party code and documentation) and writes the result to a versioned
data file, with the origin and the criterion in the header; nothing is written by hand
([derived public reference](structures.md#derived-public-reference)). One of these words
inside an inventory is masked there, but is not propagated to the conversation.

The vocabulary of the formats (reserved words, types, software names) comes from the official
documentation of each one ([structures.md](structures.md)) and is never masked.

## Declared limit

**The mask protects identifiers, not the logic of the code.**

llm-dlp replaces names and data with pseudonyms. Source code, queries, calculation rules,
business logic and the conversation text go through as they are, with the identifiers
replaced. A pricing rule, a reconciliation algorithm or the modelling of a process remain
readable to the service on the other side.

This matters because source code and business rules are, in general, a **trade secret**. The
Brazilian Industrial Property Law (Lei nº 9.279/1996), **art. 195, XI**, defines as a crime
of unfair competition disclosing, exploiting or using, without authorization, confidential
knowledge, information or data usable in industry, commerce or the provision of services
(excluding what is public knowledge or obvious to a person skilled in the art), to which one
had access through a contractual or employment relationship, even after the contract ends.

llm-dlp does not decide what may or may not be shared with an external service. That
decision belongs to your company: to the contract with the model vendor, the AI usage rules
and the classification of what you are writing. llm-dlp guarantees that, within what was
decided to share, identifiers and personal data do not go out.

Other limits (what goes through unmasked, web tools, connectors) are in
[Security and limits](security.md).

## References

- Lei nº 13.709/2018 (LGPD), arts. 5, I and II; 6, III; 46, §2 —
  https://www.planalto.gov.br/ccivil_03/_ato2015-2018/2018/lei/l13709.htm
- Lei nº 9.279/1996 (Industrial Property Law), art. 195, XI —
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
- ISO/IEC 27001:2022, Annex A, control 5.12 (Classification of information) — paid standard;
  summary at https://www.iso.org/standard/27001
