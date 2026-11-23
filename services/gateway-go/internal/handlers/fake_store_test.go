package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/coachbuddy/gateway-go/internal/modelcatalog"
	"github.com/coachbuddy/gateway-go/internal/models"
	"github.com/coachbuddy/gateway-go/internal/store"
)

// fakeStore is an in-memory Store for handler tests only. Setting fail makes
// every storage call return that error, to test how handlers report a
// database failure. pingErrs sets the result of Ping per database.
type fakeStore struct {
	mu          sync.Mutex
	plans       map[string]map[string]interface{}
	images      map[string]*models.PlanImage
	runs        map[string]*models.AgentRuns
	revisions   map[string]map[int]*models.PlanRevision
	links       map[string]*models.SharedLink
	events      map[string][]string
	subscribers map[string][]chan string
	routes      map[string]models.EphemeralAgentRoute
	fail        error
	pingErrs    map[string]error
	// Errors for a single call, to fail one step while the others work.
	createPlanErr error
	savePlanErr   error
	eventsErr     error
	// savedPlans records every SavePlan, to check progress was stored.
	savedPlans []map[string]interface{}
}

var _ Store = (*fakeStore)(nil)
var _ Store = (*store.DataStore)(nil)

func newFakeStore() *fakeStore {
	return &fakeStore{
		plans:       map[string]map[string]interface{}{},
		images:      map[string]*models.PlanImage{},
		runs:        map[string]*models.AgentRuns{},
		revisions:   map[string]map[int]*models.PlanRevision{},
		links:       map[string]*models.SharedLink{},
		events:      map[string][]string{},
		subscribers: map[string][]chan string{},
		routes: map[string]models.EphemeralAgentRoute{
			"global": {Provider: "gemini", APIKey: "test-key", Model: modelcatalog.Embedded().Default().ID},
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

func (f *fakeStore) Models() *modelcatalog.Catalog { return modelcatalog.Embedded() }

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

func (f *fakeStore) CreatePlan(_ context.Context, doc map[string]interface{}) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	if f.createPlanErr != nil {
		return f.createPlanErr
	}
	id, _ := doc["planId"].(string)
	f.plans[id] = copyDoc(doc)
	return nil
}

func (f *fakeStore) SavePlan(_ context.Context, id string, doc map[string]interface{}) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	if f.savePlanErr != nil {
		return f.savePlanErr
	}
	// Like MongoDB's filter on _id and revision: a deleted plan, or one that has
	// moved to another revision, is not written.
	if cur, ok := f.plans[id]; !ok || store.RevisionOf(cur) != store.RevisionOf(doc) {
		return store.ErrPlanNotFound
	}
	f.plans[id] = copyDoc(doc)
	f.savedPlans = append(f.savedPlans, copyDoc(doc))
	return nil
}

func (f *fakeStore) AdvancePlan(_ context.Context, id string, from int, doc map[string]interface{}) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	cur, ok := f.plans[id]
	if !ok {
		return store.ErrPlanNotFound
	}
	if store.RevisionOf(cur) != from {
		return store.ErrRevisionConflict
	}
	f.plans[id] = copyDoc(doc)
	return nil
}

func (f *fakeStore) SavePlanRevision(_ context.Context, rev *models.PlanRevision) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	cp := *rev
	if rev.Plan != nil {
		cp.Plan = copyDoc(rev.Plan)
	}
	if f.revisions[rev.PlanID] == nil {
		f.revisions[rev.PlanID] = map[int]*models.PlanRevision{}
	}
	f.revisions[rev.PlanID][rev.Revision] = &cp
	return nil
}

func (f *fakeStore) ListPlanRevisions(_ context.Context, id string) ([]*models.PlanRevision, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return nil, f.fail
	}
	var out []*models.PlanRevision
	for n := 1; n <= len(f.revisions[id])+64; n++ {
		if rev, ok := f.revisions[id][n]; ok {
			cp := *rev
			cp.Plan = nil
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (f *fakeStore) GetPlanRevision(_ context.Context, id string, n int) (*models.PlanRevision, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return nil, f.fail
	}
	rev, ok := f.revisions[id][n]
	if !ok {
		return nil, store.ErrRevisionNotFound
	}
	cp := *rev
	if rev.Plan != nil {
		cp.Plan = copyDoc(rev.Plan)
	}
	return &cp, nil
}

func (f *fakeStore) GetPlan(_ context.Context, id string) (map[string]interface{}, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return nil, f.fail
	}
	doc, ok := f.plans[id]
	if !ok {
		return nil, store.ErrPlanNotFound
	}
	return copyDoc(doc), nil
}

func (f *fakeStore) ListPlans(_ context.Context, coachID string) ([]map[string]interface{}, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return nil, f.fail
	}
	var out []map[string]interface{}
	for _, d := range f.plans {
		if d["coachId"] == coachID {
			out = append(out, copyDoc(d))
		}
	}
	return out, nil
}

func (f *fakeStore) DeletePlan(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	if _, ok := f.plans[id]; !ok {
		return store.ErrPlanNotFound
	}
	delete(f.plans, id)
	delete(f.revisions, id)
	return nil
}

func (f *fakeStore) SavePlanImage(_ context.Context, img *models.PlanImage) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	cp := *img
	f.images[img.Ref] = &cp
	return nil
}

func (f *fakeStore) SaveAgentRuns(_ context.Context, runs *models.AgentRuns) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	cp := *runs
	f.runs[runs.PlanID] = &cp
	return nil
}

// ListAgentRuns returns the coach's stored runs (one per plan in the fake), at most limit.
func (f *fakeStore) ListAgentRuns(_ context.Context, coachID string, limit int) ([]*models.AgentRunsRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return nil, f.fail
	}
	out := []*models.AgentRunsRecord{}
	for _, r := range f.runs {
		if r.CoachID == coachID && len(out) < limit {
			out = append(out, &models.AgentRunsRecord{PlanID: r.PlanID, Revision: r.Revision, Runs: r.Runs, OrchestrationLatencyMs: r.OrchestrationLatencyMs})
		}
	}
	return out, nil
}

// PublishPlanEvent stores the event and sends it to open subscriptions, like Redis.
func (f *fakeStore) PublishPlanEvent(_ context.Context, id string, event []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	f.events[id] = append(f.events[id], string(event))
	for _, ch := range f.subscribers[id] {
		ch <- string(event)
	}
	return nil
}

// PlanEvents returns the stored events as buffered and the later ones live.
func (f *fakeStore) PlanEvents(_ context.Context, id string) (*store.EventStream, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return nil, f.fail
	}
	if f.eventsErr != nil {
		return nil, f.eventsErr
	}
	ch := make(chan string, 64)
	f.subscribers[id] = append(f.subscribers[id], ch)
	var once sync.Once
	return &store.EventStream{
		Buffered: append([]string(nil), f.events[id]...),
		Live:     ch,
		Close: func() {
			once.Do(func() {
				f.mu.Lock()
				defer f.mu.Unlock()
				subs := f.subscribers[id]
				for i, c := range subs {
					if c == ch {
						f.subscribers[id] = append(subs[:i], subs[i+1:]...)
						break
					}
				}
				close(ch)
			})
		},
	}, nil
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
