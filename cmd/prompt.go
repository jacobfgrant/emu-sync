package cmd

import (
	"bufio"
	"fmt"
	"strings"
)

// prompt prints a message and reads a line of input.
func prompt(reader *bufio.Reader, message string) string {
	fmt.Print(message)
	text, _ := reader.ReadString('\n')
	return strings.TrimSpace(text)
}

// promptRequired prints a message and reads a line of input, re-prompting
// if the user presses Enter without typing anything.
func promptRequired(reader *bufio.Reader, message string) string {
	for {
		s := prompt(reader, message)
		if s != "" {
			return s
		}
		fmt.Println("  This field is required.")
	}
}

// promptWithDefault prints a label with a default value in brackets and
// reads a line of input. Returns the default if the user presses Enter.
func promptWithDefault(reader *bufio.Reader, label, defaultVal string) string {
	if defaultVal == "" {
		fmt.Printf("%s: ", label)
	} else {
		fmt.Printf("%s [%s]: ", label, defaultVal)
	}
	text, _ := reader.ReadString('\n')
	text = strings.TrimSpace(text)
	if text == "" {
		return defaultVal
	}
	return text
}
