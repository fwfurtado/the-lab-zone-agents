package publish

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
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

func TestSlackPublishRootFailsAbortsButChunkFailureDoesNot(t *testing.T) {
	// Contrato de erro (ver Publish): a raiz é a ÚNICA post dura. Se a raiz
	// falha, Publish retorna erro. Se a raiz vai mas um chunk do diagnóstico
	// falha, Publish NÃO retorna erro — senão a raiz ficaria órfã no canal
	// ("Diagnóstico completo na thread") apontando pra uma thread vazia.

	newPub := func(rt roundTripFunc) *SlackPublisher {
		// logger real: o path best-effort de chunk chama p.log.Error; nil
		// causaria panic (em produção o log nunca é nil).
		p := NewSlack("t", "#triage", "", slog.New(slog.NewTextHandler(io.Discard, nil)))
		p.postURL = "http://slack.test/chat.postMessage"
		p.hc = &http.Client{Transport: rt}
		return p
	}
	okResponse := func(ts string) (*http.Response, error) {
		body, _ := json.Marshal(slackPostResponse{OK: true, TS: ts})
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(body)),
			Header:     make(http.Header),
		}, nil
	}

	// Caso 1: raiz falha -> Publish retorna erro.
	rootFails := newPub(func(_ *http.Request) (*http.Response, error) {
		return nil, io.ErrUnexpectedEOF
	})
	if err := rootFails.Publish(context.Background(), Report{Diagnosis: "x"}); err == nil {
		t.Fatal("raiz falhando deveria abortar Publish com erro")
	}

	// Caso 2: raiz OK, primeiro (e único) chunk falha -> Publish NÃO aborta.
	var call int
	chunkFails := newPub(func(_ *http.Request) (*http.Response, error) {
		call++
		if call == 1 {
			return okResponse("root-ts") // a raiz vai
		}
		return nil, io.ErrUnexpectedEOF // o chunk falha
	})
	if err := chunkFails.Publish(context.Background(), Report{Diagnosis: "x"}); err != nil {
		t.Fatalf("falha de chunk após raiz OK não deveria abortar: %v", err)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

// TestSlackPublishRootTemBotoesDeFeedback (ADR-0014): a mensagem RAIZ ganha o
// bloco de botões Confirmar/Refutar; os chunks do diagnóstico na thread NÃO —
// o feedback se refere ao incidente como um todo, não a cada pedaço do texto.
func TestSlackPublishRootTemBotoesDeFeedback(t *testing.T) {
	var got []slackPostRequest
	pub := NewSlack("t", "#triage", "", nil)
	pub.postURL = "http://slack.test/chat.postMessage"
	pub.hc = &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		var req slackPostRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("request inválido: %v", err)
		}
		got = append(got, req)
		body, _ := json.Marshal(slackPostResponse{OK: true, TS: "root-ts"})
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(body)), Header: make(http.Header)}, nil
	})}

	if err := pub.Publish(context.Background(), sampleReport()); err != nil {
		t.Fatalf("Publish retornou erro: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("esperava raiz + thread, veio %d", len(got))
	}

	root := got[0]
	if len(root.Blocks) != 2 {
		t.Fatalf("raiz deveria ter 2 blocks (markdown + actions), veio %d: %+v", len(root.Blocks), root.Blocks)
	}
	actions, ok := root.Blocks[1].(map[string]any)
	if !ok || actions["type"] != "actions" {
		t.Fatalf("segundo block da raiz deveria ser actions, veio: %+v", root.Blocks[1])
	}
	elements, ok := actions["elements"].([]any)
	if !ok || len(elements) != 2 {
		t.Fatalf("actions deveria ter 2 botões, veio: %+v", actions["elements"])
	}

	first := elements[0].(map[string]any)
	if first["action_id"] != "triage_confirm" {
		t.Errorf("primeiro botão deveria ser triage_confirm, veio %v", first["action_id"])
	}
	second := elements[1].(map[string]any)
	if second["action_id"] != "triage_refute" {
		t.Errorf("segundo botão deveria ser triage_refute, veio %v", second["action_id"])
	}

	// O value de cada botão carrega dedup_key + triage_key (== reportSuffix):
	// é o que elimina o índice reverso no listener (ADR-0014).
	var val feedbackButtonValue
	if err := json.Unmarshal([]byte(first["value"].(string)), &val); err != nil {
		t.Fatalf("value do botão não é JSON válido: %v", err)
	}
	if val.DedupKey != "a3f9c1d2e4b6a8c0" {
		t.Errorf("dedup_key no value = %q, want a3f9c1d2e4b6a8c0", val.DedupKey)
	}
	if val.TriageKey != reportSuffix(sampleReport()) {
		t.Errorf("triage_key no value (%q) deveria ser reportSuffix (%q)", val.TriageKey, reportSuffix(sampleReport()))
	}

	// A thread (diagnóstico) NÃO leva botões — só a raiz.
	thread := got[1]
	if len(thread.Blocks) != 1 {
		t.Fatalf("chunk da thread não deveria ter o bloco de botões: %+v", thread.Blocks)
	}
}

// TestFeedbackButtonValueSemCaracteresQueQuebramJSON: dedup_key e
// namespace/alertname sanitizados nunca contêm aspas, então o value do botão
// é sempre JSON válido — mas testamos o caso adversarial mesmo assim (defesa,
// não confiança cega na sanitização upstream).
func TestFeedbackButtonValueValido(t *testing.T) {
	pub := NewSlack("t", "#triage", "", nil)
	block, err := pub.feedbackBlock(sampleReport())
	if err != nil {
		t.Fatalf("feedbackBlock retornou erro: %v", err)
	}
	if len(block.Elements) != 2 {
		t.Fatalf("esperava 2 botões, veio %d", len(block.Elements))
	}
	for _, el := range block.Elements {
		var val feedbackButtonValue
		if err := json.Unmarshal([]byte(el.Value), &val); err != nil {
			t.Fatalf("value do botão %q não é JSON válido: %v", el.ActionID, err)
		}
	}
}
