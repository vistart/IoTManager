FROM golang:alpine

ENV GO111MODULE=on \
    CGO_ENABLED=0 \
    GOOS=linux

RUN apk add --no-cache git

WORKDIR /opt/project

COPY public ./public
COPY go.mod main.go ./

RUN go mod tidy
RUN go build

CMD ["./IoTManager"]