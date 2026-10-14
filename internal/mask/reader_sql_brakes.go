package mask

import "strings"

// Brakes of the SQL reader (measured on the public corpus: TestMeasurePublicCorpus with
// LLM_DLP_SAMPLE). The false positives came from three shapes, none of them SQL:
//
//   - code comment talking about the code ("// Use x.Errors", "# delete FROM line",
//     "// Insert into hash table", "would never call drain(), so");
//   - uppercase keyword in the middle of another uppercase sentence ("# 0x3B -> CUSTOMER
//     USE THREE", character table);
//   - struct field named like a keyword ("Use   uint32", "Exec  string").
//
// The brakes are about shape (position in the line, what comes after the name), with no word list.

// lineComment: s[i] is in a code line comment (#, //, /*, block *); firstOne says whether the word
// at i is the first one after the marker.
func lineComment(s string, i int) (with, firstOne bool) {
	ls := strings.LastIndexByte(s[:i], '\n') + 1
	p := strings.TrimLeft(s[ls:i], " \t")
	if strings.HasPrefix(p, "#") || strings.HasPrefix(p, "//") || strings.HasPrefix(p, "/*") || strings.HasPrefix(p, "* ") {
		return true, strings.Trim(p, "#/*! \t") == ""
	}
	// comment at the end of a code line ("x = 1 # ...", "f() // ..."): the marker after a space
	for _, mk := range []string{" #", "\t#", " //", "\t//"} {
		if k := strings.LastIndex(p, mk); k >= 0 {
			return true, strings.Trim(p[k:], "#/*! \t") == ""
		}
	}
	return false, false
}

// wordBeforeInLine: there is a word (letters) right before s[i], on the same line, separated only
// by space: the word at i is in the middle of a sentence ("you can use x", "should call f()").
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

// statementStart: an uppercase statement does not start right after another uppercase word that is
// not SQL ("CUSTOMER USE THREE": an uppercase sentence; "EXPLAIN SELECT" still counts, EXPLAIN is
// in the vocabulary).
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

// statementOutsideProse: the statement that is not in uppercase (kw at i, shape already without SQL
// comments) is not code talking about code. prose: the keyword is a common word (use, call,
// exec...).
func statementOutsideProse(s string, i int, shape, kw string, prose bool) bool {
	target, after := sqlTarget(shape, kw)
	if with, firstOne := lineComment(s, i); with {
		// in a code comment, only a statement that opens the comment and whose target looks like an
		// identifier ("# delete from tb_log_x where"); a common word is prose there
		if prose || !firstOne || !identifierShape(strings.Trim(target, "[]\"`")) {
			return false
		}
	}
	if !prose {
		return true
	}
	// common word in the middle of a sentence ("you can use x", "can't use x", "should call f()")
	if wordBeforeInLine(s, i) {
		return false
	}
	// "Use uint32": struct field, not USE; the target of a statement is not a data type
	if isDataType(target) {
		return false
	}
	// "call drain(), so", "Call data.encode(...) but": function call cited in a sentence. SQL
	// CALL/EXEC ends after the parentheses (";", end of line or of the text). CALL always takes
	// parentheses (Snowflake, Postgres, MySQL, Oracle): "call pkg.Type" is a field
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

// sqlTarget: the first name of the statement (the table of the SELECT/INSERT/DELETE/UPDATE, the DDL
// object, the CALL procedure) and the rest of the text after it. "" if the shape is not one of the
// short ones.
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
