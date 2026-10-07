// Package openai is the OpenAI provider adapter.
package openai

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/ajay-7105/EvalCI/internal/providers"
)

// DefaultBaseURL is the public OpenAI API endpoint.
const DefaultBaseURL = "https://api.openai.com/v1"

// Client implements providers.Provider against the OpenAI API.
type Client struct {
	apiKey  string
	baseURL string
	http    *http.Client
	retry   providers.Retrier
}

// New returns a client. An empty baseURL selects DefaultBaseURL.
func New(apiKey, baseURL string, c *http.Client, r providers.Retrier) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if c == nil {
		c = http.DefaultClient
	}
	return &Client{apiKey: apiKey, baseURL: strings.TrimRight(baseURL, "/"), http: c, retry: r}
}

func (c *Client) headers() map[string]string {
	return map[string]string{"Authorization": "Bearer " + c.apiKey}
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model       string    `json:"model"`
	Messages    []message `json:"messages"`
	Temperature float64   `json:"temperature"`
	MaxTokens   int       `json:"max_completion_tokens,omitempty"`
}

type usageJSON struct {
	PromptTokens     *int `json:"prompt_tokens"`
	CompletionTokens *int `json:"completion_tokens"`
}

// toUsage maps the usage block; an absent block means usage is unknown.
func (u *usageJSON) toUsage() providers.Usage {
	if u == nil || (u.PromptTokens == nil && u.CompletionTokens == nil) {
		return providers.Usage{}
	}
	out := providers.Usage{Known: true}
	if u.PromptTokens != nil {
		out.InputTokens = *u.PromptTokens
	}
	if u.CompletionTokens != nil {
		out.OutputTokens = *u.CompletionTokens
	}
	return out
}

type chatResponse struct {
	Choices []struct {
		Message message `json:"message"`
	} `json:"choices"`
	Usage *usageJSON `json:"usage"`
}

// Complete implements providers.Provider.
func (c *Client) Complete(ctx context.Context, req providers.Request) (providers.Response, error) {
	var msgs []message
	if req.System != "" {
		msgs = append(msgs, message{Role: "system", Content: req.System})
	}
	msgs = append(msgs, message{Role: "user", Content: req.User})
	body := chatRequest{Model: req.Model, Messages: msgs, Temperature: req.Temperature, MaxTokens: req.MaxTokens}

	var out chatResponse
	err := c.retry.Do(ctx, func() error {
		return providers.PostJSON(ctx, c.http, c.baseURL+"/chat/completions", c.headers(), body, &out)
	})
	if err != nil {
		return providers.Response{}, fmt.Errorf("openai chat: %w", err)
	}
	if len(out.Choices) == 0 {
		return providers.Response{}, errors.New("openai chat: response has no choices")
	}
	return providers.Response{Text: out.Choices[0].Message.Content, Usage: out.Usage.toUsage()}, nil
}

// Embed implements providers.Provider.
func (c *Client) Embed(ctx context.Context, model, text string) (providers.Embedding, error) {
	body := map[string]any{"model": model, "input": text}
	var out struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
		Usage *usageJSON `json:"usage"`
	}
	err := c.retry.Do(ctx, func() error {
		return providers.PostJSON(ctx, c.http, c.baseURL+"/embeddings", c.headers(), body, &out)
	})
	if err != nil {
		return providers.Embedding{}, fmt.Errorf("openai embed: %w", err)
	}
	if len(out.Data) == 0 {
		return providers.Embedding{}, errors.New("openai embed: response has no data")
	}
	return providers.Embedding{Vector: out.Data[0].Embedding, Usage: out.Usage.toUsage()}, nil
}

// SupportsEmbeddings implements providers.Provider.
func (c *Client) SupportsEmbeddings() bool { return true }
