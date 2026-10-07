// Package anthropic is the Anthropic provider adapter.
package anthropic

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/ajay-7105/EvalCI/internal/providers"
)

// DefaultBaseURL is the public Anthropic API endpoint.
const DefaultBaseURL = "https://api.anthropic.com/v1"

const (
	apiVersion       = "2023-06-01"
	defaultMaxTokens = 1024
)

// Client implements providers.Provider against the Anthropic Messages API.
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

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type messagesRequest struct {
	Model       string    `json:"model"`
	MaxTokens   int       `json:"max_tokens"`
	System      string    `json:"system,omitempty"`
	Messages    []message `json:"messages"`
	Temperature float64   `json:"temperature"`
}

type messagesResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Usage *struct {
		InputTokens  *int `json:"input_tokens"`
		OutputTokens *int `json:"output_tokens"`
	} `json:"usage"`
}

// usage maps the usage block; an absent block means usage is unknown.
func (r messagesResponse) usage() providers.Usage {
	if r.Usage == nil || (r.Usage.InputTokens == nil && r.Usage.OutputTokens == nil) {
		return providers.Usage{}
	}
	u := providers.Usage{Known: true}
	if r.Usage.InputTokens != nil {
		u.InputTokens = *r.Usage.InputTokens
	}
	if r.Usage.OutputTokens != nil {
		u.OutputTokens = *r.Usage.OutputTokens
	}
	return u
}

// Complete implements providers.Provider.
func (c *Client) Complete(ctx context.Context, req providers.Request) (providers.Response, error) {
	max := req.MaxTokens
	if max <= 0 {
		max = defaultMaxTokens
	}
	body := messagesRequest{
		Model: req.Model, MaxTokens: max, System: req.System,
		Messages:    []message{{Role: "user", Content: req.User}},
		Temperature: req.Temperature,
	}
	headers := map[string]string{"x-api-key": c.apiKey, "anthropic-version": apiVersion}

	var out messagesResponse
	err := c.retry.Do(ctx, func() error {
		return providers.PostJSON(ctx, c.http, c.baseURL+"/messages", headers, body, &out)
	})
	if err != nil {
		return providers.Response{}, fmt.Errorf("anthropic messages: %w", err)
	}
	var sb strings.Builder
	for _, part := range out.Content {
		if part.Type == "text" {
			sb.WriteString(part.Text)
		}
	}
	if sb.Len() == 0 {
		return providers.Response{}, errors.New("anthropic messages: response has no text content")
	}
	return providers.Response{Text: sb.String(), Usage: out.usage()}, nil
}

// Embed implements providers.Provider; Anthropic has no embeddings endpoint.
func (c *Client) Embed(context.Context, string, string) (providers.Embedding, error) {
	return providers.Embedding{}, providers.ErrEmbeddingsUnsupported
}

// SupportsEmbeddings implements providers.Provider.
func (c *Client) SupportsEmbeddings() bool { return false }
