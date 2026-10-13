package query

import (
	"database/sql"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/Agent-Field/codeaf/internal/apps/spec"
	_ "modernc.org/sqlite"
)

func fixture(t *testing.T) (*sql.DB, *spec.RecordType) {
	t.Helper()
	schema := &spec.RecordType{Fields: map[string]*spec.Field{"name": {Type: spec.FieldText}, "amount": {Type: spec.FieldMoney}, "stage": {Type: spec.FieldEnum}, "owner": {Type: spec.FieldPerson}, "active": {Type: spec.FieldBool}, "tags": {Type: spec.FieldRefs, To: "tag"}, "touched": {Type: spec.FieldDatetime}}}
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`CREATE TABLE records_deal (id TEXT PRIMARY KEY, record TEXT NOT NULL, field_stage GENERATED ALWAYS AS (json_extract(record, '$.fields.stage')), field_tags GENERATED ALWAYS AS (json_extract(record, '$.fields.tags')))`); err != nil {
		t.Fatal(err)
	}
	fields := []map[string]any{
		{"name": "Northwind", "amount": 100, "stage": "Proposal", "owner": "owner", "active": true, "tags": []string{"red", "blue"}},
		{"name": "Globex", "amount": 100, "stage": "Won", "owner": "other", "active": false, "tags": []string{"blue"}},
		{"name": "A_100%", "amount": 200, "stage": "Lost", "owner": "owner", "active": true},
		{"name": "No amount", "stage": "Lead"},
		{"name": "Zero", "amount": 0, "stage": "Lead"},
		{"name": "Also null", "amount": nil, "stage": "Lead"},
	}
	for i, id := range []string{"a", "b", "c", "d", "e", "f"} {
		data, _ := json.Marshal(map[string]any{"id": id, "type": "deal", "fields": fields[i]})
		if _, err := db.Exec(`INSERT INTO records_deal VALUES (?,?)`, id, string(data)); err != nil {
			t.Fatal(err)
		}
	}
	return db, schema
}
func selected(t *testing.T, db *sql.DB, schema *spec.RecordType, a AST) []string {
	t.Helper()
	plan, err := Compile(schema, a, nil)
	if err != nil {
		t.Fatal(err)
	}
	statement, args, err := plan.Page("deal", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := db.Query(statement, args...)
	if err != nil {
		t.Fatalf("%s %v: %v", statement, args, err)
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var data string
		var value any
		if err := rows.Scan(&data, &value); err != nil {
			t.Fatal(err)
		}
		var row struct{ ID string }
		json.Unmarshal([]byte(data), &row)
		ids = append(ids, row.ID)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return ids
}
func TestTokensAndParameterisedOperators(t *testing.T) {
	db, schema := fixture(t)
	tests := []struct {
		query string
		want  []string
	}{
		{"stage:Proposal,Won", []string{"a", "b"}},
		{"stage:-Lost owner:me amount>50", []string{"a"}},
		{"name:north*", []string{"a"}},
		{"name~wind", []string{"a"}},
		{"name~A_100%", []string{"c"}},
		{"amount<100", []string{"e"}},
		{"amount!=100", []string{"c", "e"}},
		{"amount:null", []string{"d", "f"}},
		{"has:amount", []string{"a", "b", "c", "e"}},
		{"is:active", []string{"a", "c"}},
		{"tags:blue", []string{"a", "b"}},
		{"tags:-blue", []string{"c", "d", "e", "f"}},
		{"name:'No amount'", []string{"d"}},
		{`name:"' OR 1=1 --"`, []string{}},
	}
	for _, test := range tests {
		t.Run(test.query, func(t *testing.T) {
			a, err := Parse(schema, test.query, "owner")
			if err != nil {
				t.Fatal(err)
			}
			if got := selected(t, db, schema, a); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("got %v want %v", got, test.want)
			}
		})
	}
	for _, test := range []struct {
		op     Op
		values []any
		want   []string
	}{{Eq, []any{100.0}, []string{"a", "b"}}, {Ne, []any{100.0}, []string{"c", "e"}}, {Lt, []any{100.0}, []string{"e"}}, {Gt, []any{100.0}, []string{"c"}}, {In, []any{100.0, 200.0}, []string{"a", "b", "c"}}, {IsNull, nil, []string{"d", "f"}}} {
		t.Run(string(test.op), func(t *testing.T) {
			a := AST{Filters: []Predicate{{Field: "amount", Op: test.op, Values: test.values}}}
			if got := selected(t, db, schema, a); !reflect.DeepEqual(got, test.want) {
				t.Fatal(got)
			}
		})
	}
}
func TestInjectionIdentifiersAndMalformedTokens(t *testing.T) {
	_, schema := fixture(t)
	for _, text := range []string{`amount>0;DROP TABLE records_deal`, `sort:name;DELETE`, `x:1`, `name:"unterminated`, `amount:NaN`, `amount:Inf`, `sort:tags`, `stage:`, `name:x OR 1=1`, `name');--:x`} {
		t.Run(text, func(t *testing.T) {
			a, err := Parse(schema, text, "me")
			if err == nil {
				_, err = Compile(schema, a, nil)
			}
			if err == nil {
				t.Fatal("accepted malformed input")
			}
		})
	}
	for _, name := range []string{`deal"`, "../deal", "", `deal;drop`} {
		if _, err := Table(name); err == nil {
			t.Fatal("accepted table injection")
		}
	}
	a, err := Parse(schema, `name:"x'); DROP TABLE records_deal; --"`, "me")
	if err != nil {
		t.Fatal(err)
	}
	plan, err := Compile(schema, a, nil)
	if err != nil {
		t.Fatal(err)
	}
	sql, args, err := plan.Page("deal", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sql, "DROP") || args[0] != "x'); DROP TABLE records_deal; --" {
		t.Fatalf("input interpolated: %s %v", sql, args)
	}
	_, err = Compile(schema, AST{Filters: []Predicate{{Field: "name", Op: Op("= 1 OR 1=1")}}}, nil)
	if err == nil {
		t.Fatal("accepted operator injection")
	}
}
func TestKeysetPagingTiesNullsAndQueryBinding(t *testing.T) {
	db, schema := fixture(t)
	for _, test := range []struct {
		text string
		want []string
	}{{"sort:amount", []string{"d", "f", "e", "a", "b", "c"}}, {"sort:-amount", []string{"c", "b", "a", "e", "f", "d"}}, {"", []string{"a", "b", "c", "d", "e", "f"}}, {"sort:-id", []string{"f", "e", "d", "c", "b", "a"}}} {
		for _, size := range []int{1, 2, 3, 4} {
			t.Run(test.text+string(rune('0'+size)), func(t *testing.T) {
				a, err := Parse(schema, test.text, "me")
				if err != nil {
					t.Fatal(err)
				}
				plan, err := Compile(schema, a, nil)
				if err != nil {
					t.Fatal(err)
				}
				var got []string
				after := ""
				for range 10 {
					statement, args, err := plan.Page("deal", after, size)
					if err != nil {
						t.Fatal(err)
					}
					rows, err := db.Query(statement, args...)
					if err != nil {
						t.Fatal(err)
					}
					var ids []string
					var values []any
					for rows.Next() {
						var data string
						var value any
						if err := rows.Scan(&data, &value); err != nil {
							t.Fatal(err)
						}
						var row struct{ ID string }
						json.Unmarshal([]byte(data), &row)
						ids = append(ids, row.ID)
						values = append(values, value)
					}
					if err := rows.Err(); err != nil {
						t.Fatal(err)
					}
					rows.Close()
					if len(ids) <= size {
						got = append(got, ids...)
						break
					}
					got = append(got, ids[:size]...)
					after, err = plan.Cursor("deal", ids[size-1], values[size-1])
					if err != nil {
						t.Fatal(err)
					}
				}
				if !reflect.DeepEqual(got, test.want) {
					t.Fatalf("got %v want %v", got, test.want)
				}
				cursor, _ := plan.Cursor("deal", "a", 100)
				for _, bad := range []string{"bogus", strings.Repeat("x", 20000)} {
					if _, _, err := plan.Page("deal", bad, 2); err == nil {
						t.Fatal("accepted bad cursor")
					}
				}
				other, err := Compile(schema, AST{Sort: Sort{Field: "name"}}, nil)
				if err != nil {
					t.Fatal(err)
				}
				if _, _, err := other.Page("deal", cursor, 2); err == nil {
					t.Fatal("cursor crossed queries")
				}
				if _, _, err := plan.Page("contact", cursor, 2); err == nil {
					t.Fatal("cursor crossed types")
				}
			})
		}
	}
}
func TestPermissionFilterConstrainsSQLAndCursor(t *testing.T) {
	db, schema := fixture(t)
	permission := func(a AST) (AST, error) {
		a.Filters = append(a.Filters, Predicate{Field: "owner", Op: Eq, Values: []any{"owner"}})
		return a, nil
	}
	plan, err := Compile(schema, AST{}, permission)
	if err != nil {
		t.Fatal(err)
	}
	if got := selected(t, db, schema, plan.AST); !reflect.DeepEqual(got, []string{"a", "c"}) {
		t.Fatal(got)
	}
	cursor, _ := plan.Cursor("deal", "a", 100)
	identity, _ := Compile(schema, AST{}, Identity)
	if _, _, err := identity.Page("deal", cursor, 2); err == nil {
		t.Fatal("cursor crossed permission filters")
	}
}
