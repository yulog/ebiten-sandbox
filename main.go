package main

import (
	"image/color"
	"log"
	"math"
	"math/rand"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

const (
	// maxCircles is the maximum number of circles on screen.
	maxCircles = 50
	// minLifetime and maxLifetime define the random range of a circle's life in ticks (60 ticks = 1 second).
	minLifetime = 300 // 5 seconds
	maxLifetime = 900 // 15 seconds
)

// Circle represents a single circle object.
type Circle struct {
	x, y     float64 // Position
	vx, vy   float64 // Velocity
	radius   float64
	lifetime int
}

// Game implements ebiten.Game interface.
type Game struct {
	circles []*Circle
}

// NewGame initializes the game state.
func NewGame() *Game {
	// Seed the random number generator.
	rand.Seed(time.Now().UnixNano())
	return &Game{
		circles: []*Circle{},
	}
}

// spawnCircle creates a new circle at a random screen edge and gives it a velocity.
func (g *Game) spawnCircle(screenWidth, screenHeight int) {
	if len(g.circles) >= maxCircles {
		return
	}

	radius := 5.0 + rand.Float64()*15.0 // Random radius between 5 and 20
	var x, y float64
	edge := rand.Intn(4)

	// Determine starting position based on a random edge.
	switch edge {
	case 0: // Top edge
		x = rand.Float64() * float64(screenWidth)
		y = -radius
	case 1: // Right edge
		x = float64(screenWidth) + radius
		y = rand.Float64() * float64(screenHeight)
	case 2: // Bottom edge
		x = rand.Float64() * float64(screenWidth)
		y = float64(screenHeight) + radius
	case 3: // Left edge
		x = -radius
		y = rand.Float64() * float64(screenHeight)
	}

	// Give it a random velocity, generally directed towards the screen.
	angle := math.Atan2(float64(screenHeight/2)-y, float64(screenWidth/2)-x)
	angle += (rand.Float64() - 0.5) * (math.Pi / 2) // Add some random deviation
	speed := 0.5 + rand.Float64()*1.5 // Random speed between 0.5 and 2.0

	g.circles = append(g.circles, &Circle{
		x:        x,
		y:        y,
		vx:       math.Cos(angle) * speed,
		vy:       math.Sin(angle) * speed,
		radius:   radius,
		lifetime: minLifetime + rand.Intn(maxLifetime-minLifetime),
	})
}

// Update proceeds the game state.
func (g *Game) Update() error {
	w, h := ebiten.WindowSize()

	// Spawn a new circle periodically.
	if len(g.circles) < maxCircles && rand.Intn(20) == 0 { // Spawn roughly every 1/3 second.
		g.spawnCircle(w, h)
	}

	// Use a new slice to store circles for the next frame.
	// This is an easy way to remove circles from the slice while iterating.
	nextCircles := make([]*Circle, 0, len(g.circles))

	for _, c := range g.circles {
		// Move the circle.
		c.x += c.vx
		c.y += c.vy
		c.lifetime--

		isOutside := c.x+c.radius < 0 || c.x-c.radius > float64(w) || c.y+c.radius < 0 || c.y-c.radius > float64(h)

		// If lifetime is over and the circle is completely outside the screen, remove it.
		if c.lifetime < 0 && isOutside {
			continue // Don't add it to the next frame's slice.
		}

		// If lifetime is active, bounce off the walls.
		if c.lifetime >= 0 {
			if (c.vx < 0 && c.x-c.radius < 0) || (c.vx > 0 && c.x+c.radius > float64(w)) {
				c.vx *= -1
			}
			if (c.vy < 0 && c.y-c.radius < 0) || (c.vy > 0 && c.y+c.radius > float64(h)) {
				c.vy *= -1
			}
		}
		nextCircles = append(nextCircles, c)
	}
	g.circles = nextCircles

	return nil
}

// Draw draws the game screen.
func (g *Game) Draw(screen *ebiten.Image) {
	// The screen is automatically cleared because we have set SetScreenTransparent(true).
	for _, c := range g.circles {
		// Use ebiten/vector package to draw a circle.
		vector.DrawFilledCircle(screen, float32(c.x), float32(c.y), float32(c.radius), color.White, true)
	}
}

// Layout returns the logical screen size.
func (g *Game) Layout(outsideWidth, outsideHeight int) (int, int) {
	return outsideWidth, outsideHeight
}

func main() {
	// Set window properties according to the requirements.
	ebiten.SetWindowDecorated(false)      // No window border, title bar, etc.
	ebiten.SetWindowFloating(true)        // Always on top.
	ebiten.SetScreenTransparent(true)     // Transparent background.
	ebiten.SetWindowMousePassthrough(true) // Clicks go through the window.

	// For fullscreen, we get the monitor size and set the window to that size.
	// Ebitengine's fullscreen mode can sometimes be exclusive, this is a more reliable way
	// to create a borderless window that covers the whole screen.
	screenWidth, screenHeight := ebiten.ScreenSizeInFullscreen()
	ebiten.SetWindowSize(screenWidth, screenHeight)

	ebiten.SetWindowTitle("Floating Circles")

	game := NewGame()

	if err := ebiten.RunGame(game); err != nil {
		log.Fatal(err)
	}
}
