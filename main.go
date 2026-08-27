package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
)

const PORT = 8000

var version string = "dev"

type wrappedState struct {
	OriginalState string `json:"originalState"`
	OriginalUrl   string `json:"originalUrl"`
}

type hiddenFields map[string]string

func (h hiddenFields) String() string {
	out := ""
	for field, value := range h {
		out += fmt.Sprintf(`<input type="hidden" name="%s" value="%s">`, html.EscapeString(field), html.EscapeString(value))
	}
	return out
}

var errorTemplate, returnTemplate string

func init() {
	if content, err := os.ReadFile(filepath.Join("templates", "error.html")); err != nil {
		log.Fatalf("Error reading template: %v", err)
	} else {
		errorTemplate = string(content)
	}
	if content, err := os.ReadFile(filepath.Join("templates", "return.html")); err != nil {
		log.Fatalf("Error reading template: %v", err)
	} else {
		returnTemplate = string(content)
	}
}

func main() {
	ctx, cancel := context.WithCancel(context.Background())

	mux := http.NewServeMux()
	mux.HandleFunc("/login", loginHandler)
	mux.HandleFunc("/return", returnHandler)

	server := &http.Server{
		Addr: fmt.Sprintf(":%d", PORT),
		BaseContext: func(net.Listener) context.Context {
			return ctx
		},
		Handler: mux,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)

	go func() {
		fmt.Printf("ms-identity-redirect-handler %s is running on port %d\n", version, PORT)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Error starting server: %v\n", err)
		}
	}()

	<-stop
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("Forcibly shutting down server: %v", err)
	}

	os.Exit(0)
}

func handleError(w http.ResponseWriter, statusCode int, errorTitle, errorMessage string) {
	out := strings.ReplaceAll(errorTemplate, "%[1]s", html.EscapeString(errorTitle))
	out = strings.ReplaceAll(out, "%[2]s", html.EscapeString(errorMessage))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(statusCode)
	w.Write([]byte(out))
}

func loginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		handleError(w, http.StatusMethodNotAllowed, "Invalid Request", "Method not allowed")
		return
	}

	var hostname *string
	if v := os.Getenv("HOST"); v != "" {
		hostname = &v
	} else if v := r.Header.Get("X-Forwarded-For"); v != "" {
		hostname = &v
	} else if v := r.Header.Get("X-Original-Host"); v != "" {
		hostname = &v
	} else if v := r.Host; v != "" {
		hostname = &v
	}

	if hostname == nil || *hostname == "" {
		handleError(w, http.StatusBadRequest, "Invalid Request", "Unable to determine hostname from HTTP headers")
		return
	}

	loginUrl, err := url.Parse(r.URL.Query().Get("login_url"))
	if err != nil {
		handleError(w, http.StatusInternalServerError, "Could not parse `login_url`", err.Error())
		return
	}

	params := url.Values{}
	skipFields := []string{"login_url", "redirect_uri", "state"}
	for field := range r.URL.Query() {
		if slices.Contains(skipFields, field) {
			continue
		}
		if val := r.URL.Query().Get(field); val != "" {
			params.Set(field, val)
		}
	}

	state := wrappedState{
		OriginalState: r.URL.Query().Get("state"),
		OriginalUrl:   r.URL.Query().Get("redirect_uri"),
	}

	stateVal, err := json.Marshal(state)
	if err != nil {
		handleError(w, http.StatusInternalServerError, "Marshalling state", err.Error())
		return
	}

	params.Set("redirect_uri", fmt.Sprintf("https://%s/return", *hostname))
	params.Set("state", string(stateVal))
	loginUrl.RawQuery = params.Encode()

	http.Redirect(w, r, loginUrl.String(), http.StatusFound)
}

func returnHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		handleError(w, http.StatusMethodNotAllowed, "Invalid Request", "Method not allowed")
		return
	}

	if err := r.ParseForm(); err != nil {
		handleError(w, http.StatusInternalServerError, "Could not parse form data", err.Error())
		return
	}

	if errorCode := r.Form.Get("error"); errorCode != "" {
		errorDescription := r.Form.Get("error_description")
		handleError(w, http.StatusInternalServerError, errorCode, errorDescription)
		return
	}

	state := &wrappedState{}
	if err := json.Unmarshal([]byte(r.Form.Get("state")), state); err != nil {
		http.Error(w, fmt.Sprintf("unmarshalling state: %v", err), http.StatusInternalServerError)
		return
	}

	fields := make(hiddenFields)
	for field := range r.Form {
		if field == "state" {
			continue
		}
		fields[field] = r.Form.Get(field)
	}

	out := fmt.Sprintf(returnTemplate, state.OriginalUrl, state.OriginalState, fields)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(out))
}
