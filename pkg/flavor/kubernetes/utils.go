// Copyright 2024 Hewlett Packard Enterprise Development LP

package kubernetes

import "regexp"

// ValidateStringWithRegex validates a string with a regx
func ValidateStringWithRegex(input string, pattern string) bool {
	// Compile the regular expression
	regex := regexp.MustCompile(pattern)

	// Use the MatchString method to check if the input string matches the pattern
	return regex.MatchString(input)
}
