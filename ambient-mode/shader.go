package main

import (
	"log"
	"os"

	"github.com/hajimehoshi/ebiten/v2"
)

func loadShader(path string) *ebiten.Shader {
	src, err := os.ReadFile(path)
	if err != nil {
		log.Fatal(err)
	}
	s, err := ebiten.NewShader(src)
	if err != nil {
		log.Fatal(err)
	}
	return s
}
