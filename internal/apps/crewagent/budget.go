package crewagent

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/Agent-Field/codeaf/internal/apps/bridge"
	"github.com/Agent-Field/codeaf/internal/apps/spec"
	"github.com/Agent-Field/codeaf/internal/apps/svc"
	"github.com/Agent-Field/codeaf/internal/filelock"
)

type spendReceipt struct {
	Thread string    `json:"thread"`
	Total  float64   `json:"total"`
	Cost   float64   `json:"cost"`
	At     time.Time `json:"at"`
}

// Spent exposes the crew agent's app-owned receipts to the settings budget reader.
func (s *Service) Spent(app string, at time.Time) (float64, error) {
	receipts, err := s.spending(app)
	if err != nil {
		return 0, err
	}
	var cost float64
	for _, receipt := range receipts {
		if receipt.At.UTC().Format("2006-01") == at.UTC().Format("2006-01") {
			cost += receipt.Cost
		}
	}
	return cost, nil
}

func (s *Service) checkBudget(ctx context.Context, scope svc.Scope, def *spec.Bundle) error {
	spent, err := s.Spent(scope.App, s.now())
	if err != nil {
		return err
	}
	month := def.App.Budget.Month
	if s.all.Crews.Settings != nil {
		info, err := s.all.Crews.Settings.GetBudget(ctx, scope)
		if err != nil {
			return err
		}
		month = info.Month
		if _, includesCrew := s.all.Crews.Settings.(*crewBudget); includesCrew {
			spent = info.Spent
		} else {
			spent += info.Spent
		}
	}
	if month > 0 && spent >= month {
		return bridge.Errorf(bridge.CodeBudget, "the app's monthly budget is used up")
	}
	return nil
}

func (s *Service) spending(app string) ([]spendReceipt, error) {
	if s.root == "" {
		return nil, nil
	}
	file, err := os.Open(filepath.Join(s.root, app, "crew-agent-spend.jsonl"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	var receipts []spendReceipt
	scan := bufio.NewScanner(file)
	for scan.Scan() {
		var receipt spendReceipt
		if err := json.Unmarshal(scan.Bytes(), &receipt); err != nil {
			return nil, err
		}
		receipts = append(receipts, receipt)
	}
	return receipts, scan.Err()
}

func (s *Service) charge(app, thread string, total, before float64) (float64, error) {
	if s.root == "" {
		return max(0, total-before), nil
	}
	dir := filepath.Join(s.root, app)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return 0, err
	}
	lock, err := os.OpenFile(filepath.Join(dir, "crew-agent-spend.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return 0, err
	}
	defer lock.Close()
	if err := filelock.Lock(lock, true, false); err != nil {
		return 0, err
	}
	defer filelock.Unlock(lock)
	receipts, err := s.spending(app)
	if err != nil {
		return 0, err
	}
	previous := float64(0)
	for _, receipt := range receipts {
		if receipt.Thread == thread {
			previous = receipt.Total
		}
	}
	cost := max(0, total-max(previous, before))
	file, err := os.OpenFile(filepath.Join(dir, "crew-agent-spend.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	if err := json.NewEncoder(file).Encode(spendReceipt{Thread: thread, Total: total, Cost: cost, At: s.now().UTC()}); err != nil {
		return 0, err
	}
	return cost, file.Sync()
}
