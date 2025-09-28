package main

import (
	"bytes"
	"flag"
	"log"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"golang.org/x/image/font/gofont/goregular"
)

const version = "0.0.3"

var revision = "HEAD"

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