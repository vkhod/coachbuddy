// Package modelcatalog is the list of LLM providers and models a coach can choose.
//
// The list ships embedded (models.json) and can be replaced without a rebuild by
// pointing MODELS_FILE at another file with the same shape. The first provider's
// default model is the default for coaches who have chosen nothing.
// scripts/check-models compares the list with each provider's own model list.
package modelcatalog

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

//go:embed models.json
var embedded []byte

type Provider struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

type Model struct {
	ID       string `json:"id"`
	Provider string `json:"provider"`
	Label    string `json:"label"`
	Vision   bool   `json:"vision"`
	Default  bool   `json:"default,omitempty"`
}

type Catalog struct {
	Checked   string     `json:"checked"`
	Providers []Provider `json:"providers"`
	Models    []Model    `json:"models"`

	byID       map[string]Model
	byProvider map[string]string // provider -> default model id
}

// Load reads MODELS_FILE when path is not empty, otherwise the embedded list.
func Load(path string) (*Catalog, error) {
	data := embedded
	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("model list: %w", err)
		}
		data = b
	}
	return Parse(data)
}

// Embedded returns the built-in list. It panics only if the embedded file is broken,
// which the package tests rule out.
func Embedded() *Catalog {
	c, err := Parse(embedded)
	if err != nil {
		panic(err)
	}
	return c
}

// Parse checks the list: known providers, unique ids of the form provider/model,
// exactly one default per provider, and a vision model as each default (the
// parser reads screenshots).
func Parse(data []byte) (*Catalog, error) {
	var c Catalog
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("model list: %w", err)
	}
	if len(c.Providers) == 0 {
		return nil, fmt.Errorf("model list: no providers")
	}
	c.byID = map[string]Model{}
	c.byProvider = map[string]string{}
	known := map[string]bool{}
	for _, p := range c.Providers {
		if p.ID == "" || known[p.ID] {
			return nil, fmt.Errorf("model list: empty or repeated provider %q", p.ID)
		}
		known[p.ID] = true
	}
	for _, m := range c.Models {
		if !known[m.Provider] {
			return nil, fmt.Errorf("model list: %s has unknown provider %q", m.ID, m.Provider)
		}
		if !strings.HasPrefix(m.ID, m.Provider+"/") {
			return nil, fmt.Errorf("model list: id %q must start with %q", m.ID, m.Provider+"/")
		}
		if _, dup := c.byID[m.ID]; dup {
			return nil, fmt.Errorf("model list: %s is listed twice", m.ID)
		}
		c.byID[m.ID] = m
		if m.Default {
			if prev, ok := c.byProvider[m.Provider]; ok {
				return nil, fmt.Errorf("model list: %s has two defaults (%s, %s)", m.Provider, prev, m.ID)
			}
			if !m.Vision {
				return nil, fmt.Errorf("model list: default %s must support images", m.ID)
			}
			c.byProvider[m.Provider] = m.ID
		}
	}
	for _, p := range c.Providers {
		if _, ok := c.byProvider[p.ID]; !ok {
			return nil, fmt.Errorf("model list: provider %s has no default model", p.ID)
		}
	}
	return &c, nil
}

// Default is the model for coaches who have chosen nothing.
func (c *Catalog) Default() Model { return c.byID[c.byProvider[c.Providers[0].ID]] }

// DefaultFor is the provider's default model, and false for a provider not offered.
func (c *Catalog) DefaultFor(provider string) (string, bool) {
	id, ok := c.byProvider[provider]
	return id, ok
}

func (c *Catalog) HasProvider(provider string) bool {
	_, ok := c.byProvider[provider]
	return ok
}

// Lookup finds a model by id.
func (c *Catalog) Lookup(id string) (Model, bool) {
	m, ok := c.byID[id]
	return m, ok
}

// Allows reports whether provider and model belong together in the list.
func (c *Catalog) Allows(provider, model string) bool {
	m, ok := c.byID[model]
	return ok && m.Provider == provider
}

// ProviderIDs lists the providers in their configured order.
func (c *Catalog) ProviderIDs() []string {
	out := make([]string, 0, len(c.Providers))
	for _, p := range c.Providers {
		out = append(out, p.ID)
	}
	return out
}
