// Package sessions provides persistence and management for terminal, RDP, and file transfer sessions.
// Sessions are stored in the database and can survive temporary disconnections.
package sessions

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SessionStatus represents the current state of a session
type SessionStatus string

const (
	StatusActive    SessionStatus = "active"
	StatusSuspended SessionStatus = "suspended"
	StatusClosed    SessionStatus = "closed"
)

// TransferStatus represents the state of a file transfer
type TransferStatus string

const (
	TransferPending    TransferStatus = "pending"
	TransferInProgress TransferStatus = "in_progress"
	TransferPaused     TransferStatus = "paused"
	TransferCompleted  TransferStatus = "completed"
	TransferFailed     TransferStatus = "failed"
	TransferCancelled  TransferStatus = "cancelled"
)

// TerminalSession represents a remote terminal session
type TerminalSession struct {
	ID               uuid.UUID     `json:"id"`
	DeviceID         uuid.UUID     `json:"deviceId"`
	AgentID          string        `json:"agentId"`
	UserID           uuid.UUID     `json:"userId"`
	OrganizationID   int     `json:"organizationId"`
	SessionID        string        `json:"sessionId"`
	Status           SessionStatus `json:"status"`
	Cols             int           `json:"cols"`
	Rows             int           `json:"rows"`
	ShellType        string        `json:"shellType,omitempty"`
	WorkingDirectory string        `json:"workingDirectory,omitempty"`
	CreatedAt        time.Time     `json:"createdAt"`
	LastActivityAt   time.Time     `json:"lastActivityAt"`
	SuspendedAt      *time.Time    `json:"suspendedAt,omitempty"`
	ClosedAt         *time.Time    `json:"closedAt,omitempty"`
	CloseReason      string        `json:"closeReason,omitempty"`
}

// RDPSession represents a remote desktop session
type RDPSession struct {
	ID             uuid.UUID              `json:"id"`
	DeviceID       uuid.UUID              `json:"deviceId"`
	AgentID        string                 `json:"agentId"`
	UserID         uuid.UUID              `json:"userId"`
	OrganizationID int                    `json:"organizationId"`
	SessionID      string                 `json:"sessionId"`
	Status         SessionStatus          `json:"status"`
	Width          int                    `json:"width,omitempty"`
	Height         int                    `json:"height,omitempty"`
	Quality        string                 `json:"quality"`
	CreatedAt      time.Time              `json:"createdAt"`
	LastActivityAt time.Time              `json:"lastActivityAt"`
	SuspendedAt    *time.Time             `json:"suspendedAt,omitempty"`
	ClosedAt       *time.Time             `json:"closedAt,omitempty"`
	Metadata       map[string]interface{} `json:"metadata,omitempty"`
}

// FileTransfer represents a file upload or download
type FileTransfer struct {
	ID               uuid.UUID      `json:"id"`
	DeviceID         uuid.UUID      `json:"deviceId"`
	AgentID          string         `json:"agentId"`
	UserID           uuid.UUID      `json:"userId"`
	OrganizationID   int      `json:"organizationId"`
	TransferID       string         `json:"transferId"`
	Operation        string         `json:"operation"` // "upload" or "download"
	RemotePath       string         `json:"remotePath"`
	LocalPath        string         `json:"localPath,omitempty"`
	FileName         string         `json:"fileName,omitempty"`
	FileSize         int64          `json:"fileSize,omitempty"`
	MimeType         string         `json:"mimeType,omitempty"`
	BytesTransferred int64          `json:"bytesTransferred"`
	ChunkSize        int            `json:"chunkSize"`
	TotalChunks      int            `json:"totalChunks,omitempty"`
	CompletedChunks  int            `json:"completedChunks"`
	Status           TransferStatus `json:"status"`
	ErrorMessage     string         `json:"errorMessage,omitempty"`
	Checksum         string         `json:"checksum,omitempty"`
	ChecksumPartial  string         `json:"checksumPartial,omitempty"`
	CreatedAt        time.Time      `json:"createdAt"`
	StartedAt        *time.Time     `json:"startedAt,omitempty"`
	CompletedAt      *time.Time     `json:"completedAt,omitempty"`
	LastChunkAt      *time.Time     `json:"lastChunkAt,omitempty"`
}

