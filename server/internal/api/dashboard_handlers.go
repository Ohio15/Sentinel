package api

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	ws "github.com/sentinel/server/internal/websocket"
)

// handleDashboardMessage forwards messages from dashboard to appropriate agents
func (r *Router) handleDashboardMessage(userID uuid.UUID, message []byte) {
	var msg ws.Message
	if err := json.Unmarshal(message, &msg); err != nil {
		return
	}

	// Extract target info from payload
	var payload struct {
		AgentID    string          `json:"agentId"`
		DeviceID   string          `json:"deviceId"`
		SessionID  string          `json:"sessionId"`
		Data       json.RawMessage `json:"data"`
		Path       string          `json:"path"`
		Cols       int             `json:"cols"`
		Rows       int             `json:"rows"`
		MaxDepth   int             `json:"maxDepth"`
		IntervalMs int             `json:"intervalMs"`
	}
	json.Unmarshal(msg.Payload, &payload)

	// Get agent ID from device ID if needed
	agentID := payload.AgentID
	if agentID == "" && payload.DeviceID != "" {
		ctx := context.Background()
		deviceUUID, err := uuid.Parse(payload.DeviceID)
		if err == nil {
			r.db.Pool().QueryRow(ctx, "SELECT agent_id FROM devices WHERE id = $1", deviceUUID).Scan(&agentID)
		}
	}

	if agentID == "" {
		return
	}

	// Check if agent is online
	if !r.hub.IsAgentOnline(agentID) {
		return
	}

	switch msg.Type {
	case ws.MsgTypeTerminalStart:
		// Forward terminal start request to agent
		agentMsg, _ := json.Marshal(map[string]interface{}{
			"type":      ws.MsgTypeTerminalStart,
			"requestId": msg.RequestID,
			"data": map[string]interface{}{
				"sessionId": payload.SessionID,
				"cols":      payload.Cols,
				"rows":      payload.Rows,
			},
		})
		r.hub.SendToAgent(agentID, agentMsg)

	case ws.MsgTypeTerminalInput:
		// Forward terminal input to agent
		agentMsg, _ := json.Marshal(map[string]interface{}{
			"type":      ws.MsgTypeTerminalInput,
			"requestId": msg.RequestID,
			"data": map[string]interface{}{
				"sessionId": payload.SessionID,
				"data":      payload.Data,
			},
		})
		r.hub.SendToAgent(agentID, agentMsg)

	case ws.MsgTypeTerminalResize:
		// Forward terminal resize to agent
		agentMsg, _ := json.Marshal(map[string]interface{}{
			"type":      ws.MsgTypeTerminalResize,
			"requestId": msg.RequestID,
			"data": map[string]interface{}{
				"sessionId": payload.SessionID,
				"cols":      payload.Cols,
				"rows":      payload.Rows,
			},
		})
		r.hub.SendToAgent(agentID, agentMsg)

	case ws.MsgTypeTerminalClose:
		// Forward terminal close to agent
		agentMsg, _ := json.Marshal(map[string]interface{}{
			"type":      ws.MsgTypeTerminalClose,
			"requestId": msg.RequestID,
			"data": map[string]interface{}{
				"sessionId": payload.SessionID,
			},
		})
		r.hub.SendToAgent(agentID, agentMsg)

	case ws.MsgTypeListDrives:
		// Forward list drives request to agent
		agentMsg, _ := json.Marshal(map[string]interface{}{
			"type":      ws.MsgTypeListDrives,
			"requestId": msg.RequestID,
			"data":      map[string]interface{}{},
		})
		r.hub.SendToAgent(agentID, agentMsg)

	case ws.MsgTypeListFiles:
		// Forward file list request to agent
		agentMsg, _ := json.Marshal(map[string]interface{}{
			"type":      ws.MsgTypeListFiles,
			"requestId": msg.RequestID,
			"data": map[string]interface{}{
				"path": payload.Path,
			},
		})
		r.hub.SendToAgent(agentID, agentMsg)

	case ws.MsgTypeScanDirectory:
		// Forward directory scan request to agent
		agentMsg, _ := json.Marshal(map[string]interface{}{
			"type":      ws.MsgTypeScanDirectory,
			"requestId": msg.RequestID,
			"data": map[string]interface{}{
				"path":     payload.Path,
				"maxDepth": payload.MaxDepth,
			},
		})
		r.hub.SendToAgent(agentID, agentMsg)

	case ws.MsgTypeSetMetricsInterval:
		// Forward metrics interval request to agent
		agentMsg, _ := json.Marshal(map[string]interface{}{
			"type":      ws.MsgTypeSetMetricsInterval,
			"requestId": msg.RequestID,
			"data": map[string]interface{}{
				"intervalMs": payload.IntervalMs,
			},
		})
		r.hub.SendToAgent(agentID, agentMsg)

	case ws.MsgTypeDownloadFile:
		// Forward file download request to agent
		agentMsg, _ := json.Marshal(map[string]interface{}{
			"type":      ws.MsgTypeDownloadFile,
			"requestId": msg.RequestID,
			"data": map[string]interface{}{
				"path": payload.Path,
			},
		})
		r.hub.SendToAgent(agentID, agentMsg)

	case ws.MsgTypeUploadFile:
		// Forward file upload request to agent
		r.hub.SendToAgent(agentID, message)

	case ws.MsgTypeStartRemote:
		// Forward remote desktop start request to agent
		agentMsg, _ := json.Marshal(map[string]interface{}{
			"type":      ws.MsgTypeStartRemote,
			"requestId": msg.RequestID,
			"data": map[string]interface{}{
				"sessionId": payload.SessionID,
			},
		})
		r.hub.SendToAgent(agentID, agentMsg)

	case ws.MsgTypeStopRemote:
		// Forward remote desktop stop request to agent
		agentMsg, _ := json.Marshal(map[string]interface{}{
			"type":      ws.MsgTypeStopRemote,
			"requestId": msg.RequestID,
			"data": map[string]interface{}{
				"sessionId": payload.SessionID,
			},
		})
		r.hub.SendToAgent(agentID, agentMsg)

	case ws.MsgTypeRemoteInput:
		// Forward remote desktop input to agent (mouse/keyboard)
		r.hub.SendToAgent(agentID, message)
	}
}
