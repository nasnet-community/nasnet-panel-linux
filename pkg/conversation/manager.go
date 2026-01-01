package conversation

import (
	"errors"
	"sync"
	"time"

	"gorm.io/gorm"
)

// StateManager manages conversation states for all users
type StateManager struct {
	db *gorm.DB
	mu sync.Mutex // Mutex to prevent race conditions on session read/write
}

// NewStateManager creates a new state manager
func NewStateManager(db *gorm.DB) *StateManager {
	return &StateManager{
		db: db,
	}
}

// GetSession retrieves or creates a session for a user.
// Uses a write lock because expired sessions trigger a DB write (reset).
func (m *StateManager) GetSession(userID int64) *UserSession {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.getSessionLocked(userID)
}

// getSessionLocked retrieves or creates a session for a user (caller must hold lock)
func (m *StateManager) getSessionLocked(userID int64) *UserSession {
	var entity SessionEntity

	// Try to find existing session
	err := m.db.First(&entity, userID).Error

	session := &UserSession{
		UserID: userID,
		Data:   make(map[string]interface{}),
	}

	if errors.Is(err, gorm.ErrRecordNotFound) {
		// New session
		session.State = StateIdle
		session.CreatedAt = time.Now()
		session.ExpiresAt = time.Now().Add(SessionTimeout)
	} else {
		// Existing session found
		session.State = ConversationState(entity.State)
		session.Data = entity.Data
		session.CreatedAt = entity.UpdatedAt
		// Calculate dynamic expiry based on UpdatedAt
		session.ExpiresAt = entity.UpdatedAt.Add(SessionTimeout)

		// Check expiry logic
		if time.Now().After(session.ExpiresAt) {
			wasActive := session.State != StateIdle
			session.Reset() // Resets struct fields
			session.JustExpired = wasActive
			m.saveSession(session) // Persist the reset state
		}
	}

	return session
}

// saveSession persists the UserSession to the Database
func (m *StateManager) saveSession(session *UserSession) {
	entity := SessionEntity{
		UserID:    session.UserID,
		State:     string(session.State),
		Data:      session.Data,
		UpdatedAt: time.Now(),
	}
	// Upsert (Insert or Update)
	m.db.Save(&entity)
}

// SetState sets the conversation state for a user
func (m *StateManager) SetState(userID int64, state ConversationState) {
	m.mu.Lock()
	defer m.mu.Unlock()
	session := m.getSessionLocked(userID)
	session.SetState(state)
	m.saveSession(session)
}

// GetState gets the current conversation state for a user
func (m *StateManager) GetState(userID int64) ConversationState {
	session := m.GetSession(userID)
	return session.State
}

// IsInConversation checks if a user is in an active conversation (not idle)
func (m *StateManager) IsInConversation(userID int64) bool {
	return m.GetState(userID) != StateIdle
}

// ResetSession resets a user's session to idle
func (m *StateManager) ResetSession(userID int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.db.Model(&SessionEntity{}).Where("user_id = ?", userID).Updates(map[string]interface{}{
		"state":      string(StateIdle),
		"data":       JSONMap{},
		"updated_at": time.Now(),
	})
}

// SetData stores data in a user's session
func (m *StateManager) SetData(userID int64, key string, value interface{}) {
	m.mu.Lock()
	defer m.mu.Unlock()
	session := m.getSessionLocked(userID)
	session.Set(key, value)
	m.saveSession(session)
}

// GetData retrieves data from a user's session
func (m *StateManager) GetData(userID int64, key string) (interface{}, bool) {
	session := m.GetSession(userID)
	return session.Get(key)
}

// GetStringData retrieves string data from a user's session
func (m *StateManager) GetStringData(userID int64, key string) string {
	session := m.GetSession(userID)
	return session.GetString(key)
}

// GetIntData retrieves int data from a user's session
func (m *StateManager) GetIntData(userID int64, key string) int {
	session := m.GetSession(userID)
	return session.GetInt(key)
}

// GetFloatData retrieves float data from a user's session
func (m *StateManager) GetFloatData(userID int64, key string) float64 {
	session := m.GetSession(userID)
	return session.GetFloat(key)
}

// GetUint retrieves uint data from a user's session
func (m *StateManager) GetUint(userID int64, key string) uint {
	session := m.GetSession(userID)
	return session.GetUint(key)
}

// StartConversation starts a new conversation for a user
func (m *StateManager) StartConversation(userID int64, state ConversationState) *UserSession {
	m.mu.Lock()
	defer m.mu.Unlock()
	session := NewSession(userID)
	session.SetState(state)
	m.saveSession(session)
	return session
}

// CleanupExpired removes expired sessions (call periodically)
func (m *StateManager) CleanupExpired() {
	m.mu.Lock()
	defer m.mu.Unlock()
	expiryTime := time.Now().Add(-SessionTimeout)
	m.db.Where("updated_at < ?", expiryTime).Delete(&SessionEntity{})
}