// SessionManager provides CRUD operations for all session types
type SessionManager struct {
	db *pgxpool.Pool
	mu sync.RWMutex

	// In-memory cache for fast lookups (synced with database)
	terminalSessions map[string]*TerminalSession // sessionId -> session
	rdpSessions      map[string]*RDPSession      // sessionId -> session
	fileTransfers    map[string]*FileTransfer    // transferId -> transfer
}

// NewSessionManager creates a new session manager
func NewSessionManager(db *pgxpool.Pool) *SessionManager {
	mgr := &SessionManager{
		db:               db,
		terminalSessions: make(map[string]*TerminalSession),
		rdpSessions:      make(map[string]*RDPSession),
		fileTransfers:    make(map[string]*FileTransfer),
	}

	// Load active sessions from database on startup
	go mgr.loadActiveSessions()

	return mgr
}

// loadActiveSessions loads all active sessions from the database into memory
func (m *SessionManager) loadActiveSessions() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Load terminal sessions
	rows, err := m.db.Query(ctx, `
		SELECT id, device_id, agent_id, user_id, organization_id, session_id,
		       status, cols, rows, shell_type, working_directory,
		       created_at, last_activity_at, suspended_at, closed_at, close_reason
		FROM terminal_sessions
		WHERE status IN ('active', 'suspended')
	`)
	if err != nil {
		log.Printf("[SessionManager] Failed to load terminal sessions: %v", err)
	} else {
		defer rows.Close()
		for rows.Next() {
			var s TerminalSession
			var shellType, workDir, closeReason sql.NullString
			if err := rows.Scan(
				&s.ID, &s.DeviceID, &s.AgentID, &s.UserID, &s.OrganizationID, &s.SessionID,
				&s.Status, &s.Cols, &s.Rows, &shellType, &workDir,
				&s.CreatedAt, &s.LastActivityAt, &s.SuspendedAt, &s.ClosedAt, &closeReason,
			); err != nil {
				log.Printf("[SessionManager] Error scanning terminal session: %v", err)
				continue
			}
			s.ShellType = shellType.String
			s.WorkingDirectory = workDir.String
			s.CloseReason = closeReason.String
			m.terminalSessions[s.SessionID] = &s
		}
		log.Printf("[SessionManager] Loaded %d terminal sessions", len(m.terminalSessions))
	}

	// Load RDP sessions
	rows, err = m.db.Query(ctx, `
		SELECT id, device_id, agent_id, user_id, organization_id, session_id,
		       status, width, height, quality, created_at, last_activity_at,
		       suspended_at, closed_at, metadata
		FROM rdp_sessions
		WHERE status IN ('active', 'suspended')
	`)
	if err != nil {
		log.Printf("[SessionManager] Failed to load RDP sessions: %v", err)
	} else {
		defer rows.Close()
		for rows.Next() {
			var s RDPSession
			var width, height sql.NullInt32
			var quality sql.NullString
			var metadata []byte
			if err := rows.Scan(
				&s.ID, &s.DeviceID, &s.AgentID, &s.UserID, &s.OrganizationID, &s.SessionID,
				&s.Status, &width, &height, &quality, &s.CreatedAt, &s.LastActivityAt,
				&s.SuspendedAt, &s.ClosedAt, &metadata,
			); err != nil {
				log.Printf("[SessionManager] Error scanning RDP session: %v", err)
				continue
			}
			s.Width = int(width.Int32)
			s.Height = int(height.Int32)
			s.Quality = quality.String
			if len(metadata) > 0 {
				json.Unmarshal(metadata, &s.Metadata)
			}
			m.rdpSessions[s.SessionID] = &s
		}
		log.Printf("[SessionManager] Loaded %d RDP sessions", len(m.rdpSessions))
	}

	// Load file transfers
	rows, err = m.db.Query(ctx, `
		SELECT id, device_id, agent_id, user_id, organization_id, transfer_id,
		       operation, remote_path, local_path, file_name, file_size, mime_type,
		       bytes_transferred, chunk_size, total_chunks, completed_chunks,
		       status, error_message, checksum, checksum_partial,
		       created_at, started_at, completed_at, last_chunk_at
		FROM file_transfers
		WHERE status IN ('pending', 'in_progress', 'paused')
	`)
	if err != nil {
		log.Printf("[SessionManager] Failed to load file transfers: %v", err)
	} else {
		defer rows.Close()
		for rows.Next() {
			var t FileTransfer
			var localPath, fileName, mimeType, errorMsg, checksum, checksumPartial sql.NullString
			var fileSize sql.NullInt64
			var totalChunks sql.NullInt32
			if err := rows.Scan(
				&t.ID, &t.DeviceID, &t.AgentID, &t.UserID, &t.OrganizationID, &t.TransferID,
				&t.Operation, &t.RemotePath, &localPath, &fileName, &fileSize, &mimeType,
				&t.BytesTransferred, &t.ChunkSize, &totalChunks, &t.CompletedChunks,
				&t.Status, &errorMsg, &checksum, &checksumPartial,
				&t.CreatedAt, &t.StartedAt, &t.CompletedAt, &t.LastChunkAt,
			); err != nil {
				log.Printf("[SessionManager] Error scanning file transfer: %v", err)
				continue
			}
			t.LocalPath = localPath.String
			t.FileName = fileName.String
			t.FileSize = fileSize.Int64
			t.MimeType = mimeType.String
			t.ErrorMessage = errorMsg.String
			t.Checksum = checksum.String
			t.ChecksumPartial = checksumPartial.String
			t.TotalChunks = int(totalChunks.Int32)
			m.fileTransfers[t.TransferID] = &t
		}
		log.Printf("[SessionManager] Loaded %d file transfers", len(m.fileTransfers))
	}
}

