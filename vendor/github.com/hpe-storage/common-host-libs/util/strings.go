// (c) Copyright 2019 Hewlett Packard Enterprise Development LP

package util

import (
	"regexp"
	"strings"
)

var (
	matchFirstCap  = regexp.MustCompile("(.)([A-Z][a-z]+)")
	matchAllCap    = regexp.MustCompile("([a-z0-9])([A-Z])")
	matchSnakeCase = regexp.MustCompile("(^[A-Za-z])|_([A-Za-z])")
)

// ToSnakeCase converts the given camelCase string into snake_case
func ToSnakeCase(str string) string {
	snake := matchFirstCap.ReplaceAllString(str, "${1}_${2}")
	snake = matchAllCap.ReplaceAllString(snake, "${1}_${2}")
	return strings.ToLower(snake)
}

// ToCamelCase converts the given snake_case string to camelCase
func ToCamelCase(str string) string {
	camelCase := matchSnakeCase.ReplaceAllStringFunc(str, func(s string) string {
		return strings.ToUpper(strings.Replace(s, "_", "", -1))
	})
	if camelCase == "" {
		return ""
	}
	return strings.ToLower(camelCase[:1]) + camelCase[1:]
}
