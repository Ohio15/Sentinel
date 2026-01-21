package api

import (
	"context"
	"encoding/json"
	"log"

	"github.com/google/uuid"
	"github.com/sentinel/server/internal/constants"
	ws "github.com/sentinel/server/internal/websocket"
)

// handleDashboardMessage forwards messages from dashboard to appropriate agents
func (r *Router) handleDashboardMessage(userID uuid.UUID, message []byte) {
	log.Printf("[Dashboard] Received message: %s", string(message))

	var msg ws.Message
	if err := json.Unmarshal(message, &msg); err != nil {
		log.Printf("[Dashboard] Failed to unmarshal message: %v", err)
		return
	}
	log.Printf("[Dashboard] Message type: %s", msg.Type)

	// Extract target info from payload
	var payload struct {
		AgentID    string      `json:"agentId"`
		DeviceID   string      `json:"deviceId"`
		SessionID  string      `json:"sessionId"`
		Data       interface{} `json:"data"` // Generic data field (used for input data, terminal data, etc.)
		Path       string      `json:"path"`
		Cols       int         `json:"cols"`
		Rows       int         `json:"rows"`
		MaxDepth   int         `json:"maxDepth"`
		IntervalMs int         `json:"intervalMs"`
		InputType  string      `json:"inputType"`
	}
	json.Unmarshal(msg.Payload, &payload)

	// Helper to send error response back to dashboard
	sendError := func(errorMsg string) {
		errResponse, _ := json.Marshal(map[string]interface{}{
			"type":      "error",
			"requestId": msg.RequestID,
			"sessionId": payload.SessionID,
			"deviceId":  payload.DeviceID,
			"agentId":   payload.AgentID,
			"error":     errorMsg,
			"originalType": msg.Type,
		})
		r.hub.BroadcastToDashboards(errResponse)
	}

	// Get agent ID from device ID if needed
	agentID := payload.AgentID
	if agentID == "" && payload.DeviceID != "" {
		ctx := context.Background()
		deviceUUID, err := uuid.Parse(payload.DeviceID)
		if err == nil {
			r.db.Pool().QueryRow(ctx, "SELECT agent_id FROM devices WHERE id = $1 AND organization_id = $2", deviceUUID, constants.CurrentOrganizationID).Scan(&agentID)
		}
	}

	if agentID == "" {
		sendError("Device not found or agent ID unavailable")
		return
	}

	// Check if agent is online
	if !r.hub.IsAgentOnline(agentID) {
		log.Printf("[Dashboard] Agent %s is not online, cannot forward %s message", agentID, msg.Type)
		sendError("Agent is not connected. Please check the agent service on the device.")
		return
	}

	log.Printf("[Dashboard] Agent %s is online, forwarding %s message", agentID, msg.Type)

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

	// WebRTC signaling handlers
	case ws.MsgTypeWebRTCStart:
		// Forward WebRTC start request with SDP offer to agent
		var webrtcPayload struct {
			OfferSDP string `json:"offerSdp"`
		}
		json.Unmarshal(msg.Payload, &webrtcPayload)
		log.Printf("[WebRTC] webrtc_start received: agentId=%s, sessionId=%s, offerSdp length=%d",
			agentID, payload.SessionID, len(webrtcPayload.OfferSDP))
		agentMsg, _ := json.Marshal(map[string]interface{}{
			"type":      ws.MsgTypeWebRTCStart,
			"requestId": msg.RequestID,
			"data": map[string]interface{}{
				"sessionId": payload.SessionID,
				"offerSdp":  webrtcPayload.OfferSDP,
			},
		})
		if err := r.hub.SendToAgent(agentID, agentMsg); err != nil {
			log.Printf("[WebRTC] ERROR: Failed to send webrtc_start to agent %s: %v", agentID, err)
			sendError("Failed to forward WebRTC start to agent: " + err.Error())
			return
		}
		log.Printf("[WebRTC] Successfully forwarded webrtc_start to agent %s", agentID)

	case ws.MsgTypeWebRTCSignal:
		// Forward WebRTC signaling (ICE candidates, etc.) to agent
		var signalPayload struct {
			Signal json.RawMessage `json:"signal"`
		}
		json.Unmarshal(msg.Payload, &signalPayload)
		log.Printf("[WebRTC] webrtc_signal received: agentId=%s, sessionId=%s", agentID, payload.SessionID)
		agentMsg, _ := json.Marshal(map[string]interface{}{
			"type":      ws.MsgTypeWebRTCSignal,
			"requestId": msg.RequestID,
			"data": map[string]interface{}{
				"sessionId": payload.SessionID,
				"signal":    signalPayload.Signal,
			},
		})
		r.hub.SendToAgent(agentID, agentMsg)

	case ws.MsgTypeWebRTCStop:
		// Forward WebRTC stop request to agent
		log.Printf("[WebRTC] webrtc_stop received: agentId=%s, sessionId=%s", agentID, payload.SessionID)
		agentMsg, _ := json.Marshal(map[string]interface{}{
			"type":      ws.MsgTypeWebRTCStop,
			"requestId": msg.RequestID,
			"data": map[string]interface{}{
				"sessionId": payload.SessionID,
			},
		})
		r.hub.SendToAgent(agentID, agentMsg)
	}
}
