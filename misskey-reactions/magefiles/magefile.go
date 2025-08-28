package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"

	"github.com/magefile/mage/mg" // mg contains helpful utility functions, like Deps
	"github.com/magefile/mage/sh"
)

// Default target to run when none is specified
// If not set, running mage will list available targets
// var Default = Build

var (
	BIN                 string = "mifloat"
	VERSION             string = getVersion()
	CURRENT_REVISION, _        = sh.Output("git", "rev-parse", "--short", "HEAD")
	BUILD_LDFLAGS       string = "-s -w -X main.revision=" + CURRENT_REVISION
	BUILD_TARGET        string = "."
)

// func init() {
// 	VERSION = getVersion()
// 	CURRENT_REVISION, _ = sh.Output("git", "rev-parse", "--short", "HEAD")
// }

func getVersion() string {
	_, err := exec.LookPath("gobump")
	if err != nil {
		fmt.Println("installing gobump")
		sh.Run("go", "install", "github.com/x-motemen/gobump/cmd/gobump@latest")
	}
	v, _ := sh.Output("gobump", "show", "-r", BUILD_TARGET)
	return v
}

// A build step that requires additional params, or platform specific steps for example
func Build() error {
	// mg.Deps(InstallDeps)
	fmt.Println("Building...")
	bin := BIN
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	cmd := exec.Command("go", "build", "-trimpath", "-ldflags="+BUILD_LDFLAGS, "-o", bin, BUILD_TARGET)
	return cmd.Run()
}

// A custom install step if you need your bin someplace other than go/bin
func Install() error {
	mg.Deps(Build)
	fmt.Println("Installing...")
	cmd := exec.Command("go", "install", "-ldflags="+BUILD_LDFLAGS, BUILD_TARGET)
	return cmd.Run()
}

// Manage your deps, or running package managers.
// func InstallDeps() error {
// 	fmt.Println("Installing Deps...")
// 	cmd := exec.Command("go", "get", "github.com/stretchr/piglatin")
// 	return cmd.Run()
// }

// Clean up after yourself
func Clean() {
	fmt.Println("Cleaning...")
	os.RemoveAll("goxz")
	bin := BIN
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	os.RemoveAll(bin)
}

func ShowVersion() {
	fmt.Println(getVersion())
}

func Credits() {
	_, err := exec.LookPath("gocredits")
	if err != nil {
		fmt.Println("installing gocredits")
		sh.Run("go", "install", "github.com/Songmu/gocredits")
	}
	s, _ := sh.Output("gocredits", ".")
	f, _ := os.Create("CREDITS")
	f.WriteString(s)
	defer f.Close()
}

func Cross(goos, arch string) {
	_, err := exec.LookPath("goxz")
	if err != nil {
		fmt.Println("installing goxz")
		sh.Run("go", "install", "github.com/Songmu/goxz/cmd/goxz@latest")
	}
	if runtime.GOOS == "windows" {
		BUILD_LDFLAGS += " -H=windowsgui"
	}
	// https://github.com/syncthing/syncthing/blob/7189a3ebffb7b7bd59bce510753bc6d97988eacd/.github/workflows/build-syncthing.yaml
	if goos == "linux" && arch == "arm64" {
		os.Setenv("CC", "zig cc -target aarch64-linux-musl")
		os.Setenv("CGO_LDFLAGS", "-lglfw")
		os.Setenv("EXTRA_LDFLAGS", "-linkmode=external -extldflags=-static")
	}
	sh.Run("goxz", "-n", BIN, "-o", BIN, "-os", goos, "-arch", arch, "-pv=v"+VERSION, "-build-ldflags", BUILD_LDFLAGS, BUILD_TARGET)
}

func Bump() {
	_, err := exec.LookPath("gobump")
	if err != nil {
		fmt.Println("installing gobump")
		sh.Run("go", "install", "github.com/x-motemen/gobump/cmd/gobump@latest")
	}
	sh.Run("gobump", "up", "-w", BUILD_TARGET)
}

func Upload() {
	_, err := exec.LookPath("ghr")
	if err != nil {
		fmt.Println("installing ghr")
		sh.Run("go", "install", "github.com/tcnksm/ghr@latest")
	}
	dir, _ := os.Getwd()
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		fmt.Println(entry.Name())
	}
	sh.Run("ghr", "-draft", "v"+VERSION, "goxz")
}
