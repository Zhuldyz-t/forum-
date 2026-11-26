package view

import "testing"

func TestTemplatesParse(t *testing.T) {
	if _, err := ParseGlob("../../templates/*.html"); err != nil {
		t.Fatal(err)
	}
}
