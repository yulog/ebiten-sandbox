package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// --- Misskey WebSocket/API Communication ---
type MisskeyStreamMessage struct {
	Type string `json:"type"`
	Body struct {
		ID   string          `json:"id"`
		Type string          `json:"type"`
		Body json.RawMessage `json:"body"`
	} `json:"body"`
}

type NotificationBody struct {
	Type     string `json:"type"`
	Reaction string `json:"reaction"`
	Note     struct {
		ReactionEmojis map[string]string `json:"reactionEmojis"`
	} `json:"note"`
}

type ReactionInfo struct {
	Name string
	URL  string
}

func connectToMisskey(cfg *Config, reactionChan chan<- ReactionInfo) {
	u := url.URL{Scheme: "wss", Host: cfg.MisskeyInstance, Path: "/streaming", RawQuery: "i=" + cfg.AccessToken}
	log.Printf("Connecting to %s", u.String())
	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer c.Close()
	channelID := uuid.New().String()
	connectMsg := map[string]interface{}{"type": "connect", "body": map[string]interface{}{"channel": "main", "id": channelID}}
	if err := c.WriteJSON(connectMsg); err != nil {
		log.Fatalf("Failed to subscribe: %v", err)
	}
	log.Println("Successfully connected and subscribed.")
	for {
		var msg MisskeyStreamMessage
		if err := c.ReadJSON(&msg); err != nil {
			log.Printf("Read error: %v. Reconnecting...", err)
			time.Sleep(5 * time.Second)
			go connectToMisskey(cfg, reactionChan)
			return
		}
		if msg.Type == "channel" && msg.Body.Type == "notification" {
			var n NotificationBody
			if err := json.Unmarshal(msg.Body.Body, &n); err == nil && n.Type == "reaction" && n.Reaction != "" {
				reaction := ReactionInfo{Name: n.Reaction}
				if url, ok := n.Note.ReactionEmojis[strings.Trim(n.Reaction, ":")]; ok {
					reaction.URL = url
				}
				reactionChan <- reaction
			}
		}
	}
}

// queryEmojiAPI fetches a custom emoji URL from the instance API.
type EmojiAPIResponse struct {
	URL string `json:"url"`
}

func queryEmojiAPI(emojiName string) (string, error) {
	if appConfig == nil {
		return "", fmt.Errorf("app config not loaded")
	}
	apiURL := fmt.Sprintf("https://%s/api/emoji", appConfig.MisskeyInstance)
	payload := map[string]string{"name": emojiName}
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	resp, err := http.Post(apiURL, "application/json", bytes.NewBuffer(jsonPayload))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("emoji API returned status: %s", resp.Status)
	}

	var apiResp EmojiAPIResponse
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return "", err
	}

	if apiResp.URL == "" {
		return "", fmt.Errorf("emoji '%s' not found via API", emojiName)
	}

	return apiResp.URL, nil
}

// --- Test Mode ---
func runTestMode(reactionChan chan<- ReactionInfo) {
	log.Println("--- RUNNING IN TEST MODE ---")
	mockData := []ReactionInfo{
		{Name: "👍"},
		// {Name: ":ebiten:", URL: "https://ebitengine.org/images/logo.png"},                                                               // Valid custom emoji
		{Name: ":misskey:", URL: "https://proxy.misskeyusercontent.jp/image/media.misskeyusercontent.jp%2Femoji%2Fmisskey.png?emoji=1"}, // Valid custom emoji
		{Name: "Go"}, // Standard text, will become a Twemoji
		{Name: ":error:", URL: "https://example.com/nonexistent-image.png"}, // Invalid custom emoji to test fallback
		{Name: "❤️"},
		{Name: ":ai_nomming:", URL: "https://proxy.misskeyusercontent.jp/image/media.misskeyusercontent.jp%2Fmisskey%2Ff6294900-f678-43cc-bc36-3ee5deeca4c2.gif?emoji=1"},
		{Name: ":meowsurprised:", URL: "https://proxy.misskeyusercontent.jp/image/media.misskeyusercontent.jp%2Femoji%2FmeowSurprised.png?emoji=1"},
		{Name: ":bug:"},
		{Name: ":syuilo_yay:"}, // invalid format: chunk out of order
		{Name: ":ai_akan:"},
		{Name: ":murakamisan_spin:"},
		{Name: ":blobdance2:"},
		{Name: ":resonyance:"},
	}

	// Loop forever, sending mock data every 2 seconds
	for {
		for _, reaction := range mockData {
			log.Printf("[TEST MODE] Spawning reaction: %s", reaction.Name)
			reactionChan <- reaction
			time.Sleep(2 * time.Second)
		}
	}
}