// ==================== Terminal Session Operations ====================

// CreateTerminalSession creates a new terminal session
func (m *SessionManager) CreateTerminalSession(ctx context.Context, deviceID, userID uuid.UUID, orgID int, agentID, sessionID string, cols, rows int) (*TerminalSession, error) {
	session := &TerminalSession{
		ID:             uuid.New(),
		DeviceID:       deviceID,
		AgentID:        agentID,
		UserID:         userID,
		OrganizationID: orgID,
		SessionID:      sessionID,
		Status:         StatusActive,
		Cols:           cols,
		Rows:           rows,
		CreatedAt:      time.Now(),
		LastActivityAt: time.Now(),
	}

	_, err := m.db.Exec(ctx, `
		INSERT INTO terminal_sessions (id, device_id, agent_id, user_id, organization_id, session_id, status, cols, rows, created_at, last_activity_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`, session.ID, session.DeviceID, session.AgentID, session.UserID, session.OrganizationID,
		session.SessionID, session.Status, session.Cols, session.Rows, session.CreatedAt, session.LastActivityAt)

	if err != nil {
		return nil, fmt.Errorf("failed to create terminal session: %w", err)
	}

	m.mu.Lock()
	m.terminalSessions[sessionID] = session
	m.mu.Unlock()

	log.Printf("[SessionManager] Created terminal session: %s for device %s", sessionID, deviceID)
	return session, nil
}

// GetTerminalSession retrieves a terminal session by session ID
func (m *SessionManager) GetTerminalSession(sessionID string) (*TerminalSession, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	session, ok := m.terminalSessions[sessionID]
	return session, ok
}

// GetTerminalSessionsByAgent returns all active terminal sessions for an agent
func (m *SessionManager) GetTerminalSessionsByAgent(agentID string) []*TerminalSession {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var sessions []*TerminalSession
	for _, s := range m.terminalSessions {
		if s.AgentID == agentID && s.Status == StatusActive {
			sessions = append(sessions, s)
		}
	}
	return sessions
}

// UpdateTerminalActivity updates the last activity timestamp
func (m *SessionManager) UpdateTerminalActivity(ctx context.Context, sessionID string) error {
	m.mu.Lock()
	if session, ok := m.terminalSessions[sessionID]; ok {
		session.LastActivityAt = time.Now()
	}
	m.mu.Unlock()

	_, err := m.db.Exec(ctx, `
		UPDATE terminal_sessions SET last_activity_at = NOW()
		WHERE session_id = $1
	`, sessionID)
	return err
}

