package handlers

// Asynchronous plan generation (Phase 2 step 10).
//
//	POST /api/v1/plans               202 {planId}; generation runs in the background
//	GET  /api/v1/plans/{id}/events   SSE: one PlanEvent per section, then "plan"
//	GET  /api/v1/plans/{id}          the plan as it stands now
//
// Refinement and versions (step 11) are in revisions.go. Both run through the
// same background job (planJob), one revision at a time.
//
// The plan document, with its owner, is saved before the 202, so the owner
// check on the event stream works from the first event. agents-python publishes
// one event per section, each carrying the plan as it stands after it; the
// gateway stores those snapshots as progress, saves the final plan, and only
// then publishes the closing "plan" event. A phone that sleeps through the run
// finds the result with GET.

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/coachbuddy/gateway-go/internal/auth"
	"github.com/coachbuddy/gateway-go/internal/contract"
	"github.com/coachbuddy/gateway-go/internal/models"
	"github.com/coachbuddy/gateway-go/internal/store"
)

// planSections are the keys of sectionStatus, in pipeline order.
var planSections = []string{"wod", "warmup", "parts", "injuries", "timeline"}

// maxCreateBody leaves room for the 4,000,000-character image the contract allows.
const maxCreateBody = 4_200_000

// timeFormat has a fixed width, so createdAt strings sort in time order.
const timeFormat = "2006-01-02T15:04:05.000Z07:00"

var dataURI = regexp.MustCompile(`^data:([\w/+.-]+);base64,`)

var imageTypes = map[string]bool{
	"image/png": true, "image/jpeg": true, "image/webp": true, "image/heic": true, "image/heif": true,
}

func newID(prefix string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

func nowString() string { return time.Now().UTC().Format(timeFormat) }

// decodeImage accepts plain base64 (taken as JPEG) or a data URI, as agents-python does.
func decodeImage(s string) ([]byte, string, error) {
	mime := "image/jpeg"
	if m := dataURI.FindStringSubmatch(s); m != nil {
		mime = strings.ToLower(m[1])
		s = s[len(m[0]):]
	}
	if !imageTypes[mime] {
		return nil, "", fmt.Errorf("unsupported image type %s; send a PNG, JPEG or WebP screenshot", mime)
	}
	data, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return nil, "", errors.New("the image is not valid base64")
	}
	if len(data) == 0 {
		return nil, "", errors.New("the image is empty")
	}
	return data, mime, nil
}

// shortReason keeps a reason shown to the coach within the contract's 300 characters.
func shortReason(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 300 {
		s = s[:297] + "..."
	}
	return s
}

// validPlan checks a plan document against the generated contract type.
func validPlan(doc map[string]interface{}) error {
	raw, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	var p contract.Plan
	return json.Unmarshal(raw, &p)
}

func planFinished(doc map[string]interface{}) bool {
	status, _ := doc["sectionStatus"].(map[string]interface{})
	for _, s := range planSections {
		st, _ := status[s].(map[string]interface{})
		switch st["state"] {
		case "done", "failed", "skipped":
		default:
			return false
		}
	}
	return true
}

