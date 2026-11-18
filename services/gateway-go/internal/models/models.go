package models

import "time"

type Coach struct {
	ID                      string    `json:"id"`
	Email                   string    `json:"email"`
	PasswordHash            string    `json:"-"`
	DisplayName             string    `json:"displayName"`
	BoxName                 string    `json:"boxName"`
	DefaultClassDurationMin int       `json:"defaultClassDurationMin"`
	CreatedAt               time.Time `json:"createdAt"`
}

type LLMAPIKey struct {
	ID           string    `json:"id"`
	CoachID      string    `json:"coachId"`
	Provider     string    `json:"provider"` // a provider id from internal/modelcatalog
	EncryptedKey []byte    `json:"-"`
	Nonce        []byte    `json:"-"`
	KeyHint      string    `json:"keyHint"`
	IsPrimary    bool      `json:"isPrimary"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type AgentModelPref struct {
	ID              string    `json:"id"`
	CoachID         string    `json:"coachId"`
	AgentRole       string    `json:"agentRole"` // one of store.AgentRoles
	Provider        string    `json:"provider"`
	ModelIdentifier string    `json:"modelIdentifier"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

type EphemeralAgentRoute struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`
	APIKey   string `json:"apiKey"`
}

type SessionInput struct {
	Title                 string   `json:"title,omitempty"`
	RawText               string   `json:"rawText"`
	ImageBase64           string   `json:"imageBase64,omitempty"`
	CoachConstraints      string   `json:"coachConstraints"`
	TargetDurationMinutes int      `json:"targetDurationMinutes"`
	EquipmentFocus        []string `json:"equipmentFocus,omitempty"`
}

type OrchestrateRequest struct {
	SessionID     string                         `json:"sessionId"`
	CoachID       string                         `json:"coachId"`
	Input         SessionInput                   `json:"input"`
	RoutingConfig map[string]EphemeralAgentRoute `json:"routingConfig"`
}

type SharedLink struct {
	ShortCode         string     `json:"shortCode"`
	ShareURL          string     `json:"shareUrl"`
	MongoSessionID    string     `json:"sessionId"`
	CoachID           string     `json:"coachId"`
	IncludeCoachNotes bool       `json:"includeCoachNotes"`
	QRSVG             string     `json:"qrSvg"`
	ExpiresAt         *time.Time `json:"expiresAt,omitempty"`
	ViewCount         int        `json:"viewCount"`
	CreatedAt         time.Time  `json:"createdAt"`
}
