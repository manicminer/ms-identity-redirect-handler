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
	"slices"
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

func loginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var hostname *string
	if v := r.Header.Get("X-Forwarded-Host"); v != "" {
		hostname = &v
	} else if v := r.Header.Get("X-Original-Host"); v != "" {
		hostname = &v
	} else if v := r.Header.Get("Host"); v != "" {
		hostname = &v
	}

	if hostname == nil || *hostname == "" {
		http.Error(w, "Unable to determine hostname from HTTP headers", http.StatusBadRequest)
		return
	}

	loginUrl, err := url.Parse(r.URL.Query().Get("login_url"))
	if err != nil {
		http.Error(w, fmt.Sprintf("parsing `login_url`: %s", err.Error()), http.StatusInternalServerError)
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
		http.Error(w, fmt.Sprintf("marshalling state: %v", err), http.StatusInternalServerError)
		return
	}

	params.Set("redirect_uri", fmt.Sprintf("https://%s/return", *hostname))
	params.Set("state", string(stateVal))
	loginUrl.RawQuery = params.Encode()

	http.Redirect(w, r, loginUrl.String(), http.StatusFound)
}

func returnHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, fmt.Sprintf("parsing form: %v", err), http.StatusInternalServerError)
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

	out := fmt.Sprintf(`
	<!DOCTYPE html>
	<html lang="en">
	<head>
		<title>Login Successful</title>
		<script type="text/javascript">
			window.onload = function() {
				document.getElementById('muxForm').submit();
			};
		</script>
	</head>
	<body>
		<form id="muxForm" action="%[1]s" method="post">
			<input type="hidden" name="state" value="%[2]s">
			%[3]s
			<noscript>
				<h2>Login Successful</h2>
				<input type="submit" value="Continue">
			</noscript>
		</form>
	</body>
	</html>`, state.OriginalUrl, state.OriginalState, fields)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(out))
}
