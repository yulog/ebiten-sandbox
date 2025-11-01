package main

import (
	"bytes"
	"flag"
	"log"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"golang.org/x/image/font/gofont/goregular"
)

const version = "0.0.3"

var revision = "HEAD"

// runTestMode sends mock reaction data to the channel for testing purposes.
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
		{Name: ":bug:", URL: "https://media.misskeyusercontent.jp/misskey/7ac83d54-033b-4eee-8703-9cba7052992c.gif"},
		{Name: ":syuilo_yay:", URL: "https://media.misskeyusercontent.jp/io/939d3f91-86dc-491f-a6f2-dcfee43974b4.apng"}, // invalid format: chunk out of order
		{Name: ":ai_akan:", URL: "https://media.misskeyusercontent.jp/misskey/ff4ff841-1b94-412a-9708-76781ac5a29f.png"},
		{Name: ":murakamisan_spin:", URL: "https://media.misskeyusercontent.jp/io/45a238ca-6319-4781-8bbe-b6b4c6fcca73.gif"},
		{Name: ":blobdance2:", URL: "https://media.misskeyusercontent.jp/io/51f11775-f498-4a61-9220-08427735068f.gif"},
		{Name: ":resonyance:", URL: "https://media.misskeyusercontent.jp/emoji/resonyance.webp"},
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
	}

	// Load config only if not in test mode
	var cfg *Config
	var err error
	if !*testMode {
		cfg, err = loadConfig()
		if err != nil {
			log.Fatalf("Configuration error: %v", err)
		}
	}

	// Initialize dependencies
	misskeyClient := NewMisskeyClient(cfg) // cfg can be nil in test mode, which is fine
	imageManager := NewImageManager(misskeyClient)

	if !*testMode {
		go misskeyClient.Connect(reactionChan)
	}

	ebiten.SetWindowDecorated(false)
	ebiten.SetWindowFloating(true)
	ebiten.SetWindowMousePassthrough(true)
	ebiten.SetWindowTitle("Misskey Reactions")
	screenWidth, screenHeight := ebiten.Monitor().Size()
	s := ebiten.Monitor().DeviceScaleFactor()
	ebiten.SetWindowSize(int(float64(screenWidth)*s), int(float64(screenHeight)*s)-1)

	// Inject dependencies into the game
	game := NewGame(reactionChan, imageManager)

	opts := ebiten.RunGameOptions{ScreenTransparent: true}
	if err := ebiten.RunGameWithOptions(game, &opts); err != nil {
		log.Fatal(err)
	}
}
