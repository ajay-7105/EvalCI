// Command mockserver is a tiny fake Ollama server for the mock example. It needs no model.
//
// For POST /api/chat it replies with the system message (so the prompt file decides the
// output). If the system message starts with NOISY, it alternates GOOD and BAD per distinct
// system message instead, producing scores that swing between 0 and 100. Replies carry token
// usage (word counts) like a real server, and POST /api/embed returns a fixed vector.
package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"strings"
	"sync"
)

type chatRequest struct {
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
}

func main() {
	addr := flag.String("addr", "127.0.0.1:18080", "listen address")
	flag.Parse()

	var mu sync.Mutex
	counts := map[string]int{}

	http.HandleFunc("/api/chat", func(w http.ResponseWriter, r *http.Request) {
		var req chatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		system := ""
		for _, m := range req.Messages {
			if m.Role == "system" {
				system = m.Content
			}
		}
		reply := strings.TrimSpace(system)
		if strings.HasPrefix(reply, "NOISY") {
			mu.Lock()
			counts[reply]++
			n := counts[reply]
			mu.Unlock()
			reply = "BAD"
			if n%2 == 1 {
				reply = "GOOD"
			}
		}
		prompt := 0
		for _, m := range req.Messages {
			prompt += len(strings.Fields(m.Content))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"message":           map[string]string{"role": "assistant", "content": reply},
			"prompt_eval_count": prompt,
			"eval_count":        len(strings.Fields(reply)),
		})
	})

	http.HandleFunc("/api/embed", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"embeddings":        [][]float32{{1, 0}},
			"prompt_eval_count": len(strings.Fields(req.Input)),
		})
	})

	log.Printf("mock ollama listening on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, nil))
}
