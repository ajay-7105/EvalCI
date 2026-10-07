package ollama

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

func TestCompleteRequestAndResponse(t *testing.T) {
	var got chatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" || r.Method != http.MethodPost {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_ = json.NewEncoder(w).Encode(map[string]any{"message": map[string]string{"role": "assistant", "content": "hello"}})
	}))
	defer srv.Close()

	c := New(srv.URL+"/", srv.Client(), fastRetry())
	resp, err := c.Complete(context.Background(), providers.Request{Model: "llama3", System: "sys", User: "hi", Temperature: 0.3, MaxTokens: 50})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Text != "hello" {
		t.Fatalf("text = %q", resp.Text)
	}
	if got.Model != "llama3" || got.Stream || len(got.Messages) != 2 || got.Messages[0].Role != "system" ||
		got.Messages[1].Content != "hi" || got.Options["temperature"] != 0.3 || got.Options["num_predict"] != float64(50) {
		t.Fatalf("bad request: %+v", got)
	}
}

func TestCompleteOmitsEmptySystem(t *testing.T) {
	var got chatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"message":{"content":"x"}}`))
	}))
	defer srv.Close()
	if _, err := New(srv.URL, srv.Client(), fastRetry()).Complete(context.Background(), providers.Request{Model: "m", User: "u"}); err != nil {
		t.Fatal(err)
	}
	if len(got.Messages) != 1 || got.Messages[0].Role != "user" {
		t.Fatalf("messages = %+v", got.Messages)
	}
	if _, ok := got.Options["num_predict"]; ok {
		t.Fatal("num_predict must be omitted when MaxTokens is 0")
	}
}

func TestRetriesTransientThenSucceeds(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if n.Add(1) < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"message":{"content":"ok"}}`))
	}))
	defer srv.Close()
	resp, err := New(srv.URL, srv.Client(), fastRetry()).Complete(context.Background(), providers.Request{Model: "m", User: "u"})
	if err != nil || resp.Text != "ok" || n.Load() != 3 {
		t.Fatalf("resp=%v err=%v attempts=%d", resp, err, n.Load())
	}
}

func TestPermanentErrorNotRetried(t *testing.T) {
	var n atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n.Add(1)
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`model not found`))
	}))
	defer srv.Close()
	_, err := New(srv.URL, srv.Client(), fastRetry()).Complete(context.Background(), providers.Request{Model: "m", User: "u"})
	if err == nil || n.Load() != 1 {
		t.Fatalf("err=%v attempts=%d", err, n.Load())
	}
}

func TestEmbed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/embed" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"embeddings":[[0.5,0.25]]}`))
	}))
	defer srv.Close()
	c := New(srv.URL, srv.Client(), fastRetry())
	v, err := c.Embed(context.Background(), "nomic", "text")
	if err != nil || len(v.Vector) != 2 || v.Vector[0] != 0.5 {
		t.Fatalf("v=%v err=%v", v, err)
	}
	if v.Usage.Known {
		t.Fatalf("usage must be unknown when the server reports none: %+v", v.Usage)
	}
	if !c.SupportsEmbeddings() {
		t.Fatal("ollama supports embeddings")
	}
}

func TestEmbedEmptyResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"embeddings":[]}`))
	}))
	defer srv.Close()
	if _, err := New(srv.URL, srv.Client(), fastRetry()).Embed(context.Background(), "m", "t"); err == nil {
		t.Fatal("empty embeddings must error")
	}
}

func TestMalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()
	if _, err := New(srv.URL, srv.Client(), fastRetry()).Complete(context.Background(), providers.Request{Model: "m", User: "u"}); err == nil {
		t.Fatal("malformed body must error")
	}
}

func TestUsageReported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/embed" {
			_, _ = w.Write([]byte(`{"embeddings":[[1]],"prompt_eval_count":7}`))
			return
		}
		_, _ = w.Write([]byte(`{"message":{"content":"x"},"prompt_eval_count":11,"eval_count":5}`))
	}))
	defer srv.Close()
	c := New(srv.URL, srv.Client(), fastRetry())
	resp, err := c.Complete(context.Background(), providers.Request{Model: "m", User: "u"})
	if err != nil || resp.Usage != (providers.Usage{InputTokens: 11, OutputTokens: 5, Known: true}) {
		t.Fatalf("usage = %+v err=%v", resp.Usage, err)
	}
	e, err := c.Embed(context.Background(), "m", "t")
	if err != nil || e.Usage != (providers.Usage{InputTokens: 7, Known: true}) {
		t.Fatalf("embed usage = %+v err=%v", e.Usage, err)
	}
}

func TestUsageUnknownWhenAbsent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"message":{"content":"x"}}`))
	}))
	defer srv.Close()
	resp, err := New(srv.URL, srv.Client(), fastRetry()).Complete(context.Background(), providers.Request{Model: "m", User: "u"})
	if err != nil || resp.Usage.Known {
		t.Fatalf("usage must be unknown: %+v err=%v", resp.Usage, err)
	}
}
