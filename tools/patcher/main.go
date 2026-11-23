// Problem:
// Traditional line-based unified diff patches break whenever upstream inserts
// adjacent struct fields, edits neighboring lines, reorders functions, or alters
// comments across releases.
//
// Solution:
// Combine package-level Go overlay files (for all new methods, helpers, and unit
// tests) with a pure-stdlib Go AST semantic patcher (inspired by Coccinelle)
// that locates upstream structs, interfaces, functions, and composite literals
// by AST structure rather than line numbers, failing loudly if an upstream
// breaking refactor removes a required anchor.

package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	singTunDir := flag.String("sing-tun", "", "path to upstream sing-tun checkout")
	mihomoDir := flag.String("mihomo", "", "path to upstream mihomo checkout")
	overlayDir := flag.String("overlay", "", "path to overlay directory containing sing-tun and mihomo subtrees")
	flag.Parse()

	if *singTunDir == "" || *mihomoDir == "" || *overlayDir == "" {
		fmt.Fprintln(os.Stderr, "usage: patcher -sing-tun <dir> -mihomo <dir> -overlay <dir>")
		os.Exit(2)
	}

	singTunOverlay := filepath.Join(*overlayDir, "sing-tun")
	mihomoOverlay := filepath.Join(*overlayDir, "mihomo")

	singCount, err := copyOverlayDir(singTunOverlay, *singTunDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "copy sing-tun overlay failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Copied %d sing-tun overlay files\n", singCount)

	if err := patchSingTun(*singTunDir); err != nil {
		fmt.Fprintf(os.Stderr, "semantic patch sing-tun failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Applied sing-tun AST semantic rules cleanly")

	mihomoCount, err := copyOverlayDir(mihomoOverlay, *mihomoDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "copy mihomo overlay failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Copied %d mihomo overlay files\n", mihomoCount)

	if err := patchMihomoTun(*mihomoDir); err != nil {
		fmt.Fprintf(os.Stderr, "semantic patch mihomo TUN failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Applied mihomo TUN AST semantic rules cleanly")

	if err := patchMihomoOutbound(*mihomoDir); err != nil {
		fmt.Fprintf(os.Stderr, "semantic patch mihomo outbound failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("Applied mihomo outbound AST semantic rules cleanly")
}
