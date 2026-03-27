FROM golang:1.25 as build
ARG VERSION="latest"
WORKDIR /app
COPY go.mod *.go ./
RUN go mod download
RUN CGO_ENABLED=0 go build -ldflags="-X 'main.version=${VERSION}'" -o ms-identity-redirect-handler

FROM scratch
COPY --from=build --chmod=755 /app/ms-identity-redirect-handler /
ENTRYPOINT ["/ms-identity-redirect-handler"]
