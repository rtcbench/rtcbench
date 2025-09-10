package vp9_stats

import (
	"fmt"
	"sync"
	"time"
)

// Publisher pub-sub goroutine that fans out input samples to subscribers, while accumulating total bytes per period
type Publisher struct {
	input  <-chan VideoQualitySample
	subs   []func(Period, VideoQualitySample)
	period Period

	// shutdown related
	stop chan struct{}
	done chan struct{}
	once sync.Once
}

// Period aggregated stats for a collection period
type Period struct {
	// StartTimeUS microsecond epoch utc start time of this period
	StartTimeUS int64

	// TotalBytes total bytes seen in this period
	TotalBytes int64

	// TODO: total packets seen, total dropped, etc.
}

func (p *Period) String() string {
	return fmt.Sprintf("vp9_stats.Period[startTimeUS=%d;totalBytes=%d]", p.StartTimeUS, p.TotalBytes)
}

func NewPublisher(input <-chan VideoQualitySample) *Publisher {
	return &Publisher{
		input:  input,
		subs:   nil,
		period: Period{},
		stop:   make(chan struct{}, 1),
		done:   make(chan struct{}, 1),
		once:   sync.Once{},
	}
}

func (p *Publisher) AddSubscriber(subscriber func(Period, VideoQualitySample)) {
	p.subs = append(p.subs, subscriber)
}

func (p *Publisher) Run() {
	defer close(p.done)

	p.period.StartTimeUS = time.Now().UnixMicro()
	p.period.TotalBytes = 0

	for {
		select {
		case sample := <-p.input:
			p.onSample(sample)
		case <-p.stop:
			return
		}
	}
}

func (p *Publisher) onSample(sample VideoQualitySample) {
	p.period.TotalBytes += sample.TotalBytes

	for _, sub := range p.subs {
		sub(p.period, sample)
	}
}

func (p *Publisher) Stop() {
	p.once.Do(func() {
		close(p.stop)
	})
	<-p.done
}
