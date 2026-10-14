package mask

import "strings"

// Freios do leitor de SQL (medidos no corpus público: TestMeasurePublicCorpus com
// LLM_DLP_AMOSTRA). Os falsos positivos vinham de três formas, nenhuma delas de SQL:
//
//   - comentário de código que fala do código ("// Use x.Errors", "# delete FROM line",
//     "// Insert into hash table", "would never call drain(), so");
//   - palavra-chave em maiúsculas no meio de outra frase em maiúsculas ("# 0x3B -> CUSTOMER
//     USE THREE", tabela de caracteres);
//   - campo de struct com nome de palavra-chave ("Use   uint32", "Exec  string").
//
// Os freios são de forma (posição na linha, o que vem depois do nome), sem lista de palavras.

// lineComment: s[i] está num comentário de linha de código (#, //, /*, * de bloco); primeira
// diz se a palavra em i é a primeira depois do marcador.
func lineComment(s string, i int) (with, firstOne bool) {
	ls := strings.LastIndexByte(s[:i], '\n') + 1
	p := strings.TrimLeft(s[ls:i], " \t")
	if strings.HasPrefix(p, "#") || strings.HasPrefix(p, "//") || strings.HasPrefix(p, "/*") || strings.HasPrefix(p, "* ") {
		return true, strings.Trim(p, "#/*! \t") == ""
	}
	// comentário no fim de uma linha de código ("x = 1   # ...", "f() // ..."): o marcador
	// depois de um espaço
	for _, mk := range []string{" #", "\t#", " //", "\t//"} {
		if k := strings.LastIndex(p, mk); k >= 0 {
			return true, strings.Trim(p[k:], "#/*! \t") == ""
		}
	}
	return false, false
}

// wordBeforeInLine: há uma palavra (letras) logo antes de s[i], na mesma linha, separada
// só por espaço: a palavra em i está no meio de uma frase ("you can use x", "should call f()").
func wordBeforeInLine(s string, i int) bool {
	k := i
	for k > 0 && (s[k-1] == ' ' || s[k-1] == '\t') {
		k--
	}
	if k == i || k == 0 {
		return false
	}
	c := s[k-1]
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '\''
}

// statementStart: uma instrução em maiúsculas não começa logo depois de outra palavra em
// maiúsculas que não é do SQL ("CUSTOMER USE THREE": frase em maiúsculas; "EXPLAIN SELECT"
// continua valendo, EXPLAIN é do vocabulário).
func statementStart(s string, i int) bool {
	k := i
	for k > 0 && (s[k-1] == ' ' || s[k-1] == '\t') {
		k--
	}
	j := k
	for j > 0 && (s[j-1] >= 'A' && s[j-1] <= 'Z') {
		j--
	}
	if j == k || k-j < 2 || j > 0 && (isIdent(s[j-1]) || s[j-1] >= 'a' && s[j-1] <= 'z') {
		return true
	}
	return publicSQL(s[j:k])
}

// statementOutsideProse: a instrução que não está em maiúsculas (kw em i, forma já sem os
// comentários de SQL) não é código falando de código. prosa: a palavra-chave é palavra comum
// (use, call, exec...).
func statementOutsideProse(s string, i int, shape, kw string, prose bool) bool {
	target, after := sqlTarget(shape, kw)
	if with, firstOne := lineComment(s, i); with {
		// num comentário de código, só uma instrução que abre o comentário e cujo alvo tem cara
		// de identificador ("# delete from tb_log_x where"); palavra comum é prosa ali
		if prose || !firstOne || !identifierShape(strings.Trim(target, "[]\"`")) {
			return false
		}
	}
	if !prose {
		return true
	}
	// palavra comum no meio de uma frase ("you can use x", "can't use x", "should call f()")
	if wordBeforeInLine(s, i) {
		return false
	}
	// "Use   uint32": campo de struct, não USE; o alvo de uma instrução não é tipo de dado
	if isDataType(target) {
		return false
	}
	// "call drain(), so", "Call data.encode(...) but": chamada de função citada numa frase.
	// CALL/EXEC de SQL termina depois dos parênteses (";", fim da linha ou do texto).
	// CALL sempre leva parênteses (Snowflake, Postgres, MySQL, Oracle): "call    pkg.Type" é campo
	if kw == "CALL" && !strings.HasPrefix(strings.TrimLeft(after, " \t"), "(") {
		return false
	}
	if (kw == "CALL" || kw == "EXEC" || kw == "EXECUTE") && strings.HasPrefix(strings.TrimLeft(after, " \t"), "(") {
		d := strings.TrimLeft(after, " \t")
		n, k := 0, 0
		for ; k < len(d) && d[k] != '\n'; k++ {
			if d[k] == '(' {
				n++
			} else if d[k] == ')' {
				if n--; n == 0 {
					break
				}
			}
		}
		if n != 0 || k >= len(d) {
			return false
		}
		r := strings.TrimLeft(d[k+1:], " \t")
		return r == "" || r[0] == ';' || r[0] == '\n' || r[0] == '\r'
	}
	return true
}

// sqlTarget: o primeiro nome da instrução (a tabela do SELECT/INSERT/DELETE/UPDATE, o objeto do
// DDL, a procedure do CALL) e o resto do texto depois dele. "" se a forma não for das curtas.
func sqlTarget(shape, kw string) (string, string) {
	switch kw {
	case "SELECT":
		if m := reSelectMin.FindStringSubmatchIndex(shape); m != nil {
			return shape[m[4]:m[5]], shape[m[5]:]
		}
	case "INSERT", "DELETE", "UPDATE":
		if m := reMinTarget.FindStringSubmatchIndex(shape); m != nil {
			return shape[m[2]:m[3]], shape[m[3]:]
		}
	default:
		if m := reDDLMin.FindStringSubmatchIndex(shape); m != nil {
			return shape[m[2]:m[3]], shape[m[3]:]
		}
	}
	return "", ""
}
