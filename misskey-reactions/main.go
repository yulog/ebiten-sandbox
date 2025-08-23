package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"io"

	_ "image/jpeg"
	_ "image/png"
	"log"
	"math"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	_ "github.com/gen2brain/webp"
	_ "golang.org/x/image/webp"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"golang.org/x/image/font/gofont/goregular"
)

const (
	maxObjects  = 100
	minLifetime = 300
	maxLifetime = 900
)

var (
	// imageCache can store *ebiten.Image for static images or *AnimatedGIF for GIFs.
	imageCache   = make(map[string]any)
	cacheMutex   = &sync.RWMutex{}
	fallbackFont *text.GoTextFace
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

// --- Test Mode ---
func runTestMode(reactionChan chan<- ReactionInfo) {
	log.Println("--- RUNNING IN TEST MODE ---")
	mockData := []ReactionInfo{
		{Name: "👍"},
		{Name: ":ebiten:", URL: "https://ebitengine.org/images/logo.png"}, // Valid custom emoji
		{Name: "Go"}, // Standard text, will become a Twemoji
		{Name: ":error:", URL: "https://example.com/nonexistent-image.png"}, // Invalid custom emoji to test fallback
		{Name: "❤️"},
		{Name: ":ai_nomming:", URL: "https://proxy.misskeyusercontent.jp/image/media.misskeyusercontent.jp%2Fmisskey%2Ff6294900-f678-43cc-bc36-3ee5deeca4c2.gif?emoji=1"},
		{Name: ":meowsurprised:", URL: "https://proxy.misskeyusercontent.jp/image/media.misskeyusercontent.jp%2Femoji%2FmeowSurprised.png?emoji=1"},
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

// --- Image Handling ---

// AnimatedGIF holds all the pre-rendered frames for a GIF.
type AnimatedGIF struct {
	Frames      []*ebiten.Image
	FrameDelays []int // Delay in 1/100s of a second
}

// DecodedImage holds the result of decoding, which can be static or animated.
type DecodedImage struct {
	Static   *ebiten.Image
	Animated *AnimatedGIF
}

// fetchAndDecodeImage downloads and decodes an image, pre-rendering GIFs correctly.
func fetchAndDecodeImage(url string) (*DecodedImage, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bad status: %s", resp.Status)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	contentType := http.DetectContentType(data)

	if strings.Contains(contentType, "gif") {
		g, err := gif.DecodeAll(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}

		// Pre-render GIF frames onto a canvas to handle frame disposal methods correctly.
		canvas := image.NewRGBA(image.Rect(0, 0, g.Config.Width, g.Config.Height))
		var frames []*ebiten.Image
		for i, srcImg := range g.Image {
			// Correctly draw the frame at its offset, using the frame's bounds for the source point.
			draw.Draw(canvas, srcImg.Bounds(), srcImg, srcImg.Bounds().Min, draw.Over)
			frameCopy := image.NewRGBA(canvas.Bounds())
			draw.Draw(frameCopy, frameCopy.Bounds(), canvas, image.Point{}, draw.Src)
			frames = append(frames, ebiten.NewImageFromImage(frameCopy))

			if g.Disposal[i] == gif.DisposalBackground {
				draw.Draw(canvas, srcImg.Bounds(), image.Transparent, image.Point{}, draw.Src)
			}
		}
		return &DecodedImage{Animated: &AnimatedGIF{Frames: frames, FrameDelays: g.Delay}}, nil
	} else {
		img, _, err := image.Decode(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		return &DecodedImage{Static: ebiten.NewImageFromImage(img)}, nil
	}
}

func emojiToTwemojiURL(emoji string) string {
	var codes []string
	for _, r := range emoji {
		if r != 0xfe0f {
			codes = append(codes, fmt.Sprintf("%x", r))
		}
	}
	return fmt.Sprintf("https://cdn.jsdelivr.net/gh/twitter/twemoji@latest/assets/72x72/%s.png", strings.Join(codes, "-"))
}

// --- Ebitengine Game Loop ---
type ReactionObject struct {
	x, y, vx, vy float64
	lifetime     int
	reactionName string
	image        *ebiten.Image
	animatedGif  *AnimatedGIF
	currentFrame int
	frameCounter int
	fallbackText string
}

type Game struct {
	objects      []*ReactionObject
	reactionChan <-chan ReactionInfo
}

func NewGame(rc <-chan ReactionInfo) *Game {
	return &Game{reactionChan: rc}
}

func (g *Game) spawnReaction(reaction ReactionInfo, w, h int) {
	if len(g.objects) >= maxObjects {
		return
	}
	var x, y float64
	edge := rand.Intn(4)
	padding := 36.0
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
		lifetime:     minLifetime + rand.Intn(maxLifetime-minLifetime),
		reactionName: reaction.Name,
	}
	g.objects = append(g.objects, obj)

	go func() {
		cacheMutex.RLock()
		cachedItem, exists := imageCache[reaction.Name]
		cacheMutex.RUnlock()
		if exists {
			if staticImg, ok := cachedItem.(*ebiten.Image); ok {
				obj.image = staticImg
			} else if animatedImg, ok := cachedItem.(*AnimatedGIF); ok {
				obj.animatedGif = animatedImg
			}
			return
		}

		urlToFetch := reaction.URL
		if urlToFetch == "" {
			urlToFetch = emojiToTwemojiURL(reaction.Name)
		}

		decoded, err := fetchAndDecodeImage(urlToFetch)
		if err != nil {
			log.Printf("Failed to fetch image for %s: %v. Using fallback text.", reaction.Name, err)
			obj.fallbackText = strings.Trim(reaction.Name, ":")
			return
		}

		log.Printf("Successfully fetched image for %s", reaction.Name)
		if decoded.Animated != nil {
			cacheMutex.Lock()
			imageCache[reaction.Name] = decoded.Animated
			cacheMutex.Unlock()
			obj.animatedGif = decoded.Animated
		} else if decoded.Static != nil {
			cacheMutex.Lock()
			imageCache[reaction.Name] = decoded.Static
			cacheMutex.Unlock()
			obj.image = decoded.Static
		}
	}()
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

		if o.animatedGif != nil && len(o.animatedGif.Frames) > 0 {
			o.frameCounter++
			delayInTicks := o.animatedGif.FrameDelays[o.currentFrame] * 60 / 100
			if delayInTicks == 0 {
				delayInTicks = 6
			}
			if o.frameCounter >= delayInTicks {
				o.frameCounter = 0
				o.currentFrame = (o.currentFrame + 1) % len(o.animatedGif.Frames)
			}
		}

		padding := 36.0
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
		var imgToDraw *ebiten.Image
		if o.animatedGif != nil && len(o.animatedGif.Frames) > 0 {
			imgToDraw = o.animatedGif.Frames[o.currentFrame]
		} else if o.image != nil {
			imgToDraw = o.image
		}

		if imgToDraw != nil {
			op := &ebiten.DrawImageOptions{}
			w, h := imgToDraw.Bounds().Dx(), imgToDraw.Bounds().Dy()
			op.GeoM.Translate(-float64(w)/2, -float64(h)/2)
			op.GeoM.Translate(o.x, o.y)
			screen.DrawImage(imgToDraw, op)
		} else if o.fallbackText != "" {
			op := &text.DrawOptions{}
			width, height := text.Measure(o.fallbackText, fallbackFont, fallbackFont.Size)
			x := o.x - width/2
			y := o.y - height/2
			op.GeoM.Translate(x, y)
			op.ColorScale.ScaleWithColor(color.White)
			text.Draw(screen, o.fallbackText, fallbackFont, op)
		}
	}
}

func (g *Game) Layout(outsideWidth, outsideHeight int) (int, int) {
	return outsideWidth, outsideHeight
}

// --- Main Function ---
func init() {
	fontReader := bytes.NewReader(goregular.TTF)
	s, err := text.NewGoTextFaceSource(fontReader)
	if err != nil {
		log.Fatal(err)
	}
	fallbackFont = &text.GoTextFace{
		Source: s,
		Size:   20,
	}
}

func main() {
	testMode := flag.Bool("test", false, "Enable test mode with mock data.")
	flag.Parse()

	log.Println("Starting Misskey Reaction Visualizer...")

	reactionChan := make(chan ReactionInfo, 32)

	if *testMode {
		go runTestMode(reactionChan)
	} else {
		cfg, err := loadConfig()
		if err != nil {
			log.Fatalf("Configuration error: %v", err)
		}
		go connectToMisskey(cfg, reactionChan)
	}

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
