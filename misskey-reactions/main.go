package main

import (
	"encoding/json"
	"fmt"
	"image/color"
	"log"
	"math"
	"math/rand"
	"net/url"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
)

const (
	maxObjects  = 100
	minLifetime = 300
	maxLifetime = 900
	fontSize    = 32
	textDPI     = 72
)

var (
	// Use the v2 text.Face interface.
	emojiFont text.Face
)

// --- Configuration ---
type Config struct {
	MisskeyInstance string `json:"misskey_instance"`
	AccessToken     string `json:"access_token"`
}

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
		return nil, fmt.Errorf("please update config.json")
	}
	return &cfg, nil
}

// --- Misskey WebSocket Communication ---
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
}

func connectToMisskey(cfg *Config, reactionChan chan<- string) {
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
			var notification NotificationBody
			if err := json.Unmarshal(msg.Body.Body, &notification); err != nil {
				continue
			}
			if notification.Type == "reaction" && notification.Reaction != "" {
				reactionChan <- notification.Reaction
			}
		}
	}
}

// --- Ebitengine Game Loop ---
type ReactionObject struct {
	x, y, vx, vy float64
	lifetime     int
	emoji        string
}

type Game struct {
	objects      []*ReactionObject
	reactionChan <-chan string
}

func NewGame(rc <-chan string) *Game {
	return &Game{reactionChan: rc}
}

func (g *Game) spawnReaction(reaction string, w, h int) {
	if len(g.objects) >= maxObjects {
		return
	}
	var x, y float64
	edge := rand.Intn(4)
	padding := float64(fontSize)
	switch edge {
	case 0:
		x, y = rand.Float64()*float64(w), -padding
	case 1:
		x, y = float64(w)+padding, rand.Float64()*float64(h)
	case 2:
		x, y = rand.Float64()*float64(w), float64(h)+padding
	case 3:
		x, y = -padding, rand.Float64()*float64(h)
	}
	angle := math.Atan2(float64(h/2)-y, float64(w/2)-x) + (rand.Float64()-0.5)*(math.Pi/2)
	speed := 0.5 + rand.Float64()*1.5
	obj := &ReactionObject{
		x: x, y: y, vx: math.Cos(angle) * speed, vy: math.Sin(angle) * speed,
		lifetime: minLifetime + rand.Intn(maxLifetime-minLifetime),
		emoji:    reaction,
	}
	g.objects = append(g.objects, obj)
}

func (g *Game) Update() error {
	w, h := ebiten.WindowSize()
	select {
	case reaction := <-g.reactionChan:
		g.spawnReaction(reaction, w, h)
	default:
	}

	nextObjects := make([]*ReactionObject, 0, len(g.objects))
	for _, o := range g.objects {
		o.x += o.vx
		o.y += o.vy
		o.lifetime--
		padding := float64(fontSize)
		isOutside := o.x+padding < 0 || o.x-padding > float64(w) || o.y+padding < 0 || o.y-padding > float64(h)
		if o.lifetime < 0 && isOutside {
			continue
		}
		if o.lifetime >= 0 {
			if (o.vx < 0 && o.x-padding < 0) || (o.vx > 0 && o.x+padding > float64(w)) {
				o.vx *= -1
			}
			if (o.vy < 0 && o.y-padding < 0) || (o.vy > 0 && o.y+padding > float64(h)) {
				o.vy *= -1
			}
		}
		nextObjects = append(nextObjects, o)
	}
	g.objects = nextObjects
	return nil
}

func (g *Game) Draw(screen *ebiten.Image) {
	for _, o := range g.objects {
		emojiToDraw := o.emoji
		if len(o.emoji) > 2 && o.emoji[0] == ':' && o.emoji[len(o.emoji)-1] == ':' {
			emojiToDraw = "💖"
		}

		// Use the v2 text API.
		op := &text.DrawOptions{}
		width, height := text.Measure(emojiToDraw, emojiFont, float64(fontSize))

		// Calculate coordinates to center the text.
		x := o.x - width/2
		y := o.y - height/2
		op.GeoM.Translate(x, y)

		// For color fonts, the color multiplier should be white.
		op.ColorM.ScaleWithColor(color.Black)

		text.Draw(screen, emojiToDraw, emojiFont, op)
	}
}

func (g *Game) Layout(outsideWidth, outsideHeight int) (int, int) {
	return outsideWidth, outsideHeight
}

// --- Main Function ---
func init() {
	fontData, err := os.Open("NotoColorEmoji-Regular.ttf")
	if err != nil {
		log.Fatalf("Failed to read font file: %v. Please download NotoColorEmoji-Regular.ttf and place it in the project directory.", err)
	}

	src, err := text.NewGoTextFaceSource(fontData)
	if err != nil {
		log.Fatal(err)
	}

	emojiFont = &text.GoTextFace{
		Source: src,
		Size:   fontSize,
	}
}

func main() {
	log.Println("Starting Misskey Reaction Visualizer...")
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("Configuration error: %v", err)
	}
	reactionChan := make(chan string, 32)
	go connectToMisskey(cfg, reactionChan)
	ebiten.SetWindowDecorated(false)
	ebiten.SetWindowFloating(true)
	ebiten.SetWindowMousePassthrough(true)
	ebiten.SetWindowTitle("Misskey Reactions")
	screenWidth, screenHeight := ebiten.Monitor().Size()
	ebiten.SetWindowSize(screenWidth, screenHeight-1)
	game := NewGame(reactionChan)
	opts := ebiten.RunGameOptions{ScreenTransparent: true}
	if err := ebiten.RunGameWithOptions(game, &opts); err != nil {
		log.Fatal(err)
	}
}
