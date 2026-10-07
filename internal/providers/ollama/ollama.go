// Package ollama is the Ollama provider adapter.
package ollama

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/ajay-7105/EvalCI/internal/providers"
)

// Client implements providers.Provider against an Ollama server.
type Client struct {
	baseURL string
	http    *http.Client
	retry   providers.Retrier
}

// New returns a client for the Ollama server at baseURL (for example http://localhost:11434).
func New(baseURL string, c *http.Client, r providers.Retrier) *Client {
	if c == nil {
		c = http.DefaultClient
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), http: c, retry: r}
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model    string         `json:"model"`
	Messages []message      `json:"messages"`
	Stream   bool           `json:"stream"`
	Options  map[string]any `json:"options"`
}

type chatResponse struct {
	Message         message `json:"message"`
	PromptEvalCount *int    `json:"prompt_eval_count"`
	EvalCount       *int    `json:"eval_count"`
}

// usage maps Ollama's counters; when neither is present usage is unknown.
func usage(in, out *int) providers.Usage {
	if in == nil && out == nil {
		return providers.Usage{}
	}
	u := providers.Usage{Known: true}
	if in != nil {
		u.InputTokens = *in
	}
	if out != nil {
		u.OutputTokens = *out
	}
	return u
}

// Complete implements providers.Provider.
func (c *Client) Complete(ctx context.Context, req providers.Request) (providers.Response, error) {
	var msgs []message
	if req.System != "" {
		msgs = append(msgs, message{Role: "system", Content: req.System})
	}
	msgs = append(msgs, message{Role: "user", Content: req.User})
	opts := map[string]any{"temperature": req.Temperature}
	if req.MaxTokens > 0 {
		opts["num_predict"] = req.MaxTokens
	}
	body := chatRequest{Model: req.Model, Messages: msgs, Stream: false, Options: opts}

	var out chatResponse
	err := c.retry.Do(ctx, func() error {
		return providers.PostJSON(ctx, c.http, c.baseURL+"/api/chat", nil, body, &out)
	})
	if err != nil {
		return providers.Response{}, fmt.Errorf("ollama chat: %w", err)
	}
	return providers.Response{Text: out.Message.Content, Usage: usage(out.PromptEvalCount, out.EvalCount)}, nil
}

// Embed implements providers.Provider.
func (c *Client) Embed(ctx context.Context, model, text string) (providers.Embedding, error) {
	body := map[string]any{"model": model, "input": text}
	var out struct {
		Embeddings      [][]float32 `json:"embeddings"`
		PromptEvalCount *int        `json:"prompt_eval_count"`
	}
	err := c.retry.Do(ctx, func() error {
		return providers.PostJSON(ctx, c.http, c.baseURL+"/api/embed", nil, body, &out)
	})
	if err != nil {
		return providers.Embedding{}, fmt.Errorf("ollama embed: %w", err)
	}
	if len(out.Embeddings) == 0 {
		return providers.Embedding{}, errors.New("ollama embed: empty embeddings in response")
	}
	return providers.Embedding{Vector: out.Embeddings[0], Usage: usage(out.PromptEvalCount, nil)}, nil
}

// SupportsEmbeddings implements providers.Provider.
func (c *Client) SupportsEmbeddings() bool { return true }
