package service

import "testing"

func TestValidateOrderNumber(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{name: "zero", input: "0"},
		{name: "twoDigits", input: "18"},
		{name: "zeros", input: "000"},
		{name: "long", input: "79927398713"},
		{name: "trimmed", input: " 18 "},
		{name: "empty", input: "", wantErr: true},
		{name: "spaces", input: "   ", wantErr: true},
		{name: "space inside", input: "12 34", wantErr: true},
		{name: "letters", input: "ABC", wantErr: true},
		{name: "plus", input: "+123", wantErr: true},
		{name: "negative", input: "-1", wantErr: true},
		{name: "fails luhn", input: "12", wantErr: true},
		{name: "single fails", input: "1", wantErr: true},
	}

	for _, tc := range cases {
		c := tc
		t.Run(c.name, func(t *testing.T) {
			err := ValidateOrderNumber(c.input)
			if c.wantErr && err == nil {
				t.Fatalf("expected error for input %q", c.input)
			}
			if !c.wantErr && err != nil {
				t.Fatalf("unexpected error for input %q: %v", c.input, err)
			}
		})
	}
}
