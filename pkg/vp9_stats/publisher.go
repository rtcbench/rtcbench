package vp9_stats

import (
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

const (
	ErrViewerReadFromTrackID int = iota
	ErrViewerUnmarshalPacketID
	ErrViewerParseVP9PayloadID
	ErrViewerSeqNoJumpID
	ErrViewerInputChannelID
	ErrViewerIVFSegmenterFailed
	ErrViewerPcapWriteID
	NumErrIDs
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

	// periodErrors stored on publisher struct rather than period to avoid copying atomics when passed to subscriber
	periodErrors [NumErrIDs]atomic.Int32
}

// Period aggregated stats for a collection period
type Period struct {
	// StartTimeUS microsecond epoch utc start time of this period
	StartTimeUS int64

	// TotalBytes total bytes seen in this period
	TotalBytes int64

	// TODO: total packets seen, total dropped, etc.

	Errors [NumErrIDs]int32
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

	for i := 0; i < NumErrIDs; i++ {
		p.periodErrors[i].Store(0)
	}

	for {
		select {
		case sample := <-p.input:
			p.onSample(sample)
		case <-p.stop:
			return
		}
	}
}

func (p *Publisher) IncrError(errID int) {
	if errID < 0 || errID >= NumErrIDs {
		panic("unknown errID: " + strconv.Itoa(errID))
	}
	p.periodErrors[errID].Add(1)
}

func (p *Publisher) Errors() []error {
	var errs []error
	for errID := 0; errID < NumErrIDs; errID++ {
		val := p.periodErrors[errID].Load()
		if val > 0 {
			errs = append(errs, fmt.Errorf("%s[%d]", ErrIDToString(errID), val))
		}
	}
	return errs
}

func (p *Publisher) onSample(sample VideoQualitySample) {
	p.period.TotalBytes += sample.Sample.TotalBytes

	for i := 0; i < NumErrIDs; i++ {
		p.period.Errors[i] = p.periodErrors[i].Load()
	}

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

func ErrIDToString(errID int) string {
	s := ""
	switch errID {
	case ErrViewerReadFromTrackID:
		s = "ErrViewerReadFromTrackID"
	case ErrViewerUnmarshalPacketID:
		s = "ErrViewerUnmarshalPacketID"
	case ErrViewerParseVP9PayloadID:
		s = "ErrViewerParseVP9PayloadID"
	case ErrViewerSeqNoJumpID:
		s = "ErrViewerSeqNoJumpID"
	case ErrViewerInputChannelID:
		s = "ErrViewerInputChannelID"
	case ErrViewerIVFSegmenterFailed:
		s = "ErrViewerIVFSegmenterFailed"
	case ErrViewerPcapWriteID:
		s = "ErrViewerPcapWriteID"
	default:
		panic("invalid error id: " + strconv.Itoa(errID))
	}
	return fmt.Sprintf("vp9_stats.Publisher.%s", s)
}
