package pionutil

import (
	"hash/fnv"
	"math/rand"
	"sync"
	"time"
)

type packetScheduler struct {
	deliver   func([]byte)
	profile   *ImpairmentProfile
	rng       *rand.Rand
	queue     []scheduledPacket
	nextSend  time.Time
	wake      chan struct{}
	done      chan struct{}
	closeOnce sync.Once
	wg        sync.WaitGroup
	mu        sync.Mutex
	closed    bool
}

type scheduledPacket struct {
	sendAt time.Time
	data   []byte
}

func newPacketScheduler(profile *ImpairmentProfile, salt string, deliver func([]byte)) *packetScheduler {
	if !profileHasImpairment(profile) {
		return nil
	}

	scheduler := &packetScheduler{
		deliver: deliver,
		profile: profile,
		rng:     newProfileRNG(profile.Seed, salt),
		wake:    make(chan struct{}, 1),
		done:    make(chan struct{}),
	}
	scheduler.wg.Add(1)
	go scheduler.run()

	return scheduler
}

func (s *packetScheduler) Enqueue(payload []byte) {
	if s == nil || len(payload) == 0 {
		return
	}

	s.mu.Lock()
	if s.closed || s.shouldDropLocked() {
		s.mu.Unlock()
		return
	}

	now := time.Now()
	sendAt := s.scheduleLocked(now, len(payload))
	wasEmpty := len(s.queue) == 0
	s.queue = append(s.queue, scheduledPacket{
		sendAt: sendAt,
		data:   append([]byte(nil), payload...),
	})
	s.mu.Unlock()

	if wasEmpty {
		select {
		case s.wake <- struct{}{}:
		default:
		}
	}
}

func (s *packetScheduler) Close() {
	if s == nil {
		return
	}

	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.queue = nil
		s.nextSend = time.Time{}
		s.mu.Unlock()
		close(s.done)
		s.wg.Wait()
	})
}

func (s *packetScheduler) run() {
	defer s.wg.Done()

	timer := time.NewTimer(time.Hour)
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}

	for {
		s.mu.Lock()
		if len(s.queue) == 0 {
			s.mu.Unlock()
			select {
			case <-s.wake:
				continue
			case <-s.done:
				return
			}
		}

		next := s.queue[0].sendAt
		s.mu.Unlock()

		wait := time.Until(next)
		if wait > 0 {
			timer.Reset(wait)
			select {
			case <-timer.C:
			case <-s.done:
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				return
			}
		}

		s.mu.Lock()
		if len(s.queue) == 0 {
			s.mu.Unlock()
			continue
		}
		if time.Until(s.queue[0].sendAt) > 0 {
			s.mu.Unlock()
			continue
		}

		packet := s.queue[0]
		s.queue = s.queue[1:]
		if len(s.queue) == 0 {
			s.nextSend = time.Time{}
		}
		s.mu.Unlock()

		s.deliver(packet.data)
	}
}

func (s *packetScheduler) shouldDropLocked() bool {
	return s.profile.LossPercent > 0 && s.rng.Intn(100) < s.profile.LossPercent
}

func (s *packetScheduler) scheduleLocked(now time.Time, size int) time.Time {
	sendAt := now.Add(s.sampleDelayLocked())
	if sendAt.Before(now) {
		sendAt = now
	}
	if s.nextSend.After(sendAt) {
		sendAt = s.nextSend
	}
	if s.profile.BandwidthBps > 0 {
		sendAt = sendAt.Add(transmitDuration(size, s.profile.BandwidthBps))
	}
	s.nextSend = sendAt
	return sendAt
}

func (s *packetScheduler) sampleDelayLocked() time.Duration {
	delay := s.profile.BaseLatency
	if s.profile.JitterStddev > 0 {
		delay += time.Duration(s.rng.NormFloat64() * float64(s.profile.JitterStddev))
	}
	if delay < 0 {
		return 0
	}
	return delay
}

func profileHasImpairment(profile *ImpairmentProfile) bool {
	if profile == nil {
		return false
	}
	return profile.BandwidthBps > 0 || profile.BaseLatency > 0 || profile.JitterStddev > 0 || profile.LossPercent > 0
}

func transmitDuration(sizeBytes int, bandwidthBps int) time.Duration {
	if sizeBytes <= 0 || bandwidthBps <= 0 {
		return 0
	}
	bits := int64(sizeBytes) * 8
	return time.Duration(bits*int64(time.Second)+int64(bandwidthBps)-1) / time.Duration(bandwidthBps)
}

func newProfileRNG(seed int64, salt string) *rand.Rand {
	if seed == 0 {
		return rand.New(rand.NewSource(time.Now().UnixNano())) //nolint:gosec
	}

	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte(salt))
	return rand.New(rand.NewSource(seed ^ int64(hasher.Sum64()))) //nolint:gosec
}
