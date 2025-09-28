package main

import (
	"image/color"
	"log"
	"math"
	"math/rand"
	"strings"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
)

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
	fallbackFont *text.GoTextFace
)

// ReactionObject represents a single floating reaction on the screen.
type ReactionObject struct {
	x, y, vx, vy         float64
	lifetime             int
	reactionName         string
	image                *ebiten.Image
	animatedImage        *AnimatedImage
	currentFrame         int
	frameTimeAccumulator float64
	fallbackText         string
	scale                float64
}

// Game holds the main game state and dependencies.
type Game struct {
	objects       []*ReactionObject
	reactionChan  <-chan ReactionInfo
	misskeyClient *MisskeyClient
	imageManager  *ImageManager
}

// NewGame creates a new game instance with its dependencies.
func NewGame(rc <-chan ReactionInfo, mc *MisskeyClient, im *ImageManager) *Game {
	return &Game{
		reactionChan:  rc,
		misskeyClient: mc,
		imageManager:  im,
	}
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

	go g.loadReactionImage(obj, reaction)
}

// loadReactionImage handles the asynchronous fetching, decoding, and caching of a reaction image.
func (g *Game) loadReactionImage(obj *ReactionObject, reaction ReactionInfo) {
	// Check cache first
	cachedItem, exists := g.imageManager.Get(reaction.Name)
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
			urlToFetch, err = g.misskeyClient.QueryEmojiAPI(emojiName) // Use the client
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
		g.imageManager.Set(reaction.Name, decoded.Animated) // Use the manager
		obj.animatedImage = decoded.Animated
	} else if decoded.Static != nil {
		g.imageManager.Set(reaction.Name, decoded.Static) // Use the manager
		obj.image = decoded.Static
	}
}

// Update proceeds the game state.
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
			o.frameTimeAccumulator += 1000.0 / 60.0 // Ebiten runs at 60 TPS

			delayMs := float64(o.animatedImage.FrameDelays[o.currentFrame])
			if delayMs == 0 {
				// Use a default delay if the animation doesn't specify one.
				// defaultFrameDelayTicks is 6, which is 100ms.
				delayMs = 100.0
			}

			if o.frameTimeAccumulator >= delayMs {
				o.frameTimeAccumulator -= delayMs
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

// Draw draws the game screen.
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

// Layout takes the outside size (e.g., the window size) and returns the (logical) screen size.
func (g *Game) Layout(outsideWidth, outsideHeight int) (int, int) {
	s := ebiten.Monitor().DeviceScaleFactor()
	return int(float64(outsideWidth) * s), int(float64(outsideHeight) * s)
}
