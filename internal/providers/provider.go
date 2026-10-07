// Package providers defines the single adapter interface every LLM provider implements.
package providers

import (
	"context"
	"errors"
	"fmt"
	"net"
)

// Request is a provider-neutral chat completion request.
type Request struct {
	// Model is the provider's model name.
	Model string
	// System is the optional system message.
	System string
	// User is the user message.
	User string
	// Temperature is the sampling temperature.
	Temperature float64
	// MaxTokens caps the response length; 0 lets the adapter choose.
	MaxTokens int
}

// Usage is the token usage a provider reported for one call.
type Usage struct {
	// InputTokens is the number of prompt tokens.
	InputTokens int
	// OutputTokens is the number of completion tokens (0 for embeddings).
	OutputTokens int
	// Known is false when the provider reported no usage for the call.
	Known bool
}

// Response is a provider-neutral completion result.
type Response struct {
	// Text is the model's reply.
	Text string
	// Usage is the token usage reported for the call.
	Usage Usage
}

// Embedding is a provider-neutral embedding result.
type Embedding struct {
	// Vector is the embedding.
	Vector []float32
	// Usage is the token usage reported for the call.
	Usage Usage
}

// Provider is the one interface every LLM provider adapter implements.
type Provider interface {
	// Complete generates a reply for req.
	Complete(ctx context.Context, req Request) (Response, error)
	// Embed returns an embedding vector for text. It returns ErrEmbeddingsUnsupported
	// when SupportsEmbeddings is false.
	Embed(ctx context.Context, model, text string) (Embedding, error)
	// SupportsEmbeddings reports whether Embed is available for this provider.
	SupportsEmbeddings() bool
}

// ErrEmbeddingsUnsupported is returned by providers that have no embeddings endpoint.
var ErrEmbeddingsUnsupported = errors.New("provider does not support embeddings")

// StatusError is a non-success HTTP response from a provider.
type StatusError struct {
	// Status is the HTTP status code.
	Status int
	// Body is a truncated response body for diagnostics.
	Body string
}

// Error implements error.
func (e *StatusError) Error() string {
	return fmt.Sprintf("provider returned HTTP %d: %s", e.Status, e.Body)
}

// IsTransient reports whether err is worth retrying: network errors, HTTP 429, and 5xx.
// Context cancellation and deadlines are never transient.
func IsTransient(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var se *StatusError
	if errors.As(err, &se) {
		return se.Status == 429 || se.Status >= 500
	}
	var ne net.Error
	return errors.As(err, &ne)
}
