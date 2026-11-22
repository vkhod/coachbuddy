package handlers

// Refinement and versions (Phase 2 step 11).
//
//	POST /api/v1/plans/{id}/sections/{section}/regenerate   202; one section built again
//	GET  /api/v1/plans/{id}/revisions                       what each revision changed
//	GET  /api/v1/plans/{id}/revisions/{n}                   the plan as it was at revision n
//	POST /api/v1/plans/{id}/revisions/{n}/restore           200; revision n copied into a new one
//
// Every change makes a new revision, so nothing is lost: a regeneration whose
// section fails is a revision with that section failed, and the one before it
// can be restored. One change runs at a time; a second one gets 409.
//
// A regeneration moves the plan to revision n+1 before the 202, with the
// section pending and its old content removed, as in a new plan. agents-python
// gets the plan as it was at n. After a warm-up change it also rebuilds the
// timeline, when the warm-up minutes changed.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/coachbuddy/gateway-go/internal/auth"
	"github.com/coachbuddy/gateway-go/internal/contract"
	"github.com/coachbuddy/gateway-go/internal/models"
	"github.com/coachbuddy/gateway-go/internal/store"
)

// refinable are the sections a coach can change. Reading the WOD again is
// building a new plan.
var refinable = map[string]bool{"warmup": true, "parts": true, "injuries": true, "timeline": true}

const maxRegenerateBody = 8 << 10

// recordRevision saves the revision record for a job. plan is nil while it runs.
func (h *APIHandler) recordRevision(ctx context.Context, job planJob, state string, plan map[string]interface{}) error {
	return h.Store.SavePlanRevision(ctx, &models.PlanRevision{
		PlanID: job.PlanID, CoachID: job.CoachID, Revision: job.Revision,
		CreatedAt: h.revisionCreatedAt(ctx, job), Change: job.Change,
		Sections: job.Sections, State: state, Plan: plan,
	})
}

// revisionCreatedAt keeps the time a revision started across its records.
func (h *APIHandler) revisionCreatedAt(ctx context.Context, job planJob) string {
	if job.Revision == 1 {
		return job.CreatedAt
	}
	if rev, err := h.Store.GetPlanRevision(ctx, job.PlanID, job.Revision); err == nil && rev.CreatedAt != "" {
		return rev.CreatedAt
	}
	return nowString()
}

// revisionState is "failed" when a section this revision built failed.
func revisionState(job planJob, doc map[string]interface{}) string {
	status, _ := doc["sectionStatus"].(map[string]interface{})
	for _, s := range job.Sections {
		if st, _ := status[s].(map[string]interface{}); st["state"] == "failed" {
			return "failed"
		}
	}
	return "done"
}

// sectionsRun lists the sections in agentRuns, in order; fallback when there are none.
func sectionsRun(runs interface{}, fallback []string) []string {
	list, _ := runs.([]interface{})
	var out []string
	seen := map[string]bool{}
	for _, r := range list {
		m, _ := r.(map[string]interface{})
		s, _ := m["section"].(string)
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return fallback
	}
	return out
}

func sectionState(doc map[string]interface{}, section string) string {
	status, _ := doc["sectionStatus"].(map[string]interface{})
	st, _ := status[section].(map[string]interface{})
	s, _ := st["state"].(string)
	return s
}

// planRevisionEntry is one entry of the revisions list (schema plan-revision).
func planRevisionEntry(rev *models.PlanRevision) contract.PlanRevision {
	sections := make([]contract.RevisionSection, 0, len(rev.Sections))
	for _, s := range rev.Sections {
		sections = append(sections, contract.RevisionSection(s))
	}
	created, _ := time.Parse(time.RFC3339Nano, rev.CreatedAt)
	return contract.PlanRevision{
		Revision: rev.Revision, CreatedAt: created, Change: rev.Change,
		Sections: sections, State: contract.RevisionState(rev.State),
	}
}

// ensureFirstRevision records revision 1 of a finished plan that has no record
// (one built before versions existed, or whose record could not be saved), so
// it can be restored after a change.
func (h *APIHandler) ensureFirstRevision(ctx context.Context, doc map[string]interface{}) {
	if store.RevisionOf(doc) != 1 {
		return
	}
	planID, _ := doc["planId"].(string)
	if _, err := h.Store.GetPlanRevision(ctx, planID, 1); !errors.Is(err, store.ErrRevisionNotFound) {
		return
	}
	coachID, _ := doc["coachId"].(string)
	createdAt, _ := doc["createdAt"].(string)
	job := planJob{
		PlanID: planID, CoachID: coachID, CreatedAt: createdAt, Revision: 1,
		Change: contract.RevisionChange{Kind: contract.RevisionKindCreate}, Sections: planSections,
	}
	if err := h.recordRevision(ctx, job, revisionState(job, doc), doc); err != nil {
		log.Printf("[gateway] plan %s: revision 1 not recorded: %v", planID, err)
	}
}

