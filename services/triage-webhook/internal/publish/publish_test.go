package publish

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
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
	h := withURL.header(Report{Summary: "KubePodCrashLooping em ai"})
	if !strings.Contains(h, "KubePodCrashLooping em ai") || !strings.Contains(h, "am.example.com/#/alerts") {
		t.Fatalf("header deveria conter grupo e link: %q", h)
	}

	noURL := NewSlack("t", "#triage", "", nil)
	h2 := noURL.header(Report{Summary: "x"})
	if strings.Contains(h2, "http") {
		t.Fatalf("sem externalURL não deveria haver link: %q", h2)
	}
}

func TestSlackPublishPostsRootAndDiagnosisInThread(t *testing.T) {
	var got []slackPostRequest
	pub := NewSlack("t", "#triage", "https://am.example.com", nil)
	pub.postURL = "http://slack.test/chat.postMessage"
	pub.hc = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPost {
			t.Fatalf("método inesperado: %s", r.Method)
		}
		var req slackPostRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("request inválido: %v", err)
		}
		got = append(got, req)
		body, err := json.Marshal(slackPostResponse{OK: true, TS: "root-ts"})
		if err != nil {
			t.Fatalf("resposta inválida: %v", err)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(body)),
			Header:     make(http.Header),
		}, nil
	})}

	err := pub.Publish(context.Background(), Report{
		Summary:   "KubePodCrashLooping em ai",
		Diagnosis: "diagnóstico detalhado",
	})
	if err != nil {
		t.Fatalf("Publish retornou erro: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("esperava raiz + mensagem no thread, veio %d", len(got))
	}
	if got[0].ThreadTS != "" {
		t.Fatalf("mensagem raiz não deve ter thread_ts: %q", got[0].ThreadTS)
	}
	if !strings.Contains(got[0].Text, "Diagnóstico completo na thread.") {
		t.Fatalf("mensagem raiz deveria ser padronizada: %q", got[0].Text)
	}
	if strings.Contains(got[0].Text, "diagnóstico detalhado") {
		t.Fatalf("mensagem raiz não deve conter diagnóstico: %q", got[0].Text)
	}
	if got[1].ThreadTS != "root-ts" {
		t.Fatalf("diagnóstico deveria ir no thread da raiz, thread_ts=%q", got[1].ThreadTS)
	}
	if !strings.Contains(got[1].Text, "diagnóstico detalhado") {
		t.Fatalf("mensagem no thread deveria conter diagnóstico: %q", got[1].Text)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
