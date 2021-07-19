package route

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/data"
	"github.com/Laplace-Game-Development/Laplace-Entangled-Environment/internal/util"
)

// Typical Verification of users for authentication. Used in most
// other endpoints as SigVerify in RequestBodyFactories
//
// Takes the authID, Signature (hash of token and content), and content
// to see if the user can indeed make the request (they are who they say
// they are).
//
// returns an error if they are not who they say they are.
func SigVerification(authID string, signature string, content *[]byte) error {
	token, err := data.GetToken(authID)
	if err != nil {
		log.Printf("Error in Signature Verification! AuthID:%s\tSignature:%s\nErr: %v\n", authID, signature, err)
	}

	tokenByte := []byte(token.Token)
	counterByte := []byte(fmt.Sprintf("%d", token.Uses))

	if token.Stale.Before(time.Now().UTC()) {
		return errors.New("Token Is Stale!")
	}

	contentLen := len(*content)
	tokenLen := len(tokenByte)
	counterLen := len(counterByte)

	input := make([]byte, contentLen+tokenLen+counterLen)
	err = util.Concat(&input, content, 0)
	if err != nil {
		return err
	}

	err = util.Concat(&input, &tokenByte, contentLen)
	if err != nil {
		return err
	}

	err = util.Concat(&input, &counterByte, contentLen+tokenLen)
	if err != nil {
		return err
	}

	checksumByte := sha256.Sum256(input)
	checksum := string(checksumByte[:])
	if signature == checksum {
		return data.IncrementTokenUses(authID, token.Uses)
	}

	return errors.New(fmt.Sprintf("Signature is Incorrect!: %s vs %s", signature, checksum))
}
