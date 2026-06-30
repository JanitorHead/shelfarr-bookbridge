package scheduler

import (
	"time"

	"github.com/robfig/cron/v3"
)

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

// Next returns the next activation time of a 5-field cron expression after the
// given time, used by the GUI to show the upcoming scheduled run.
func Next(expr string, after time.Time) (time.Time, error) {
	sched, err := cron.ParseStandard(expr)
	if err != nil {
		return time.Time{}, err
	}
	return sched.Next(after), nil
}
