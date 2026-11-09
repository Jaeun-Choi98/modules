package shell

import (
	"log"
	"testing"
)

func TestInvalidFilename(t *testing.T) {
	if err := ValidateFilename(`filename|(rm -rf )`); err != nil {
		log.Printf("%+v", err)
	}
}
