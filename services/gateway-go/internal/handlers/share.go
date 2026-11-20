package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/coachbuddy/gateway-go/internal/auth"
	"github.com/coachbuddy/gateway-go/internal/models"
	"github.com/coachbuddy/gateway-go/internal/store"
	"github.com/google/uuid"
)

// CreateShareLink makes a public link and QR code for one of the coach's plans.
// The link shows the athlete view unless the coach asks for the coach view
// with {"includeCoachNotes": true}.
func (h *APIHandler) CreateShareLink(w http.ResponseWriter, r *http.Request) {
	coachID := auth.GetCoachID(r.Context())
	planID := r.PathValue("id")

	if _, ok := h.getOwnedPlan(w, r, planID); !ok {
		return
	}

	var req struct {
		IncludeCoachNotes bool `json:"includeCoachNotes"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	// 12 hex chars (48 bits) so public share codes cannot be guessed by enumeration.
	shortCode := "cb-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	shareURL := fmt.Sprintf("%s/%s", h.PublicShareBaseURL, shortCode)

	qrSVG, err := h.fetchQRFromRustMCP(r.Context(), shareURL)
	if err != nil {
		log.Printf("[gateway] QR code for %s: %v", planID, err)
		writeError(w, http.StatusBadGateway, "QR code service unavailable, try again")
		return
	}

	link := &models.SharedLink{
		ShortCode:         shortCode,
		ShareURL:          shareURL,
		PlanID:            planID,
		CoachID:           coachID,
		IncludeCoachNotes: req.IncludeCoachNotes,
		QRSVG:             qrSVG,
	}
	if err := h.Store.SaveSharedLink(r.Context(), link); err != nil {
		writeStoreError(w, "save share link", err)
		return
	}
	writeJSON(w, http.StatusCreated, link)
}

// ResolveShareLink answers a public share code with the plan, filtered on the
// server for the link's audience.
func (h *APIHandler) ResolveShareLink(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	link, err := h.Store.GetSharedLink(r.Context(), code)
	if errors.Is(err, store.ErrLinkNotFound) {
		writeError(w, http.StatusNotFound, "shared link expired or not found")
		return
	}
	if err != nil {
		writeStoreError(w, "resolve share link", err)
		return
	}
	link.ShareURL = fmt.Sprintf("%s/%s", h.PublicShareBaseURL, link.ShortCode)

	doc, err := h.Store.GetPlan(r.Context(), link.PlanID)
	if errors.Is(err, store.ErrPlanNotFound) {
		writeError(w, http.StatusNotFound, "the shared plan no longer exists")
		return
	}
	if err != nil {
		writeStoreError(w, "resolve share link", err)
		return
	}
	view := athleteView(doc)
	if link.IncludeCoachNotes {
		view = coachView(doc)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"share": link,
		"plan":  view,
	})
}

// --- Share views ---
//
// Both views are built by copying named fields (an allowlist), so a field added
// to the plan later stays private until it is added here on purpose.
//
// Never shared, in either view: input (the screenshot reference and the coach's
// comment, which names injuries), section errors (they name models), and the
// agent runs (stored apart from the plan).
//
// Athlete view (plan section 2.1): the WOD, the timeline and the tiers. No
// warm-up, no "today" groups, no injuries, no cues or details, no coach warnings.
// Coach view: adds the warm-up, the "today" groups, the injury blocks, cues,
// details and timeline warnings.

type obj = map[string]interface{}

func pick(m obj, keys ...string) obj {
	out := obj{}
	for _, k := range keys {
		if v, ok := m[k]; ok && v != nil {
			out[k] = v
		}
	}
	return out
}

func asObj(v interface{}) obj {
	m, _ := v.(map[string]interface{})
	return m
}

func eachObj(v interface{}, f func(obj) obj) []interface{} {
	list, _ := v.([]interface{})
	out := make([]interface{}, 0, len(list))
	for _, x := range list {
		if m := asObj(x); m != nil {
			out = append(out, f(m))
		}
	}
	return out
}

func sharedStatus(doc obj) obj {
	out := obj{}
	for k, v := range asObj(doc["sectionStatus"]) {
		out[k] = pick(asObj(v), "state")
	}
	return out
}

func sharedWod(doc obj) obj {
	wod := asObj(doc["wod"])
	if wod == nil {
		return nil
	}
	out := pick(wod, "title", "date")
	out["parts"] = eachObj(wod["parts"], func(p obj) obj { return pick(p, "id", "name", "kind", "prescription", "minutes") })
	return out
}

func sharedTiers(t obj) obj {
	out := obj{}
	for _, k := range []string{"rx", "rxPlus", "scaled", "senior"} {
		if tier := asObj(t[k]); tier != nil {
			out[k] = pick(tier, "line", "cells")
		}
	}
	return out
}

func sharedTimeline(doc obj, withWarnings bool) obj {
	tl := asObj(doc["timeline"])
	if tl == nil {
		return nil
	}
	keys := []string{"totalMinutes", "status", "provisional"}
	if withWarnings {
		keys = append(keys, "warnings")
	}
	out := pick(tl, keys...)
	out["blocks"] = eachObj(tl["blocks"], func(b obj) obj { return pick(b, "startMin", "endMin", "label", "section") })
	return out
}

func setIf(out obj, key string, v interface{}) {
	switch t := v.(type) {
	case nil:
		return
	case obj:
		if t == nil {
			return
		}
	}
	out[key] = v
}

func athleteView(doc obj) obj {
	out := pick(doc, "planId", "revision")
	out["sectionStatus"] = sharedStatus(doc)
	setIf(out, "wod", sharedWod(doc))
	if _, ok := doc["parts"]; ok {
		out["parts"] = eachObj(doc["parts"], func(p obj) obj {
			part := pick(p, "partId", "layout", "columns")
			part["tiers"] = sharedTiers(asObj(p["tiers"]))
			return part
		})
	}
	setIf(out, "timeline", sharedTimeline(doc, false))
	return out
}

func coachView(doc obj) obj {
	out := athleteView(doc)
	if _, ok := doc["parts"]; ok {
		out["parts"] = eachObj(doc["parts"], func(p obj) obj {
			part := pick(p, "partId", "layout", "columns", "cues", "more")
			part["tiers"] = sharedTiers(asObj(p["tiers"]))
			return part
		})
	}
	setIf(out, "timeline", sharedTimeline(doc, true))
	for _, k := range []string{"warmup", "today", "injuries"} {
		if v, ok := doc[k]; ok && v != nil {
			out[k] = v
		}
	}
	return out
}
