package models

import (
	"time"

	"github.com/coachbuddy/gateway-go/internal/contract"
)

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

// OrchestrateRequest is what the gateway sends to agents-python's
// POST /api/v1/orchestrate. Input is the coach's request as the contract
// describes it; the routing config carries decrypted keys and is never public.
type OrchestrateRequest struct {
	PlanID        string                         `json:"planId"`
	CoachID       string                         `json:"coachId"`
	ImageRef      string                         `json:"imageRef"`
	CreatedAt     string                         `json:"createdAt"`
	Input         contract.CreatePlanRequest     `json:"input"`
	RoutingConfig map[string]EphemeralAgentRoute `json:"routingConfig"`
}

// RegenerateRequest is what the gateway sends to agents-python's
// POST /api/v1/regenerate: change one section of the stored plan. Plan is the
// plan at Revision-1, with every section it has.
type RegenerateRequest struct {
	PlanID        string                         `json:"planId"`
	CoachID       string                         `json:"coachId"`
	Revision      int                            `json:"revision"`
	Section       string                         `json:"section"`
	Instruction   string                         `json:"instruction"`
	Plan          map[string]interface{}         `json:"plan"`
	RoutingConfig map[string]EphemeralAgentRoute `json:"routingConfig"`
}

// PlanRevision is one revision of a plan (MongoDB plan_revisions): what changed,
// the sections it built, its state, and the plan as it ended (nil while running).
type PlanRevision struct {
	PlanID    string
	CoachID   string
	Revision  int
	CreatedAt string
	Change    contract.RevisionChange
	Sections  []string
	State     string // running, done, failed
	Plan      map[string]interface{}
}

// PlanImage is a plan's screenshot, stored once in MongoDB under Ref
// (the plan's input.imageRef).
type PlanImage struct {
	Ref      string
	CoachID  string
	MimeType string
	Data     []byte
}

// AgentRuns is what ran for one revision of a plan: the agent traces that
// agents-python returns as agentRuns. Stored apart from the plan.
type AgentRuns struct {
	PlanID                 string
	CoachID                string
	Revision               int
	Runs                   interface{}
	OrchestrationLatencyMs interface{}
}

// AgentRunsRecord is one stored AgentRuns as Settings lists it, with the plan's
// WOD title and date so the coach can tell the runs apart.
type AgentRunsRecord struct {
	PlanID                 string      `json:"planId"`
	Revision               int         `json:"revision"`
	CreatedAt              time.Time   `json:"createdAt"`
	PlanTitle              string      `json:"planTitle"`
	WodDate                string      `json:"wodDate"`
	Runs                   interface{} `json:"runs"`
	OrchestrationLatencyMs interface{} `json:"orchestrationLatencyMs"`
}

type SharedLink struct {
	ShortCode         string     `json:"shortCode"`
	ShareURL          string     `json:"shareUrl"`
	PlanID            string     `json:"planId"` // column mongo_session_id
	CoachID           string     `json:"coachId"`
	IncludeCoachNotes bool       `json:"includeCoachNotes"`
	QRSVG             string     `json:"qrSvg"`
	ExpiresAt         *time.Time `json:"expiresAt,omitempty"`
	ViewCount         int        `json:"viewCount"`
	CreatedAt         time.Time  `json:"createdAt"`
}
