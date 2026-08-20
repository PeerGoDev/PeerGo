package main

import (
	"testing"
)

func TestValidateLoopbackURL(t *testing.T) {
	for _, test := range []struct {
		name    string
		value   string
		schemes []string
		valid   bool
	}{
		{name: "postgres ipv4", value: "postgres://user:pass@127.0.0.1:5432/db", schemes: []string{"postgres"}, valid: true},
		{name: "postgres localhost", value: "postgresql://localhost/db", schemes: []string{"postgres", "postgresql"}, valid: true},
		{name: "nats ipv6", value: "nats://[::1]:4222", schemes: []string{"nats"}, valid: true},
		{name: "remote refused", value: "postgres://database.example/db", schemes: []string{"postgres"}, valid: false},
		{name: "wrong scheme", value: "https://127.0.0.1:4222", schemes: []string{"nats"}, valid: false},
		{name: "missing host", value: "postgres:///db", schemes: []string{"postgres"}, valid: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := validateLoopbackURL(test.value, test.schemes...)
			if (err == nil) != test.valid {
				t.Fatalf("validateLoopbackURL(%q) error = %v, valid %v", test.value, err, test.valid)
			}
		})
	}
}
