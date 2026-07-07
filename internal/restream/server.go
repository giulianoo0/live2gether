package restream

import (
	"context"
	"embed"
	"html/template"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

//go:embed web/index.html
var webFiles embed.FS

type Server struct {
	manager  *Manager
	upgrader websocket.Upgrader
	index    *template.Template
}

type createSessionResponse struct {
	Snapshot
	HostToken string `json:"hostToken,omitempty"`
}

func NewServer(manager *Manager) (*Server, error) {
	indexBytes, err := webFiles.ReadFile("web/index.html")
	if err != nil {
		return nil, err
	}

	index, err := template.New("index").Parse(string(indexBytes))
	if err != nil {
		return nil, err
	}

	return &Server{
		manager: manager,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(*http.Request) bool { return true },
		},
		index: index,
	}, nil
}

func (s *Server) Router() *gin.Engine {
	router := gin.Default()

	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	router.GET("/", s.renderIndex(""))
	router.GET("/watch/:id", func(c *gin.Context) {
		s.renderIndex(c.Param("id"))(c)
	})
	router.POST("/api/sessions", s.createSession)
	router.GET("/api/sessions/:id", s.getSession)
	router.POST("/api/sessions/:id/quality", s.setQuality)
	router.GET("/ws/:id", s.watchSocket)
	router.GET("/hls/:id/:name", s.serveHLS)

	return router
}

func (s *Server) renderIndex(sessionID string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.Status(http.StatusOK)
		_ = s.index.Execute(c.Writer, gin.H{"SessionID": sessionID})
	}
}

func (s *Server) createSession(c *gin.Context) {
	var request struct {
		URL string `json:"url"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "JSON body must include url"})
		return
	}

	session, created, err := s.manager.GetOrCreate(c.Request.Context(), request.URL)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	response := createSessionResponse{Snapshot: session.Snapshot()}
	if created {
		response.HostToken = session.HostToken
	}
	c.JSON(http.StatusOK, response)
}

func (s *Server) getSession(c *gin.Context) {
	session, ok := s.manager.Get(c.Param("id"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}
	c.JSON(http.StatusOK, session.Snapshot())
}

func (s *Server) setQuality(c *gin.Context) {
	var request struct {
		QualityID string `json:"qualityId"`
		HostToken string `json:"hostToken"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "JSON body must include qualityId and hostToken"})
		return
	}

	if err := s.manager.SetQuality(c.Request.Context(), c.Param("id"), request.QualityID, request.HostToken); err != nil {
		status := http.StatusBadRequest
		if err.Error() == "host token is invalid" {
			status = http.StatusForbidden
		}
		if err.Error() == "session not found" {
			status = http.StatusNotFound
		}
		c.JSON(status, gin.H{"error": err.Error()})
		return
	}

	session, _ := s.manager.Get(c.Param("id"))
	c.JSON(http.StatusOK, session.Snapshot())
}

func (s *Server) watchSocket(c *gin.Context) {
	session, ok := s.manager.Get(c.Param("id"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}

	conn, err := s.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()

	viewerID, _, updates := session.SubscribeViewer(ctx)
	go func() {
		defer cancel()
		for {
			var message struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}
			if err := conn.ReadJSON(&message); err != nil {
				return
			}
			if message.Type == "chat" {
				session.AddChat(viewerID, message.Text)
			}
		}
	}()

	for snapshot := range updates {
		if err := conn.WriteJSON(snapshot); err != nil {
			cancel()
			return
		}
	}
}

func (s *Server) serveHLS(c *gin.Context) {
	session, ok := s.manager.Get(c.Param("id"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "session not found"})
		return
	}

	name, err := SafeHLSName(c.Param("name"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	path := filepath.Join(session.HLSDir, name)
	if !strings.HasPrefix(path, session.HLSDir) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid HLS path"})
		return
	}

	switch strings.ToLower(filepath.Ext(name)) {
	case ".m3u8":
		c.Header("Content-Type", "application/vnd.apple.mpegurl")
		c.Header("Cache-Control", "no-store")
	case ".ts":
		c.Header("Content-Type", "video/mp2t")
	default:
		c.Header("Cache-Control", "public, max-age=30")
	}

	c.File(path)
}
