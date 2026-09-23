package export

import "testing"

func TestCSVIsWhatTheBankImports(t *testing.T) {
	got := CSV([]Row{
		{Date: "2024-01-02", Memo: "coffee", Cents: -450},
		{Date: "2024-01-03", Memo: "salary", Cents: 523456},
		{Date: "2024-01-04", Memo: "rent", Cents: -150000},
	})
	want := "2024-01-02,coffee,-4.50\n" +
		"2024-01-03,salary,5234.56\n" +
		"2024-01-04,rent,-1500.00\n"
	if got != want {
		t.Fatalf("CSV =\n%s\nwant\n%s", got, want)
	}
}
