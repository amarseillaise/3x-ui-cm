package config

import "testing"

const sampleYAML = `
currency: RUB
plans:
  - { id: p30,  title: "30 дней",  days: 30,  price: 180 }
  - { id: p90,  title: "90 дней",  days: 90,  price: 500 }
requisites:
  - { label: "СБП", value: "+7 900 000-00-00", note: "Т-Банк" }
payment_note: "После перевода нажмите «Я перевёл»."
`

func TestParseAppConfig(t *testing.T) {
	c, err := ParseAppConfig([]byte(sampleYAML))
	if err != nil {
		t.Fatal(err)
	}
	if !c.RenewalEnabled() {
		t.Error("renewal should be enabled")
	}
	p, ok := c.Plan("p90")
	if !ok || p.Days != 90 || p.Price != 500 {
		t.Errorf("plan lookup failed: %+v %v", p, ok)
	}
	if _, ok := c.Plan("nope"); ok {
		t.Error("unknown plan must not be found")
	}
}

func TestParseAppConfigErrors(t *testing.T) {
	bad := []string{
		"plans:\n  - { id: a, title: A, days: 0, price: 1 }",
		"plans:\n  - { id: a, title: A, days: 1, price: 1 }\n  - { id: a, title: B, days: 2, price: 2 }",
		"unknown_key: 1",
		"currency: ''",
		"requisites:\n  - { label: '', value: x }",
	}
	for _, y := range bad {
		if _, err := ParseAppConfig([]byte(y)); err == nil {
			t.Errorf("expected error for %q", y)
		}
	}
}

func TestParseAppConfigEmpty(t *testing.T) {
	c, err := ParseAppConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	if c.RenewalEnabled() {
		t.Error("empty config must disable renewal")
	}
}
