// (c) Copyright 2018 Hewlett Packard Enterprise Development LP

package jsonutil

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"

	"github.com/mitchellh/mapstructure"
	log "github.com/hpe-storage/common-host-libs/logger"
)

// GetPrettyJSON to encode data object into bytes format.
func GetPrettyJSON(data interface{}) (string, error) {
	buffer := new(bytes.Buffer)
	encoder := json.NewEncoder(buffer)
	encoder.SetIndent("", "\t") // Delimiters for JSON output

	err := encoder.Encode(data)
	if err != nil {
		fmt.Println("Error in encoding the data into bytes format.")
		return "", err
	}
	return buffer.String(), nil
}

// PrintPrettyJSONToFile to write JSON data into file
func PrintPrettyJSONToFile(data interface{}, file string) error {
	outStr, _ := GetPrettyJSON(data)
	var err error
	if file != "" { // Write to file
		if err = ioutil.WriteFile(file, []byte(outStr), 0644); err != nil {
			return err
		}
	} else {
		err = fmt.Errorf("Invalid file name '%s'", file)
	}
	return err
}

// PrintPrettyJSONToConsole to dump JSON data into console
func PrintPrettyJSONToConsole(data interface{}) {
	outStr, _ := GetPrettyJSON(data)
	fmt.Println(outStr) // std output
}

// PrintPrettyJSONToLog to log JSON data
func PrintPrettyJSONToLog(data interface{}) {
	outStr, _ := GetPrettyJSON(data)
	log.Traceln(outStr)
}

// Decode JSON data to map/struct data types using mapstructure pkg.
func Decode(inData interface{}, outData interface{}) error {
	log.Tracef(">>>>> Decode, data: %v, outData Type: %T", inData, outData)
	defer log.Trace("<<<<< Decode")

	decConfig := mapstructure.DecoderConfig{
		TagName: "json",
		Result:  outData,
	}
	dec, err := mapstructure.NewDecoder(&decConfig)
	if err != nil {
		log.Error("Failed to create new decoder, err: ", err.Error())
		return err
	}
	err = dec.Decode(inData)
	if err != nil {
		log.Error("Failed to decode, err: ", err.Error())
		return err
	}

	return nil
}
