# syntax=docker/dockerfile:1

##
## Build
##
FROM golang:1.16-buster AS build

WORKDIR /app

COPY go.mod ./
COPY go.sum ./
RUN go mod download

COPY *.go ./

RUN go build -o /google-wifi-prom

##
## Deploy
##
FROM gcr.io/distroless/base-debian10

WORKDIR /

COPY --from=build /google-wifi-prom /google-wifi-prom

EXPOSE 8080

USER nonroot:nonroot

ENTRYPOINT ["/google-wifi-prom"]

