package publish

import "strings"

// maxSlackChars é o teto por mensagem. O bloco markdown do Slack aceita 12k,
// mas mensagens menores leem melhor e dão folga pro overhead do bloco. Mesmo
// valor do bot Python (shared/slack/message_updater.py) — o diagnóstico deve
// renderizar idêntico ao QA bot.
const maxSlackChars = 3500

// splitMarkdown fatia texto Markdown em pedaços <= limit sem quebrar
// formatação. Porta fiel do _split_markdown do bot Python; a lógica é
// deliberadamente a mesma para o rendering ser idêntico.
//
// Regras de corte, em ordem de preferência:
//   - nunca corta dentro de um code fence (``` aberto): o pedaço vai até o
//     fechamento do fence, mesmo que passe um pouco do limite;
//   - senão, corta em fronteira de linha (o acúmulo estoura o limite).
//
// Relatório de triagem tem tabelas e blocos de comando — preservar fence é o
// que impede um ``` de ser cortado no meio e virar texto cru no Slack.
func splitMarkdown(text string, limit int) []string {
	if len(text) <= limit {
		return []string{text}
	}

	lines := strings.Split(text, "\n")
	var chunks []string
	var current []string
	currentLen := 0
	inFence := false

	flush := func() {
		if len(current) > 0 {
			chunks = append(chunks, strings.Join(current, "\n"))
			current = nil
			currentLen = 0
		}
	}

	for _, line := range lines {
		lineLen := len(line) + 1 // +1 pelo \n reinserido no Join
		isFence := strings.HasPrefix(strings.TrimSpace(line), "```")

		// Estoura o limite E não estamos dentro de fence E já há conteúdo:
		// fecha o pedaço antes de adicionar (corte em fronteira segura).
		if currentLen+lineLen > limit && !inFence && len(current) > 0 {
			flush()
		}

		current = append(current, line)
		currentLen += lineLen

		if isFence {
			inFence = !inFence
		}
	}

	flush()
	return chunks
}