// CreatePlan accepts a screenshot and a comment, saves the plan with every
// section pending, answers 202 with the plan ID, and builds the plan in the
// background.
func (h *APIHandler) CreatePlan(w http.ResponseWriter, r *http.Request) {
	coachID := auth.GetCoachID(r.Context())
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxCreateBody))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "the request is too large; send a screenshot under 3 MB")
		return
	}
	// The generated type checks lengths and ranges but lets unknown fields through
	// (for example the old rawText); refuse those first.
	strict := json.NewDecoder(bytes.NewReader(body))
	strict.DisallowUnknownFields()
	var known struct {
		ImageBase64  json.RawMessage `json:"imageBase64"`
		Comment      json.RawMessage `json:"comment"`
		ClassMinutes json.RawMessage `json:"classMinutes"`
	}
	if err := strict.Decode(&known); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request: "+shortReason(err.Error()))
		return
	}
	var input contract.CreatePlanRequest
	if err := json.Unmarshal(body, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request: "+shortReason(err.Error()))
		return
	}
	image, mime, err := decodeImage(input.ImageBase64)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	routing, err := h.Store.BuildEphemeralRoutingConfig(r.Context(), coachID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Cannot build a plan: "+err.Error())
		return
	}

	planID, imageRef, now := newID("pl_"), newID("img_"), nowString()
	classInput := map[string]interface{}{"imageRef": imageRef, "classMinutes": input.ClassMinutes}
	if input.Comment != nil && strings.TrimSpace(*input.Comment) != "" {
		classInput["comment"] = strings.TrimSpace(*input.Comment)
	}
	status := map[string]interface{}{}
	for _, s := range planSections {
		status[s] = map[string]interface{}{"state": "pending"}
	}
	doc := map[string]interface{}{
		"planId":        planID,
		"coachId":       coachID,
		"revision":      1,
		"input":         classInput,
		"sectionStatus": status,
		"createdAt":     now,
		"updatedAt":     now,
	}
	if err := h.Store.SavePlanImage(r.Context(), &models.PlanImage{Ref: imageRef, CoachID: coachID, MimeType: mime, Data: image}); err != nil {
		writeStoreError(w, "save screenshot", err)
		return
	}
	if err := h.Store.CreatePlan(r.Context(), doc); err != nil {
		writeStoreError(w, "create plan", err)
		return
	}

	job := planJob{
		PlanID: planID, CoachID: coachID, CreatedAt: now, Revision: 1,
		Path: "/api/v1/orchestrate",
		Body: models.OrchestrateRequest{
			PlanID: planID, CoachID: coachID, ImageRef: imageRef, CreatedAt: now,
			Input: input, RoutingConfig: routing,
		},
		Change:   contract.RevisionChange{Kind: contract.RevisionKindCreate},
		Sections: planSections,
	}
	if err := h.recordRevision(r.Context(), job, "running", nil); err != nil {
		log.Printf("[gateway] plan %s: revision 1 not recorded: %v", planID, err)
	}
	h.jobs.Add(1)
	go h.generate(job)

	writeJSON(w, http.StatusAccepted, map[string]interface{}{
		"planId":    planID,
		"revision":  1,
		"planUrl":   "/api/v1/plans/" + planID,
		"eventsUrl": "/api/v1/plans/" + planID + "/events",
	})
}

// planJob is one revision built in the background: a new plan (revision 1,
// agents-python's /orchestrate) or a section change (/regenerate).
type planJob struct {
	PlanID    string
	CoachID   string
	CreatedAt string // the plan's, kept on every revision
	Revision  int
	Path      string      // agents-python endpoint
	Body      interface{} // the request sent there
	Change    contract.RevisionChange
	// Sections this revision builds. A section change reports the ones that
	// actually ran (the timeline follows a warm-up change only when needed).
	Sections []string
}

// generate builds one revision in the background. Whatever happens, the plan
// ends with every section done, skipped or failed, and a closing "plan" event.
func (h *APIHandler) generate(job planJob) {
	defer h.jobs.Done()
	ctx, cancel := context.WithTimeout(h.jobsCtx, h.GenerateTimeout)
	defer cancel()

	// Progress: store each section's snapshot as it arrives. A Redis failure
	// here loses progress only; the final plan is still saved.
	stopProgress := func() {}
	if events, err := h.Store.PlanEvents(ctx, job.PlanID); err != nil {
		log.Printf("[gateway] plan %s: no progress events: %v", job.PlanID, err)
	} else {
		done := make(chan struct{})
		go func() {
			defer close(done)
			for evt := range events.Live {
				h.storeProgress(job, evt)
			}
		}()
		stopProgress = func() { events.Close(); <-done }
	}

	result, err := h.orchestrate(ctx, job)
	stopProgress()

	// Saving uses its own context, so a plan cut short by a shutdown is still recorded.
	saveCtx, saveCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer saveCancel()
	if err != nil {
		if errors.Is(ctx.Err(), context.Canceled) {
			err = errors.New("the gateway restarted during generation; build the plan again")
		} else if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			err = fmt.Errorf("generation took longer than %s", h.GenerateTimeout)
		}
		log.Printf("[gateway] plan %s failed: %v", job.PlanID, err)
		h.failPlan(saveCtx, job, err.Error())
		return
	}
	h.finishPlan(saveCtx, job, result)
}

