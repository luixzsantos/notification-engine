package domain

import "testing"

func TestParseRichMessage_PlainMessageUnchanged(t *testing.T) {
	msg, table := ParseRichMessage("mensagem normal, sem nenhum marcador")
	if msg != "mensagem normal, sem nenhum marcador" {
		t.Errorf("mensagem alterada inesperadamente: %q", msg)
	}
	if table != nil {
		t.Errorf("esperava table nil, obteve %+v", table)
	}
}

func TestParseRichMessage_Headings(t *testing.T) {
	msg, table := ParseRichMessage("/h1 Título principal\ntexto normal\n/h2 Subtítulo\nmais texto\n/h3 Nível 3")
	want := "# Título principal\ntexto normal\n## Subtítulo\nmais texto\n### Nível 3"
	if msg != want {
		t.Errorf("mensagem =\n%q\nesperava\n%q", msg, want)
	}
	if table != nil {
		t.Errorf("esperava table nil, obteve %+v", table)
	}
}

func TestParseRichMessage_TableBlock(t *testing.T) {
	raw := "Resumo do dia:\n/table\nNome, Status\nJoão, Aprovado\nMaria, Pendente"
	msg, table := ParseRichMessage(raw)

	if msg != "Resumo do dia:" {
		t.Errorf("esperava mensagem sem o bloco /table, obteve %q", msg)
	}
	if table == nil {
		t.Fatal("esperava table não-nil")
	}
	if len(table.Headers) != 2 || table.Headers[0] != "Nome" || table.Headers[1] != "Status" {
		t.Errorf("headers incorretos: %+v", table.Headers)
	}
	if len(table.Rows) != 2 || table.Rows[0][0] != "João" || table.Rows[1][0] != "Maria" {
		t.Errorf("rows incorretas: %+v", table.Rows)
	}
}

func TestParseRichMessage_TableBlockEndsAtBlankLine(t *testing.T) {
	raw := "/table\nNome, Status\nJoão, Aprovado\n\ntexto depois da tabela"
	msg, table := ParseRichMessage(raw)

	if msg != "texto depois da tabela" {
		t.Errorf("esperava só o texto após a linha em branco, obteve %q", msg)
	}
	if table == nil || len(table.Rows) != 1 {
		t.Fatalf("table incorreta: %+v", table)
	}
}

func TestParseRichMessage_HeadingAndTableCombined(t *testing.T) {
	raw := "/h1 Aprovações pendentes\n/table\nNome, Status\nJoão, Aprovado"
	msg, table := ParseRichMessage(raw)

	if msg != "# Aprovações pendentes" {
		t.Errorf("esperava só o título normalizado, obteve %q", msg)
	}
	if table == nil || len(table.Rows) != 1 {
		t.Fatalf("table incorreta: %+v", table)
	}
}

func TestParseRichMessage_OnlyTableSatisfiesMessage(t *testing.T) {
	msg, table := ParseRichMessage("/table\nNome, Status\nJoão, Aprovado")
	if msg != "" {
		t.Errorf("esperava mensagem vazia, obteve %q", msg)
	}
	if table == nil {
		t.Fatal("esperava table não-nil")
	}
}

func TestParseRichMessage_SlashWithoutSpaceIsNotAHeading(t *testing.T) {
	msg, _ := ParseRichMessage("/h1semespaço não é título")
	if msg != "/h1semespaço não é título" {
		t.Errorf("não deveria reconhecer como título sem o espaço: %q", msg)
	}
}