// RegenerateSection builds one section of a finished plan again with the
// coach's instruction, as a new revision, in the background.
func (h *APIHandler) RegenerateSection(w http.ResponseWriter, r *http.Request) {
	planID, section := r.PathValue("id"), r.PathValue("section")
	if !refinable[section] {
		writeError(w, http.StatusBadRequest, "section must be warmup, parts, injuries or timeline; to read the WOD again, build a new plan")
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRegenerateBody))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, "the request is too large")
		return
	}
	// As for CreatePlan: the generated type lets unknown fields through.
	strict := json.NewDecoder(bytes.NewReader(body))
	strict.DisallowUnknownFields()
	var known struct {
		Instruction json.RawMessage `json:"instruction"`
	}
	if err := strict.Decode(&known); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request: "+shortReason(err.Error()))
		return
	}
	var input contract.RegenerateSectionRequest
	if err := json.Unmarshal(body, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request: "+shortReason(err.Error()))
		return
	}
	instruction := strings.TrimSpace(input.Instruction)
	if instruction == "" {
		writeError(w, http.StatusBadRequest, "invalid request: the instruction is empty")
		return
	}

	doc, ok := h.getOwnedPlan(w, r, planID)
	if !ok {
		return
	}
	if !planFinished(doc) {
		writeError(w, http.StatusConflict, "the plan is still being built or changed; try again when it has finished")
		return
	}
	if sectionState(doc, "wod") != "done" {
		writeError(w, http.StatusConflict, "the WOD could not be read for this plan; build a new plan")
		return
	}
	coachID := auth.GetCoachID(r.Context())
	routing, err := h.Store.BuildEphemeralRoutingConfig(r.Context(), coachID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "Cannot change the plan: "+err.Error())
		return
	}
	h.ensureFirstRevision(r.Context(), doc)

	from := store.RevisionOf(doc)
	next := copyPlan(doc)
	next["revision"] = from + 1
	delete(next, section)
	next["sectionStatus"].(map[string]interface{})[section] = map[string]interface{}{"state": "pending"}
	next["updatedAt"] = nowString()
	if err := h.Store.AdvancePlan(r.Context(), planID, from, next); err != nil {
		writeAdvanceError(w, err)
		return
	}

	createdAt, _ := doc["createdAt"].(string)
	job := planJob{
		PlanID: planID, CoachID: coachID, CreatedAt: createdAt, Revision: from + 1,
		Path: "/api/v1/regenerate",
		Body: models.RegenerateRequest{
			PlanID: planID, CoachID: coachID, Revision: from + 1, Section: section,
			Instruction: instruction, Plan: doc, RoutingConfig: routing,
		},
		Change:   contract.RevisionChange{Kind: contract.RevisionKindRegenerate, Section: sectionPtr(section), Instruction: &instruction},
		Sections: []string{section},
	}
	if err := h.recordRevision(r.Context(), job, "running", nil); err != nil {
		log.Printf("[gateway] plan %s: revision %d not recorded: %v", planID, from+1, err)
	}
	h.jobs.Add(1)
	go h.generate(job)

	writeJSON(w, http.StatusAccepted, map[string]interface{}{
		"planId":    planID,
		"revision":  from + 1,
		"planUrl":   "/api/v1/plans/" + planID,
		"eventsUrl": "/api/v1/plans/" + planID + "/events",
	})
}

func sectionPtr(s string) *contract.RevisionSection {
	v := contract.RevisionSection(s)
	return &v
}

// copyPlan is a deep copy through JSON, so the stored plan sent to
// agents-python is not changed by the next revision.
func copyPlan(doc map[string]interface{}) map[string]interface{} {
	raw, _ := json.Marshal(doc)
	var out map[string]interface{}
	_ = json.Unmarshal(raw, &out)
	return out
}

func writeAdvanceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrPlanNotFound):
		writeError(w, http.StatusNotFound, "plan not found")
	case errors.Is(err, store.ErrRevisionConflict):
		writeError(w, http.StatusConflict, "the plan was changed at the same time; reload it and try again")
	default:
		writeStoreError(w, "change plan", err)
	}
}

