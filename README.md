# Microsoft Identity Redirect Handler

A small redirect wrapper for Microsoft Identity Platform sign-ins.

It lets multiple web applications share a single redirect URI on one Entra ID application registration. Instead of sending users directly to the Microsoft `/authorize` endpoint, the application sends them to this service first. This service wraps the original `state` and application callback URL, swaps in its own `/return` endpoint as the Microsoft `redirect_uri`, then forwards the user to Microsoft.

When Microsoft posts the result back to `/return`, the wrapper restores the original state and posts the auth response back to the application callback URL.

This is useful when you have several deployments, several related apps, or multiple instances of the same application that need to use a shared Entra ID app registration, but you do not want to add every application callback URL to the registration. For example, this can help with dev environments or applications running in a Kubernetes cluster.

## Supported flows

This service is intended for the Microsoft Identity Platform authorization endpoint and works with both:

- Microsoft Entra ID sign-ins, including B2B accounts
- Azure AD B2C user flows and custom policies

It expects Microsoft to return the authorization response using `response_mode=form_post`, because `/return` accepts a `POST` and then renders a small auto-submitting HTML form back to the original application.

Useful Microsoft documentation:

- [Microsoft identity platform and OAuth 2.0 authorization code flow](https://learn.microsoft.com/en-us/entra/identity-platform/v2-oauth2-auth-code-flow)
- [Microsoft identity platform authentication flows and app scenarios](https://learn.microsoft.com/en-us/entra/identity-platform/authentication-flows-app-scenarios)
- [Redirect URI restrictions and limitations](https://learn.microsoft.com/en-us/entra/identity-platform/reply-url)
- [Azure AD B2C OpenID Connect](https://learn.microsoft.com/en-us/azure/active-directory-b2c/openid-connect)

## How it works

The application starts a login by calling:

```text
https://redirect.example.com/login?login_url=<microsoft-authorize-url>&redirect_uri=<application-callback-url>&state=<application-state>&...
```

The handler then:

1. Reads the target Microsoft authorization endpoint from `login_url`.
2. Copies the remaining query parameters to that endpoint.
3. Stores the original `state` and original application `redirect_uri` in a wrapped JSON state value.
4. Sets `redirect_uri=https://redirect.example.com/return` for Microsoft.
5. Redirects the browser to Microsoft.

After sign-in, Microsoft posts the result to:

```text
https://redirect.example.com/return
```

The handler then:

1. Unwraps the JSON state.
2. Restores the original application state.
3. Copies the remaining form fields from Microsoft, such as `code` and `session_state`.
4. Posts the result back to the original application callback URL using an auto-submitting HTML form.

## Endpoints

### `GET /login`

Starts a wrapped sign-in.

Required query parameters:

| Parameter | Description |
| --- | --- |
| `login_url` | The Microsoft authorization endpoint to redirect to. |
| `redirect_uri` | The real application callback URL. This is stored in wrapped state and restored after Microsoft returns. |
| `state` | The application's original state value. |

All other query parameters are passed through to Microsoft. Typical parameters include `client_id`, `response_type`, `scope`, `response_mode`, `nonce`, `prompt`, `domain_hint`, and B2C policy parameters.

### `POST /return`

Receives the authorization response from Microsoft. The Microsoft app registration must contain this URI as an allowed redirect URI.

If Microsoft returns an `error`, the handler displays a simple error page with the error details. Otherwise it posts the response back to the original application callback URL.

## Configuration

There is no runtime configuration at the moment. The service listens on port `8000`.

The external hostname is inferred from request headers in this order:

1. `X-Forwarded-For`
2. `X-Original-Host`
3. `Host`

Deploy it behind a reverse proxy or ingress that provides HTTPS and passes the correct host header. The redirect URI registered in Entra ID should be the public URL for `/return`, for example:

```text
https://redirect.example.com/return
```

## Usage examples

### Entra ID or B2B sign-in

Register this redirect URI on the shared Entra ID application registration:

```text
https://identity.example.com/return
```

Then send users to the wrapper instead of directly to Microsoft:

```text
https://identity.example.com/login
  ?login_url=https%3A%2F%2Flogin.microsoftonline.com%2Fcommon%2Foauth2%2Fv2.0%2Fauthorize
  &client_id=00000000-0000-0000-0000-000000000000
  &response_type=code
  &response_mode=form_post
  &scope=openid%20profile%20email
  &redirect_uri=https%3A%2F%2Fapp1.example.com%2Fauth%2Fmicrosoft%2Fcallback
  &state=app1-generated-state
  &nonce=app1-generated-nonce
```

The user is redirected to Microsoft with `redirect_uri=https://identity.example.com/return`. After sign-in, the handler posts the response back to:

```text
https://app1.example.com/auth/microsoft/callback
```

### Azure AD B2C sign-in

For B2C, set `login_url` to the B2C authorization endpoint for the user flow or custom policy:

```text
https://identity.example.com/login
  ?login_url=https%3A%2F%2Fcontoso.b2clogin.com%2Fcontoso.onmicrosoft.com%2FB2C_1_signupsignin%2Foauth2%2Fv2.0%2Fauthorize
  &client_id=11111111-1111-1111-1111-111111111111
  &response_type=code
  &response_mode=form_post
  &scope=openid%20offline_access
  &redirect_uri=https%3A%2F%2Fapp2.example.com%2Fauth%2Fb2c%2Fcallback
  &state=app2-generated-state
  &nonce=app2-generated-nonce
```

Again, only the wrapper callback needs to be registered with B2C:

```text
https://identity.example.com/return
```

## Deployment

Docker images are published to Docker Hub as:

```text
manicminer/ms-identity-redirect-handler
```

The release workflow builds Linux `amd64` and `arm64` images when a tag matching `v*` is pushed.

Run with Docker:

```bash
docker run --rm -p 8000:8000 manicminer/ms-identity-redirect-handler:latest
```

Example Docker Compose service:

```yaml
services:
  ms-identity-redirect-handler:
    image: manicminer/ms-identity-redirect-handler:latest
    restart: unless-stopped
    ports:
      - "8000:8000"
```

In production, put this behind a TLS-terminating reverse proxy or ingress and route the public hostname to port `8000` in the container.

## Building locally

```bash
go build -o ms-identity-redirect-handler
./ms-identity-redirect-handler
```

Or build the container image locally:

```bash
docker build -t ms-identity-redirect-handler .
docker run --rm -p 8000:8000 ms-identity-redirect-handler
```

## Licensing

This project is free to use, modify or redistribute for any purpose.

## Notes

- Use `response_mode=form_post`. The `/return` endpoint does not accept `GET` responses.
- The original application callback URL is carried inside the wrapped state value. Treat the wrapper URL as part of the sign-in surface and deploy it accordingly.
- This service does not validate `login_url` or the restored application callback URL. It is intended to be called by trusted applications or protected by controls at the edge.
- The handler does not exchange authorization codes for tokens. The original application remains responsible for completing the flow.
