package api

import (
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/sentinel/server/internal/metrics"
	"github.com/sentinel/server/internal/push"
	"github.com/sentinel/server/internal/queue"
	ws "github.com/sentinel/server/internal/websocket"
	"github.com/sentinel/server/pkg/cache"
	"github.com/sentinel/server/pkg/config"
	"github.com/sentinel/server/pkg/database"
)

// WebSocketHub defines the interface for WebSocket hub operations
type WebSocketHub interface {
	SendToAgent(agentID string, message []byte) error
	BroadcastToDashboards(message []byte)
	IsAgentOnline(agentID string) bool
	RegisterAgent(conn *websocket.Conn, agentID string, deviceID uuid.UUID) *ws.Client
	RegisterDashboard(conn *websocket.Conn, userID uuid.UUID) *ws.Client
}

// Services contains all service dependencies for the API
type Services struct {
	Config       *config.Config
	DB           *database.Database
	Redis        *cache.Cache
	Hub          WebSocketHub
	BulkInserter *metrics.BulkInserter
	CommandQueue *queue.CommandQueue
	PushService  *push.Service
}