// orchestrate calls agents-python and returns its document (the plan plus agentRuns).
func (h *APIHandler) orchestrate(ctx context.Context, job planJob) (map[string]interface{}, error) {
	body, err := json.Marshal(job.Body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.AgentsServiceURL+job.Path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Internal-Gateway-Token", h.InternalToken)
	// No client timeout: ctx (GenerateTimeout) bounds the call.
	resp, err := (&http.Client{Transport: h.HttpClient.Transport}).Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		log.Printf("[gateway] plan %s: agents-python: %v", job.PlanID, err)
		return nil, errors.New("the plan service is unreachable")
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("reading the plan service answer: %w", err)
	}
	if resp.StatusCode == http.StatusBadRequest {
		// The coach's input or settings (for example no key for a role): say why.
		var detail struct {
			Detail string `json:"detail"`
		}
		_ = json.Unmarshal(raw, &detail)
		return nil, errors.New(shortReason(detail.Detail))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		log.Printf("[gateway] plan %s: agents-python %d: %.300s", job.PlanID, resp.StatusCode, raw)
		return nil, fmt.Errorf("the plan service failed (status %d)", resp.StatusCode)
	}
	var doc map[string]interface{}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, errors.New("the plan service sent an unreadable answer")
	}
	return doc, nil
}

// ownPlan sets the fields only the gateway decides: the owner, creation time and revision.
func ownPlan(job planJob, doc map[string]interface{}) {
	doc["planId"] = job.PlanID
	doc["coachId"] = job.CoachID
	doc["createdAt"] = job.CreatedAt
	doc["revision"] = job.Revision
}

