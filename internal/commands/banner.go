package commands

import "fmt"

// goCyan is the ANSI escape sequence for Go's branding color (#00ADD8).
const goCyan = "\033[38;2;0;173;216m"
const reset = "\033[0m"

const banner = `
                __          _                                                                                                      
               / _|        | |                                                                                                     
    __ _  ___ | |_ __ _ ___| |_ __ _
   / _  |/ _ \|  _/ _  / __| __/ _  |                                                                                              
  | (_| | (_) | || (_| \__ \ || (_| |                                                                                              
   \__, |\___/|_| \__,_|___/\__\__,_|
    __/ |                                                                                                                          
   |___/`

func printBanner() {
	fmt.Println(goCyan + banner + reset)
	fmt.Println()
}
