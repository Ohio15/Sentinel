package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const (
	commandStreamKey  = "sentinel:commands:stream"
	responseStreamKey = "sentinel:responses:stream"
	consumerGroup     = "sentinel-servers"
	streamMaxLen      = 10000
	readBlockDuration = 5 * time.Second
	commandTTL        = 5 * time.Minute
)

// CommandMessage represents a command to be sent to an agent
type CommandMessage struct {
	ID          string    `json:"id"`
	DeviceID    uuid.UUID `json:"deviceId"`
	AgentID     string    `json:"agentId"`
	CommandType string    `json:"commandType"`
	Command     string    `json:"command"`
	RequestID   string    `json:"requestId"`
	CreatedBy   uuid.UUID `json:"createdBy"`
	CreatedAt   time.Time `json:"createdAt"`
	Timeout     int       `json:"timeout,omitempty"` // seconds
}

// ResponseMessage represents a command response from an agent
type ResponseMessage struct {
	CommandID string    `json:"commandId"`
	RequestID string    `json:"requestId"`
	AgentID   string    `json:"agentId"`
	Success   bool      `json:"success"`
	Output    string    `json:"output"`
	Error     string    `json:"error,omitempty"`
	ExitCode  int       `json:"exitCode"`
	Timestamp time.Time `json:"timestamp"`
}

// CommandHandler processes commands from the queue
type CommandHandler func(cmd CommandMessage) error

// ResponseHandler processes responses from agents
type ResponseHandler func(resp ResponseMessage)

// CommandQueue manages distributed command distribution via Redis Streams
type CommandQueue struct {
	redis      *redis.Client
	serverID   string
	handlers   map[string]CommandHandler
	respSubs   map[string]ResponseHandler

	ctx    context.Context
	cancel context.CancelFunc
}

// NewCommandQueue creates a new command queue backed by Redis Streams
func NewCommandQueue(redisClient *redis.Client, serverID string) *CommandQueue {
	ctx, cancel := context.WithCancel(context.Background())

	cq := &CommandQueue{
		redis:    redisClient,
		serverID: serverID,
		handlers: make(map[string]CommandHandler),
		respSubs: make(map[string]ResponseHandler),
		ctx:      ctx,
		cancel:   cancel,
	}

	// Create consumer group if not exists
	cq.createConsumerGroup()

	return cq
}

// createConsumerGroup ensures the consumer group exists
func (cq *CommandQueue) createConsumerGroup() {
	ctx := context.Background()

	// Create stream and consumer group for commands
	err := cq.redis.XGroupCreateMkStream(ctx, commandStreamKey, consumerGroup, "0").Err()
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		log.Printf("Warning: Could not create command consumer group: %v", err)
	}

	// Create stream and consumer group for responses
	err = cq.redis.XGroupCreateMkStream(ctx, responseStreamKey, consumerGroup, "0").Err()
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		log.Printf("Warning: Could not create response consumer group: %v", err)
	}
}

// PublishCommand adds a command to the queue for processing
func (cq *CommandQueue) PublishCommand(cmd CommandMessage) error {
	ctx := context.Background()

	// Generate ID if not set
	if cmd.ID == "" {
		cmd.ID = uuid.New().String()
	}
	if cmd.CreatedAt.IsZero() {
		cmd.CreatedAt = time.Now()
	}
	if cmd.RequestID == "" {
		cmd.RequestID = uuid.New().String()
	}

	cmdJSON, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("failed to marshal command: %w", err)
	}

	_, err = cq.redis.XAdd(ctx, &redis.XAddArgs{
		Stream: commandStreamKey,
		MaxLen: streamMaxLen,
		Approx: true,
		Values: map[string]interface{}{
			"command":   string(cmdJSON),
			"agentId":   cmd.AgentID,
			"type":      cmd.CommandType,
			"requestId": cmd.RequestID,
		},
	}).Result()

	if err != nil {
		return fmt.Errorf("failed to publish command: %w", err)
	}

	log.Printf("Published command %s for agent %s (type: %s)", cmd.ID, cmd.AgentID, cmd.CommandType)
	return nil
}

// PublishResponse publishes a command response
func (cq *CommandQueue) PublishResponse(resp ResponseMessage) error {
	ctx := context.Background()

	if resp.Timestamp.IsZero() {
		resp.Timestamp = time.Now()
	}

	respJSON, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("failed to marshal response: %w", err)
	}

	// Add to stream for persistence
	_, err = cq.redis.XAdd(ctx, &redis.XAddArgs{
		Stream: responseStreamKey,
		MaxLen: streamMaxLen,
		Approx: true,
		Values: map[string]interface{}{
			"response":  string(respJSON),
			"commandId": resp.CommandID,
			"requestId": resp.RequestID,
		},
	}).Result()

	if err != nil {
		return fmt.Errorf("failed to publish response: %w", err)
	}

	// Also publish to pub/sub for real-time notifications
	err = cq.redis.Publish(ctx, "sentinel:responses:pubsub", respJSON).Err()
	if err != nil {
		log.Printf("Warning: Failed to publish response to pub/sub: %v", err)
	}

	return nil
}

// StartConsumer begins consuming commands from the queue
func (cq *CommandQueue) StartConsumer(handler CommandHandler) {
	go cq.consumeLoop(handler)
}

