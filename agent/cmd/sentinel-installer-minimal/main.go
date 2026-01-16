package main

import (
	"fmt"
	"os"
	"bufio"
)

func main() {
	fmt.Println("Sentinel Installer - Minimal Test")
	fmt.Println("If you see this, the binary loaded correctly!")
	fmt.Println()
	fmt.Println("Press ENTER to exit...")
	bufio.NewReader(os.Stdin).ReadBytes('\n')
}
