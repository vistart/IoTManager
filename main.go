package main

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rhosocial/go-rush-common/component/logger"
	"github.com/rhosocial/go-rush-common/component/response"
)

// Event keeps a list of clients those are currently attached
// and broadcasting events to those clients.
type Event struct {
	// Events are pushed to this channel by the main events-gathering routine
	Message chan string

	// New client connections
	NewClients chan chan string

	// Closed client connections
	ClosedClients chan chan string

	// Total client connections
	TotalClients map[chan string]bool
}

// ClientChan New event messages are broadcast to all registered client connection channels
type ClientChan chan string

func main() {
	router := gin.Default()

	// Initialize new streaming server
	stream := NewServer()

	// We are streaming current time to clients in the interval 10 seconds
	go func() {
		for {
			time.Sleep(time.Second * 10)
			now := time.Now().Format("2006-01-02 15:04:05")
			currentTime := fmt.Sprintf("The Current Time Is %v", now)

			// Send current time to clients message channel
			stream.Message <- currentTime
		}
	}()

	// Basic Authentication
	authorized := router.Group("/")

	// Authorized client can stream the event
	// Add event-streaming headers
	authorized.GET("/stream", HeadersMiddleware(), stream.serveHTTP(), func(c *gin.Context) {
		v, ok := c.Get("clientChan")
		if !ok {
			return
		}
		clientChan, ok := v.(ClientChan)
		if !ok {
			return
		}
		c.Stream(func(w io.Writer) bool {
			// Stream message to client from message channel
			if msg, ok := <-clientChan; ok {
				c.SSEvent("message", msg)
				return true
			}
			return false
		})
	})

	authorized.GET("/ping",
		logger.AppendRequestID(), func(c *gin.Context) {
			stream.Message <- c.ClientIP() + ": ping"
			c.JSON(http.StatusOK, response.NewBase(c, 0, "pong"))
		})
	authorized.POST("/upload", logger.AppendRequestID(), func(c *gin.Context) {
		file, err := c.FormFile("file.amr")
		if err != nil {
			stream.Message <- c.ClientIP() + ": uploaded, but failed, " + err.Error()
			c.AbortWithStatusJSON(http.StatusBadRequest, response.NewBase(c, 1, err.Error()))
			return
		}
		log.Println(file.Filename)

		savedFilename := time.Now().Format("2006-01-02-15-04-05_") + file.Filename
		dst := "./" + savedFilename
		// 上传文件至指定的完整文件路径
		err = c.SaveUploadedFile(file, dst)
		if err != nil {
			stream.Message <- c.ClientIP() + ": uploaded, but failed, " + err.Error()
			c.AbortWithStatusJSON(http.StatusBadRequest, response.NewBase(c, 1, err.Error()))
			return
		}

		stream.Message <- c.ClientIP() + fmt.Sprintf("'<a href=\"/download?file=%s\" download=\"%s\">%s</a>' with %d bytes uploaded!", savedFilename, savedFilename, savedFilename, file.Size)
		c.JSON(http.StatusOK, response.NewBase(c, 0, fmt.Sprintf("'%s' with %d bytes uploaded!", time.Now().Format("2006-01-02-15-04-05_")+file.Filename, file.Size)))
	})
	authorized.GET("/download", logger.AppendRequestID(), func(c *gin.Context) {
		filename := c.Query("file")
		if len(filename) == 0 {
			c.AbortWithStatusJSON(http.StatusNotFound, response.NewBase(c, 1, "file not found"))
			return
		}
		matched, err := regexp.MatchString("^20[0-9]{2}-[0-9]{2}-[0-9]{2}-[0-9]{2}-[0-9]{2}-[0-9]{2}_file.amr", filename)
		if err != nil || !matched {
			c.AbortWithStatusJSON(http.StatusNotFound, response.NewBase(c, 1, "file not found"))
			return
		}
		if _, err := os.Stat(filename); err == nil {
			c.File(filename)
			return
		}
		c.AbortWithStatusJSON(http.StatusNotFound, response.NewBase(c, 1, "file not found"))
		return
	})

	// Parse Static files
	router.StaticFile("/", "./public/index.html")

	router.Run(":8085")
}

// NewServer Initialize event and Start preprocessing requests
func NewServer() (event *Event) {
	event = &Event{
		Message:       make(chan string),
		NewClients:    make(chan chan string),
		ClosedClients: make(chan chan string),
		TotalClients:  make(map[chan string]bool),
	}

	go event.listen()

	return
}

// It Listens all incoming requests from clients.
// Handles addition and removal of clients and broadcast messages to clients.
func (stream *Event) listen() {
	for {
		select {
		// Add new available client
		case client := <-stream.NewClients:
			stream.TotalClients[client] = true
			log.Printf("Client added. %d registered clients", len(stream.TotalClients))

		// Remove closed client
		case client := <-stream.ClosedClients:
			delete(stream.TotalClients, client)
			close(client)
			log.Printf("Removed client. %d registered clients", len(stream.TotalClients))

		// Broadcast message to client
		case eventMsg := <-stream.Message:
			for clientMessageChan := range stream.TotalClients {
				clientMessageChan <- eventMsg
			}
		}
	}
}

func (stream *Event) serveHTTP() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Initialize client channel
		clientChan := make(ClientChan)

		// Send new connection to event server
		stream.NewClients <- clientChan

		defer func() {
			// Send closed connection to event server
			stream.ClosedClients <- clientChan
		}()

		c.Set("clientChan", clientChan)

		c.Next()
	}
}

func HeadersMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Content-Type", "text/event-stream")
		c.Writer.Header().Set("Cache-Control", "no-cache")
		c.Writer.Header().Set("Connection", "keep-alive")
		c.Writer.Header().Set("Transfer-Encoding", "chunked")
		c.Next()
	}
}
