package util

import (
	"errors"
	"log"
)

// A Universal Type to represent any function that only returns an error.
// This is an old predecessor of ServerTask
type ErrorRunnable func() error

// Clears a given data slice provided to the input using an iterative loop.
func Clear(data *[]byte) {
	for i := 0; i < len(*data); i++ {
		(*data)[i] = 0
	}
}

// Creates a JSON representing an object with an "error" field. The str parameter
// represents the definition or value for said field. The byte slice represents 
// the "string" version of the JSON.
func NewErrorJson(str string) []byte {
	return []byte("{\"error\": \"" + str + "\"}")
}

// Tokenizes a byte slice similar to C styled strtok.
// seperator :: searched for byte slice. Reaching this in the string results in a returned slice
// escape    :: escape byte slice. If escape slice is reached before seperator, the search continues
// str       :: the searched through slice
// start     :: the starting index to search
// Return    :: slice representing the bytes from the start and including the seperator (this may be a bug)
//     it will return a nil slice otherwise. This is paired with the length of the slice (or 0 for nil)
//
// This was useful for the original schema of authentication. I wanted to parse the information from a 
// tilde (~) delimited string. This function may prove useful later.
func StrTokWithEscape(seperator *[]byte, escape *[]byte, str *[]byte, start uint) ([]byte, uint) {
	matchesSepIndex := 0
	sepLength := len(*seperator)
	matchesEscIndex := 0
	escLength := len(*escape)
	end := uint(len(*str))
	var cur byte

	for strIndex := start; strIndex <= end; strIndex += 1 {
		cur = (*str)[strIndex]
		if cur == (*escape)[matchesEscIndex] {
			matchesEscIndex += 1
			if matchesEscIndex >= escLength {
				matchesEscIndex = 0
				matchesSepIndex = 0
				continue
			}
		}

		if cur == (*seperator)[matchesEscIndex] {
			matchesSepIndex += 1
			if matchesSepIndex >= sepLength {
				return (*str)[start : strIndex+1], strIndex + 1
			}
		}
	}

	return nil, 0
}

// Concatenates the input slice to index outputStart of the output slice.
// This means that output[outputStart:outputStart + len(input)] will be 
// overwritten with the value of input. In addition, if output cannot store
// bytes up to that last index, an error will be returned.
func Concat(output *[]byte, input *[]byte, outputStart int) error {
	outputLength := len(*output)
	inputLength := len(*input)

	if outputStart+inputLength > outputLength {
		return errors.New("Out of Index Error!")
	}

	for i := 0; i < inputLength; i++ {
		(*output)[outputStart] = (*input)[i]
		outputStart += 1
	}

	return nil
}

// Faults the application and logs if the function in the parameter returns
// an error.
func Errorless(fn ErrorRunnable) {
	err := fn()

	if err != nil {
		log.Fatalln(err)
	}
}
