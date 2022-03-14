package tori

import (
	"log"
	"os"
)

func cwd() string {
	dir, err := os.Getwd()
	if err != nil {
		log.Panic(err)
	}
	return dir
}