// SuspendTerminalSession marks a session as suspended (e.g., on agent disconnect)
func (m *SessionManager) SuspendTerminalSession(ctx context.Context, sessionID, reason string) error {
	now := time.Now()

	m.mu.Lock()
	if session, ok := m.terminalSessions[sessionID]; ok {
		session.Status = StatusSuspended
		session.SuspendedAt = &now
	}
	m.mu.Unlock()

	_, err := m.db.Exec(ctx, `
		UPDATE terminal_sessions
		SET status = 'suspended', suspended_at = $2, close_reason = $3
		WHERE session_id = $1
	`, sessionID, now, reason)

	if err == nil {
		log.Printf("[SessionManager] Suspended terminal session: %s (%s)", sessionID, reason)
	}
	return err
}

// CloseTerminalSession closes a terminal session
func (m *SessionManager) CloseTerminalSession(ctx context.Context, sessionID, reason string) error {
	now := time.Now()

	m.mu.Lock()
	if session, ok := m.terminalSessions[sessionID]; ok {
		session.Status = StatusClosed
		session.ClosedAt = &now
		session.CloseReason = reason
		delete(m.terminalSessions, sessionID)
	}
	m.mu.Unlock()

	_, err := m.db.Exec(ctx, `
		UPDATE terminal_sessions
		SET status = 'closed', closed_at = $2, close_reason = $3
		WHERE session_id = $1
	`, sessionID, now, reason)

	if err == nil {
		log.Printf("[SessionManager] Closed terminal session: %s (%s)", sessionID, reason)
	}
	return err
}

// ReactivateTerminalSession reactivates a suspended session
func (m *SessionManager) ReactivateTerminalSession(ctx context.Context, sessionID string) error {
	m.mu.Lock()
	if session, ok := m.terminalSessions[sessionID]; ok {
		session.Status = StatusActive
		session.SuspendedAt = nil
		session.LastActivityAt = time.Now()
	}
	m.mu.Unlock()

	_, err := m.db.Exec(ctx, `
		UPDATE terminal_sessions
		SET status = 'active', suspended_at = NULL, last_activity_at = NOW()
		WHERE session_id = $1
	`, sessionID)

	if err == nil {
		log.Printf("[SessionManager] Reactivated terminal session: %s", sessionID)
	}
	return err
}

// ==================== RDP Session Operations ====================

// CreateRDPSession creates a new RDP session
func (m *SessionManager) CreateRDPSession(ctx context.Context, deviceID, userID uuid.UUID, orgID int, agentID, sessionID string, width, height int, quality string) (*RDPSession, error) {
	session := &RDPSession{
		ID:             uuid.New(),
		DeviceID:       deviceID,
		AgentID:        agentID,
		UserID:         userID,
		OrganizationID: orgID,
		SessionID:      sessionID,
		Status:         StatusActive,
		Width:          width,
		Height:         height,
		Quality:        quality,
		CreatedAt:      time.Now(),
		LastActivityAt: time.Now(),
		Metadata:       make(map[string]interface{}),
	}

	metadata, _ := json.Marshal(session.Metadata)

	_, err := m.db.Exec(ctx, `
		INSERT INTO rdp_sessions (id, device_id, agent_id, user_id, organization_id, session_id, status, width, height, quality, created_at, last_activity_at, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`, session.ID, session.DeviceID, session.AgentID, session.UserID, session.OrganizationID,
		session.SessionID, session.Status, session.Width, session.Height, session.Quality,
		session.CreatedAt, session.LastActivityAt, metadata)

	if err != nil {
		return nil, fmt.Errorf("failed to create RDP session: %w", err)
	}

	m.mu.Lock()
	m.rdpSessions[sessionID] = session
	m.mu.Unlock()

	log.Printf("[SessionManager] Created RDP session: %s for device %s", sessionID, deviceID)
	return session, nil
}

// GetRDPSession retrieves an RDP session by session ID
func (m *SessionManager) GetRDPSession(sessionID string) (*RDPSession, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	session, ok := m.rdpSessions[sessionID]
	return session, ok
}