// storeProgress saves the snapshot carried by one section event. Anything that
// is not a valid event with a valid plan is ignored: it is progress, not the record.
func (h *APIHandler) storeProgress(job planJob, raw string) {
	var evt contract.PlanEvent
	if err := json.Unmarshal([]byte(raw), &evt); err != nil || evt.Plan == nil || evt.Section == contract.EventSectionPlan || evt.Revision != job.Revision {
		return
	}
	doc := map[string]interface{}(evt.Plan)
	ownPlan(job, doc)
	if err := validPlan(doc); err != nil {
		log.Printf("[gateway] plan %s: progress snapshot rejected: %v", job.PlanID, err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := h.Store.SavePlan(ctx, job.PlanID, doc); err != nil && !errors.Is(err, store.ErrPlanNotFound) {
		log.Printf("[gateway] plan %s: progress not saved: %v", job.PlanID, err)
	}
}

// finishPlan stores the final plan, its revision record and agent runs, then announces it.
func (h *APIHandler) finishPlan(ctx context.Context, job planJob, result map[string]interface{}) {
	runs := &models.AgentRuns{
		PlanID: job.PlanID, CoachID: job.CoachID, Revision: job.Revision,
		Runs: result["agentRuns"], OrchestrationLatencyMs: result["orchestrationLatencyMs"],
	}
	if job.Change.Kind == contract.RevisionKindRegenerate {
		job.Sections = sectionsRun(result["agentRuns"], job.Sections)
	}
	delete(result, "agentRuns")
	delete(result, "orchestrationLatencyMs")
	ownPlan(job, result)
	if err := validPlan(result); err != nil {
		log.Printf("[gateway] plan %s: invalid plan from agents-python: %v", job.PlanID, err)
		h.failPlan(ctx, job, "the plan service sent a plan that does not match the contract")
		return
	}
	if err := h.Store.SavePlan(ctx, job.PlanID, result); err != nil {
		if !errors.Is(err, store.ErrPlanNotFound) { // deleted, or moved on, while it was being built
			log.Printf("[gateway] plan %s: final plan not saved: %v", job.PlanID, err)
			h.publishEnd(ctx, job.PlanID, job.Revision, "failed", "the plan could not be saved; build it again", nil)
		}
		return
	}
	if err := h.recordRevision(ctx, job, revisionState(job, result), result); err != nil {
		log.Printf("[gateway] plan %s: revision %d not recorded: %v", job.PlanID, job.Revision, err)
	}
	if err := h.Store.SaveAgentRuns(ctx, runs); err != nil {
		log.Printf("[gateway] plan %s: agent runs not saved: %v", job.PlanID, err)
	}
	h.publishEnd(ctx, job.PlanID, job.Revision, "done", "", result)
}

// failPlan marks every section that has not finished as failed with the reason,
// keeping the sections that did finish, and announces the failure.
func (h *APIHandler) failPlan(ctx context.Context, job planJob, reason string) {
	reason = shortReason(reason)
	doc, err := h.Store.GetPlan(ctx, job.PlanID)
	if errors.Is(err, store.ErrPlanNotFound) {
		return
	}
	if err != nil {
		log.Printf("[gateway] plan %s: cannot record the failure: %v", job.PlanID, err)
		h.publishEnd(ctx, job.PlanID, job.Revision, "failed", reason, nil)
		return
	}
	if store.RevisionOf(doc) != job.Revision { // another revision has taken over
		return
	}
	status, _ := doc["sectionStatus"].(map[string]interface{})
	if status == nil {
		status = map[string]interface{}{}
		doc["sectionStatus"] = status
	}
	for _, s := range planSections {
		st, _ := status[s].(map[string]interface{})
		switch st["state"] {
		case "done", "skipped", "failed":
		default:
			status[s] = map[string]interface{}{"state": "failed", "error": reason}
		}
	}
	doc["updatedAt"] = nowString()
	if err := h.Store.SavePlan(ctx, job.PlanID, doc); err != nil {
		log.Printf("[gateway] plan %s: cannot record the failure: %v", job.PlanID, err)
	}
	if err := h.recordRevision(ctx, job, "failed", doc); err != nil {
		log.Printf("[gateway] plan %s: revision %d not recorded: %v", job.PlanID, job.Revision, err)
	}
	h.publishEnd(ctx, job.PlanID, job.Revision, "failed", reason, doc)
}

// publishEnd sends the closing "plan" event that ends every event stream.
func (h *APIHandler) publishEnd(ctx context.Context, planID string, revision int, state, reason string, doc map[string]interface{}) {
	evt := contract.PlanEvent{PlanId: planID, Revision: revision, Section: contract.EventSectionPlan, State: contract.EventState(state)}
	if reason != "" {
		r := shortReason(reason)
		evt.Error = &r
	}
	if doc != nil {
		evt.Plan = contract.PlanEventPlan(doc)
	}
	raw, err := json.Marshal(evt)
	if err != nil {
		log.Printf("[gateway] plan %s: closing event: %v", planID, err)
		return
	}
	if err := h.Store.PublishPlanEvent(ctx, planID, raw); err != nil {
		log.Printf("[gateway] plan %s: closing event not published: %v", planID, err)
	}
}

// --- Reading plans ---

// getOwnedPlan loads a plan and checks that it belongs to the authenticated
// coach. A plan owned by someone else is reported as not found, so its
// existence is not revealed. It writes the error response itself and returns
// false when the caller should stop.
func (h *APIHandler) getOwnedPlan(w http.ResponseWriter, r *http.Request, planID string) (map[string]interface{}, bool) {
	coachID := auth.GetCoachID(r.Context())
	if coachID == "" {
		writeError(w, http.StatusNotFound, "plan not found")
		return nil, false
	}
	doc, err := h.Store.GetPlan(r.Context(), planID)
	if errors.Is(err, store.ErrPlanNotFound) {
		writeError(w, http.StatusNotFound, "plan not found")
		return nil, false
	}
	if err != nil {
		writeStoreError(w, "get plan", err)
		return nil, false
	}
	if owner, _ := doc["coachId"].(string); owner != coachID {
		writeError(w, http.StatusNotFound, "plan not found")
		return nil, false
	}
	return doc, true
}

func (h *APIHandler) GetPlan(w http.ResponseWriter, r *http.Request) {
	doc, ok := h.getOwnedPlan(w, r, r.PathValue("id"))
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"plan": doc})
}

func (h *APIHandler) ListPlans(w http.ResponseWriter, r *http.Request) {
	plans, err := h.Store.ListPlans(r.Context(), auth.GetCoachID(r.Context()))
	if err != nil {
		writeStoreError(w, "list plans", err)
		return
	}
	if plans == nil {
		plans = []map[string]interface{}{}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"plans": plans})
}