// ListRevisions lists what each revision of the plan changed, oldest first.
func (h *APIHandler) ListRevisions(w http.ResponseWriter, r *http.Request) {
	planID := r.PathValue("id")
	doc, ok := h.getOwnedPlan(w, r, planID)
	if !ok {
		return
	}
	if planFinished(doc) {
		h.ensureFirstRevision(r.Context(), doc)
	}
	list, err := h.Store.ListPlanRevisions(r.Context(), planID)
	if err != nil {
		writeStoreError(w, "list revisions", err)
		return
	}
	out := make([]contract.PlanRevision, 0, len(list))
	for _, rev := range list {
		out = append(out, planRevisionEntry(rev))
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"current": store.RevisionOf(doc), "revisions": out})
}

// revisionParam reads {n}; it writes the error itself.
func revisionParam(w http.ResponseWriter, r *http.Request) (int, bool) {
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil || n < 1 {
		writeError(w, http.StatusBadRequest, "revision must be a number from 1")
		return 0, false
	}
	return n, true
}

// storedRevision returns revision n of an owned plan with its plan, or writes 404.
func (h *APIHandler) storedRevision(w http.ResponseWriter, r *http.Request, planID string, n int) (*models.PlanRevision, bool) {
	rev, err := h.Store.GetPlanRevision(r.Context(), planID, n)
	if errors.Is(err, store.ErrRevisionNotFound) || (err == nil && rev.Plan == nil) {
		writeError(w, http.StatusNotFound, "revision not found or not finished")
		return nil, false
	}
	if err != nil {
		writeStoreError(w, "get revision", err)
		return nil, false
	}
	return rev, true
}

// GetRevision returns the plan as it was at revision n.
func (h *APIHandler) GetRevision(w http.ResponseWriter, r *http.Request) {
	planID := r.PathValue("id")
	n, ok := revisionParam(w, r)
	if !ok {
		return
	}
	doc, ok := h.getOwnedPlan(w, r, planID)
	if !ok {
		return
	}
	if planFinished(doc) {
		h.ensureFirstRevision(r.Context(), doc)
	}
	rev, ok := h.storedRevision(w, r, planID, n)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"revision": planRevisionEntry(rev), "plan": rev.Plan})
}

// RestoreRevision copies the plan of revision n into a new revision. No model
// is called; the restored plan is the stored one, section states included.
func (h *APIHandler) RestoreRevision(w http.ResponseWriter, r *http.Request) {
	planID := r.PathValue("id")
	n, ok := revisionParam(w, r)
	if !ok {
		return
	}
	doc, ok := h.getOwnedPlan(w, r, planID)
	if !ok {
		return
	}
	if !planFinished(doc) {
		writeError(w, http.StatusConflict, "the plan is still being built or changed; try again when it has finished")
		return
	}
	from := store.RevisionOf(doc)
	if n == from {
		writeError(w, http.StatusBadRequest, "revision "+strconv.Itoa(n)+" is the current one")
		return
	}
	h.ensureFirstRevision(r.Context(), doc)
	rev, ok := h.storedRevision(w, r, planID, n)
	if !ok {
		return
	}

	next := copyPlan(rev.Plan)
	for _, k := range []string{"planId", "coachId", "createdAt", "input"} {
		next[k] = doc[k]
	}
	next["revision"] = from + 1
	next["updatedAt"] = nowString()
	if err := validPlan(next); err != nil {
		log.Printf("[gateway] plan %s: revision %d does not match the contract: %v", planID, n, err)
		writeError(w, http.StatusInternalServerError, "revision "+strconv.Itoa(n)+" cannot be restored")
		return
	}
	if err := h.Store.AdvancePlan(r.Context(), planID, from, next); err != nil {
		writeAdvanceError(w, err)
		return
	}
	createdAt, _ := doc["createdAt"].(string)
	job := planJob{
		PlanID: planID, CoachID: auth.GetCoachID(r.Context()), CreatedAt: createdAt, Revision: from + 1,
		Change: contract.RevisionChange{Kind: contract.RevisionKindRestore, RestoredFrom: &n}, Sections: []string{},
	}
	if err := h.recordRevision(r.Context(), job, "done", next); err != nil {
		log.Printf("[gateway] plan %s: revision %d not recorded: %v", planID, from+1, err)
	}
	h.publishEnd(r.Context(), planID, from+1, "done", "", next)
	writeJSON(w, http.StatusOK, map[string]interface{}{"plan": next})
}
