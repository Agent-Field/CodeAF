package data

import (
	"reflect"
	"testing"
)

func TestIdentityMappingNormalizers(t *testing.T) {
	for _, tc := range []struct {
		op, arg, input string
		want           any
	}{
		{"trim", "", "  name \t", "name"},
		{"lower", "", "MiXeD", "mixed"},
		{"upper", "", "usd", "USD"},
		{"email", "", " ALEX@Example.ORG ", "alex@example.org"},
		{"email", "", "Alex <alex@example.org>", nil},
		{"phone", "1", "(415) 555-0132", "+14155550132"},
		{"phone", "", "+44 20 7946 0958", "+442079460958"},
		{"phone", "", "555-0132", nil},
		{"domain", "", "https://www.Example.org/path", "example.org"},
		{"date", "02/01/2006", "31/12/2026", "2026-12-31"},
		{"datetime", "", "2026-11-25T18:00:00-05:00", "2026-11-25T23:00:00Z"},
		{"money", "USD", "usd 123456789012345678.2300", "123456789012345678.2300"},
		{"money", "USD", "CAD 12.50", nil},
		{"currency", "", " gbp ", "GBP"},
	} {
		t.Run(tc.op+"/"+tc.input, func(t *testing.T) {
			got, err := (MappingTransform{Op: tc.op, Arg: tc.arg}).apply(tc.input)
			if (err != nil) != (tc.want == nil) || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %#v (%v), want %#v", got, err, tc.want)
			}
		})
	}
}