// consumeLoop continuously reads and processes commands
func (cq *CommandQueue) consumeLoop(handler CommandHandler) {
	consumerName := cq.serverID

	for {
		select {
		case <-cq.ctx.Done():
			return
		default:
		}

		// Read from stream
		streams, err := cq.redis.XReadGroup(cq.ctx, &redis.XReadGroupArgs{
			Group:    consumerGroup,
			Consumer: consumerName,
			Streams:  []string{commandStreamKey, ">"},
			Count:    10,
			Block:    readBlockDuration,
		}).Result()

		if err != nil {
			if err == redis.Nil {
				continue
			}
			if cq.ctx.Err() != nil {
				return // Context cancelled
			}
			log.Printf("Error reading from command stream: %v", err)
			time.Sleep(time.Second)
			continue
		}

		for _, stream := range streams {
			for _, message := range stream.Messages {
				cq.processMessage(message, handler)
			}
		}
	}
}

// processMessage handles a single command message
func (cq *CommandQueue) processMessage(message redis.XMessage, handler CommandHandler) {
	cmdJSON, ok := message.Values["command"].(string)
	if !ok {
		cq.ackMessage(message.ID)
		return
	}

	var cmd CommandMessage
	if err := json.Unmarshal([]byte(cmdJSON), &cmd); err != nil {
		log.Printf("Failed to unmarshal command: %v", err)
		cq.ackMessage(message.ID)
		return
	}

	// Check if command is too old
	if time.Since(cmd.CreatedAt) > commandTTL {
		log.Printf("Skipping expired command %s (created: %s)", cmd.ID, cmd.CreatedAt)
		cq.ackMessage(message.ID)
		return
	}

	// Process the command
	if err := handler(cmd); err != nil {
		log.Printf("Failed to handle command %s: %v", cmd.ID, err)
		// Could implement retry logic here
	}

	cq.ackMessage(message.ID)
}

// ackMessage acknowledges a message has been processed
func (cq *CommandQueue) ackMessage(messageID string) {
	ctx := context.Background()
	cq.redis.XAck(ctx, commandStreamKey, consumerGroup, messageID)
}

// SubscribeResponses listens for command responses via pub/sub
func (cq *CommandQueue) SubscribeResponses(requestID string, handler ResponseHandler) func() {
	cq.respSubs[requestID] = handler

	// Start a subscriber if not already running
	go func() {
		pubsub := cq.redis.Subscribe(cq.ctx, "sentinel:responses:pubsub")
		defer pubsub.Close()

		ch := pubsub.Channel()
		for {
			select {
			case <-cq.ctx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}

				var resp ResponseMessage
				if err := json.Unmarshal([]byte(msg.Payload), &resp); err != nil {
					continue
				}

				// Check if we have a subscriber for this request
				if h, exists := cq.respSubs[resp.RequestID]; exists {
					h(resp)
				}
			}
		}
	}()

	// Return unsubscribe function
	return func() {
		delete(cq.respSubs, requestID)
	}
}

// WaitForResponse waits for a response to a specific request
func (cq *CommandQueue) WaitForResponse(requestID string, timeout time.Duration) (*ResponseMessage, error) {
	ctx, cancel := context.WithTimeout(cq.ctx, timeout)
	defer cancel()

	responseChan := make(chan ResponseMessage, 1)

	unsubscribe := cq.SubscribeResponses(requestID, func(resp ResponseMessage) {
		select {
		case responseChan <- resp:
		default:
		}
	})
	defer unsubscribe()

	select {
	case resp := <-responseChan:
		return &resp, nil
	case <-ctx.Done():
		return nil, fmt.Errorf("timeout waiting for response to request %s", requestID)
	}
}

// GetPendingCommands returns commands that haven't been acknowledged
func (cq *CommandQueue) GetPendingCommands() ([]CommandMessage, error) {
	ctx := context.Background()

	pending, err := cq.redis.XPendingExt(ctx, &redis.XPendingExtArgs{
		Stream: commandStreamKey,
		Group:  consumerGroup,
		Start:  "-",
		End:    "+",
		Count:  100,
	}).Result()

	if err != nil {
		return nil, fmt.Errorf("failed to get pending commands: %w", err)
	}

	var commands []CommandMessage
	for _, p := range pending {
		// Claim old messages that were abandoned
		if p.Idle > 5*time.Minute {
			messages, err := cq.redis.XClaim(ctx, &redis.XClaimArgs{
				Stream:   commandStreamKey,
				Group:    consumerGroup,
				Consumer: cq.serverID,
				MinIdle:  5 * time.Minute,
				Messages: []string{p.ID},
			}).Result()

			if err != nil {
				continue
			}

			for _, msg := range messages {
				cmdJSON, ok := msg.Values["command"].(string)
				if !ok {
					continue
				}

				var cmd CommandMessage
				if err := json.Unmarshal([]byte(cmdJSON), &cmd); err == nil {
					commands = append(commands, cmd)
				}
			}
		}
	}

	return commands, nil
}

// Stats returns queue statistics
func (cq *CommandQueue) Stats() (map[string]interface{}, error) {
	ctx := context.Background()

	cmdLen, _ := cq.redis.XLen(ctx, commandStreamKey).Result()
	respLen, _ := cq.redis.XLen(ctx, responseStreamKey).Result()

	pending, _ := cq.redis.XPending(ctx, commandStreamKey, consumerGroup).Result()

	return map[string]interface{}{
		"command_stream_length":  cmdLen,
		"response_stream_length": respLen,
		"pending_commands":       pending.Count,
		"server_id":              cq.serverID,
	}, nil
}

// Close shuts down the command queue
func (cq *CommandQueue) Close() {
	log.Printf("Shutting down command queue for server: %s", cq.serverID)
	cq.cancel()
}
