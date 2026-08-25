package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestLoginHandlerRedirectsToEntraIDAuthorizeEndpoint(t *testing.T) {
	query := url.Values{}
	query.Set("login_url", "https://login.microsoftonline.com/common/oauth2/v2.0/authorize")
	query.Set("client_id", "00000000-0000-0000-0000-000000000000")
	query.Set("response_type", "code")
	query.Set("response_mode", "form_post")
	query.Set("scope", "openid profile email")
	query.Set("redirect_uri", "https://app1.example.com/auth/microsoft/callback")
	query.Set("state", "app1-generated-state")
	query.Set("nonce", "app1-generated-nonce")
	query.Set("prompt", "select_account")

	req := httptest.NewRequest(http.MethodGet, "/login?"+query.Encode(), nil)
	req.Header.Set("X-Forwarded-For", "identity.example.com")
	rr := httptest.NewRecorder()

	loginHandler(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected status %d, got %d", http.StatusFound, rr.Code)
	}

	location := rr.Header().Get("Location")
	redirectURL, err := url.Parse(location)
	if err != nil {
		t.Fatalf("parsing redirect location %q: %v", location, err)
	}

	if redirectURL.Scheme != "https" || redirectURL.Host != "login.microsoftonline.com" || redirectURL.Path != "/common/oauth2/v2.0/authorize" {
		t.Fatalf("unexpected Microsoft authorize URL: %s", redirectURL.String())
	}

	params := redirectURL.Query()
	assertQueryParam(t, params, "client_id", "00000000-0000-0000-0000-000000000000")
	assertQueryParam(t, params, "response_type", "code")
	assertQueryParam(t, params, "response_mode", "form_post")
	assertQueryParam(t, params, "scope", "openid profile email")
	assertQueryParam(t, params, "nonce", "app1-generated-nonce")
	assertQueryParam(t, params, "prompt", "select_account")
	assertQueryParam(t, params, "redirect_uri", "https://identity.example.com/return")

	state := decodeWrappedState(t, params.Get("state"))
	if state.OriginalState != "app1-generated-state" {
		t.Fatalf("expected original state to be preserved, got %q", state.OriginalState)
	}
	if state.OriginalUrl != "https://app1.example.com/auth/microsoft/callback" {
		t.Fatalf("expected original redirect URI to be preserved, got %q", state.OriginalUrl)
	}
}

func TestLoginHandlerRedirectsToB2CAuthorizeEndpoint(t *testing.T) {
	query := url.Values{}
	query.Set("login_url", "https://contoso.b2clogin.com/contoso.onmicrosoft.com/B2C_1_signupsignin/oauth2/v2.0/authorize")
	query.Set("client_id", "11111111-1111-1111-1111-111111111111")
	query.Set("response_type", "code")
	query.Set("response_mode", "form_post")
	query.Set("scope", "openid offline_access")
	query.Set("redirect_uri", "https://app2.example.com/auth/b2c/callback")
	query.Set("state", "app2-generated-state")
	query.Set("nonce", "app2-generated-nonce")

	req := httptest.NewRequest(http.MethodGet, "/login?"+query.Encode(), nil)
	req.Header.Set("X-Forwarded-For", "identity.example.com")
	rr := httptest.NewRecorder()

	loginHandler(rr, req)

	if rr.Code != http.StatusFound {
		t.Fatalf("expected status %d, got %d", http.StatusFound, rr.Code)
	}

	location := rr.Header().Get("Location")
	redirectURL, err := url.Parse(location)
	if err != nil {
		t.Fatalf("parsing redirect location %q: %v", location, err)
	}

	if redirectURL.Scheme != "https" || redirectURL.Host != "contoso.b2clogin.com" || redirectURL.Path != "/contoso.onmicrosoft.com/B2C_1_signupsignin/oauth2/v2.0/authorize" {
		t.Fatalf("unexpected B2C authorize URL: %s", redirectURL.String())
	}

	params := redirectURL.Query()
	assertQueryParam(t, params, "client_id", "11111111-1111-1111-1111-111111111111")
	assertQueryParam(t, params, "response_type", "code")
	assertQueryParam(t, params, "response_mode", "form_post")
	assertQueryParam(t, params, "scope", "openid offline_access")
	assertQueryParam(t, params, "nonce", "app2-generated-nonce")
	assertQueryParam(t, params, "redirect_uri", "https://identity.example.com/return")

	state := decodeWrappedState(t, params.Get("state"))
	if state.OriginalState != "app2-generated-state" {
		t.Fatalf("expected original state to be preserved, got %q", state.OriginalState)
	}
	if state.OriginalUrl != "https://app2.example.com/auth/b2c/callback" {
		t.Fatalf("expected original redirect URI to be preserved, got %q", state.OriginalUrl)
	}
}

