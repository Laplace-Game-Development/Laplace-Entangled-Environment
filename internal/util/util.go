package util

import (
	"errors"
	"log"
)

type ErrorRunnable func() error

func Clear(data *[]byte) {
	for i := 0; i < len(*data); i++ {
		(*data)[i] = 0
	}
}

func NewErrorJson(str string) []byte {
	return []byte("{\"error\": \"" + str + "\"}")
}

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

		if cur == (*seperator)[sepLength] {
			matchesSepIndex += 1
			if matchesSepIndex >= sepLength {
				return (*str)[start : strIndex+1], strIndex + 1
			}
		}
	}

	return nil, 0
}

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

func Errorless(fn ErrorRunnable) {
	err := fn()

	if err != nil {
		log.Fatalln(err)
	}
}
