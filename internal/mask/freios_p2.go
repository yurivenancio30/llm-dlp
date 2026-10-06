package mask

import "strings"

// Freios das regras acima de 2% nas sessões reais (P2, item D2; TestMedirSessoesReais, só
// contagens e formas). Pela forma e pelos vocabulários públicos que já existem.

// Conexão (medido nas sessões: conexão/tnsnames e conexão/uri acima de 2%). Os
// valores públicos que as regras de conexão aprendiam tinham todos a mesma forma: palavra de
// tipo ou de atributo seguida de número ou colada a outra ("host1", "user2", "db01", "dbName",
// "serverHost"). É o nome de modelo de exemplo e de documentação, não nome de recurso. A
// decisão é pela forma: no máximo dois pedaços, todos do vocabulário que já existe (entPedaco,
// entAntesDeNome, atributoChave, vocabDev), com ou sem número no fim. Uma palavra só ("host")
// já era pública. Vale só na URI de banco e no tnsnames: nas outras regras de conexão e de
// endereço, "broker1" num bootstrap.servers continua nome (TestLeitorChaveValor).

// nomeDeModelo: v é palavra(s) de tipo com número ou colada(s), sem nada de próprio.
func nomeDeModelo(v string) bool {
	base := strings.TrimRight(v, "0123456789")
	if base == "" || len(v) > 24 {
		return false
	}
	var ps []string
	ini := 0
	for i := 1; i <= len(base); i++ {
		if i == len(base) || base[i] == '_' || base[i] == '-' || base[i] >= 'A' && base[i] <= 'Z' && base[i-1] >= 'a' && base[i-1] <= 'z' {
			if p := strings.Trim(base[ini:i], "_-"); p != "" {
				ps = append(ps, strings.ToLower(p))
			}
			ini = i
		}
	}
	if len(ps) == 0 || len(ps) > 2 || len(ps) == 1 && base == v {
		return false // uma palavra sem número: as listas públicas de sempre decidem
	}
	for _, p := range ps {
		_, t1 := entPedaco[p]
		_, t2 := entAntesDeNome[p]
		if !(t1 || t2 || atributoChave[p] || vocabDev[p]) {
			return false
		}
	}
	return true
}

