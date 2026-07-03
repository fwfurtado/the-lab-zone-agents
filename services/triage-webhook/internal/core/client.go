// Package core é o cliente do núcleo Python de triagem (sidecar, localhost).
//
// Contrato: POST CoreURL com {"context": "..."} → 200 {"report": "..."}.
// Síncrono com timeout por contexto — é localhost, sem LB no meio; job
// assíncrono com polling seria complexidade sem benefício (ADR-0017).
package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client fala com o núcleo de triagem.
type Client struct {
	url       string
	healthURL string
	hc        *http.Client
}

// New cria o cliente. Sem timeout global no http.Client: o teto de cada
// triagem vem do contexto do worker (TriageTimeout); um timeout fixo aqui
// brigaria com ele.
func New(url, healthURL string) *Client {
	return &Client{
		url:       url,
		healthURL: healthURL,
		hc:        &http.Client{},
	}
}

type triageRequest struct {
	Context string `json:"context"`
}

type triageResponse struct {
	Report string `json:"report"`
	Error  string `json:"error,omitempty"`
}

// Triage envia o contexto ao núcleo e devolve o relatório de diagnóstico.
func (c *Client) Triage(ctx context.Context, contextText string) (string, error) {
	body, err := json.Marshal(triageRequest{Context: contextText})
	if err != nil {
		return "", fmt.Errorf("serializando requisição: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("montando requisição: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.hc.Do(req)
	if err != nil {
		return "", fmt.Errorf("chamando o núcleo: %w", err)
	}
	defer resp.Body.Close()

	// Relatórios têm dezenas de KB; 4 MiB é folga, não licença.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", fmt.Errorf("lendo resposta do núcleo: %w", err)
	}

	var tr triageResponse
	if err := json.Unmarshal(raw, &tr); err != nil {
		return "", fmt.Errorf("resposta do núcleo não é JSON (status %d): %w", resp.StatusCode, err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("núcleo respondeu %d: %s", resp.StatusCode, tr.Error)
	}
	return tr.Report, nil
}

// Healthy verifica o healthcheck do núcleo — consumido pelo /readyz da borda.
func (c *Client) Healthy(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.healthURL, nil)
	if err != nil {
		return err
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("núcleo inalcançável: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<10))

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthcheck do núcleo respondeu %d", resp.StatusCode)
	}
	return nil
}
