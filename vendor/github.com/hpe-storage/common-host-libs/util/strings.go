// (c) Copyright 2019 Hewlett Packard Enterprise Development LP

package util

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
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

// ConvertArrayOfIntToString converts a given list of integer to comma separated string value
func ConvertArrayOfIntToString(lun_ids []int32) string {
	var buffer bytes.Buffer

	for i := 0; i < len(lun_ids); i++ {
		if i == (len(lun_ids) - 1) {
			buffer.WriteString(fmt.Sprintf("%d", lun_ids[i]))
		} else {
			buffer.WriteString(fmt.Sprintf("%d,", lun_ids[i]))
		}
	}
	return buffer.String()
}

func GetMD5HashOfTwoStrings(string1, string2 string) string {
	h := md5.New()
	io.WriteString(h, string1+string2)
	return hex.EncodeToString(h.Sum(nil))
}