func (h *APIHandler) DeletePlan(w http.ResponseWriter, r *http.Request) {
	planID := r.PathValue("id")
	if _, ok := h.getOwnedPlan(w, r, planID); !ok {
		return
	}
	err := h.Store.DeletePlan(r.Context(), planID)
	if errors.Is(err, store.ErrPlanNotFound) {
		writeError(w, http.StatusNotFound, "plan not found")
		return
	}
	if err != nil {
		writeStoreError(w, "delete plan", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// --- SSE progress stream ---

// eventInfo reads an event's revision, and whether it is the closing "plan" event.
func eventInfo(raw string) (revision int, ends bool) {
	var evt struct {
		Revision int    `json:"revision"`
		Section  string `json:"section"`
		State    string `json:"state"`
	}
	if json.Unmarshal([]byte(raw), &evt) != nil {
		return 0, false
	}
	return evt.Revision, evt.Section == "plan" && (evt.State == "done" || evt.State == "failed")
}

// closingEvent rebuilds the "plan" event for a finished plan from the stored document.
func closingEvent(planID string, doc map[string]interface{}) contract.PlanEvent {
	rev, _ := doc["revision"].(float64)
	if rev < 1 {
		rev = 1
	}
	evt := contract.PlanEvent{
		PlanId: planID, Revision: int(rev), Section: contract.EventSectionPlan,
		State: contract.EventStateFailed, Plan: contract.PlanEventPlan(doc),
	}
	status, _ := doc["sectionStatus"].(map[string]interface{})
	for _, s := range planSections {
		if st, _ := status[s].(map[string]interface{}); st["state"] != "failed" {
			evt.State = contract.EventStateDone
		}
	}
	if evt.State == contract.EventStateFailed {
		if st, _ := status["wod"].(map[string]interface{}); st != nil {
			if reason, _ := st["error"].(string); reason != "" {
				evt.Error = &reason
			}
		}
	}
	return evt
}

func (h *APIHandler) StreamPlanEvents(w http.ResponseWriter, r *http.Request) {
	planID := r.PathValue("id")
	// Same rule as read, delete and share: another coach's plan, or an unknown
	// one, is reported as not found before the stream opens. The owner is saved
	// before the 202, so this holds from the first event.
	doc, ok := h.getOwnedPlan(w, r, planID)
	if !ok {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	startStream := func() {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()
	}
	send := func(evt string) {
		fmt.Fprintf(w, "data: %s\n\n", evt)
		flusher.Flush()
	}

	ctx, cancel := context.WithTimeout(r.Context(), h.GenerateTimeout+time.Minute)
	defer cancel()
	// Open the subscription before the stream, so a Redis failure is a plain
	// error response rather than an event stream that never says anything.
	events, err := h.Store.PlanEvents(ctx, planID)
	if err != nil {
		log.Printf("[gateway] plan events %s: %v", planID, err)
		if !planFinished(doc) {
			writeError(w, http.StatusServiceUnavailable, "plan events unavailable")
			return
		}
	} else {
		defer events.Close()
	}

	// The replay list keeps the events of earlier revisions too; a stream
	// follows the plan's current revision only.
	current := store.RevisionOf(doc)
	var buffered []string
	if events != nil {
		for _, evt := range events.Buffered {
			if rev, _ := eventInfo(evt); rev >= current {
				buffered = append(buffered, evt)
			}
		}
	}

	// A finished plan whose events have expired (or Redis is down): one closing
	// event with the stored plan. "failed" when no section could be built.
	ended := false
	for _, evt := range buffered {
		_, end := eventInfo(evt)
		ended = ended || end
	}
	if !ended && planFinished(doc) {
		raw, err := json.Marshal(closingEvent(planID, doc))
		if err != nil {
			writeStoreError(w, "plan events", err)
			return
		}
		startStream()
		send(string(raw))
		return
	}
	startStream()

	for _, evt := range buffered {
		send(evt)
		if _, end := eventInfo(evt); end {
			return
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-events.Live:
			if !ok {
				return
			}
			rev, end := eventInfo(evt)
			if rev < current {
				continue
			}
			send(evt)
			if end {
				return
			}
		}
	}
}
