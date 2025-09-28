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

	"github.com/kettek/apng"

	"github.com/gen2brain/webp"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"golang.org/x/image/font/gofont/goregular"
)

const version = "0.0.3"

var revision = "HEAD"

const (
	maxObjects             = 100
	minLifetime            = 300
	maxLifetime            = 900
	objectHalfSize         = 36.0 // Assumes 72x72 images, used for padding
	minObjectSpeed         = 0.5
	maxObjectSpeed         = 2.0
	objectAngleSpread      = math.Pi / 2
	defaultFrameDelayTicks = 6
)

var (
	// imageCache can store *ebiten.Image for static images or *AnimatedImage for animations.
	imageCache   = make(map[string]any)
	cacheMutex   = &sync.RWMutex{}
	fallbackFont *text.GoTextFace
	// Global config for API calls outside of the main connection loop
	appConfig *Config
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
	appConfig = &cfg // Store loaded config globally
	return &cfg, nil
}

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

// --- Image Handling ---

// AnimatedImage holds all the pre-rendered frames for an animation.
type AnimatedImage struct {
	Frames      []*ebiten.Image
	FrameDelays []int // Delay in 1/100s of a second
}

// DecodedImage holds the result of decoding, which can be static or animated.
type DecodedImage struct {
	Static   *ebiten.Image
	Animated *AnimatedImage
}

// preRenderApngAnimation composites an APNG's frames onto a canvas.
func preRenderApngAnimation(animation *apng.APNG, canvasWidth, canvasHeight int) *AnimatedImage {
	var frames []*ebiten.Image
	var frameDelays []int

	// Prepare canvases for composition.
	canvas := image.NewRGBA(image.Rect(0, 0, canvasWidth, canvasHeight))
	prevCanvas := image.NewRGBA(image.Rect(0, 0, canvasWidth, canvasHeight)) // For DISPOSE_OP_PREVIOUS

	// Loop through frames and composite them.
	for _, frame := range animation.Frames {
		// Skip the default image, it's not part of the animation.
		if frame.IsDefault {
			continue
		}

		// Save canvas state before drawing, in case we need to revert for DISPOSE_OP_PREVIOUS.
		draw.Draw(prevCanvas, prevCanvas.Bounds(), canvas, image.Point{}, draw.Src)

		// Determine the drawing operator (draw.Src or draw.Over).
		var op draw.Op = draw.Over
		if frame.BlendOp == apng.BLEND_OP_SOURCE {
			op = draw.Src
		}

		// Calculate the destination rectangle using the frame's X/Y offsets and dimensions.
		frameWidth := frame.Image.Bounds().Dx()
		frameHeight := frame.Image.Bounds().Dy()
		dstRect := image.Rect(frame.XOffset, frame.YOffset, frame.XOffset+frameWidth, frame.YOffset+frameHeight)

		// Draw the frame image onto the canvas at the correct offset.
		draw.Draw(canvas, dstRect, frame.Image, frame.Image.Bounds().Min, op)

		// Create a true copy of the canvas for this animation frame.
		frameCopy := image.NewRGBA(canvas.Bounds())
		draw.Draw(frameCopy, frameCopy.Bounds(), canvas, image.Point{}, draw.Src)
		frames = append(frames, ebiten.NewImageFromImage(frameCopy))

		// Convert frame delay and append.
		delaySeconds := frame.GetDelay() // Returns delay in seconds as float64
		delayInHundredths := int(math.Round(delaySeconds * 100))
		frameDelays = append(frameDelays, delayInHundredths)

		// Handle disposal method to prepare canvas for the *next* frame.
		switch frame.DisposeOp {
		case apng.DISPOSE_OP_BACKGROUND:
			// Clear the frame's area to transparent.
			draw.Draw(canvas, dstRect, image.Transparent, image.Point{}, draw.Src)
		case apng.DISPOSE_OP_PREVIOUS:
			// Revert the canvas to the state before this frame was drawn.
			draw.Draw(canvas, canvas.Bounds(), prevCanvas, image.Point{}, draw.Src)
		}
	}

	return &AnimatedImage{
		Frames:      frames,
		FrameDelays: frameDelays,
	}
}

// preRenderWebpAnimation composites a WebP animation's frames.
func preRenderWebpAnimation(animation *webp.WEBP) *AnimatedImage {
	var frames []*ebiten.Image
	for _, frame := range animation.Image {
		frames = append(frames, ebiten.NewImageFromImage(frame))
	}

	// Convert delay from milliseconds to 1/100s of a second.
	delaysInHundredths := make([]int, len(animation.Delay))
	for i, d := range animation.Delay {
		delaysInHundredths[i] = int(math.Round(float64(d) / 10.0))
	}

	return &AnimatedImage{Frames: frames, FrameDelays: delaysInHundredths}
}

