package teams

import (
	"fmt"
	"testing"
	"time"
)

// BenchmarkDisbandScale measures the public mutation after fixture construction.
// Every conversation has an existing home, including the unrelated teams whose
// reporting must survive closing one branch.
func BenchmarkDisbandScale(b *testing.B) {
	for _, size := range []struct{ teams, members int }{{100, 100}, {200, 200}} {
		b.Run(fmt.Sprintf("%dteams_%dmembers", size.teams, size.members), func(b *testing.B) {
			fixture := make([]Team, size.teams)
			for i := range fixture {
				id := fmt.Sprintf("%012x", i+1)
				manager := fmt.Sprintf("manager-%d", i)
				fixture[i] = Team{ID: id, Name: id, Manager: manager}
				for j := 0; j < size.members; j++ {
					fixture[i].Members = append(fixture[i].Members, Member{
						Key: fmt.Sprintf("worker-%d-%d", i, j), Home: true,
					})
				}
			}
			at := time.Unix(100, 0)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				f := &File{Teams: make([]Team, len(fixture))}
				for j, team := range fixture {
					f.Teams[j] = team.Clone()
				}
				b.StartTimer()
				if err := f.Disband(fixture[0].ID, at, "finished"); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
