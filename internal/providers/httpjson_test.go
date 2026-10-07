package providers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPostJSON(t *testing.T) {
	var gotHeader, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Test") + "|" + r.Header.Get("Content-Type")
		b := make([]byte, 64)
		n, _ := r.Body.Read(b)
		gotBody = string(b[:n])
		_, _ = w.Write([]byte(`{"ok": true}`))
	}))
	defer srv.Close()

	var out struct{ OK bool }
	err := PostJSON(context.Background(), srv.Client(), srv.URL, map[string]string{"X-Test": "1"}, map[string]int{"a": 1}, &out)
	if err != nil || !out.OK || gotHeader != "1|application/json" || !strings.Contains(gotBody, `"a":1`) {
		t.Fatalf("out=%+v err=%v header=%q body=%q", out, err, gotHeader, gotBody)
	}
}

func TestPostJSONErrors(t *testing.T) {
	status := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(strings.Repeat("x", 2000)))
	}))
	defer status.Close()
	var se *StatusError
	err := PostJSON(context.Background(), status.Client(), status.URL, nil, struct{}{}, &struct{}{})
	if !errors.As(err, &se) || se.Status != 503 || len(se.Body) > maxErrorBody || !IsTransient(err) {
		t.Fatalf("err = %v, want a transient StatusError with a truncated body", err)
	}

	badJSON := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("nope")) }))
	defer badJSON.Close()
	if err := PostJSON(context.Background(), badJSON.Client(), badJSON.URL, nil, struct{}{}, &struct{}{}); err == nil {
		t.Fatal("malformed JSON must error")
	}
	if err := PostJSON(context.Background(), http.DefaultClient, "http://127.0.0.1:1", nil, struct{}{}, &struct{}{}); err == nil {
		t.Fatal("connection failure must error")
	}
	if err := PostJSON(context.Background(), http.DefaultClient, "http://x", nil, make(chan int), &struct{}{}); err == nil {
		t.Fatal("an unencodable body must error")
	}
	if err := PostJSON(context.Background(), http.DefaultClient, "://bad", nil, struct{}{}, &struct{}{}); err == nil {
		t.Fatal("an invalid URL must error")
	}
}