// CloseRDPSession closes an RDP session
func (m *SessionManager) CloseRDPSession(ctx context.Context, sessionID string) error {
	now := time.Now()

	m.mu.Lock()
	if session, ok := m.rdpSessions[sessionID]; ok {
		session.Status = StatusClosed
		session.ClosedAt = &now
		delete(m.rdpSessions, sessionID)
	}
	m.mu.Unlock()

	_, err := m.db.Exec(ctx, `
		UPDATE rdp_sessions
		SET status = 'closed', closed_at = $2
		WHERE session_id = $1
	`, sessionID, now)

	if err == nil {
		log.Printf("[SessionManager] Closed RDP session: %s", sessionID)
	}
	return err
}

// ==================== File Transfer Operations ====================

// CreateFileTransfer creates a new file transfer record
func (m *SessionManager) CreateFileTransfer(ctx context.Context, deviceID, userID uuid.UUID, orgID int, agentID, transferID, operation, remotePath string, fileSize int64) (*FileTransfer, error) {
	transfer := &FileTransfer{
		ID:             uuid.New(),
		DeviceID:       deviceID,
		AgentID:        agentID,
		UserID:         userID,
		OrganizationID: orgID,
		TransferID:     transferID,
		Operation:      operation,
		RemotePath:     remotePath,
		FileSize:       fileSize,
		ChunkSize:      65536,
		Status:         TransferPending,
		CreatedAt:      time.Now(),
	}

	_, err := m.db.Exec(ctx, `
		INSERT INTO file_transfers (id, device_id, agent_id, user_id, organization_id, transfer_id, operation, remote_path, file_size, chunk_size, status, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`, transfer.ID, transfer.DeviceID, transfer.AgentID, transfer.UserID, transfer.OrganizationID,
		transfer.TransferID, transfer.Operation, transfer.RemotePath, transfer.FileSize,
		transfer.ChunkSize, transfer.Status, transfer.CreatedAt)

	if err != nil {
		return nil, fmt.Errorf("failed to create file transfer: %w", err)
	}

	m.mu.Lock()
	m.fileTransfers[transferID] = transfer
	m.mu.Unlock()

	log.Printf("[SessionManager] Created file transfer: %s (%s %s)", transferID, operation, remotePath)
	return transfer, nil
}

// UpdateTransferProgress updates transfer progress
func (m *SessionManager) UpdateTransferProgress(ctx context.Context, transferID string, bytesTransferred int64, completedChunks int) error {
	now := time.Now()

	m.mu.Lock()
	if transfer, ok := m.fileTransfers[transferID]; ok {
		transfer.BytesTransferred = bytesTransferred
		transfer.CompletedChunks = completedChunks
		transfer.LastChunkAt = &now
		if transfer.Status == TransferPending {
			transfer.Status = TransferInProgress
			transfer.StartedAt = &now
		}
	}
	m.mu.Unlock()

	_, err := m.db.Exec(ctx, `
		UPDATE file_transfers
		SET bytes_transferred = $2, completed_chunks = $3, last_chunk_at = $4,
		    status = CASE WHEN status = 'pending' THEN 'in_progress' ELSE status END,
		    started_at = CASE WHEN started_at IS NULL THEN $4 ELSE started_at END
		WHERE transfer_id = $1
	`, transferID, bytesTransferred, completedChunks, now)

	return err
}

// CompleteFileTransfer marks a transfer as completed
func (m *SessionManager) CompleteFileTransfer(ctx context.Context, transferID, checksum string) error {
	now := time.Now()

	m.mu.Lock()
	if transfer, ok := m.fileTransfers[transferID]; ok {
		transfer.Status = TransferCompleted
		transfer.CompletedAt = &now
		transfer.Checksum = checksum
		delete(m.fileTransfers, transferID)
	}
	m.mu.Unlock()

	_, err := m.db.Exec(ctx, `
		UPDATE file_transfers
		SET status = 'completed', completed_at = $2, checksum = $3
		WHERE transfer_id = $1
	`, transferID, now, checksum)

	if err == nil {
		log.Printf("[SessionManager] Completed file transfer: %s", transferID)
	}
	return err
}

