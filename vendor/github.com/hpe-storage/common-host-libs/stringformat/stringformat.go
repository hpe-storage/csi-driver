// (c) Copyright 2018 Hewlett Packard Enterprise Development LP

package stringformat

import (
	"fmt"
	"strconv"

	log "github.com/hpe-storage/common-host-libs/logger"
)

// AlignmentType for formatted output
type AlignmentType int

const (
	// LeftAlign to left-justify text
	LeftAlign AlignmentType = 1 + iota
	// RightAlign to right-justify text
	RightAlign
	// CenterAlign to align the text to the center
	CenterAlign
)

// CheckIfKeysNonEmpty to check if both strings are non-empty
func CheckIfKeysNonEmpty(s1 string, s2 string) bool {
	return (s1 != "" && s2 != "")
}

// CheckValidRange to check if an integer value is within a specified range.
func CheckValidRange(val int64, min int64, max int64) bool {
	return (val >= min && val <= max)
}

// StringsLookup returns true if all the items in the inputList exists in the refList, else returns false
func StringsLookup(refList []string, inputList []string) bool {
	for _, item := range inputList {
		if !StringLookup(refList, item) {
			return false
		}
	}
	return true
}

// StringLookup to check if a value exists in the given input
func StringLookup(input interface{}, value string) bool {
	switch input.(type) {
	case []string:
		valueList := input.([]string)
		for _, val := range valueList {
			if value == val {
				return true
			}
		}
		return false
	case string:
		val := input.(string)
		return value == val
	default:
		log.Traceln("Unsupported input type:", input)
		return false
	}
}

// FixedLengthString to convert the given string into fixed length by truncating or padding.
func FixedLengthString(length int, value interface{}, alignment AlignmentType) string {
	var str string
	// TODO: Add support for other datatypes
	switch valType := value.(type) {
	case uint64:
		str = fmt.Sprintf("%v", value.(uint64))
	case string:
		str = value.(string)
	case bool:
		str = strconv.FormatBool(value.(bool))
	default:
		log.Errorln("Received unexpected value type: ", valType)
		return ""
	}
	switch alignment {
	case LeftAlign:
		return fmt.Sprintf(fmt.Sprintf("%%-%d.%dv", length, length), str)
	case RightAlign:
		return fmt.Sprintf(fmt.Sprintf("%%%d.%dv", length, length), str)
	case CenterAlign:
		return fmt.Sprintf("%[1]*s", -length, fmt.Sprintf("%[1]*s", (length+len(str))/2, str))
	default:
		log.Errorf("Invalid alignment value '%v', specified\n", alignment)
	}
	return ""
}
