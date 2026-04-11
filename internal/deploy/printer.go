package deploy

import "fmt"

// PrintStep prints a numbered step message.
func PrintStep(step, total int, msg string) {
	fmt.Printf("\033[1m==> [%d/%d] %s\033[0m\n", step, total, msg)
}

// PrintSuccess prints a green success message.
func PrintSuccess(msg string) {
	fmt.Printf("\033[32m✓  %s\033[0m\n", msg)
}

// PrintWarning prints a yellow warning message.
func PrintWarning(msg string) {
	fmt.Printf("\033[33m⚠  %s\033[0m\n", msg)
}

// PrintError prints a red error message.
func PrintError(msg string) {
	fmt.Printf("\033[31m✗  %s\033[0m\n", msg)
}

// PrintInfo prints an info message.
func PrintInfo(msg string) {
	fmt.Printf("   %s\n", msg)
}
