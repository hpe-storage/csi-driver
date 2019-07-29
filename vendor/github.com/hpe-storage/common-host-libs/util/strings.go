// (c) Copyright 2017 Hewlett Packard Enterprise Development LP

package util

import (
	"regexp"
	"strings"
)

var (
	matchFirstCap = regexp.MustCompile("(.)([A-Z][a-z]+)")
	matchAllCap   = regexp.MustCompile("([a-z0-9])([A-Z])")
)

// ToSnakeCase converts the given camelCase string into snake_case
func ToSnakeCase(str string) string {
	snake := matchFirstCap.ReplaceAllString(str, "${1}_${2}")
	snake = matchAllCap.ReplaceAllString(snake, "${1}_${2}")
	return strings.ToLower(snake)
}

// IsSensitive checks if the given key exists in the list of bad words (sensitive info)
func IsSensitive(key string) bool {
	// TODO: Add more sensitive words (lower-case) to this list
	badWords := []string{
		"x-auth-token",
		"username",
		"user",
		"password",
		"passwd",
		"secret",
		"token",
	}
	for _, bad := range badWords {
		// Perform case-insensitive and substring match
		if strings.Contains(bad, strings.ToLower(key)) {
			return true
		}
	}
	return false
}
