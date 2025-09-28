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

// MisskeyAPI defines the interface for interacting with Misskey.
// This allows for mocking in tests.
type MisskeyAPI interface {
	Connect(reactionChan chan<- ReactionInfo)
	QueryEmojiAPI(emojiName string) (string, error)
}

// MisskeyClient handles all communication with the Misskey API and WebSocket.
type MisskeyClient struct {
	config *Config
}

// Statically check that *MisskeyClient implements MisskeyAPI.
var _ MisskeyAPI = (*MisskeyClient)(nil)

// NewMisskeyClient creates a new client for interacting with Misskey.
func NewMisskeyClient(cfg *Config) *MisskeyClient {
	return &MisskeyClient{config: cfg}
}

// MisskeyStreamMessage defines the structure for incoming WebSocket messages.
type MisskeyStreamMessage struct {
	Type string `json:"type"`
	Body struct {
		ID   string          `json:"id"`
		Type string          `json:"type"`
		Body json.RawMessage `json:"body"`
	} `json:"body"`
}

// NotificationBody is the structure for the body of a reaction notification.
type NotificationBody struct {
	Type     string `json:"type"`
	Reaction string `json:"reaction"`
	Note     struct {
		ReactionEmojis map[string]string `json:"reactionEmojis"`
	} `json:"note"`
}

// ReactionInfo holds the name and optional URL of a reaction.
type ReactionInfo struct {
	Name string
	URL  string
}

// Connect establishes a WebSocket connection and listens for reactions.
func (mc *MisskeyClient) Connect(reactionChan chan<- ReactionInfo) {
	u := url.URL{Scheme: "wss", Host: mc.config.MisskeyInstance, Path: "/streaming", RawQuery: "i=" + mc.config.AccessToken}
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
			go mc.Connect(reactionChan) // Reconnect using the method
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

// EmojiAPIResponse is the structure for the emoji API response.
type EmojiAPIResponse struct {
	URL string `json:"url"`
}

// QueryEmojiAPI fetches a custom emoji URL from the instance API.
func (mc *MisskeyClient) QueryEmojiAPI(emojiName string) (string, error) {
	if mc.config == nil {
		return "", fmt.Errorf("misskey client config not loaded")
	}
	apiURL := fmt.Sprintf("https://%s/api/emoji", mc.config.MisskeyInstance)
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