// preRenderGifAnimation composites a GIF's frames onto a canvas.
func preRenderGifAnimation(g *gif.GIF) *AnimatedImage {
	canvas := image.NewRGBA(image.Rect(0, 0, g.Config.Width, g.Config.Height))
	var frames []*ebiten.Image
	for i, srcImg := range g.Image {
		draw.Draw(canvas, srcImg.Bounds(), srcImg, srcImg.Bounds().Min, draw.Over)
		frameCopy := image.NewRGBA(canvas.Bounds())
		draw.Draw(frameCopy, frameCopy.Bounds(), canvas, image.Point{}, draw.Src)
		frames = append(frames, ebiten.NewImageFromImage(frameCopy))
		if g.Disposal[i] == gif.DisposalBackground {
			draw.Draw(canvas, srcImg.Bounds(), image.Transparent, image.Point{}, draw.Src)
		}
	}
	return &AnimatedImage{Frames: frames, FrameDelays: g.Delay}
}

// fetchAndDecodeImage downloads and decodes an image. It distinguishes between static
// and animated images to process them more efficiently.
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
		// First, decode the full GIF to check the frame count.
		g, err := gif.DecodeAll(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}

		// If it's a single-frame GIF, treat it as a static image.
		if len(g.Image) <= 1 {
			img, _, err := image.Decode(bytes.NewReader(data))
			if err != nil {
				return nil, err
			}
			return &DecodedImage{Static: ebiten.NewImageFromImage(img)}, nil
		}

		// Otherwise, process it as an animation by pre-rendering it.
		anim := preRenderGifAnimation(g)
		return &DecodedImage{Animated: anim}, nil

	} else if strings.Contains(contentType, "png") {
		// To properly handle APNG, we need to decode it fully first.
		// We need two readers because we first read config, then the whole animation.
		reader1 := bytes.NewReader(data)
		reader2 := bytes.NewReader(data)

		config, err := apng.DecodeConfig(reader1)
		if err != nil {
			// If it's not even a valid PNG, we can't proceed.
			return nil, err
		}

		animation, err := apng.DecodeAll(reader2)
		if err != nil {
			// If DecodeAll fails, it might be a simple static PNG that DecodeAll doesn't handle.
			// Fallback to the standard image/png decoder.
			img, _, staticErr := image.Decode(bytes.NewReader(data))
			if staticErr != nil {
				return nil, err // Return original apng error
			}
			return &DecodedImage{Static: ebiten.NewImageFromImage(img)}, nil
		}

		// Check number of actual animation frames (non-default).
		numFrames := 0
		for _, f := range animation.Frames {
			if !f.IsDefault {
				numFrames++
			}
		}

		if numFrames <= 1 {
			// If there's only one frame (or none), treat it as a static image.
			img, _, err := image.Decode(bytes.NewReader(data))
			if err != nil {
				return nil, err
			}
			return &DecodedImage{Static: ebiten.NewImageFromImage(img)}, nil
		}

		// It's an animation, so pre-render the frames.
		anim := preRenderApngAnimation(&animation, config.Width, config.Height)
		return &DecodedImage{Animated: anim}, nil

	} else if strings.Contains(contentType, "webp") {
		animation, err := webp.DecodeAll(bytes.NewReader(data))
		if err != nil {
			// Fallback to static image decoding if animation fails
			img, staticErr := webp.Decode(bytes.NewReader(data))
			if staticErr != nil {
				return nil, err // Return original animation error
			}
			return &DecodedImage{Static: ebiten.NewImageFromImage(img)}, nil
		}

		if len(animation.Image) <= 1 {
			img, err := webp.Decode(bytes.NewReader(data))
			if err != nil {
				return nil, err
			}
			return &DecodedImage{Static: ebiten.NewImageFromImage(img)}, nil
		}

		anim := preRenderWebpAnimation(animation)
		return &DecodedImage{Animated: anim}, nil
	} else {
		// For all other image types (jpeg, etc.)
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
	x, y, vx, vy  float64
	lifetime      int
	reactionName  string
	image         *ebiten.Image
	animatedImage *AnimatedImage
	currentFrame  int
	frameCounter  int
	fallbackText  string
	scale         float64
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
	scale := 0.5 + rand.Float64() // Random scale from 0.5 to 1.5
	padding := objectHalfSize * scale
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
	angle := math.Atan2(float64(h/2)-y, float64(w/2)-x) + (rand.Float64()-0.5)*objectAngleSpread
	speed := minObjectSpeed + rand.Float64()*(maxObjectSpeed-minObjectSpeed)
	obj := &ReactionObject{
		x: x, y: y, vx: math.Cos(angle) * speed, vy: math.Sin(angle) * speed,
		lifetime:     minLifetime + rand.Intn(maxLifetime-minLifetime),
		reactionName: reaction.Name,
		scale:        scale,
	}
	g.objects = append(g.objects, obj)

	go loadReactionImage(obj, reaction)
}

