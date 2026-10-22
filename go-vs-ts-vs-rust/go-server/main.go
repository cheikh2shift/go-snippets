package main

import (
	"encoding/json"
	"log"
	"net/http"
)

type Msg struct {
	Message string `json:"message"`
	Status  string `json:"status"`
}

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(Msg{Message: "hello", Status: "ok"})
	})
	log.Println("go :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}