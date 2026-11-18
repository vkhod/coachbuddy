package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/coachbuddy/gateway-go/internal/models"
	"github.com/coachbuddy/gateway-go/internal/store"
)

// fakeStore is an in-memory Store for handler tests only. Setting fail makes
// every storage call return that error, to test how handlers report a
// database failure. pingErrs sets the result of Ping per database.
type fakeStore struct {
	mu       sync.Mutex
	sessions map[string]map[string]interface{}
	links    map[string]*models.SharedLink
	events   map[string][]string
	routes   map[string]models.EphemeralAgentRoute
	fail     error
	pingErrs map[string]error
	// Errors for a single call, to fail one step while the others work.
	saveSessionErr error
	eventsErr      error
}

var _ Store = (*fakeStore)(nil)
var _ Store = (*store.DataStore)(nil)

func newFakeStore() *fakeStore {
	return &fakeStore{
		sessions: map[string]map[string]interface{}{},
		links:    map[string]*models.SharedLink{},
		events:   map[string][]string{},
		routes: map[string]models.EphemeralAgentRoute{
			"global": {Provider: "gemini", APIKey: "test-key", Model: store.DefaultModel},
		},
		pingErrs: map[string]error{},
	}
}

var errNotInFake = errors.New("not implemented in fakeStore")

func (f *fakeStore) Ping(context.Context) map[string]error {
	out := map[string]error{"postgres": nil, "mongodb": nil, "redis": nil}
	for k, v := range f.pingErrs {
		out[k] = v
	}
	return out
}

func (f *fakeStore) CreateCoach(context.Context, string, string, string, string, int) (*models.Coach, error) {
	return nil, errNotInFake
}
func (f *fakeStore) AuthenticateCoach(context.Context, string, string) (*models.Coach, error) {
	return nil, errNotInFake
}
func (f *fakeStore) GetCoach(context.Context, string) (*models.Coach, error) {
	return nil, errNotInFake
}
func (f *fakeStore) UpdateCoachProfile(context.Context, string, string, string, int) (*models.Coach, error) {
	return nil, errNotInFake
}
func (f *fakeStore) SaveAPIKey(context.Context, string, string, string, bool) (*models.LLMAPIKey, error) {
	return nil, errNotInFake
}
func (f *fakeStore) ListAPIKeys(context.Context, string) ([]*models.LLMAPIKey, error) {
	return nil, errNotInFake
}
func (f *fakeStore) DeleteAPIKey(context.Context, string, string) error { return errNotInFake }
func (f *fakeStore) GetAgentPrefs(context.Context, string) ([]*models.AgentModelPref, error) {
	return nil, errNotInFake
}
func (f *fakeStore) SaveAgentPrefs(context.Context, string, []models.AgentModelPref) ([]*models.AgentModelPref, error) {
	return nil, errNotInFake
}

func (f *fakeStore) BuildEphemeralRoutingConfig(context.Context, string) (map[string]models.EphemeralAgentRoute, error) {
	if f.fail != nil {
		return nil, f.fail
	}
	return f.routes, nil
}

// copyDoc round-trips through JSON, as a real database would.
func copyDoc(doc map[string]interface{}) map[string]interface{} {
	raw, _ := json.Marshal(doc)
	var out map[string]interface{}
	_ = json.Unmarshal(raw, &out)
	return out
}

func (f *fakeStore) SaveSession(_ context.Context, id string, doc map[string]interface{}) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	if f.saveSessionErr != nil {
		return f.saveSessionErr
	}
	doc["_id"], doc["sessionId"] = id, id
	f.sessions[id] = copyDoc(doc)
	return nil
}

func (f *fakeStore) GetSession(_ context.Context, id string) (map[string]interface{}, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return nil, f.fail
	}
	doc, ok := f.sessions[id]
	if !ok {
		return nil, store.ErrSessionNotFound
	}
	return copyDoc(doc), nil
}

func (f *fakeStore) ListSessions(context.Context, string) ([]map[string]interface{}, error) {
	return nil, errNotInFake
}

func (f *fakeStore) DeleteSession(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	if _, ok := f.sessions[id]; !ok {
		return store.ErrSessionNotFound
	}
	delete(f.sessions, id)
	return nil
}

// SessionEvents returns the stored events as buffered and a closed live channel.
func (f *fakeStore) SessionEvents(_ context.Context, id string) (*store.EventStream, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return nil, f.fail
	}
	if f.eventsErr != nil {
		return nil, f.eventsErr
	}
	live := make(chan string)
	close(live)
	return &store.EventStream{Buffered: append([]string(nil), f.events[id]...), Live: live, Close: func() {}}, nil
}

func (f *fakeStore) SaveSharedLink(_ context.Context, link *models.SharedLink) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	cp := *link
	cp.CreatedAt = time.Now()
	f.links[link.ShortCode] = &cp
	return nil
}

func (f *fakeStore) GetSharedLink(_ context.Context, code string) (*models.SharedLink, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return nil, f.fail
	}
	link, ok := f.links[code]
	if !ok || (link.ExpiresAt != nil && time.Now().After(*link.ExpiresAt)) {
		return nil, store.ErrLinkNotFound
	}
	link.ViewCount++
	cp := *link
	return &cp, nil
}
