package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	workers "github.com/syumai/workers-go"
	"github.com/syumai/workers-go/cloudflare"
	"github.com/syumai/workers-go/cloudflare/fetch"
	"github.com/syumai/workers-go/cloudflare/kv"
)

type Msg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatReq struct {
	SessionID string `json:"sessionId"`
	Message   string `json:"message"`
}

func chat(w http.ResponseWriter, r *http.Request) {
	var in chatReq
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	store, err := kv.NewNamespace("CHAT_KV")
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}

	// Load history (a missing key just means a new session)
	var history []Msg
	if raw, err := store.GetString("session:"+in.SessionID, nil); err == nil && raw != "" {
		_ = json.Unmarshal([]byte(raw), &history)
	}
	history = append(history, Msg{"user", in.Message})
	if len(history) > 20 {
		history = history[len(history)-20:] // bound the context window
	}

	// Call Llama 3.3 via the Workers AI REST API
	payload, _ := json.Marshal(map[string]any{
		"messages": append([]Msg{{"system", "You are a helpful assistant."}}, history...),
	})
	url := fmt.Sprintf(
		"https://api.cloudflare.com/client/v4/accounts/%s/ai/run/@cf/meta/llama-3.3-70b-instruct-fp8-fast",
		cloudflare.Getenv("CF_ACCOUNT_ID"),
	)
	req, _ := fetch.NewRequest(r.Context(), http.MethodPost, url, bytes.NewReader(payload))
	req.Header.Set("Authorization", "Bearer "+cloudflare.Getenv("CF_API_TOKEN"))
	req.Header.Set("Content-Type", "application/json")

	resp, err := fetch.NewClient().Do(req, nil)
	if err != nil {
		http.Error(w, err.Error(), 502)
		return
	}
	defer resp.Body.Close()

	var out struct {
		Result struct {
			Response string `json:"response"`
		} `json:"result"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&out)

	history = append(history, Msg{"assistant", out.Result.Response})
	saved, _ := json.Marshal(history)
	_ = store.PutString("session:"+in.SessionID, string(saved), nil)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"reply": out.Result.Response})
}

func main() {
	http.HandleFunc("/api/chat", chat)
	workers.Serve(nil) // uses http.DefaultServeMux
}
