package scheduler

import "github.com/robfig/cron/v3"

type Scheduler struct{ c *cron.Cron }

// New builds a scheduler that runs fn on the given 5-field cron expression.
func New(cronExpr string, fn func()) (*Scheduler, error) {
	c := cron.New()
	if _, err := c.AddFunc(cronExpr, fn); err != nil {
		return nil, err
	}
	return &Scheduler{c: c}, nil
}

func (s *Scheduler) Start() { s.c.Start() }
func (s *Scheduler) Stop()  { s.c.Stop() }
