package main

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"math"
	"net/http"
	"strings"
	"sync"

	"github.com/gen2brain/webp"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/kettek/apng"
)

// ImageManager handles caching and decoding of images.
type ImageManager struct {
	cache      map[string]any
	cacheMutex *sync.RWMutex
}

// NewImageManager creates a new manager for image assets.
func NewImageManager() *ImageManager {
	return &ImageManager{
		cache:      make(map[string]any),
		cacheMutex: &sync.RWMutex{},
	}
}

// Get retrieves an image (static or animated) from the cache.
func (im *ImageManager) Get(key string) (any, bool) {
	im.cacheMutex.RLock()
	defer im.cacheMutex.RUnlock()
	item, exists := im.cache[key]
	return item, exists
}

// Set adds an image (static or animated) to the cache.
func (im *ImageManager) Set(key string, value any) {
	im.cacheMutex.Lock()
	defer im.cacheMutex.Unlock()
	im.cache[key] = value
}

// AnimatedImage holds all the pre-rendered frames for an animation.
type AnimatedImage struct {
	Frames      []*ebiten.Image
	FrameDelays []int // Delay in milliseconds
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
		delayInMilliseconds := int(math.Round(delaySeconds * 1000))
		frameDelays = append(frameDelays, delayInMilliseconds)

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

	return &AnimatedImage{Frames: frames, FrameDelays: animation.Delay}
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
	// Convert delay from 1/100s of a second to milliseconds.
	delaysInMs := make([]int, len(g.Delay))
	for i, d := range g.Delay {
		delaysInMs[i] = d * 10
	}
	return &AnimatedImage{Frames: frames, FrameDelays: delaysInMs}
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
