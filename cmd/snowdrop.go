package main

import (
	"log"
	"net/http"

	"github.com/zemnmez/snowdrop"
)

func main() {
	log.Fatal(http.ListenAndServe(":8080", snowdrop.Server{}))
}