// FailFileTransfer marks a transfer as failed
func (m *SessionManager) FailFileTransfer(ctx context.Context, transferID, errorMessage string) error {
	m.mu.Lock()
	if transfer, ok := m.fileTransfers[transferID]; ok {
		transfer.Status = TransferFailed
		transfer.ErrorMessage = errorMessage
		delete(m.fileTransfers, transferID)
	}
	m.mu.Unlock()

	_, err := m.db.Exec(ctx, `
		UPDATE file_transfers
		SET status = 'failed', error_message = $2
		WHERE transfer_id = $1
	`, transferID, errorMessage)

	if err == nil {
		log.Printf("[SessionManager] Failed file transfer: %s (%s)", transferID, errorMessage)
	}
	return err
}

// ==================== Agent Recovery Operations ====================

// SuspendAgentSessions suspends all active sessions for an agent (on disconnect)
func (m *SessionManager) SuspendAgentSessions(ctx context.Context, agentID string) error {
	reason := "agent_disconnected"

	m.mu.Lock()
	for id, s := range m.terminalSessions {
		if s.AgentID == agentID && s.Status == StatusActive {
			s.Status = StatusSuspended
			now := time.Now()
			s.SuspendedAt = &now
			log.Printf("[SessionManager] Suspending terminal session %s (agent disconnect)", id)
		}
	}
	for id, s := range m.rdpSessions {
		if s.AgentID == agentID && s.Status == StatusActive {
			s.Status = StatusSuspended
			now := time.Now()
			s.SuspendedAt = &now
			log.Printf("[SessionManager] Suspending RDP session %s (agent disconnect)", id)
		}
	}
	m.mu.Unlock()

	_, err := m.db.Exec(ctx, `
		UPDATE terminal_sessions
		SET status = 'suspended', suspended_at = NOW(), close_reason = $2
		WHERE agent_id = $1 AND status = 'active'
	`, agentID, reason)
	if err != nil {
		return fmt.Errorf("failed to suspend terminal sessions: %w", err)
	}

	_, err = m.db.Exec(ctx, `
		UPDATE rdp_sessions
		SET status = 'suspended', suspended_at = NOW()
		WHERE agent_id = $1 AND status = 'active'
	`, agentID)
	if err != nil {
		return fmt.Errorf("failed to suspend RDP sessions: %w", err)
	}

	log.Printf("[SessionManager] Suspended all sessions for agent %s", agentID)
	return nil
}

// GetRecoverableSessions returns sessions that can be recovered for an agent
func (m *SessionManager) GetRecoverableSessions(agentID string) (terminals []*TerminalSession, rdps []*RDPSession) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for _, s := range m.terminalSessions {
		if s.AgentID == agentID && (s.Status == StatusActive || s.Status == StatusSuspended) {
			terminals = append(terminals, s)
		}
	}
	for _, s := range m.rdpSessions {
		if s.AgentID == agentID && (s.Status == StatusActive || s.Status == StatusSuspended) {
			rdps = append(rdps, s)
		}
	}
	return
}

// ==================== Cleanup Operations ====================

// RunCleanup executes the cleanup function to close stale sessions
func (m *SessionManager) RunCleanup(ctx context.Context) (terminalClosed, rdpClosed, transfersFailed int, err error) {
	row := m.db.QueryRow(ctx, `SELECT * FROM cleanup_stale_sessions()`)
	err = row.Scan(&terminalClosed, &rdpClosed, &transfersFailed)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("cleanup failed: %w", err)
	}

	// Refresh in-memory cache
	if terminalClosed > 0 || rdpClosed > 0 || transfersFailed > 0 {
		go m.loadActiveSessions()
		log.Printf("[SessionManager] Cleanup: %d terminals, %d RDP, %d transfers",
			terminalClosed, rdpClosed, transfersFailed)
	}

	return
}

// GetSessionCounts returns counts of active sessions
func (m *SessionManager) GetSessionCounts() (terminals, rdps, transfers int) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.terminalSessions), len(m.rdpSessions), len(m.fileTransfers)
}