func TestLoginHandlerRejectsUnsupportedMethod(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/login", nil)
	rr := httptest.NewRecorder()

	loginHandler(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status %d, got %d", http.StatusMethodNotAllowed, rr.Code)
	}
	assertBodyContains(t, rr.Body.String(), "Method not allowed")
}

func TestReturnHandlerPostsSuccessfulResponseBackToOriginalApplication(t *testing.T) {
	state := encodeWrappedState(t, wrappedState{
		OriginalState: "app1-generated-state",
		OriginalUrl:   "https://app1.example.com/auth/microsoft/callback",
	})
	form := url.Values{}
	form.Set("state", state)
	form.Set("code", "auth-code-from-microsoft")
	form.Set("session_state", "session-state-from-microsoft")

	req := httptest.NewRequest(http.MethodPost, "/return", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()

	returnHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}

	body := rr.Body.String()
	assertBodyContains(t, body, `<form id="muxForm" action="https://app1.example.com/auth/microsoft/callback" method="post">`)
	assertBodyContains(t, body, `<input type="hidden" name="state" value="app1-generated-state">`)
	assertBodyContains(t, body, `<input type="hidden" name="code" value="auth-code-from-microsoft">`)
	assertBodyContains(t, body, `<input type="hidden" name="session_state" value="session-state-from-microsoft">`)
	assertBodyContains(t, body, "Login Successful")
}

func TestReturnHandlerDisplaysMicrosoftLoginError(t *testing.T) {
	state := encodeWrappedState(t, wrappedState{
		OriginalState: "app1-generated-state",
		OriginalUrl:   "https://app1.example.com/auth/microsoft/callback",
	})
	form := url.Values{}
	form.Set("state", state)
	form.Set("error", "access_denied")
	form.Set("error_description", "The user cancelled the sign-in flow.")

	req := httptest.NewRequest(http.MethodPost, "/return", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()

	returnHandler(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d", http.StatusInternalServerError, rr.Code)
	}

	body := rr.Body.String()
	assertBodyContains(t, body, "Login Error")
	assertBodyContains(t, body, "access_denied")
	assertBodyContains(t, body, "The user cancelled the sign-in flow.")
	assertBodyDoesNotContain(t, body, "Login Successful")
	assertBodyDoesNotContain(t, body, `action="https://app1.example.com/auth/microsoft/callback"`)
}

func TestReturnHandlerRejectsUnsupportedMethod(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/return", nil)
	rr := httptest.NewRecorder()

	returnHandler(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected status %d, got %d", http.StatusMethodNotAllowed, rr.Code)
	}
	assertBodyContains(t, rr.Body.String(), "Method not allowed")
}

func assertQueryParam(t *testing.T, params url.Values, name, want string) {
	t.Helper()
	if got := params.Get(name); got != want {
		t.Fatalf("expected query parameter %s=%q, got %q", name, want, got)
	}
}

func assertBodyContains(t *testing.T, body, want string) {
	t.Helper()
	if !strings.Contains(body, want) {
		t.Fatalf("expected body to contain %q, got:\n%s", want, body)
	}
}

func assertBodyDoesNotContain(t *testing.T, body, unwanted string) {
	t.Helper()
	if strings.Contains(body, unwanted) {
		t.Fatalf("expected body not to contain %q, got:\n%s", unwanted, body)
	}
}

func encodeWrappedState(t *testing.T, state wrappedState) string {
	t.Helper()
	content, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("encoding wrapped state: %v", err)
	}
	return string(content)
}

func decodeWrappedState(t *testing.T, value string) wrappedState {
	t.Helper()
	state := wrappedState{}
	if err := json.Unmarshal([]byte(value), &state); err != nil {
		t.Fatalf("decoding wrapped state %q: %v", value, err)
	}
	return state
}
