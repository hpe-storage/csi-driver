// Copyright 2024 Hewlett Packard Enterprise Development LP

package kubernetes

import "regexp"

func validateStringWithRegex(input string, pattern string) bool {
	// Compile the regular expression
	regex := regexp.MustCompile(pattern)

	// Use the MatchString method to check if the input string matches the pattern
	return regex.MatchString(input)
}
