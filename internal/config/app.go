package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Plan is a renewal option offered to the client.
type Plan struct {
	ID    string `yaml:"id" json:"id"`
	Title string `yaml:"title" json:"title"`
	Days  int    `yaml:"days" json:"days"`
	Price int    `yaml:"price" json:"price"`
}

// Requisite is one payment destination line shown to the client as-is.
type Requisite struct {
	Label string `yaml:"label" json:"label"`
	Value string `yaml:"value" json:"value"`
	Note  string `yaml:"note,omitempty" json:"note,omitempty"`
}

// AppConfig holds non-secret product settings from config.yaml.
type AppConfig struct {
	Currency    string      `yaml:"currency" json:"currency"`
	Plans       []Plan      `yaml:"plans" json:"plans"`
	Requisites  []Requisite `yaml:"requisites" json:"requisites"`
	PaymentNote string      `yaml:"payment_note" json:"paymentNote"`
}

// DefaultAppConfig returns a config with no plans: renewal is disabled.
func DefaultAppConfig() *AppConfig {
	return &AppConfig{Currency: "RUB"}
}

// LoadAppConfig reads and validates config.yaml. A missing file is reported as
// os.ErrNotExist so the caller can fall back to DefaultAppConfig.
func LoadAppConfig(path string) (*AppConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	c, err := ParseAppConfig(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return c, nil
}

// ParseAppConfig parses YAML and validates it. Unknown keys are rejected.
func ParseAppConfig(data []byte) (*AppConfig, error) {
	c := DefaultAppConfig()
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(c); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return c, nil
}

// Validate checks plan and requisite invariants.
func (c *AppConfig) Validate() error {
	var errs []error
	c.Currency = strings.TrimSpace(c.Currency)
	if c.Currency == "" {
		errs = append(errs, errors.New("currency is required"))
	}
	seen := map[string]bool{}
	for i, p := range c.Plans {
		p.ID = strings.TrimSpace(p.ID)
		p.Title = strings.TrimSpace(p.Title)
		c.Plans[i] = p
		switch {
		case p.ID == "":
			errs = append(errs, fmt.Errorf("plans[%d]: id is required", i))
		case seen[p.ID]:
			errs = append(errs, fmt.Errorf("plans[%d]: duplicate id %q", i, p.ID))
		}
		seen[p.ID] = true
		if p.Title == "" {
			errs = append(errs, fmt.Errorf("plans[%d]: title is required", i))
		}
		if p.Days < 1 {
			errs = append(errs, fmt.Errorf("plans[%d]: days must be >= 1", i))
		}
		if p.Price < 0 {
			errs = append(errs, fmt.Errorf("plans[%d]: price must be >= 0", i))
		}
	}
	for i, r := range c.Requisites {
		if strings.TrimSpace(r.Label) == "" || strings.TrimSpace(r.Value) == "" {
			errs = append(errs, fmt.Errorf("requisites[%d]: label and value are required", i))
		}
	}
	return errors.Join(errs...)
}

// Plan looks a plan up by id.
func (c *AppConfig) Plan(id string) (Plan, bool) {
	for _, p := range c.Plans {
		if p.ID == id {
			return p, true
		}
	}
	return Plan{}, false
}

// RenewalEnabled reports whether the renewal flow can be offered.
func (c *AppConfig) RenewalEnabled() bool {
	return len(c.Plans) > 0 && len(c.Requisites) > 0
}
