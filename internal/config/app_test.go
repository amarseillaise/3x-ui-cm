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
		"contacts:\n  - { label: '', url: 'https://t.me/x' }",
		"contacts:\n  - { label: TG, url: 't.me/x' }",
		"contacts:\n  - { label: TG, url: 'javascript:alert(1)' }",
		"contacts:\n  - { label: TG, url: 'tg://resolve?domain=x' }",
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

func TestParseAppConfigContacts(t *testing.T) {
	c, err := ParseAppConfig([]byte(`
contacts:
  - { label: " Канал в Telegram ", url: "https://t.me/channel" }
  - { label: "ВКонтакте", url: " https://vk.com/someone " }
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Contacts) != 2 || c.Contacts[0].Label != "Канал в Telegram" || c.Contacts[1].URL != "https://vk.com/someone" {
		t.Errorf("contacts must be parsed and trimmed: %+v", c.Contacts)
	}
	// Contacts do not depend on renewal being configured.
	if c.RenewalEnabled() {
		t.Error("contacts alone must not enable renewal")
	}
}

// A fresh install copies this file as is, so it must always parse: with
// unknown keys rejected, a stale example would stop the server from starting.
func TestExampleConfigParses(t *testing.T) {
	c, err := LoadAppConfig("../../deploy/config.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !c.RenewalEnabled() || len(c.Contacts) == 0 {
		t.Errorf("example should show plans, requisites and contacts: %+v", c)
	}
}
