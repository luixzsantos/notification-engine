package channel

import "testing"

func TestHeadingLines_PlainTextHasNoHeading(t *testing.T) {
	lines := headingLines("linha 1\nlinha 2")
	if hasHeading(lines) {
		t.Fatal("não deveria detectar título em texto comum")
	}
	if len(lines) != 2 || lines[0].Text != "linha 1" || lines[1].Text != "linha 2" {
		t.Errorf("linhas incorretas: %+v", lines)
	}
}

func TestHeadingLines_DetectsAllLevels(t *testing.T) {
	lines := headingLines("# H1\n## H2\n### H3\ntexto normal")
	if !hasHeading(lines) {
		t.Fatal("esperava detectar título")
	}

	want := []messageLine{
		{Level: 1, Text: "H1"},
		{Level: 2, Text: "H2"},
		{Level: 3, Text: "H3"},
		{Level: 0, Text: "texto normal"},
	}
	if len(lines) != len(want) {
		t.Fatalf("esperava %d linhas, obteve %d", len(want), len(lines))
	}
	for i, w := range want {
		if lines[i] != w {
			t.Errorf("linha %d = %+v, esperava %+v", i, lines[i], w)
		}
	}
}

func TestTelegramFormatHeadings_NoHeadingReturnsUnchanged(t *testing.T) {
	text, usedHTML := telegramFormatHeadings("mensagem normal")
	if usedHTML {
		t.Error("não deveria marcar usedHTML sem título")
	}
	if text != "mensagem normal" {
		t.Errorf("texto alterado inesperadamente: %q", text)
	}
}

func TestTelegramFormatHeadings_WrapsHeadingInBold(t *testing.T) {
	text, usedHTML := telegramFormatHeadings("# Alerta\ntexto <script>")
	if !usedHTML {
		t.Fatal("esperava usedHTML=true")
	}
	want := "<b>Alerta</b>\ntexto &lt;script&gt;"
	if text != want {
		t.Errorf("text = %q, esperava %q", text, want)
	}
}

func TestWhatsappFormatHeadings_NoHeadingReturnsUnchanged(t *testing.T) {
	got := whatsappFormatHeadings("mensagem normal")
	if got != "mensagem normal" {
		t.Errorf("texto alterado inesperadamente: %q", got)
	}
}

func TestWhatsappFormatHeadings_WrapsHeadingInAsterisks(t *testing.T) {
	got := whatsappFormatHeadings("# Alerta\ntexto normal")
	want := "*Alerta*\ntexto normal"
	if got != want {
		t.Errorf("got = %q, esperava %q", got, want)
	}
}

func TestEmailMessageHTML_NoHeadingWrapsInParagraph(t *testing.T) {
	out, hasHeadingLines := emailMessageHTML("texto <b>simples</b>")
	if hasHeadingLines {
		t.Error("não deveria marcar hasHeadingLines sem título")
	}
	want := "<p>texto &lt;b&gt;simples&lt;/b&gt;</p>"
	if out != want {
		t.Errorf("out = %q, esperava %q", out, want)
	}
}

func TestEmailMessageHTML_RendersRealHeadingTags(t *testing.T) {
	out, hasHeadingLines := emailMessageHTML("# Título\ntexto\n## Sub")
	if !hasHeadingLines {
		t.Fatal("esperava hasHeadingLines=true")
	}
	want := "<h1>Título</h1><p>texto</p><h2>Sub</h2>"
	if out != want {
		t.Errorf("out = %q, esperava %q", out, want)
	}
}

func TestEmailMessageHTML_Empty(t *testing.T) {
	out, hasHeadingLines := emailMessageHTML("")
	if out != "" || hasHeadingLines {
		t.Errorf("esperava vazio/false, obteve %q/%v", out, hasHeadingLines)
	}
}
