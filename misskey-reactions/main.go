package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/hajimehoshi/ebiten/v2"
)

// --- Configuration ---

// Config stores application configuration.
type Config struct {
	MisskeyInstance string `json:"misskey_instance"`
	AccessToken     string `json:"access_token"`
}

// loadConfig reads and validates the configuration file.
func loadConfig() (*Config, error) {
	data, err := os.ReadFile("config.json")
	if err != nil {
		return nil, fmt.Errorf("cannot read config.json: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("invalid format in config.json: %w", err)
	}

	if cfg.MisskeyInstance == "" || cfg.MisskeyInstance == "your.misskey.instance.com" || cfg.AccessToken == "" || cfg.AccessToken == "YOUR_MISSKEY_ACCESS_TOKEN" {
		return nil, fmt.Errorf("please update config.json with your actual Misskey instance and access token")
	}

	return &cfg, nil
}

// --- Misskey WebSocket Communication ---

// Structures for parsing Misskey's streaming messages.
type MisskeyStreamMessage struct {
	Type string `json:"type"`
	Body struct {
		ID   string `json:"id"`
		Type string `json:"type"`
		Body struct {
			Reaction string `json:"reaction"`
		} `json:"body"`
	} `json:"body"`
}

// connectToMisskey establishes a WebSocket connection and listens for reactions.
func connectToMisskey(cfg *Config, reactionChan chan<- string) {
	// Construct the WebSocket URL.
	u := url.URL{Scheme: "wss", Host: cfg.MisskeyInstance, Path: "/streaming", RawQuery: "i=" + cfg.AccessToken}
	log.Printf("Connecting to %s", u.String())

	// Connect to the server.
	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Fatalf("Failed to connect to Misskey: %v", err)
		return
	}
	defer c.Close()

	// Subscribe to the 'main' channel to receive notifications.
	channelID := uuid.New().String()
	connectMsg := map[string]interface{}{
		"type": "connect",
		"body": map[string]interface{}{
			"channel": "main",
			"id":      channelID,
		},
	}
	if err := c.WriteJSON(connectMsg); err != nil {
		log.Fatalf("Failed to subscribe to channel: %v", err)
		return
	}
	log.Println("Successfully connected and subscribed to 'main' channel.")

	// Loop to read messages from the WebSocket.
	for {
		var msg MisskeyStreamMessage
		if err := c.ReadJSON(&msg); err != nil {
			log.Printf("Error reading message: %v", err)
			// Simple reconnect logic
			time.Sleep(5 * time.Second)
			go connectToMisskey(cfg, reactionChan) // Attempt to reconnect
			return
		}

		// Check if the message is a reaction to one of your notes.
		if msg.Type == "channel" && msg.Body.Type == "noteUpdated" && msg.Body.Body.Reaction != "" {
			log.Printf("Reaction received: %s", msg.Body.Body.Reaction)
			reactionChan <- msg.Body.Body.Reaction
		}
	}
}

// --- Ebitengine Game Loop ---

// ReactionObject represents a single emoji object on screen.
type ReactionObject struct {
	// TODO: Add fields for position, velocity, etc.
}

// Game implements ebiten.Game interface.
type Game struct {
	objects      []*ReactionObject
	reactionChan <-chan string
}

// NewGame initializes the game state.
func NewGame(rc <-chan string) *Game {
	return &Game{
		reactionChan: rc,
	}
}

func (g *Game) Update() error {
	// Check for new reactions from the channel.
	select {
	case reaction := <-g.reactionChan:
		log.Printf("Game loop received reaction: %s. Spawning object...", reaction)
		// TODO: Create a new ReactionObject and add it to g.objects.
	default:
		// No new reaction.
	}

	// TODO: Update positions of all objects in g.objects.
	return nil
}

func (g *Game) Draw(screen *ebiten.Image) {
	// TODO: Draw all objects in g.objects.
}

func (g *Game) Layout(outsideWidth, outsideHeight int) (int, int) {
	return outsideWidth, outsideHeight
}

// --- Main Function ---

func main() {
	log.Println("Starting Misskey Reaction Visualizer...")

	// Load configuration.
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	// Create a channel to pass reactions from the WebSocket goroutine to the game loop.
	reactionChan := make(chan string, 32)

	// Start the WebSocket connection in the background.
	go connectToMisskey(cfg, reactionChan)

	// Set up Ebitengine window.
	ebiten.SetWindowDecorated(false)
	ebiten.SetWindowFloating(true)
	ebiten.SetWindowMousePassthrough(true)
	ebiten.SetWindowTitle("Misskey Reactions")

	screenWidth, screenHeight := ebiten.Monitor().Size()
	ebiten.SetWindowSize(screenWidth, screenHeight-1)

	// Create and run the game.
	game := NewGame(reactionChan)
	opts := ebiten.RunGameOptions{
		ScreenTransparent: true,
	}

	if err := ebiten.RunGameWithOptions(game, &opts); err != nil {
		log.Fatal(err)
	}
}
