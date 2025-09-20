package main

import (
	"image/color"
	"log"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
)

const (
	screenWidth  = 640
	screenHeight = 480
	blurSize     = 30.0 // ぼかしの強度をここで調整
)

var (
	gopherImage   *ebiten.Image
	ambientShader *ebiten.Shader
)

type Game struct{}

func (g *Game) Update() error {
	return nil
}

func (g *Game) Draw(screen *ebiten.Image) {
	// 画面全体を黒でクリア
	screen.Fill(color.Black)

	// 描画ターゲットとなる中間画像を作成
	tmpImg := ebiten.NewImage(screenWidth, screenHeight)

	// gopher.pngを中間画像の中央に描画
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Translate(float64(screenWidth)/2-float64(gopherImage.Bounds().Dx())/2, float64(screenHeight)/2-float64(gopherImage.Bounds().Dy())/2)
	tmpImg.DrawImage(gopherImage, op)

	// Kageシェーダーを適用するための描画オプション
	shaderOp := &ebiten.DrawRectShaderOptions{}
	shaderOp.Images[0] = tmpImg // 最初の入力画像として中間画像を渡す

	// Kageシェーダーにユニフォーム変数を渡す
	shaderOp.Uniforms = map[string]any{
		"BlurSize": blurSize,
	}

	// Kageシェーダーを使って、tmpImgをscreenに描画
	screen.DrawRectShader(screenWidth, screenHeight, ambientShader, shaderOp)

	// オリジナルの画像をぼかした背景の上に描画
	// これにより、アンビエント効果が完成する
	screen.DrawImage(gopherImage, op)
}

func (g *Game) Layout(outsideWidth, outsideHeight int) (int, int) {
	return screenWidth, screenHeight
}

func main() {
	var err error
	gopherImage, _, err = ebitenutil.NewImageFromFile("gopher.png")
	if err != nil {
		log.Fatal(err)
	}

	ambientShader = loadShader("ambient.kage")

	ebiten.SetWindowSize(screenWidth, screenHeight)
	ebiten.SetWindowTitle("Ambient Mode Example")
	if err := ebiten.RunGame(&Game{}); err != nil {
		log.Fatal(err)
	}
}