// loadReactionImage handles the asynchronous fetching, decoding, and caching of a reaction image.
func loadReactionImage(obj *ReactionObject, reaction ReactionInfo) {
	// Check cache first
	cacheMutex.RLock()
	cachedItem, exists := imageCache[reaction.Name]
	cacheMutex.RUnlock()
	if exists {
		if staticImg, ok := cachedItem.(*ebiten.Image); ok {
			obj.image = staticImg
		} else if anim, ok := cachedItem.(*AnimatedImage); ok {
			obj.animatedImage = anim
		}
		return
	}

	// Determine URL to fetch
	urlToFetch := reaction.URL
	if urlToFetch == "" {
		if len(reaction.Name) > 2 && reaction.Name[0] == ':' && reaction.Name[len(reaction.Name)-1] == ':' {
			var err error
			emojiName := strings.Trim(reaction.Name, ":")
			urlToFetch, err = queryEmojiAPI(emojiName)
			if err != nil {
				log.Printf("Failed to query API for emoji '%s': %v", emojiName, err)
				obj.fallbackText = emojiName
				return
			}
		} else {
			urlToFetch = emojiToTwemojiURL(reaction.Name)
		}
	}

	// Fetch and decode the image
	decoded, err := fetchAndDecodeImage(urlToFetch)
	if err != nil {
		log.Printf("Failed to fetch image for %s: %v. Using fallback text.", reaction.Name, err)
		obj.fallbackText = strings.Trim(reaction.Name, ":")
		return
	}

	// Update object and cache
	log.Printf("Successfully fetched image for %s", reaction.Name)
	if decoded.Animated != nil {
		cacheMutex.Lock()
		imageCache[reaction.Name] = decoded.Animated
		cacheMutex.Unlock()
		obj.animatedImage = decoded.Animated
	} else if decoded.Static != nil {
		cacheMutex.Lock()
		imageCache[reaction.Name] = decoded.Static
		cacheMutex.Unlock()
		obj.image = decoded.Static
	}
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

		if o.animatedImage != nil && len(o.animatedImage.Frames) > 0 {
			o.frameCounter++
			delayInTicks := o.animatedImage.FrameDelays[o.currentFrame] * 60 / 100
			if delayInTicks == 0 {
				delayInTicks = defaultFrameDelayTicks
			}
			if o.frameCounter >= delayInTicks {
				o.frameCounter = 0
				o.currentFrame = (o.currentFrame + 1) % len(o.animatedImage.Frames)
			}
		}

		padding := objectHalfSize * o.scale
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
		if o.animatedImage != nil && len(o.animatedImage.Frames) > 0 {
			imgToDraw = o.animatedImage.Frames[o.currentFrame]
		} else if o.image != nil {
			imgToDraw = o.image
		}

		if imgToDraw != nil {
			op := &ebiten.DrawImageOptions{}
			w, h := imgToDraw.Bounds().Dx(), imgToDraw.Bounds().Dy()
			op.GeoM.Translate(-float64(w)/2, -float64(h)/2)
			op.GeoM.Scale(o.scale, o.scale)
			scale := ebiten.Monitor().DeviceScaleFactor()
			op.GeoM.Scale(scale, scale)
			op.GeoM.Translate(o.x, o.y)
			op.Filter = ebiten.FilterLinear
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
	s := ebiten.Monitor().DeviceScaleFactor()
	return int(float64(outsideWidth) * s), int(float64(outsideHeight) * s)
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

	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("Configuration error: %v", err)
	}
	if *testMode {
		go runTestMode(reactionChan)
	} else {
		go connectToMisskey(cfg, reactionChan)
	}

	ebiten.SetWindowDecorated(false)
	ebiten.SetWindowFloating(true)
	ebiten.SetWindowMousePassthrough(true)
	ebiten.SetWindowTitle("Misskey Reactions")
	screenWidth, screenHeight := ebiten.Monitor().Size()
	s := ebiten.Monitor().DeviceScaleFactor()
	ebiten.SetWindowSize(int(float64(screenWidth)*s), int(float64(screenHeight)*s)-1)
	game := NewGame(reactionChan)
	opts := ebiten.RunGameOptions{ScreenTransparent: true}
	if err := ebiten.RunGameWithOptions(game, &opts); err != nil {
		log.Fatal(err)
	}
}
