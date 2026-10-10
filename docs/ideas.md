# Ideas for later

[← back to the README](../README.md) · [Português](pt-BR/ideias.md)

One line per idea: what was seen and has not been done yet. Nothing here is a promise.

## Mask more (goes through today)

- A project folder that is a plain word (`/home/ubuntu/faturahx/...`): only an identifier-looking folder (hyphen, digit, `_`) is masked.
- `import polaris` in a file that was read: a plain word decided elsewhere is not carried into a file.
- A Portuguese key that is not a type word (`"Banco": "vendashx_prod"`): `banco` is also a bank for money, so it did not enter the vocabulary.
- An identifier-looking CTE name (`WITH clientes_inadimplentes AS (`): today every CTE name is a local name of the query, like a table alias.
- The session's working directory (it comes in the request): use it as the origin, to know that every path under it belongs to the project.

## Mask less (too much today)

- `package@version` (`lodash@4.17.21`, `testify@v1.8.4`) goes out as an e-mail. Before letting it through, check that the client's package name is still covered by another reader.
- An installed dependency named after a dictionary word (`pandas`, `requests`): the folders of the path follow the ordinary rule. A list of the most downloaded PyPI and npm packages would give the second key without depending on the dictionary.
- A translated label of a type key in a catalog in another script (`"User": "Użytkownik"`).
- DataHub: `allow`/`deny` patterns of `table_pattern`/`schema_pattern` (regex) in ingestion recipes; mask only the literal spans that are a whole name, without breaking the regex.
