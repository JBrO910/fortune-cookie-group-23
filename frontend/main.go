package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"math/rand"
	"net/http"
	"time"
)

var BACKEND_DNS = getEnv("BACKEND_DNS", "localhost")
var BACKEND_PORT = getEnv("BACKEND_PORT", "9000")
var templatePath = "./templates/fortunes.html"
var staticPath = "./static"
var httpListenAndServe = http.ListenAndServe

type fortune struct {
	ID      string `json:"id" redis:"id"`
	Message string `json:"message" redis:"message"`
}

type newFortune struct {
	Message string `json:"message"`
}

// use a custom client, because we don't do blocking operations wihout timeouts
var myClient = &http.Client{Timeout: 10 * time.Second}

func HealthzHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "healthy")
}

func registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/healthz", HealthzHandler)

	mux.HandleFunc("/api/random", func(w http.ResponseWriter, r *http.Request) {
		resp, err := myClient.Get(fmt.Sprintf("http://%s:%s/fortunes/random", BACKEND_DNS, BACKEND_PORT))
		if err != nil {
			log.Println("Error communicating with backend:", err)
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}

		f := new(fortune)
		if err := json.NewDecoder(resp.Body).Decode(f); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		_, _ = fmt.Fprint(w, f.Message)
	})

	mux.HandleFunc("/api/all", func(w http.ResponseWriter, r *http.Request) {
		resp, err := myClient.Get(fmt.Sprintf("http://%s:%s/fortunes", BACKEND_DNS, BACKEND_PORT))
		if err != nil {
			log.Println("Error communicating with backend:", err)
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}

		fortunes := new([]fortune)
		if err := json.NewDecoder(resp.Body).Decode(fortunes); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		tmpl, err := template.ParseFiles(templatePath)

		if err != nil {
			log.Println("Error parsing template:", err)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if err := tmpl.Execute(w, fortunes); err != nil {
			log.Println(err)
		}
	})

	mux.HandleFunc("/api/add", func(w http.ResponseWriter, r *http.Request) {

		if r.Method != "POST" {
			http.Error(w, "Use POST", http.StatusMethodNotAllowed)
			return
		}

		f := new(newFortune)
		if err := json.NewDecoder(r.Body).Decode(f); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		var postUrl = fmt.Sprintf("http://%s:%s/fortunes", BACKEND_DNS, BACKEND_PORT)
		var jsonStr = []byte(fmt.Sprintf(`{"id": "%d", "message": "%s"}`, rand.Intn(10000), f.Message))

		_, err := myClient.Post(postUrl, "application/json", bytes.NewBuffer(jsonStr))
		if err != nil {
			log.Println("Error communicating with backend:", err)
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}

		_, _ = fmt.Fprint(w, "Cookie added!")
	})

	mux.Handle("/", http.FileServer(http.Dir(staticPath)))
}

func main() {
	registerRoutes(http.DefaultServeMux)
	err := httpListenAndServe(":8080", nil)
	fmt.Printf("%v", err)
}
