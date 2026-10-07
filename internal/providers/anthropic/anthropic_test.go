package anthropic

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ajay-7105/EvalCI/internal/providers"
)

func fastRetry() providers.Retrier { return providers.Retrier{Retries: 2, Base: time.Millisecond} }

func TestCompleteRequestShapeAndHeaders(t *testing.T) {
	var got messagesRequest
	var key, ver string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key, ver = r.Header.Get("x-api-key"), r.Header.Get("anthropic-version")
		if r.URL.Path != "/messages" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"Hello "},{"type":"text","text":"world"}]}`))
	}))
	defer srv.Close()
	c := New("sk-ant", srv.URL, srv.Client(), fastRetry())
	resp, err := c.Complete(context.Background(), providers.Request{Model: "claude", System: "sys", User: "hi", Temperature: 0.5})
	if err != nil || resp.Text != "Hello world" {
		t.Fatalf("resp=%v err=%v", resp, err)
	}
	if key != "sk-ant" || ver != apiVersion || got.System != "sys" || got.MaxTokens != defaultMaxTokens ||
		len(got.Messages) != 1 || got.Messages[0].Role != "user" || got.Temperature != 0.5 {
		t.Fatalf("key=%q ver=%q req=%+v", key, ver, got)
	}
}

func TestExplicitMaxTokens(t *testing.T) {
	var got messagesRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"x"}]}`))
	}))
	defer srv.Close()
	if _, err := New("k", srv.URL, srv.Client(), fastRetry()).Complete(context.Background(), providers.Request{Model: "m", User: "u", MaxTokens: 77}); err != nil {
		t.Fatal(err)
	}
	if got.MaxTokens != 77 || got.System != "" {
		t.Fatalf("req = %+v", got)
	}
}

func TestRetriesTransient(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if n.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"ok"}]}`))
	}))
	defer srv.Close()
	resp, err := New("k", srv.URL, srv.Client(), fastRetry()).Complete(context.Background(), providers.Request{Model: "m", User: "u"})
	if err != nil || resp.Text != "ok" || n.Load() != 2 {
		t.Fatalf("resp=%v err=%v attempts=%d", resp, err, n.Load())
	}
}

func TestNoTextContentIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"content":[{"type":"tool_use"}]}`))
	}))
	defer srv.Close()
	if _, err := New("k", srv.URL, srv.Client(), fastRetry()).Complete(context.Background(), providers.Request{Model: "m", User: "u"}); err == nil {
		t.Fatal("no text must error")
	}
}

func TestEmbeddingsUnsupported(t *testing.T) {
	c := New("k", "", nil, fastRetry())
	if c.SupportsEmbeddings() {
		t.Fatal("anthropic has no embeddings")
	}
	if _, err := c.Embed(context.Background(), "m", "x"); !errors.Is(err, providers.ErrEmbeddingsUnsupported) {
		t.Fatalf("err = %v", err)
	}
	if c.baseURL != DefaultBaseURL {
		t.Fatalf("baseURL = %s", c.baseURL)
	}
}

func TestUsageReported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"x"}],"usage":{"input_tokens":30,"output_tokens":8}}`))
	}))
	defer srv.Close()
	resp, err := New("k", srv.URL, srv.Client(), fastRetry()).Complete(context.Background(), providers.Request{Model: "m", User: "u"})
	if err != nil || resp.Usage != (providers.Usage{InputTokens: 30, OutputTokens: 8, Known: true}) {
		t.Fatalf("usage = %+v err=%v", resp.Usage, err)
	}
}

func TestUsageUnknownWhenAbsent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"content":[{"type":"text","text":"x"}]}`))
	}))
	defer srv.Close()
	resp, err := New("k", srv.URL, srv.Client(), fastRetry()).Complete(context.Background(), providers.Request{Model: "m", User: "u"})
	if err != nil || resp.Usage.Known {
		t.Fatalf("usage must be unknown: %+v err=%v", resp.Usage, err)
	}
}
