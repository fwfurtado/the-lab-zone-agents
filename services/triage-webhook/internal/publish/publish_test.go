package publish

import (
	"strings"
	"testing"
)

func TestSplitMarkdownShortStaysWhole(t *testing.T) {
	in := "linha curta\noutra linha"
	got := splitMarkdown(in, 3500)
	if len(got) != 1 || got[0] != in {
		t.Fatalf("texto curto não deveria ser fatiado: %v", got)
	}
}

func TestSplitMarkdownCutsOnLineBoundary(t *testing.T) {
	// 10 linhas de ~20 chars, limite 50 => força vários pedaços.
	var b strings.Builder
	for i := 0; i < 10; i++ {
		b.WriteString("linha-de-teste-aqui\n")
	}
	got := splitMarkdown(b.String(), 50)
	if len(got) < 2 {
		t.Fatalf("esperava múltiplos pedaços, veio %d", len(got))
	}
	// Nenhum pedaço parte uma linha no meio.
	for _, chunk := range got {
		for _, line := range strings.Split(chunk, "\n") {
			if line != "" && line != "linha-de-teste-aqui" {
				t.Fatalf("linha cortada no meio: %q", line)
			}
		}
	}
}

func TestSplitMarkdownNeverBreaksCodeFence(t *testing.T) {
	// Fence com corpo que sozinho excede o limite: deve sair inteiro num
	// único pedaço, fence aberto e fechado juntos.
	fence := "```\n" + strings.Repeat("comando muito longo aqui\n", 20) + "```"
	text := "abertura\n\n" + fence + "\n\nfechamento"
	got := splitMarkdown(text, 100)

	// O pedaço que contém a abertura do fence deve conter também o fechamento.
	var fenceChunk string
	for _, c := range got {
		if strings.Contains(c, "```") {
			fenceChunk = c
			break
		}
	}
	if fenceChunk == "" {
		t.Fatal("nenhum pedaço contém o fence")
	}
	if strings.Count(fenceChunk, "```") != 2 {
		t.Fatalf("fence foi partido — esperava abertura e fechamento no mesmo pedaço, got %d marcadores:\n%s",
			strings.Count(fenceChunk, "```"), fenceChunk)
	}
}

func TestSlackHeaderWithAndWithoutURL(t *testing.T) {
	withURL := NewSlack("t", "#triage", "https://am.example.com", nil)
	h := withURL.header(Report{GroupKey: "grp-1"})
	if !strings.Contains(h, "grp-1") || !strings.Contains(h, "am.example.com/#/alerts") {
		t.Fatalf("header deveria conter grupo e link: %q", h)
	}

	noURL := NewSlack("t", "#triage", "", nil)
	h2 := noURL.header(Report{GroupKey: "grp-2"})
	if strings.Contains(h2, "http") {
		t.Fatalf("sem externalURL não deveria haver link: %q", h2)
	}
}
