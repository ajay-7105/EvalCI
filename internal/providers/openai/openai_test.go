package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ajay-7105/EvalCI/internal/providers"
)

func fastRetry() providers.Retrier { return providers.Retrier{Retries: 2, Base: time.Millisecond} }

func TestCompleteRequestShapeAndAuth(t *testing.T) {
	var got chatRequest
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"hi there"}}]}`))
	}))
	defer srv.Close()
	c := New("sk-test", srv.URL, srv.Client(), fastRetry())
	resp, err := c.Complete(context.Background(), providers.Request{Model: "gpt", System: "sys", User: "hi", Temperature: 0.2, MaxTokens: 10})
	if err != nil || resp.Text != "hi there" {
		t.Fatalf("resp=%v err=%v", resp, err)
	}
	if auth != "Bearer sk-test" || got.Model != "gpt" || len(got.Messages) != 2 || got.Messages[0].Role != "system" ||
		got.Temperature != 0.2 || got.MaxTokens != 10 {
		t.Fatalf("auth=%q req=%+v", auth, got)
	}
}

func TestRetries429And5xx(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		switch n.Add(1) {
		case 1:
			w.WriteHeader(http.StatusTooManyRequests)
		case 2:
			w.WriteHeader(http.StatusBadGateway)
		default:
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
		}
	}))
	defer srv.Close()
	resp, err := New("k", srv.URL, srv.Client(), fastRetry()).Complete(context.Background(), providers.Request{Model: "m", User: "u"})
	if err != nil || resp.Text != "ok" || n.Load() != 3 {
		t.Fatalf("resp=%v err=%v attempts=%d", resp, err, n.Load())
	}
}

func TestAuthFailureNotRetried(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	if _, err := New("bad", srv.URL, srv.Client(), fastRetry()).Complete(context.Background(), providers.Request{Model: "m", User: "u"}); err == nil || n.Load() != 1 {
		t.Fatalf("err=%v attempts=%d", err, n.Load())
	}
}

func TestEmptyChoicesIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"choices":[]}`)) }))
	defer srv.Close()
	if _, err := New("k", srv.URL, srv.Client(), fastRetry()).Complete(context.Background(), providers.Request{Model: "m", User: "u"}); err == nil {
		t.Fatal("no choices must error")
	}
}

func TestEmbed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embeddings" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"data":[{"embedding":[0.1,0.2,0.3]}]}`))
	}))
	defer srv.Close()
	c := New("k", srv.URL, srv.Client(), fastRetry())
	v, err := c.Embed(context.Background(), "text-embedding-3-small", "x")
	if err != nil || len(v.Vector) != 3 {
		t.Fatalf("v=%v err=%v", v, err)
	}
	if !c.SupportsEmbeddings() {
		t.Fatal("openai supports embeddings")
	}
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"data":[]}`)) }))
	defer empty.Close()
	if _, err := New("k", empty.URL, empty.Client(), fastRetry()).Embed(context.Background(), "m", "x"); err == nil {
		t.Fatal("empty data must error")
	}
}

func TestDefaultBaseURL(t *testing.T) {
	if c := New("k", "", nil, fastRetry()); c.baseURL != DefaultBaseURL {
		t.Fatalf("baseURL = %s", c.baseURL)
	}
}

func TestUsageReported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/embeddings" {
			_, _ = w.Write([]byte(`{"data":[{"embedding":[1]}],"usage":{"prompt_tokens":9,"total_tokens":9}}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"x"}}],"usage":{"prompt_tokens":20,"completion_tokens":4}}`))
	}))
	defer srv.Close()
	c := New("k", srv.URL, srv.Client(), fastRetry())
	resp, err := c.Complete(context.Background(), providers.Request{Model: "m", User: "u"})
	if err != nil || resp.Usage != (providers.Usage{InputTokens: 20, OutputTokens: 4, Known: true}) {
		t.Fatalf("usage = %+v err=%v", resp.Usage, err)
	}
	e, err := c.Embed(context.Background(), "m", "t")
	if err != nil || e.Usage != (providers.Usage{InputTokens: 9, Known: true}) {
		t.Fatalf("embed usage = %+v err=%v", e.Usage, err)
	}
}

func TestUsageUnknownWhenAbsent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"x"}}]}`))
	}))
	defer srv.Close()
	resp, err := New("k", srv.URL, srv.Client(), fastRetry()).Complete(context.Background(), providers.Request{Model: "m", User: "u"})
	if err != nil || resp.Usage.Known {
		t.Fatalf("usage must be unknown: %+v err=%v", resp.Usage, err)
	}
}
