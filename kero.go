package kero

import (
	"github.com/galaco/kero/framework/graphics"
	"github.com/galaco/kero/framework/window"
	"github.com/galaco/kero/game"
	"github.com/galaco/kero/gui"
	"github.com/galaco/kero/middleware"
	"github.com/galaco/kero/renderer"
	"github.com/galaco/kero/scene"
	"time"
)

// Kero provides a game loop
type Kero struct {
	isRunning bool

	scene *scene.Scene

	input *middleware.Input
	renderer *renderer.Renderer
	ui *gui.Gui
}

// RegisterGameDefinitions sets up provided game-specific configuration
func (kero *Kero) RegisterGameDefinitions(def game.Definition) {
	def.RegisterEntityClasses()
}

// Start runs the game loop
func (kero *Kero) Start() {
	kero.input = middleware.InitializeInput()
	kero.renderer = renderer.NewRenderer()
	kero.ui = gui.NewGui()
	kero.scene = scene.NewScene()

	kero.isRunning = true

	kero.scene.Initialize()

	kero.renderer.Initialize()
	kero.ui.Initialize()

	dt := 0.0
	startingTime := time.Now().UTC()
	for kero.isRunning && (window.CurrentWindow()!= nil && !window.CurrentWindow().Handle().Handle().ShouldClose()) {
		kero.input.Poll()

		kero.renderer.Render()
		kero.ui.Render()

		kero.scene.Update(dt)

		window.CurrentWindow().SwapBuffers()
		graphics.ClearColor(0, 0, 0, 1)
		graphics.ClearAll()

		dt = float64(time.Now().UTC().Sub(startingTime).Nanoseconds()/1000000) / 1000
		startingTime = time.Now().UTC()
	}

	kero.exit()
}

func (kero *Kero) exit() {

}

// NewKero returns a new Kero instance
func NewKero() *Kero {
	return &Kero{
		isRunning: false,
	}
}
